package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func newAdminStack(t *testing.T) (*AdminService, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	tasks := repository.NewWorkerTaskRepo(db)
	assets := repository.NewMarketAssetRepo(db)
	svc := NewAdminService(
		tasks,
		repository.NewJobRepo(db),
		repository.NewPostProcessRecordRepo(db),
		assets,
		NewMarketAssetService(db, tasks, assets),
		nil, // resource db absent: storage degrades to zero
		"",
	)
	return svc, db
}

type adminTaskSeed struct {
	ID             string
	Type           string
	Status         string
	DedupeKey      string
	CreatedAt      int64
	StartedAt      *int64
	PreCompletedAt *int64
	FinishedAt     *int64
	HeartbeatAt    *int64
}

func seedAdminTask(t *testing.T, db *sql.DB, s adminTaskSeed) {
	t.Helper()
	if s.Type == "" {
		s.Type = repository.WorkerTaskTypeAssetHistorySync
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO worker_tasks
			(id, version_no, type, status, dedupe_key, payload_json, result_data,
			 heartbeat_at, created_at, started_at, pre_completed_at, finished_at)
		VALUES (?,?,?,?,?,'{"k":1}','',?,?,?,?,?)`,
		s.ID, 1, s.Type, s.Status, s.DedupeKey,
		s.HeartbeatAt, s.CreatedAt, s.StartedAt, s.PreCompletedAt, s.FinishedAt); err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func ptr(v int64) *int64 { return &v }

func TestAdminOverview_Aggregation(t *testing.T) {
	svc, db := newAdminStack(t)
	fixed := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	now := fixed.UnixMilli()
	ctx := context.Background()

	// Worker tasks: 1 pending, 2 running (one stale at exactly 61s, one live
	// at exactly 60s — the boundary is strict "<").
	seedAdminTask(t, db, adminTaskSeed{ID: "t_p", Status: "pending", CreatedAt: now})
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_stale", Status: "running", CreatedAt: now, StartedAt: ptr(now),
		HeartbeatAt: ptr(now - 61_000),
	})
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_live", Status: "running", CreatedAt: now, StartedAt: ptr(now),
		HeartbeatAt: ptr(now - 60_000),
	})
	// 24h window boundary: finished exactly at the boundary counts; one ms
	// earlier does not.
	dayAgo := now - 24*3600_000
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_fail_in", Status: "failed", CreatedAt: dayAgo, FinishedAt: ptr(dayAgo),
	})
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_fail_out", Status: "failed", CreatedAt: dayAgo - 10, FinishedAt: ptr(dayAgo - 1),
	})
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_done", Status: "complete", CreatedAt: now - 100, FinishedAt: ptr(now - 50),
	})

	// Jobs.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, input_hash, progress_current, progress_total,
			phase, cancel_requested, retry_count, created_at, finished_at)
		VALUES
			('j_q','simulation','queued','',0,0,'',0,0,?,NULL),
			('j_r','simulation','running','',1,10,'mc',0,0,?,NULL),
			('j_f','stress','failed','',0,0,'',0,0,?,?),
			('j_s','simulation','succeeded','',0,0,'',0,0,?,?)`,
		now, now, now-7200_000, now-3600_000, now-7200_000, now-3600_000); err != nil {
		t.Fatal(err)
	}

	// Callbacks.
	records := repository.NewPostProcessRecordRepo(db)
	for _, rec := range []repository.PostProcessRecord{
		{TaskID: "t_done", Result: "success", CreatedAt: now - 1000},
		{TaskID: "t_fail_in", Result: "permanent_error", CreatedAt: now - 2000},
		{TaskID: "t_old", Result: "retryable_error", CreatedAt: dayAgo - 1},
	} {
		if err := records.Insert(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	// Sync state + versions + history dimensions.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO market_asset_sync_state (scope, last_task_id, last_success_task_id, last_success_at, updated_at)
		VALUES ('cn_all', 't_done', 't_done', ?, ?), ('us_all', '', 't_old', ?, ?)`,
		now-3600_000, now, now-8*24*3600_000, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO market_data_versions (version_key, version_no, task_id, updated_at)
		VALUES ('fx_rate|USDCNY', 3, 't_fx', ?)`, now-1800_000); err != nil {
		t.Fatal(err)
	}

	out, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if out.WorkerTasks.Active != 3 ||
		out.WorkerTasks.ByStatus["pending"] != 1 || out.WorkerTasks.ByStatus["running"] != 2 {
		t.Fatalf("worker task stats=%+v", out.WorkerTasks)
	}
	if out.WorkerTasks.FailedLast24h != 1 {
		t.Fatalf("failed_last_24h=%d, want boundary-inclusive 1", out.WorkerTasks.FailedLast24h)
	}
	if out.WorkerTasks.CompletedLast24h != 1 {
		t.Fatalf("completed_last_24h=%d", out.WorkerTasks.CompletedLast24h)
	}
	if out.WorkerTasks.StaleRunning != 1 {
		t.Fatalf("stale_running=%d, want strict 60s boundary", out.WorkerTasks.StaleRunning)
	}

	if out.Jobs.Queued != 1 || out.Jobs.Running != 1 ||
		out.Jobs.FailedLast24h != 1 || out.Jobs.SucceededLast24h != 1 {
		t.Fatalf("job stats=%+v", out.Jobs)
	}

	if out.Callbacks.TotalLast24h != 2 || out.Callbacks.FailedLast24h != 1 {
		t.Fatalf("callback stats=%+v", out.Callbacks)
	}

	if len(out.SyncHealth.DirectoryScopes) != 3 {
		t.Fatalf("directory scopes=%d", len(out.SyncHealth.DirectoryScopes))
	}
	byScope := map[string]AdminDirectoryScopeHealth{}
	for _, s := range out.SyncHealth.DirectoryScopes {
		byScope[s.Scope] = s
	}
	if byScope["cn_all"].Stale || byScope["cn_all"].LastSuccessAt == nil {
		t.Fatalf("cn_all=%+v", byScope["cn_all"])
	}
	if !byScope["us_all"].Stale {
		t.Fatalf("us_all should be stale: %+v", byScope["us_all"])
	}
	if byScope["hk_all"].Stale || byScope["hk_all"].LastSuccessAt != nil {
		t.Fatalf("hk_all never synced should not be stale: %+v", byScope["hk_all"])
	}

	if len(out.SyncHealth.FXPairs) != len(FXPairs) {
		t.Fatalf("fx pairs=%d", len(out.SyncHealth.FXPairs))
	}
	fxByPair := map[string]AdminFXPairHealth{}
	for _, p := range out.SyncHealth.FXPairs {
		fxByPair[p.Pair] = p
	}
	if fxByPair["USDCNY"].LastSuccessAt == nil || fxByPair["HKDCNY"].LastSuccessAt != nil {
		t.Fatalf("fx pairs=%+v", fxByPair)
	}

	if out.Storage.MainDBBytes != 0 || out.Storage.ResourceCount != 0 {
		t.Fatalf("storage should degrade to zero without paths: %+v", out.Storage)
	}
}

func TestAdminOverview_ActiveDirectoryTaskStatus(t *testing.T) {
	svc, db := newAdminStack(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_dir", Type: repository.WorkerTaskTypeAssetDirectorySync,
		Status: "running", CreatedAt: now, StartedAt: ptr(now), HeartbeatAt: ptr(now),
	})
	if _, err := db.ExecContext(ctx, `
		INSERT INTO market_asset_sync_state (scope, last_task_id, updated_at)
		VALUES ('cn_all', 't_dir', ?)`, now); err != nil {
		t.Fatal(err)
	}

	out, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range out.SyncHealth.DirectoryScopes {
		if s.Scope == "cn_all" && s.ActiveTaskStatus != "running" {
			t.Fatalf("cn_all active status=%q", s.ActiveTaskStatus)
		}
		if s.Scope == "hk_all" && s.ActiveTaskStatus != "" {
			t.Fatalf("hk_all active status=%q", s.ActiveTaskStatus)
		}
	}
}

