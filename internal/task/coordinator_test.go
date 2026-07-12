package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func testCoordinator(t *testing.T) (*Coordinator, *repository.WorkerTaskRepo, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	repo := repository.NewWorkerTaskRepo(db)
	return NewCoordinator(db, repo, DefaultRegistry(), NewEventHub()), repo, db
}

func createTask(t *testing.T, c *Coordinator, db *sql.DB, id, workerType, taskType string) {
	t.Helper()
	err := fdb.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		return c.CreateTx(context.Background(), tx, &repository.WorkerTask{
			ID: id, WorkerType: workerType, Type: taskType,
			Status: repository.WorkerTaskStatusPending, PayloadJSON: `{}`,
			ScopeType: "test", ScopeID: id, DedupeKey: taskType + "|" + id,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func claimRequest(workerType, token string) ClaimRequest {
	return ClaimRequest{WorkerType: workerType, WorkerID: workerType + ":test", ClaimToken: token}
}

func TestConcurrentClaimHasSingleWinnerAndIdempotentRetry(t *testing.T) {
	c, _, db := testCoordinator(t)
	createTask(t, c, db, "task_claim", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetHistorySync)

	var winners atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			token := fmt.Sprintf("claim-token-000000000000-%d", index)
			if _, err := c.Claim(context.Background(), "task_claim",
				claimRequest(repository.WorkerTypeSidecar, token)); err == nil {
				winners.Add(1)
			}
		}(i)
	}
	group.Wait()
	if winners.Load() != 1 {
		t.Fatalf("claim winners=%d, want 1", winners.Load())
	}
	item, err := c.Get(context.Background(), "task_claim")
	if err != nil {
		t.Fatal(err)
	}
	// The current owner can safely retry claim with the same token. Recover the
	// winning token from a deterministic second task to assert the contract.
	createTask(t, c, db, "task_idempotent", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetHistorySync)
	req := claimRequest(repository.WorkerTypeSidecar, "same-token-0000000000001")
	first, err := c.Claim(context.Background(), "task_idempotent", req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Claim(context.Background(), "task_idempotent", req)
	if err != nil || second.AttemptCount != first.AttemptCount {
		t.Fatalf("idempotent claim=%+v err=%v", second, err)
	}
	if item.AttemptCount != 1 {
		t.Fatalf("winner attempt_count=%d", item.AttemptCount)
	}
}

func TestHeartbeatOwnershipProgressAndWorkerType(t *testing.T) {
	c, _, db := testCoordinator(t)
	createTask(t, c, db, "task_hb", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetDirectorySync)
	if _, err := c.Claim(context.Background(), "task_hb",
		claimRequest(repository.WorkerTypeGo, "wrong-worker-token-00001")); !isTaskError(err, ErrWorkerTypeMismatch) {
		t.Fatalf("wrong worker error=%v", err)
	}
	req := claimRequest(repository.WorkerTypeSidecar, "heartbeat-token-000000001")
	claimed, err := c.Claim(context.Background(), "task_hb", req)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := c.Heartbeat(context.Background(), "task_hb", HeartbeatRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID, ClaimToken: req.ClaimToken,
		ProgressCurrent: 5, ProgressTotal: 10, Phase: "fetching",
	})
	if err != nil || updated.ProgressCurrent != 5 || updated.LeaseExpiresAt == nil ||
		*updated.LeaseExpiresAt < *claimed.LeaseExpiresAt {
		t.Fatalf("heartbeat=%+v err=%v", updated, err)
	}
	_, err = c.Heartbeat(context.Background(), "task_hb", HeartbeatRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID, ClaimToken: req.ClaimToken,
		ProgressCurrent: 4, ProgressTotal: 10,
	})
	if err == nil {
		t.Fatal("progress regression must fail")
	}
	_, err = c.Heartbeat(context.Background(), "task_hb", HeartbeatRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID,
		ClaimToken: "different-token-00000001", ProgressCurrent: 5, ProgressTotal: 10,
	})
	if !isTaskError(err, ErrLeaseLost) {
		t.Fatalf("wrong token error=%v", err)
	}
}

