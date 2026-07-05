package service

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/testutil"
)

func newPostProcessDBs(t *testing.T) (*sql.DB, *resourcedb.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	resources, err := resourcedb.Open(context.Background(), filepath.Join(t.TempDir(), "resource.db"))
	if err != nil {
		t.Fatalf("open resource db: %v", err)
	}
	t.Cleanup(func() { _ = resources.Close() })
	return db, resources
}

func newPostProcessService(db *sql.DB, resources *resourcedb.DB, records postProcessRecordStore) *PostProcessService {
	return NewPostProcessService(
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
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO worker_tasks
			(id, version_no, type, status, dedupe_key, payload_json, result_data,
			 post_process_attempts, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		id, 1, taskType, status, "dk_"+id, "{}", resultData, attempts,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func lastRecord(t *testing.T, repo *repository.PostProcessRecordRepo, taskID string) repository.PostProcessRecord {
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

func TestPostProcess_RecordsSuccessCallback(t *testing.T) {
	db, resources := newPostProcessDBs(t)
	records := repository.NewPostProcessRecordRepo(db)
	svc := newPostProcessService(db, resources, records)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetHistorySync,
		repository.WorkerTaskStatusComplete, "", 2)

	res := svc.Process(context.Background(), "wt_ok")
	if res.Result != PostProcessSuccess {
		t.Fatalf("result=%+v", res)
	}
	rec := lastRecord(t, records, "wt_ok")
	if rec.Result != PostProcessSuccess || rec.TaskType != repository.WorkerTaskTypeAssetHistorySync {
		t.Fatalf("record=%+v", rec)
	}
	if rec.AttemptNo != 2 {
		t.Fatalf("attempt_no=%d, want snapshot of post_process_attempts", rec.AttemptNo)
	}
}

func TestPostProcess_RecordsPermanentCallbackForMissingTask(t *testing.T) {
	db, resources := newPostProcessDBs(t)
	records := repository.NewPostProcessRecordRepo(db)
	svc := newPostProcessService(db, resources, records)

	res := svc.Process(context.Background(), "wt_missing")
	if res.Result != PostProcessPermanentError || res.ErrorCode != "task_not_found" {
		t.Fatalf("result=%+v", res)
	}
	// The invalid callback itself is recorded, with empty snapshots.
	rec := lastRecord(t, records, "wt_missing")
	if rec.Result != PostProcessPermanentError || rec.ErrorCode != "task_not_found" {
		t.Fatalf("record=%+v", rec)
	}
	if rec.TaskType != "" || rec.AttemptNo != 0 {
		t.Fatalf("missing task should leave empty snapshots: %+v", rec)
	}
}

func TestPostProcess_RecordsRetryableCallback(t *testing.T) {
	db, resources := newPostProcessDBs(t)
	records := repository.NewPostProcessRecordRepo(db)
	svc := newPostProcessService(db, resources, records)
	// Valid envelope, but the resource pool is closed: an unclassified read
	// failure maps to retryable_error.
	_ = resources.Close()
	seedTaskRow(t, db, "wt_retry", repository.WorkerTaskTypeFXRateSync,
		repository.WorkerTaskStatusPreComplete,
		`{"resource_key":"abc","content_type":"application/json","content_encoding":"gzip","schema_version":1,"sha256":"abc","size_bytes":1}`,
		1)

	res := svc.Process(context.Background(), "wt_retry")
	if res.Result != PostProcessRetryableError {
		t.Fatalf("result=%+v", res)
	}
	rec := lastRecord(t, records, "wt_retry")
	if rec.Result != PostProcessRetryableError || rec.TaskType != repository.WorkerTaskTypeFXRateSync {
		t.Fatalf("record=%+v", rec)
	}
}

// failingRecordStore injects observation-layer failures.
type failingRecordStore struct {
	insertErr error
	inserted  []repository.PostProcessRecord
}

func (f *failingRecordStore) Insert(_ context.Context, rec repository.PostProcessRecord) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.inserted = append(f.inserted, rec)
	return nil
}

func (f *failingRecordStore) DeleteBefore(context.Context, int64) (int64, error) {
	return 0, errors.New("cleanup boom")
}

func TestPostProcess_RecordInsertFailureKeepsClassification(t *testing.T) {
	db, resources := newPostProcessDBs(t)
	store := &failingRecordStore{insertErr: errors.New("insert boom")}
	svc := newPostProcessService(db, resources, store)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 0)

	res := svc.Process(context.Background(), "wt_ok")
	if res.Result != PostProcessSuccess {
		t.Fatalf("classification changed by record failure: %+v", res)
	}
}

func TestPostProcess_CleanupFailureKeepsClassification(t *testing.T) {
	db, resources := newPostProcessDBs(t)
	store := &failingRecordStore{}
	svc := newPostProcessService(db, resources, store)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 0)

	res := svc.Process(context.Background(), "wt_ok")
	if res.Result != PostProcessSuccess {
		t.Fatalf("classification changed by cleanup failure: %+v", res)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("inserted=%d, want 1", len(store.inserted))
	}
}

func TestPostProcess_RetentionCleanupRemovesOldRecords(t *testing.T) {
	db, resources := newPostProcessDBs(t)
	records := repository.NewPostProcessRecordRepo(db)
	svc := newPostProcessService(db, resources, records)
	seedTaskRow(t, db, "wt_ok", repository.WorkerTaskTypeAssetDirectorySync,
		repository.WorkerTaskStatusComplete, "", 0)

	// One record beyond retention, one inside.
	old := time.Now().Add(-31 * 24 * time.Hour).UnixMilli()
	fresh := time.Now().Add(-1 * time.Hour).UnixMilli()
	for _, at := range []int64{old, fresh} {
		if err := records.Insert(context.Background(), repository.PostProcessRecord{
			TaskID: "wt_history", Result: PostProcessSuccess, CreatedAt: at,
		}); err != nil {
			t.Fatal(err)
		}
	}

	svc.Process(context.Background(), "wt_ok")

	items, total, err := records.List(context.Background(), repository.PostProcessRecordFilter{
		TaskID: "wt_history", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || items[0].CreatedAt != fresh {
		t.Fatalf("retention cleanup kept %d records (%+v), want only the fresh one", total, items)
	}
}
