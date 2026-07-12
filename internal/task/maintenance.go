package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

const (
	FinalizeMaxAttempts = 10
	FinalizeHardTimeout = time.Hour
	FinalizeReservation = time.Minute
)

const (
	terminalTaskRetention   = 90 * 24 * time.Hour
	finalizeRecordRetention = 30 * 24 * time.Hour
)

type FinalizeReservationItem struct {
	Task            repository.WorkerTask
	ReservationEnds int64
}

// RecoverStartup immediately recovers Go attempts from a previous backend
// process and only expired sidecar attempts.
func (c *Coordinator) RecoverStartup(ctx context.Context) (int, error) {
	return c.recoverRunning(ctx, true)
}

// RecoverExpired applies the lease policy to all expired running tasks.
func (c *Coordinator) RecoverExpired(ctx context.Context) (int, error) {
	return c.recoverRunning(ctx, false)
}

//nolint:gocognit,gocyclo,lll,staticcheck,wrapcheck // Recovery keeps lease transitions in one CAS loop.
func (c *Coordinator) recoverRunning(ctx context.Context, startup bool) (int, error) {
	now := c.now().UnixMilli()
	query := `SELECT id FROM worker_tasks WHERE status=? AND lease_expires_at IS NOT NULL AND lease_expires_at<=?`
	args := []any{repository.WorkerTaskStatusRunning, now}
	if startup {
		query = `SELECT id FROM worker_tasks WHERE status=? AND (worker_type=? OR (lease_expires_at IS NOT NULL AND lease_expires_at<=?))`
		args = []any{repository.WorkerTaskStatusRunning, repository.WorkerTypeGo, now}
	}
	ids, err := queryTaskIDs(ctx, c.db, query, args...)
	if err != nil {
		return 0, fmt.Errorf("list recoverable tasks: %w", err)
	}
	changed := 0
	for _, id := range ids {
		var updated repository.WorkerTask
		didChange := false
		err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
			current, err := c.repo.GetByIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if current.Status != repository.WorkerTaskStatusRunning {
				return nil
			}
			if !(startup && current.WorkerType == repository.WorkerTypeGo) &&
				(current.LeaseExpiresAt == nil || *current.LeaseExpiresAt > now) {
				return nil
			}
			status := repository.WorkerTaskStatusPending
			code, message := repository.WorkerTaskErrorInterrupted, "worker attempt interrupted"
			availableAt := now + retryBackoff(current.AttemptCount).Milliseconds()
			var finished any
			attemptOutcome := "lease_retry"
			if current.CancelRequested {
				status, code, message = repository.WorkerTaskStatusCanceled, repository.WorkerTaskErrorCanceled, "task canceled by user"
				finished, attemptOutcome = now, "canceled"
			} else if current.AttemptCount >= current.MaxAttempts {
				status, code, message = repository.WorkerTaskStatusFailed, repository.WorkerTaskErrorHeartbeatTimeout, "worker heartbeat lease expired"
				finished, attemptOutcome = now, "failed"
			}
			result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,available_at=?,claimed_by='',claim_token_hash='',
				attempt_started_at=NULL,heartbeat_at=NULL,lease_expires_at=NULL,progress_current=0,phase='',
				error_code=?,error_message=?,finished_at=?,updated_at=? WHERE id=? AND status=? AND claim_token_hash=?`,
				status, availableAt, code, message, finished, now, id, repository.WorkerTaskStatusRunning, current.ClaimTokenHash)
			if err != nil {
				return err
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return nil
			}
			didChange = true
			attemptResult, err := tx.ExecContext(ctx, `UPDATE worker_task_attempts SET released_at=?,outcome=?,error_code=?,error_message=?
				WHERE task_id=? AND attempt_no=?`, now, attemptOutcome, code, message, id, current.AttemptCount)
			if err := requireOne(attemptResult, err, ErrLeaseLost, "expired task attempt was not found"); err != nil {
				return err
			}
			updated, err = c.repo.GetByIDTx(ctx, tx, id)
			return err
		})
		if err != nil {
			return changed, err
		}
		if didChange {
			changed++
			c.publish(updated)
		}
	}
	return changed, nil
}

//nolint:gocognit,gocyclo,wrapcheck // Reservation and retry-exhaustion commit atomically per task.
func (c *Coordinator) ReserveDueFinalizations(ctx context.Context, limit int) ([]FinalizeReservationItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	now := c.now().UnixMilli()
	ids, err := queryTaskIDs(ctx, c.db, `SELECT id FROM worker_tasks WHERE status=? AND
		(next_finalize_at IS NULL OR next_finalize_at<=?) ORDER BY pre_completed_at,id LIMIT ?`,
		repository.WorkerTaskStatusPreComplete, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due task finalizations: %w", err)
	}
	var out []FinalizeReservationItem
	for _, id := range ids {
		var item repository.WorkerTask
		terminal := false
		reservedUntil := now + FinalizeReservation.Milliseconds()
		err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
			current, err := c.repo.GetByIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if current.Status != repository.WorkerTaskStatusPreComplete ||
				(current.NextFinalizeAt != nil && *current.NextFinalizeAt > now) {
				return sql.ErrNoRows
			}
			if current.FinalizeAttempts >= FinalizeMaxAttempts ||
				(current.PreCompletedAt != nil && *current.PreCompletedAt <= now-FinalizeHardTimeout.Milliseconds()) {
				result, updateErr := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,next_finalize_at=NULL,
					error_code=?,error_message=?,finished_at=?,updated_at=?
					WHERE id=? AND status=?`, repository.WorkerTaskStatusFailed, ErrFinalizeTimeout,
					"task finalization retry limit or hard timeout reached", now, now, id, repository.WorkerTaskStatusPreComplete)
				if updateErr != nil {
					return updateErr
				}
				n, _ := result.RowsAffected()
				if n != 1 {
					return sql.ErrNoRows
				}
				terminal = true
				item, updateErr = c.repo.GetByIDTx(ctx, tx, id)
				return updateErr
			}
			result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET finalize_attempts=finalize_attempts+1,
				next_finalize_at=?,updated_at=? WHERE id=? AND status=? AND (next_finalize_at IS NULL OR next_finalize_at<=?)`,
				reservedUntil, now, id, repository.WorkerTaskStatusPreComplete, now)
			if err != nil {
				return err
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				return sql.ErrNoRows
			}
			item, err = c.repo.GetByIDTx(ctx, tx, id)
			return err
		})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return out, err
		}
		if terminal {
			c.publish(item)
			continue
		}
		out = append(out, FinalizeReservationItem{Task: item, ReservationEnds: reservedUntil})
	}
	return out, nil
}

func queryTaskIDs(
	ctx context.Context, db *sql.DB, query string, args ...any,
) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query task ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan task id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task ids: %w", err)
	}
	return ids, nil
}

// CompleteFinalizationTx is called by a result handler after all business
// writes, within that same transaction.
//
//nolint:lll,wrapcheck // Finalization completion is one guarded update in the business-result transaction.
func (c *Coordinator) CompleteFinalizationTx(ctx context.Context, tx *sql.Tx, id string, reservationEnds, now int64) error {
	result, err := tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,finished_at=?,next_finalize_at=NULL,
		phase='',error_code='',error_message='',updated_at=? WHERE id=? AND status=? AND next_finalize_at=? AND cancel_requested=0`,
		repository.WorkerTaskStatusComplete, now, now, id, repository.WorkerTaskStatusPreComplete, reservationEnds)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return NewError(ErrLeaseLost, "finalization reservation was lost or task was canceled", nil)
	}
	return nil
}