func TestAdminListWorkerTasks_ValidationAndActive(t *testing.T) {
	svc, db := newAdminStack(t)
	ctx := context.Background()

	seedAdminTask(t, db, adminTaskSeed{ID: "t1", Status: "pending", CreatedAt: 100})
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t2", Status: "failed", CreatedAt: 200,
		StartedAt: ptr(210), FinishedAt: ptr(400),
	})

	if _, err := svc.ListWorkerTasks(ctx, AdminWorkerTaskListParams{Type: "bogus"}); err == nil {
		t.Fatal("expected invalid type error")
	}
	if _, err := svc.ListWorkerTasks(ctx, AdminWorkerTaskListParams{Status: "bogus"}); err == nil {
		t.Fatal("expected invalid status error")
	}

	page, err := svc.ListWorkerTasks(ctx, AdminWorkerTaskListParams{Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != "t1" {
		t.Fatalf("active page=%+v", page)
	}
	if page.Limit != 20 || page.Offset != 0 {
		t.Fatalf("default paging=%d/%d", page.Limit, page.Offset)
	}

	page, err = svc.ListWorkerTasks(ctx, AdminWorkerTaskListParams{Limit: 1000, Offset: -5})
	if err != nil {
		t.Fatal(err)
	}
	if page.Limit != 100 || page.Offset != 0 {
		t.Fatalf("normalized paging=%d/%d, want cap 100 / floor 0", page.Limit, page.Offset)
	}

	// duration_ms derived only when both ends exist.
	var failedItem, pendingItem *AdminWorkerTaskItem
	for i := range page.Items {
		switch page.Items[i].ID {
		case "t1":
			pendingItem = &page.Items[i]
		case "t2":
			failedItem = &page.Items[i]
		}
	}
	if failedItem == nil || failedItem.DurationMs == nil || *failedItem.DurationMs != 190 {
		t.Fatalf("failed item duration=%+v", failedItem)
	}
	if pendingItem == nil || pendingItem.DurationMs != nil {
		t.Fatalf("pending item should have nil duration: %+v", pendingItem)
	}
}

func TestAdminWorkerTaskDetail_TimelineShapes(t *testing.T) {
	svc, db := newAdminStack(t)
	ctx := context.Background()
	fixed := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }
	now := fixed.UnixMilli()

	// Pending: only created.
	seedAdminTask(t, db, adminTaskSeed{ID: "t_pending", Status: "pending", CreatedAt: 100})
	detail, err := svc.GetWorkerTaskDetail(ctx, "t_pending")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Timeline) != 1 || detail.Timeline[0].Phase != "created" {
		t.Fatalf("pending timeline=%+v", detail.Timeline)
	}
	if detail.Heartbeat != nil {
		t.Fatalf("pending heartbeat=%+v", detail.Heartbeat)
	}
	if detail.PostProcessRecords == nil {
		t.Fatal("records must be non-nil for JSON shape")
	}

	// Full lifecycle: all four phases plus terminal status on finished.
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_full", Status: "failed", CreatedAt: 100,
		StartedAt: ptr(200), PreCompletedAt: ptr(300), FinishedAt: ptr(400),
	})
	detail, err = svc.GetWorkerTaskDetail(ctx, "t_full")
	if err != nil {
		t.Fatal(err)
	}
	phases := make([]string, 0, len(detail.Timeline))
	for _, p := range detail.Timeline {
		phases = append(phases, p.Phase)
	}
	want := []string{"created", "started", "pre_complete", "finished"}
	for i, p := range want {
		if phases[i] != p {
			t.Fatalf("timeline=%v, want %v", phases, want)
		}
	}
	if detail.Timeline[3].Status != "failed" {
		t.Fatalf("finished status=%q", detail.Timeline[3].Status)
	}

	// Running with stale heartbeat.
	seedAdminTask(t, db, adminTaskSeed{
		ID: "t_run", Status: "running", CreatedAt: now - 300_000,
		StartedAt: ptr(now - 300_000), HeartbeatAt: ptr(now - 120_000),
	})
	detail, err = svc.GetWorkerTaskDetail(ctx, "t_run")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Heartbeat == nil || !detail.Heartbeat.Stale {
		t.Fatalf("running heartbeat=%+v, want stale", detail.Heartbeat)
	}
	// payload passes through verbatim.
	if detail.Task.PayloadJSON != `{"k":1}` {
		t.Fatalf("payload=%q", detail.Task.PayloadJSON)
	}

	// Not found maps to task_not_found.
	_, err = svc.GetWorkerTaskDetail(ctx, "missing")
	if err == nil {
		t.Fatal("expected task_not_found")
	}
	var ae *AppError
	if !errors.As(err, &ae) || ae.Code != "task_not_found" {
		t.Fatalf("err=%v", err)
	}
}

