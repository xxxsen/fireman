package repository

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/testutil"
)

func insertRecord(t *testing.T, repo *PostProcessRecordRepo, rec PostProcessRecord) {
	t.Helper()
	if err := repo.Insert(context.Background(), rec); err != nil {
		t.Fatalf("insert record: %v", err)
	}
}

func TestPostProcessRecordRepo_InsertAndListByTask(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewPostProcessRecordRepo(db)
	ctx := context.Background()

	insertRecord(t, repo, PostProcessRecord{
		TaskID: "wt_a", TaskType: WorkerTaskTypeAssetHistorySync, AttemptNo: 0,
		Result: "retryable_error", ErrorCode: "resource_not_found",
		ErrorMessage: "gone", DurationMs: 45, CreatedAt: 1000,
	})
	insertRecord(t, repo, PostProcessRecord{
		TaskID: "wt_a", TaskType: WorkerTaskTypeAssetHistorySync, AttemptNo: 1,
		Result: "success", DurationMs: 30, CreatedAt: 2000,
	})
	insertRecord(t, repo, PostProcessRecord{
		TaskID: "wt_b", TaskType: WorkerTaskTypeFXRateSync,
		Result: "permanent_error", ErrorCode: "invalid_result_data", CreatedAt: 1500,
	})

	got, err := repo.ListByTask(ctx, "wt_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("records=%d, want 2", len(got))
	}
	// Newest first.
	if got[0].Result != "success" || got[0].AttemptNo != 1 {
		t.Fatalf("first record = %+v, want newest success", got[0])
	}
	if got[1].ErrorCode != "resource_not_found" || got[1].DurationMs != 45 {
		t.Fatalf("second record = %+v", got[1])
	}
}

func TestPostProcessRecordRepo_ListFilters(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewPostProcessRecordRepo(db)
	ctx := context.Background()

	insertRecord(t, repo, PostProcessRecord{
		TaskID: "wt_1", TaskType: WorkerTaskTypeAssetDirectorySync,
		Result: "success", CreatedAt: 100,
	})
	insertRecord(t, repo, PostProcessRecord{
		TaskID: "wt_2", TaskType: WorkerTaskTypeAssetHistorySync,
		Result: "retryable_error", CreatedAt: 200,
	})
	insertRecord(t, repo, PostProcessRecord{
		TaskID: "wt_2", TaskType: WorkerTaskTypeAssetHistorySync,
		Result: "permanent_error", CreatedAt: 300,
	})

	items, total, err := repo.List(ctx, PostProcessRecordFilter{TaskID: "wt_2", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("task filter total=%d len=%d, want 2/2", total, len(items))
	}
	if items[0].Result != "permanent_error" {
		t.Fatalf("expected newest first, got %q", items[0].Result)
	}

	items, total, err = repo.List(ctx, PostProcessRecordFilter{Result: "success", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || items[0].TaskID != "wt_1" {
		t.Fatalf("result filter total=%d items=%+v", total, items)
	}

	items, total, err = repo.List(ctx, PostProcessRecordFilter{
		TaskType: WorkerTaskTypeAssetHistorySync, Limit: 1, Offset: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("paged total=%d len=%d, want total 2 page 1", total, len(items))
	}
	if items[0].Result != "retryable_error" {
		t.Fatalf("page 2 item = %+v", items[0])
	}

	_, total, err = repo.List(ctx, PostProcessRecordFilter{TaskID: "missing", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("empty filter total=%d", total)
	}
}

func TestPostProcessRecordRepo_DeleteBeforeBoundary(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewPostProcessRecordRepo(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	cutoff := now - 30*24*time.Hour.Milliseconds()

	insertRecord(t, repo, PostProcessRecord{TaskID: "old", Result: "success", CreatedAt: cutoff - 1})
	insertRecord(t, repo, PostProcessRecord{TaskID: "edge", Result: "success", CreatedAt: cutoff})
	insertRecord(t, repo, PostProcessRecord{TaskID: "new", Result: "success", CreatedAt: now})

	n, err := repo.DeleteBefore(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("deleted=%d, want 1 (strictly before cutoff)", n)
	}
	_, total, err := repo.List(ctx, PostProcessRecordFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("remaining=%d, want exactly-30d and newer kept", total)
	}
}

func TestPostProcessRecordRepo_CountSince(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewPostProcessRecordRepo(db)
	ctx := context.Background()

	insertRecord(t, repo, PostProcessRecord{TaskID: "a", Result: "success", CreatedAt: 100})
	insertRecord(t, repo, PostProcessRecord{TaskID: "b", Result: "retryable_error", CreatedAt: 200})
	insertRecord(t, repo, PostProcessRecord{TaskID: "c", Result: "permanent_error", CreatedAt: 300})

	total, failed, err := repo.CountSince(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || failed != 2 {
		t.Fatalf("total=%d failed=%d, want 2/2 (>=since window)", total, failed)
	}
	total, failed, err = repo.CountSince(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || failed != 2 {
		t.Fatalf("total=%d failed=%d, want 3/2", total, failed)
	}
}