//nolint:lll,wrapcheck // Retry and terminal finalization updates deliberately mirror one another.
func (c *Coordinator) FinishFinalizationFailure(
	ctx context.Context, id string, reservationEnds int64, retryable bool, code, message string,
) (repository.WorkerTask, error) {
	now := c.now().UnixMilli()
	code, message = SanitizeError(code, message)
	var updated repository.WorkerTask
	err := fdb.WithTx(ctx, c.db, func(tx *sql.Tx) error {
		current, err := c.repo.GetByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status != repository.WorkerTaskStatusPreComplete || current.NextFinalizeAt == nil || *current.NextFinalizeAt != reservationEnds {
			return NewError(ErrLeaseLost, "finalization reservation was lost", nil)
		}
		terminal := !retryable || current.FinalizeAttempts >= FinalizeMaxAttempts ||
			(current.PreCompletedAt != nil && *current.PreCompletedAt <= now-FinalizeHardTimeout.Milliseconds())
		var result sql.Result
		if terminal {
			if retryable {
				code = ErrFinalizeFailed
			}
			result, err = tx.ExecContext(ctx, `UPDATE worker_tasks SET status=?,next_finalize_at=NULL,error_code=?,error_message=?,
				finished_at=?,updated_at=? WHERE id=? AND status=? AND next_finalize_at=?`,
				repository.WorkerTaskStatusFailed, code, message, now, now, id,
				repository.WorkerTaskStatusPreComplete, reservationEnds)
		} else {
			next := now + retryBackoff(current.FinalizeAttempts).Milliseconds()
			result, err = tx.ExecContext(ctx, `UPDATE worker_tasks SET next_finalize_at=?,error_code=?,error_message=?,updated_at=?
				WHERE id=? AND status=? AND next_finalize_at=?`, next, code, message, now, id,
				repository.WorkerTaskStatusPreComplete, reservationEnds)
		}
		if err := requireOne(result, err, ErrLeaseLost, "finalization reservation was lost"); err != nil {
			return err
		}
		updated, err = c.repo.GetByIDTx(ctx, tx, id)
		return err
	})
	if err == nil {
		c.publish(updated)
	}
	return updated, err
}

// CleanupRetention deletes only unreferenced terminal tasks. Business-linked
// and idempotency-linked task rows retain the lifecycle of their owner.
//
//nolint:wrapcheck // Cleanup callers only need failure identity; SQL errors remain the direct cause.
func (c *Coordinator) CleanupRetention(ctx context.Context) (int64, error) {
	now := c.now()
	if _, err := c.db.ExecContext(ctx, `DELETE FROM worker_task_finalize_records WHERE created_at<?`,
		now.Add(-finalizeRecordRetention).UnixMilli()); err != nil {
		return 0, err
	}
	result, err := c.db.ExecContext(ctx, `DELETE FROM worker_tasks WHERE id IN (
		SELECT t.id FROM worker_tasks t
		WHERE t.status IN (?,?,?) AND t.finished_at<?
		AND NOT EXISTS (SELECT 1 FROM simulation_runs r WHERE r.task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM analysis_results r WHERE r.task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM research_backtest_runs r WHERE r.task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM research_optimization_runs r WHERE r.task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM market_asset_sync_state r WHERE r.last_task_id=t.id OR r.last_success_task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM market_asset_history_state r WHERE r.last_task_id=t.id OR r.last_success_task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM market_data_versions r WHERE r.task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM market_data_auto_update_rules r WHERE r.last_task_id=t.id)
		AND NOT EXISTS (SELECT 1 FROM worker_task_idempotency_keys r WHERE r.task_id=t.id)
		LIMIT 500
	)`, repository.WorkerTaskStatusComplete, repository.WorkerTaskStatusFailed,
		repository.WorkerTaskStatusCanceled, now.Add(-terminalTaskRetention).UnixMilli())
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	return count, nil
}