func TestAdminListJobs_Validation(t *testing.T) {
	svc, db := newAdminStack(t)
	ctx := context.Background()

	if _, err := svc.ListJobs(ctx, AdminJobListParams{Type: "bogus"}); err == nil {
		t.Fatal("expected invalid type error")
	}
	if _, err := svc.ListJobs(ctx, AdminJobListParams{Status: "bogus"}); err == nil {
		t.Fatal("expected invalid status error")
	}

	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, input_hash, progress_current, progress_total,
			phase, cancel_requested, retry_count, created_at)
		VALUES ('j1','simulation','queued','',0,0,'',0,0,?),
		       ('j2','simulation','succeeded','',0,0,'',0,0,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListJobs(ctx, AdminJobListParams{Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != "j1" {
		t.Fatalf("active jobs=%+v", page)
	}
}

func TestAdminListPostProcessRecords_Validation(t *testing.T) {
	svc, db := newAdminStack(t)
	ctx := context.Background()

	if _, err := svc.ListPostProcessRecords(ctx, AdminPostProcessRecordParams{Result: "bogus"}); err == nil {
		t.Fatal("expected invalid result error")
	}
	if _, err := svc.ListPostProcessRecords(ctx, AdminPostProcessRecordParams{TaskType: "bogus"}); err == nil {
		t.Fatal("expected invalid task_type error")
	}

	records := repository.NewPostProcessRecordRepo(db)
	if err := records.Insert(ctx, repository.PostProcessRecord{
		TaskID: "t1", TaskType: repository.WorkerTaskTypeFXRateSync,
		Result: "success", CreatedAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListPostProcessRecords(ctx, AdminPostProcessRecordParams{Result: "success"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].TaskID != "t1" {
		t.Fatalf("records=%+v", page)
	}
}

func TestAdminListDataVersions(t *testing.T) {
	svc, db := newAdminStack(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO market_data_versions (version_key, version_no, task_id, updated_at)
		VALUES ('asset_directory|cn_all', 5, 't1', 100),
		       ('fx_rate|USDCNY', 6, 't2', 200)`); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListDataVersions(ctx, "asset_directory", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].VersionKey != "asset_directory|cn_all" {
		t.Fatalf("versions=%+v", page)
	}
	if page.Limit != 20 {
		t.Fatalf("default limit=%d", page.Limit)
	}
}
