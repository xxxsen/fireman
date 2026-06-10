// Package app wires together the long-lived dependencies that the Fireman
// backend needs (configuration, structured logger, database pool, HTTP
// router) and exposes a single Run entry point consumed by cmd/fireman.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fireman/fireman/internal/api"
	"github.com/fireman/fireman/internal/config"
	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
)

// Run boots the backend and blocks until the process receives SIGINT/SIGTERM
// or the HTTP server fails. It is the single entry point used by main.
func Run(ctx context.Context, cfg config.Config) error {

	logger := buildLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	if err := ensureDataDir(cfg.DBPath); err != nil {
		return fmt.Errorf("ensure db parent dir: %w", err)
	}

	pool, err := fdb.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}

	if err := fdb.Migrate(ctx, pool, cfg.DBPath, logger); err != nil {
		pool.Close()
		return fmt.Errorf("migrate: %w", err)
	}

	maintenance := &service.MaintenanceGate{}
	services := api.NewServices(pool, cfg.DBPath, cfg.MarketProviderURL, maintenance)
	jobRepo := repository.NewJobRepo(pool)
	simRepo := repository.NewSimulationRepo(pool)
	instRepo := repository.NewInstrumentRepo(pool)
	marketRepo := repository.NewMarketDataRepo(pool)
	annualRepo := repository.NewAnnualReturnsRepo(pool)
	fetchProvider := marketdata.NewProviderClient(cfg.MarketProviderURL).FetchClient()
	instrumentFetchRunner := jobs.NewInstrumentFetchRunner(pool, instRepo, marketRepo, annualRepo, fetchProvider)
	runner := jobs.NewSimulationRunner(pool, simRepo)
	analysisRunner := jobs.NewAnalysisRunner(repository.NewAnalysisRepo(pool))
	worker := jobs.NewWorker(pool, jobRepo, simRepo, runner, analysisRunner, instrumentFetchRunner, services.EventHub, logger, maintenance.Active)

	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		worker.Start(workerCtx, cfg.WorkerConcurrency)
		close(workerDone)
	}()

	router := api.NewRouter(api.Deps{DB: pool, DBPath: cfg.DBPath, Logger: logger, MarketProviderURL: cfg.MarketProviderURL, Services: services})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return runServer(ctx, server, pool, logger, workerCancel, workerDone)
}

func runServer(ctx context.Context, server *http.Server, pool *sql.DB, logger *slog.Logger, workerCancel context.CancelFunc, workerDone <-chan struct{}) error {
	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case <-signalCtx.Done():
		logger.Info("shutdown requested")
	case err := <-serverErr:
		if err != nil {
			workerCancel()
			<-workerDone
			_ = pool.Close()
			return fmt.Errorf("http server: %w", err)
		}
		workerCancel()
		<-workerDone
		_ = pool.Close()
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}

	logger.Info("stopping worker")
	workerCancel()
	select {
	case <-workerDone:
		logger.Info("worker stopped")
	case <-time.After(30 * time.Second):
		logger.Error("worker shutdown timeout")
	}

	if err := pool.Close(); err != nil {
		logger.Error("db close error", "error", err)
	}
	return nil
}

func buildLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}

func ensureDataDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
