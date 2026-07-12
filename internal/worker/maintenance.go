package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
)

const maintenanceInterval = 15 * time.Second

// Maintenance owns stale-attempt recovery and finalization. It is
// infrastructure rather than a worker task so it can repair the task system.
type Maintenance struct {
	coordinator *taskcore.Coordinator
	finalizer   *service.TaskFinalizer
	logger      *slog.Logger
	interval    time.Duration
	lastCleanup time.Time
}

func NewMaintenance(
	coordinator *taskcore.Coordinator, finalizer *service.TaskFinalizer, logger *slog.Logger,
) *Maintenance {
	if logger == nil {
		logger = slog.Default()
	}
	return &Maintenance{
		coordinator: coordinator, finalizer: finalizer, logger: logger,
		interval: maintenanceInterval,
	}
}

func (m *Maintenance) Run(ctx context.Context) {
	if count, err := m.coordinator.RecoverStartup(ctx); err != nil {
		m.logger.Error("worker task startup recovery failed", "error", err)
	} else if count > 0 {
		m.logger.Info("worker task startup recovery completed", "count", count)
	}
	m.runOnce(ctx)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runOnce(ctx)
		}
	}
}

//nolint:gocognit,nestif // Independent maintenance phases log failures and continue by design.
func (m *Maintenance) runOnce(ctx context.Context) {
	if count, err := m.coordinator.RecoverExpired(ctx); err != nil {
		m.logger.Error("worker task lease recovery failed", "error", err)
	} else if count > 0 {
		m.logger.Info("worker task leases recovered", "count", count)
	}
	if m.lastCleanup.IsZero() || time.Since(m.lastCleanup) >= time.Hour {
		if count, err := m.coordinator.CleanupRetention(ctx); err != nil {
			m.logger.Error("worker task retention cleanup failed", "error", err)
		} else {
			m.lastCleanup = time.Now()
			if count > 0 {
				m.logger.Info("worker task retention cleanup completed", "count", count)
			}
		}
	}
	for {
		items, err := m.coordinator.ReserveDueFinalizations(ctx, 20)
		if err != nil {
			m.logger.Error("reserve worker task finalizations failed", "error", err)
			return
		}
		if len(items) == 0 {
			return
		}
		for _, item := range items {
			result := m.finalizer.Finalize(ctx, item.Task.ID, item.ReservationEnds)
			if result.Result == service.TaskFinalizeSuccess {
				if err := m.coordinator.PublishCurrent(ctx, item.Task.ID); err != nil {
					m.logger.Error("publish finalized worker task failed", "task_id", item.Task.ID, "error", err)
				}
				continue
			}
			retryable := result.Result == service.TaskFinalizeRetryableError
			if _, err := m.coordinator.FinishFinalizationFailure(
				ctx, item.Task.ID, item.ReservationEnds, retryable,
				result.ErrorCode, result.ErrorMessage,
			); err != nil {
				m.logger.Error("finish worker task finalization failure failed",
					"task_id", item.Task.ID, "error", err)
			}
		}
		if len(items) < 20 {
			return
		}
	}
}