func TestExternalResultIdempotencyConflictRetryAndCancel(t *testing.T) {
	c, _, db := testCoordinator(t)
	createTask(t, c, db, "task_result", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeFXRateSync)
	req := claimRequest(repository.WorkerTypeSidecar, "result-token-00000000001")
	if _, err := c.Claim(context.Background(), "task_result", req); err != nil {
		t.Fatal(err)
	}
	report := ResultRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID,
		ClaimToken: req.ClaimToken, Outcome: "success", ResultKey: "resource:abc",
	}
	first, err := c.Report(context.Background(), "task_result", report)
	if err != nil || first.Status != repository.WorkerTaskStatusPreComplete {
		t.Fatalf("first result=%+v err=%v", first, err)
	}
	if _, err := c.Report(context.Background(), "task_result", report); err != nil {
		t.Fatalf("duplicate result: %v", err)
	}
	report.ResultKey = "resource:different"
	if _, err := c.Report(context.Background(), "task_result", report); !isTaskError(err, ErrResultConflict) {
		t.Fatalf("different result key error=%v", err)
	}

	createTask(t, c, db, "task_retry", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeFXRateSync)
	retryReq := claimRequest(repository.WorkerTypeSidecar, "retry-token-00000000001")
	_, _ = c.Claim(context.Background(), "task_retry", retryReq)
	retried, err := c.Report(context.Background(), "task_retry", ResultRequest{
		WorkerType: retryReq.WorkerType, WorkerID: retryReq.WorkerID,
		ClaimToken: retryReq.ClaimToken, Outcome: "failed", Retryable: true,
		ErrorCode: "temporary", ErrorMessage: "retry",
	})
	if err != nil || retried.Status != repository.WorkerTaskStatusPending {
		t.Fatalf("retry result=%+v err=%v", retried, err)
	}
	if retried.ClaimedBy != "" || retried.ClaimTokenHash != "" || retried.LeaseExpiresAt != nil {
		t.Fatalf("retried task retained ownership: %+v", retried)
	}
	if _, err := c.Report(context.Background(), "task_retry", ResultRequest{
		WorkerType: retryReq.WorkerType, WorkerID: retryReq.WorkerID,
		ClaimToken: retryReq.ClaimToken, Outcome: "failed", Retryable: true,
	}); err != nil {
		t.Fatalf("duplicate retry report=%v", err)
	}

	createTask(t, c, db, "task_retry_exhausted", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeFXRateSync)
	if _, err := db.Exec(`UPDATE worker_tasks SET max_attempts=1 WHERE id='task_retry_exhausted'`); err != nil {
		t.Fatal(err)
	}
	exhaustedReq := claimRequest(repository.WorkerTypeSidecar, "exhausted-token-00000001")
	_, _ = c.Claim(context.Background(), "task_retry_exhausted", exhaustedReq)
	exhausted, err := c.Report(context.Background(), "task_retry_exhausted", ResultRequest{
		WorkerType: exhaustedReq.WorkerType, WorkerID: exhaustedReq.WorkerID,
		ClaimToken: exhaustedReq.ClaimToken, Outcome: "failed", Retryable: true,
		ErrorCode: "temporary", ErrorMessage: "still unavailable",
	})
	if err != nil || exhausted.Status != repository.WorkerTaskStatusFailed || exhausted.ErrorCode != ErrRetryExhausted {
		t.Fatalf("retry exhaustion=%+v err=%v", exhausted, err)
	}

	createTask(t, c, db, "task_cancel", repository.WorkerTypeGo,
		repository.WorkerTaskTypeSimulation)
	canceled, err := c.RequestCancel(context.Background(), "task_cancel")
	if err != nil || canceled.Status != repository.WorkerTaskStatusCanceled {
		t.Fatalf("cancel pending=%+v err=%v", canceled, err)
	}
}

func TestExpiredLeaseRejectsHeartbeatUploadAndResultBeforeMaintenance(t *testing.T) {
	c, _, db := testCoordinator(t)
	now := time.Unix(1_800_000_000, 0)
	c.now = func() time.Time { return now }
	createTask(t, c, db, "task_expired_owner", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetHistorySync)
	req := claimRequest(repository.WorkerTypeSidecar, "expired-token-0000000001")
	if _, err := c.Claim(context.Background(), "task_expired_owner", req); err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return now.Add(61 * time.Second) }
	if _, err := c.Heartbeat(context.Background(), "task_expired_owner", HeartbeatRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID, ClaimToken: req.ClaimToken,
	}); !isTaskError(err, ErrLeaseLost) {
		t.Fatalf("expired heartbeat error=%v", err)
	}
	if _, err := c.CheckOwned(
		context.Background(), "task_expired_owner", OwnedRequest(req),
	); !isTaskError(err, ErrLeaseLost) {
		t.Fatalf("expired resource ownership error=%v", err)
	}
	if _, err := c.Report(context.Background(), "task_expired_owner", ResultRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID, ClaimToken: req.ClaimToken,
		Outcome: "success", ResultKey: "resource:abc",
	}); !isTaskError(err, ErrLeaseLost) {
		t.Fatalf("expired result error=%v", err)
	}
}

