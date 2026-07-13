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
	"sync"
	"syscall"
	"time"

	"github.com/fireman/fireman/internal/api"
	"github.com/fireman/fireman/internal/bootstrap"
	"github.com/fireman/fireman/internal/config"
	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
	workerpkg "github.com/fireman/fireman/internal/worker"
	"github.com/fireman/fireman/migrations"
	"github.com/google/uuid"
)

// resourceCleanupInterval controls how often expired rows are purged from
// resource_db.
const resourceCleanupInterval = time.Hour

var errAutoUpdateScanIdempotencyConflict = errors.New("auto-update scan idempotency conflict")

// Run boots the backend and blocks until the process receives SIGINT/SIGTERM
// or the HTTP server fails. It is the single entry point used by main.
//
//nolint:funlen // Startup wiring remains contiguous so ownership and shutdown order are auditable.
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
		closePoolAfterStartupFailure(pool, logger, "migrate")
		return fmt.Errorf("migrate: %w", err)
	}
	if err := bootstrap.EnsureBuiltinData(ctx, pool); err != nil {
		closePoolAfterStartupFailure(pool, logger, "bootstrap builtin data")
		return fmt.Errorf("bootstrap builtin data: %w", err)
	}

	if err := ensureDataDir(cfg.ResourceDBPath); err != nil {
		closePoolAfterStartupFailure(pool, logger, "resource dir")
		return fmt.Errorf("ensure resource db parent dir: %w", err)
	}
	resources, err := resourcedb.Open(ctx, cfg.ResourceDBPath)
	if err != nil {
		closePoolAfterStartupFailure(pool, logger, "resource db")
		return fmt.Errorf("open resource database: %w", err)
	}

	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		closePoolAfterStartupFailure(pool, logger, "timezone")
		return fmt.Errorf("load timezone %q: %w", cfg.Timezone, err)
	}

	maintenance := &service.MaintenanceGate{}
	services := api.NewServices(pool, cfg.DBPath, maintenance, resources, loc)
	services.Research.SetOptimizationConcurrency(cfg.ResearchOptimizationConcurrency)
	taskRepo := repository.NewWorkerTaskRepo(pool)
	processors := workerpkg.NewProcessorSet(
		pool, services.TaskCoordinator, services.Research, services.AutoUpdates, resources,
	)
	processors.SetImprovementParallelism(cfg.FirePlanImprovementConcurrency)
	supervisor := workerpkg.NewSupervisor(
		services.TaskCoordinator, processors, logger, maintenance.Active,
	)
	finalizer := newTaskFinalizer(pool, resources, services.TaskCoordinator)
	taskMaintenance := workerpkg.NewMaintenance(services.TaskCoordinator, finalizer, logger)
	autoScheduler := service.NewAutoUpdateScheduler(
		services.AutoUpdates, cfg.AutoUpdateScanIntervalMinutes,
	)
	autoScheduler.SetTaskEnqueuer(func(ctx context.Context, slot int64) error {
		return enqueueAutoUpdateScanTask(ctx, pool, taskRepo, services.TaskCoordinator, slot)
	})
	autoScheduler.Start(ctx)

	resourcedb.StartCleanup(ctx, resources, resourceCleanupInterval, logger.Warn)

	workerCtx, workerCancel := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		var group sync.WaitGroup
		group.Add(2)
		go func() { defer group.Done(); supervisor.Start(workerCtx, cfg.WorkerConcurrency) }()
		go func() { defer group.Done(); taskMaintenance.Run(workerCtx) }()
		group.Wait()
		close(workerDone)
	}()

	router := api.NewRouter(ctx, api.Deps{
		DB: pool, DBPath: cfg.DBPath, Logger: logger, Services: services,
	})

	internalRouter := api.NewInternalRouter(ctx, api.InternalDeps{
		Logger: logger, Coordinator: services.TaskCoordinator, Resources: resources,
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

	return runServer(ctx, server, internalServer, pool, resources, logger, workerCancel, workerDone, autoScheduler)
}

func closePoolAfterStartupFailure(pool *sql.DB, logger *slog.Logger, phase string) {
	if err := pool.Close(); err != nil {
		logger.Error("db close after startup failure", "phase", phase, "error", err)
	}
}

func newTaskFinalizer(
	pool *sql.DB, resources *resourcedb.DB, coordinator *taskcore.Coordinator,
) *service.TaskFinalizer {
	svc := service.NewTaskFinalizer(
		pool,
		repository.NewWorkerTaskRepo(pool),
		repository.NewMarketAssetRepo(pool),
		repository.NewInstrumentRepo(pool),
		repository.NewMarketDataRepo(pool),
		resources,
		repository.NewWorkerTaskFinalizeRecordRepo(pool),
	)
	svc.SetAutoUpdateRepo(repository.NewMarketDataAutoUpdateRepo(pool))
	svc.SetCoordinator(coordinator)
	return svc
}

//nolint:wrapcheck // Repository and coordinator errors retain identity for startup diagnostics.
func enqueueAutoUpdateScanTask(
	ctx context.Context, pool *sql.DB, repo *repository.WorkerTaskRepo,
	coordinator *taskcore.Coordinator, slot int64,
) error {
	idempotencyKey := fmt.Sprintf("%d", slot)
	dedupe := repository.WorkerTaskTypeAutoUpdateScan + "|system"
	inputHash := fmt.Sprintf("slot:%d", slot)
	if _, storedHash, err := repo.FindIdempotency(ctx, "system", "auto_update",
		repository.WorkerTaskTypeAutoUpdateScan, idempotencyKey); err == nil {
		if storedHash != inputHash {
			return fmt.Errorf("%w for slot %d", errAutoUpdateScanIdempotencyConflict, slot)
		}
		return nil
	} else if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return err
	}
	return fdb.WithTx(ctx, pool, func(tx *sql.Tx) error {
		if existing, err := repo.FindActiveByDedupeTx(
			ctx, tx, repository.WorkerTypeGo, repository.WorkerTaskTypeAutoUpdateScan, dedupe,
		); err == nil {
			// A task can own only one idempotency key. Skipping this scheduler
			// slot is intentional while the previous scan is still active.
			_ = existing
			return nil
		} else if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return err
		}
		task := &repository.WorkerTask{
			ID: "task_" + uuid.NewString(), WorkerType: repository.WorkerTypeGo,
			Type:   repository.WorkerTaskTypeAutoUpdateScan,
			Status: repository.WorkerTaskStatusPending, Priority: 50,
			ScopeType: "system", ScopeID: "auto_update_scan", DedupeKey: dedupe,
			InputHash: inputHash, PayloadJSON: fmt.Sprintf(`{"slot_timestamp":%d}`, slot),
			ProgressTotal: 1,
		}
		if err := coordinator.CreateTx(ctx, tx, task); err != nil {
			if !repository.IsWorkerTaskUniqueConstraint(err) {
				return err
			}
			_, findErr := repo.FindActiveByDedupeTx(
				ctx, tx, repository.WorkerTypeGo, repository.WorkerTaskTypeAutoUpdateScan, dedupe,
			)
			if findErr != nil {
				return findErr
			}
			return nil
		}
		return repo.SaveIdempotency(ctx, tx, "system", "auto_update",
			repository.WorkerTaskTypeAutoUpdateScan, idempotencyKey, task.ID, inputHash)
	})
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
	autoScheduler *service.AutoUpdateScheduler,
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
		workerCancel, workerDone, autoScheduler, shutdownHTTP)
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
	autoScheduler *service.AutoUpdateScheduler,
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
	logger.Info("stopping auto update scheduler")
	autoScheduler.Stop()

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
