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
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/migrations"
)

// resourceCleanupInterval controls how often expired rows are purged from
// resource_db.
const resourceCleanupInterval = time.Hour

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
		return fmt.Errorf("open database: %w", err)
	}

	fdb.SetMigrations(migrations.FS)

	if err := fdb.Migrate(ctx, pool, cfg.DBPath, logger); err != nil {
		if closeErr := pool.Close(); closeErr != nil {
			logger.Error("db close after migrate failure", "error", closeErr)
		}
		return fmt.Errorf("migrate: %w", err)
	}

	if err := ensureDataDir(cfg.ResourceDBPath); err != nil {
		if closeErr := pool.Close(); closeErr != nil {
			logger.Error("db close after resource dir failure", "error", closeErr)
		}
		return fmt.Errorf("ensure resource db parent dir: %w", err)
	}
	resources, err := resourcedb.Open(ctx, cfg.ResourceDBPath)
	if err != nil {
		if closeErr := pool.Close(); closeErr != nil {
			logger.Error("db close after resource db failure", "error", closeErr)
		}
		return fmt.Errorf("open resource database: %w", err)
	}

	maintenance := &service.MaintenanceGate{}
	services := api.NewServices(pool, cfg.DBPath, maintenance, resources)
	jobRepo := repository.NewJobRepo(pool)
	simRepo := repository.NewSimulationRepo(pool)
	runner := jobs.NewSimulationRunner(pool, simRepo)
	analysisRunner := jobs.NewAnalysisRunner(repository.NewAnalysisRepo(pool))
	worker := jobs.NewWorker(
		pool, jobRepo, simRepo, runner, analysisRunner, services.Research,
		services.EventHub, logger, maintenance.Active,
	)

	resourcedb.StartCleanup(ctx, resources, resourceCleanupInterval, logger.Warn)

	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		worker.Start(workerCtx, cfg.WorkerConcurrency)
		close(workerDone)
	}()

	router := api.NewRouter(ctx, api.Deps{
		DB: pool, DBPath: cfg.DBPath, Logger: logger, Services: services,
	})

	internalRouter := api.NewInternalRouter(api.InternalDeps{
		Logger: logger, PostProcess: newPostProcessService(pool, resources), Resources: resources,
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}
	internalServer := &http.Server{
		Addr:              cfg.InternalAddr,
		Handler:           internalRouter,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return runServer(ctx, server, internalServer, pool, resources, logger, workerCancel, workerDone)
}

func newPostProcessService(pool *sql.DB, resources *resourcedb.DB) *service.PostProcessService {
	return service.NewPostProcessService(
		pool,
		repository.NewWorkerTaskRepo(pool),
		repository.NewMarketAssetRepo(pool),
		repository.NewInstrumentRepo(pool),
		repository.NewMarketDataRepo(pool),
		resources,
		repository.NewPostProcessRecordRepo(pool),
	)
}

func runServer(
	ctx context.Context,
	server *http.Server,
	internalServer *http.Server,
	pool *sql.DB,
	resources *resourcedb.DB,
	logger *slog.Logger,
	workerCancel context.CancelFunc,
	workerDone <-chan struct{},
) error {
	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 2)
	go func() {
		logger.Info("http server starting", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("http server: %w", err)
			return
		}
		serverErr <- nil
	}()
	go func() {
		logger.Info("internal http server starting", "addr", internalServer.Addr)
		if err := internalServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("internal http server: %w", err)
			return
		}
		serverErr <- nil
	}()

	shutdownHTTP := false
	var firstErr error
	select {
	case <-signalCtx.Done():
		logger.Info("shutdown requested")
		shutdownHTTP = true
	case err := <-serverErr:
		firstErr = err
	}

	shutdownApp(ctx, logger, []*http.Server{server, internalServer}, pool, resources,
		workerCancel, workerDone, shutdownHTTP)
	return firstErr
}

// shutdownApp stops HTTP (when still serving), waits for worker exit, then closes the databases.
func shutdownApp(
	ctx context.Context,
	logger *slog.Logger,
	servers []*http.Server,
	pool *sql.DB,
	resources *resourcedb.DB,
	workerCancel context.CancelFunc,
	workerDone <-chan struct{},
	shutdownHTTP bool,
) {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	for _, srv := range servers {
		if !shutdownHTTP {
			// One listener already failed; still close the others so shutdown
			// can proceed.
			_ = srv.Close()
			continue
		}
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http server shutdown error", "addr", srv.Addr, "error", err)
		}
	}

	logger.Info("stopping worker")
	workerCancel()
	<-workerDone
	logger.Info("worker stopped")

	if err := resources.Close(); err != nil {
		logger.Error("resource db close error", "error", err)
	}
	if err := pool.Close(); err != nil {
		logger.Error("db close error", "error", err)
	}
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}
	return nil
}