func TestGracefulReleaseRequeuesAndClearsOwnership(t *testing.T) {
	c, repo, db := testCoordinator(t)
	createTask(t, c, db, "task_release", repository.WorkerTypeGo,
		repository.WorkerTaskTypeSimulation)
	req := claimRequest(repository.WorkerTypeGo, "release-token-0000000001")
	if _, err := c.Claim(context.Background(), "task_release", req); err != nil {
		t.Fatal(err)
	}
	released, err := c.Release(
		context.Background(), "task_release", OwnedRequest(req),
	)
	if err != nil || released.Status != repository.WorkerTaskStatusPending ||
		released.ClaimedBy != "" || released.ClaimTokenHash != "" {
		t.Fatalf("released task=%+v err=%v", released, err)
	}
	attempts, err := repo.ListAttempts(context.Background(), "task_release")
	if err != nil || len(attempts) != 1 || attempts[0].Outcome != "retry_scheduled" {
		t.Fatalf("release attempts=%+v err=%v", attempts, err)
	}
}

func TestDirectCompletionRollsBackWithBusinessTransaction(t *testing.T) {
	c, _, db := testCoordinator(t)
	createTask(t, c, db, "task_atomic", repository.WorkerTypeGo,
		repository.WorkerTaskTypeSimulation)
	req := claimRequest(repository.WorkerTypeGo, "atomic-token-00000000001")
	_, err := c.Claim(context.Background(), "task_atomic", req)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("business write failed")
	err = fdb.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		if err := c.CompleteOwnedTx(context.Background(), tx, "task_atomic", req.WorkerID,
			repository.HashClaimToken(req.ClaimToken), "simulation_run:run", map[string]any{},
			time.Now().UnixMilli()); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error=%v", err)
	}
	item, _ := c.Get(context.Background(), "task_atomic")
	if item.Status != repository.WorkerTaskStatusRunning {
		t.Fatalf("task escaped rollback: %+v", item)
	}
}

func TestStartupAndExpiredLeaseRecovery(t *testing.T) {
	c, _, db := testCoordinator(t)
	now := time.Unix(1_800_000_000, 0)
	c.now = func() time.Time { return now }
	createTask(t, c, db, "task_go", repository.WorkerTypeGo,
		repository.WorkerTaskTypeSimulation)
	createTask(t, c, db, "task_sidecar", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetHistorySync)
	goReq := claimRequest(repository.WorkerTypeGo, "startup-go-token-0000001")
	sideReq := claimRequest(repository.WorkerTypeSidecar, "startup-side-token-0001")
	_, _ = c.Claim(context.Background(), "task_go", goReq)
	_, _ = c.Claim(context.Background(), "task_sidecar", sideReq)

	count, err := c.RecoverStartup(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("startup recovery count=%d err=%v", count, err)
	}
	goTask, _ := c.Get(context.Background(), "task_go")
	sideTask, _ := c.Get(context.Background(), "task_sidecar")
	if goTask.Status != repository.WorkerTaskStatusPending || sideTask.Status != repository.WorkerTaskStatusRunning {
		t.Fatalf("startup recovery go=%s sidecar=%s", goTask.Status, sideTask.Status)
	}
	c.now = func() time.Time { return now.Add(61 * time.Second) }
	count, err = c.RecoverExpired(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("expired recovery count=%d err=%v", count, err)
	}
}

func TestFinalizerReservationIsExclusive(t *testing.T) {
	c, _, db := testCoordinator(t)
	createTask(t, c, db, "task_finalize", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetHistorySync)
	req := claimRequest(repository.WorkerTypeSidecar, "finalize-token-000000001")
	_, _ = c.Claim(context.Background(), "task_finalize", req)
	_, err := c.Report(context.Background(), "task_finalize", ResultRequest{
		WorkerType: req.WorkerType, WorkerID: req.WorkerID, ClaimToken: req.ClaimToken,
		Outcome: "success", ResultKey: "resource:abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.ReserveDueFinalizations(context.Background(), 20)
	if err != nil || len(first) != 1 {
		t.Fatalf("first reservation=%+v err=%v", first, err)
	}
	second, err := c.ReserveDueFinalizations(context.Background(), 20)
	if err != nil || len(second) != 0 {
		t.Fatalf("second reservation=%+v err=%v", second, err)
	}
}

func TestFinalizerLimitFailsWithoutReturningEmptyReservation(t *testing.T) {
	c, _, db := testCoordinator(t)
	createTask(t, c, db, "task_finalize_limit", repository.WorkerTypeSidecar,
		repository.WorkerTaskTypeAssetHistorySync)
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`UPDATE worker_tasks SET status='pre_complete',pre_completed_at=?,
		finalize_attempts=?,next_finalize_at=? WHERE id='task_finalize_limit'`,
		now, FinalizeMaxAttempts, now); err != nil {
		t.Fatal(err)
	}
	items, err := c.ReserveDueFinalizations(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("terminal finalization returned reservations: %+v", items)
	}
	task, err := c.Get(context.Background(), "task_finalize_limit")
	if err != nil || task.Status != repository.WorkerTaskStatusFailed || task.ErrorCode != ErrFinalizeTimeout {
		t.Fatalf("finalization limit task=%+v err=%v", task, err)
	}
}

func isTaskError(err error, code string) bool {
	var taskErr *Error
	return errors.As(err, &taskErr) && taskErr.Code == code
}
