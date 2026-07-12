package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/testutil"
)

var taskFinalizeTaskVersion int64

func newTaskFinalizeDBs(t *testing.T) (*sql.DB, *resourcedb.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	resources, err := resourcedb.Open(context.Background(), filepath.Join(t.TempDir(), "resource.db"))
	if err != nil {
		t.Fatalf("open resource db: %v", err)
	}
	t.Cleanup(func() { _ = resources.Close() })
	return db, resources
}

func newTaskFinalizer(db *sql.DB, resources *resourcedb.DB, records taskFinalizeRecordStore) *TaskFinalizer {
	return NewTaskFinalizer(
		db,
		repository.NewWorkerTaskRepo(db),
		repository.NewMarketAssetRepo(db),
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
		resources,
		records,
	)
}

func seedTaskRow(t *testing.T, db *sql.DB, id, taskType, status, resultData string, attempts int) {
	t.Helper()
	taskFinalizeTaskVersion++
	resultKey := ""
	if resultData != "" {
		var env resourcedb.Envelope
		if err := json.Unmarshal([]byte(resultData), &env); err == nil && env.ResourceKey != "" {
			resultKey = "resource:" + env.ResourceKey
		}
	}
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO worker_tasks
			(id, version_no, worker_type, type, status, dedupe_key, payload_json,
			 result_key, result_meta_json, finalize_attempts, available_at, created_at, updated_at)
		VALUES (?,?,'sidecar_worker',?,?,?,?,?,?,?,?,?,?)`,
		id, taskFinalizeTaskVersion, taskType, status, "dk_"+id, "{}", resultKey,
		resultData, attempts, now, now, now); err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func lastRecord(t *testing.T, repo *repository.WorkerTaskFinalizeRecordRepo, taskID string) repository.WorkerTaskFinalizeRecord {
	t.Helper()
	records, err := repo.ListByTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatalf("no record for task %s", taskID)
	}
	return records[0]
}

func TestTaskFinalize_RecordsSuccessFinalization(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	records := repository.NewWorkerTaskFinalizeRecordRepo(db)
	svc := newTaskFinalizer(db, resources, records)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetHistorySync,
		repository.WorkerTaskStatusComplete, "", 2)

	res := svc.processAndRecord(context.Background(), "wt_ok")
	if res.Result != TaskFinalizeSuccess {
		t.Fatalf("result=%+v", res)
	}
	rec := lastRecord(t, records, "wt_ok")
	if rec.Result != TaskFinalizeSuccess || rec.TaskType != repository.WorkerTaskTypeAssetHistorySync {
		t.Fatalf("record=%+v", rec)
	}
	if rec.AttemptNo != 2 {
		t.Fatalf("attempt_no=%d, want snapshot of finalize_attempts", rec.AttemptNo)
	}
}

func TestTaskFinalize_SuccessUpdatesLinkedAutoUpdateRule(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	svc := newTaskFinalizer(db, resources, repository.NewWorkerTaskFinalizeRecordRepo(db))
	autoRepo := repository.NewMarketDataAutoUpdateRepo(db)
	svc.SetAutoUpdateRepo(autoRepo)
	seedTaskRow(t, db, "wt_auto_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 1)
	now := time.Now().UnixMilli()
	rule, err := autoRepo.UpsertDirectory(context.Background(), "cn_exchange_stock", 24, now, now+86_400_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := autoRepo.BindTask(context.Background(), rule.ID, rule.Version, "wt_auto_ok", now, now+int64(24*time.Hour/time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	if result := svc.processAndRecord(context.Background(), "wt_auto_ok"); result.Result != TaskFinalizeSuccess {
		t.Fatalf("result=%+v", result)
	}
	updated, err := autoRepo.Get(context.Background(), rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastSuccessAt == nil {
		t.Fatal("last_success_at was not recorded")
	}
}

func TestTaskFinalize_RecordsPermanentFinalizationForMissingTask(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	records := repository.NewWorkerTaskFinalizeRecordRepo(db)
	svc := newTaskFinalizer(db, resources, records)

	res := svc.processAndRecord(context.Background(), "wt_missing")
	if res.Result != TaskFinalizePermanentError || res.ErrorCode != "task_not_found" {
		t.Fatalf("result=%+v", res)
	}
	// The invalid Go finalizer itself is recorded, with empty snapshots.
	rec := lastRecord(t, records, "wt_missing")
	if rec.Result != TaskFinalizePermanentError || rec.ErrorCode != "task_not_found" {
		t.Fatalf("record=%+v", rec)
	}
	if rec.TaskType != "" || rec.AttemptNo != 0 {
		t.Fatalf("missing task should leave empty snapshots: %+v", rec)
	}
}

func TestTaskFinalize_RecordsRetryableFinalization(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	records := repository.NewWorkerTaskFinalizeRecordRepo(db)
	svc := newTaskFinalizer(db, resources, records)
	// Valid envelope, but the resource pool is closed: an unclassified read
	// failure maps to retryable_error.
	_ = resources.Close()
	seedTaskRow(t, db, "wt_retry", repository.WorkerTaskTypeFXRateSync,
		repository.WorkerTaskStatusPreComplete,
		`{"resource_key":"abc","content_type":"application/json","content_encoding":"gzip","schema_version":1,"sha256":"abc","size_bytes":1}`,
		1)

	res := svc.processAndRecord(context.Background(), "wt_retry")
	if res.Result != TaskFinalizeRetryableError {
		t.Fatalf("result=%+v", res)
	}
	rec := lastRecord(t, records, "wt_retry")
	if rec.Result != TaskFinalizeRetryableError || rec.TaskType != repository.WorkerTaskTypeFXRateSync {
		t.Fatalf("record=%+v", rec)
	}
}

// failingRecordStore injects observation-layer failures.
type failingRecordStore struct {
	insertErr error
	inserted  []repository.WorkerTaskFinalizeRecord
}

func (f *failingRecordStore) Insert(_ context.Context, rec repository.WorkerTaskFinalizeRecord) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, rec)
	return nil
}

func (f *failingRecordStore) DeleteBefore(context.Context, int64) (int64, error) {
	return 0, errors.New("cleanup boom")
}

func TestTaskFinalize_RecordInsertFailureKeepsClassification(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	store := &failingRecordStore{insertErr: errors.New("insert boom")}
	svc := newTaskFinalizer(db, resources, store)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 0)

	res := svc.processAndRecord(context.Background(), "wt_ok")
	if res.Result != TaskFinalizeSuccess {
		t.Fatalf("classification changed by record failure: %+v", res)
	}
}

func TestTaskFinalize_CleanupFailureKeepsClassification(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	store := &failingRecordStore{}
	svc := newTaskFinalizer(db, resources, store)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 0)

	res := svc.processAndRecord(context.Background(), "wt_ok")
	if res.Result != TaskFinalizeSuccess {
		t.Fatalf("classification changed by cleanup failure: %+v", res)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("inserted=%d, want 1", len(store.inserted))
	}
}

func TestTaskFinalize_RetentionCleanupRemovesOldRecords(t *testing.T) {
	db, resources := newTaskFinalizeDBs(t)
	records := repository.NewWorkerTaskFinalizeRecordRepo(db)
	svc := newTaskFinalizer(db, resources, records)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 0)

	// One record beyond retention, one inside.
	old := time.Now().Add(-31 * 24 * time.Hour).UnixMilli()
	fresh := time.Now().Add(-1 * time.Hour).UnixMilli()
	for _, at := range []int64{old, fresh} {
		if err := records.Insert(context.Background(), repository.WorkerTaskFinalizeRecord{
			TaskID: "wt_history", Result: TaskFinalizeSuccess, CreatedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	svc.processAndRecord(context.Background(), "wt_ok")

	items, total, err := records.List(context.Background(), repository.WorkerTaskFinalizeRecordFilter{
		TaskID: "wt_history", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || items[0].CreatedAt != fresh {
		t.Fatalf("retention cleanup kept %d records (%+v), want only the fresh one", total, items)
	}
}
