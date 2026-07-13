package app

import (
	"context"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/fireman/fireman/internal/testutil"
)

func TestAutoUpdateScanDoesNotOverlapAcrossSchedulerSlots(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewWorkerTaskRepo(db)
	coordinator := taskcore.NewCoordinator(db, repo, taskcore.DefaultRegistry(), taskcore.NewEventHub())
	ctx := context.Background()

	if err := enqueueAutoUpdateScanTask(ctx, db, repo, coordinator, 100); err != nil {
		t.Fatal(err)
	}
	if err := enqueueAutoUpdateScanTask(ctx, db, repo, coordinator, 101); err != nil {
		t.Fatal(err)
	}
	items, total, err := repo.List(ctx, repository.WorkerTaskFilter{
		WorkerType: repository.WorkerTypeGo,
		Type:       repository.WorkerTaskTypeAutoUpdateScan,
		Statuses:   []string{repository.WorkerTaskStatusPending},
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("active scans=%d, want one", total)
	}
	mapped, _, err := repo.FindIdempotency(
		ctx, "system", "auto_update", repository.WorkerTaskTypeAutoUpdateScan, "100",
	)
	if err != nil || mapped.ID != items[0].ID {
		t.Fatalf("first slot mapping=%s err=%v, want %s", mapped.ID, err, items[0].ID)
	}
	if _, _, err := repo.FindIdempotency(
		ctx, "system", "auto_update", repository.WorkerTaskTypeAutoUpdateScan, "101",
	); !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		t.Fatalf("overlapped slot should be skipped without a second key, err=%v", err)
	}
}
