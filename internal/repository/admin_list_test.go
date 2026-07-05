package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/testutil"
)

type taskSeed struct {
	ID          string
	Type        string
	Status      string
	DedupeKey   string
	CreatedAt   int64
	StartedAt   *int64
	FinishedAt  *int64
	HeartbeatAt *int64
}

func seedWorkerTask(t *testing.T, db *sql.DB, s taskSeed) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO worker_tasks
			(id, version_no, type, status, dedupe_key, payload_json,
			 heartbeat_at, created_at, started_at, finished_at)
		VALUES (?,?,?,?,?,'{}',?,?,?,?)`,
		s.ID, 1, s.Type, s.Status, s.DedupeKey,
		s.HeartbeatAt, s.CreatedAt, s.StartedAt, s.FinishedAt); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}
}

func i64(v int64) *int64 { return &v }

func TestWorkerTaskRepo_ListFiltersAndPaging(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewWorkerTaskRepo(db)
	ctx := context.Background()

	seedWorkerTask(t, db, taskSeed{
		ID: "wt_1", Type: WorkerTaskTypeAssetDirectorySync, Status: WorkerTaskStatusComplete,
		DedupeKey: "asset_directory|cn_all", CreatedAt: 100, StartedAt: i64(110), FinishedAt: i64(150),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_2", Type: WorkerTaskTypeAssetHistorySync, Status: WorkerTaskStatusRunning,
		DedupeKey: "asset_history|CN|cn_exchange_fund|sh|510300|none|close", CreatedAt: 200, StartedAt: i64(210),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_3", Type: WorkerTaskTypeAssetHistorySync, Status: WorkerTaskStatusFailed,
		DedupeKey: "asset_history|CN|cn_mutual_fund||007194|none|nav", CreatedAt: 300,
		StartedAt: i64(310), FinishedAt: i64(400),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_4", Type: WorkerTaskTypeFXRateSync, Status: WorkerTaskStatusPending,
		DedupeKey: "fx_rate|all", CreatedAt: 400,
	})

	// No filter: everything, newest first.
	items, total, err := repo.List(ctx, WorkerTaskFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(items) != 4 {
		t.Fatalf("total=%d len=%d, want 4/4", total, len(items))
	}
	if items[0].ID != "wt_4" || items[3].ID != "wt_1" {
		t.Fatalf("order = %s..%s, want created_at DESC", items[0].ID, items[3].ID)
	}
	// The slim projection never reads payload/result.
	if items[0].PayloadJSON != "" || items[0].ResultData != "" {
		t.Fatalf("list projection leaked payload/result: %+v", items[0])
	}

	// Type filter.
	items, total, err = repo.List(ctx, WorkerTaskFilter{Type: WorkerTaskTypeAssetHistorySync, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || items[0].ID != "wt_3" {
		t.Fatalf("type filter total=%d first=%s", total, items[0].ID)
	}

	// Status set filter (the service expands "active").
	items, total, err = repo.List(ctx, WorkerTaskFilter{
		Statuses: []string{WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete},
		Limit:    20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("active filter total=%d", total)
	}

	// Query: id prefix.
	_, total, err = repo.List(ctx, WorkerTaskFilter{Query: "wt_2", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("id prefix query total=%d", total)
	}
	// Query: dedupe_key substring.
	_, total, err = repo.List(ctx, WorkerTaskFilter{Query: "510300", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("dedupe substring query total=%d", total)
	}
	// LIKE escaping: a literal % must not match everything.
	_, total, err = repo.List(ctx, WorkerTaskFilter{Query: "%", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("escaped %% query total=%d, want 0", total)
	}

	// Paging: total stays the filtered count.
	items, total, err = repo.List(ctx, WorkerTaskFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(items) != 2 || items[0].ID != "wt_2" {
		t.Fatalf("page 2 total=%d len=%d first=%s", total, len(items), items[0].ID)
	}

	// Empty result.
	items, total, err = repo.List(ctx, WorkerTaskFilter{Query: "nothing-matches", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("empty result total=%d len=%d", total, len(items))
	}
}

func TestWorkerTaskRepo_Aggregates(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewWorkerTaskRepo(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	seedWorkerTask(t, db, taskSeed{
		ID: "wt_ok", Type: WorkerTaskTypeAssetDirectorySync, Status: WorkerTaskStatusComplete,
		CreatedAt: now - 3600_000, StartedAt: i64(now - 3500_000), FinishedAt: i64(now - 3400_000),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_old_fail", Type: WorkerTaskTypeAssetHistorySync, Status: WorkerTaskStatusFailed,
		CreatedAt: now - 48*3600_000, FinishedAt: i64(now - 25*3600_000),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_new_fail", Type: WorkerTaskTypeAssetHistorySync, Status: WorkerTaskStatusFailed,
		CreatedAt: now - 3600_000, FinishedAt: i64(now - 1800_000),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_stale", Type: WorkerTaskTypeAssetHistorySync, Status: WorkerTaskStatusRunning,
		CreatedAt: now - 300_000, StartedAt: i64(now - 300_000), HeartbeatAt: i64(now - 120_000),
	})
	seedWorkerTask(t, db, taskSeed{
		ID: "wt_live", Type: WorkerTaskTypeAssetHistorySync, Status: WorkerTaskStatusRunning,
		CreatedAt: now - 10_000, StartedAt: i64(now - 10_000), HeartbeatAt: i64(now - 5_000),
	})

	byStatus, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if byStatus[WorkerTaskStatusRunning] != 2 || byStatus[WorkerTaskStatusFailed] != 2 {
		t.Fatalf("byStatus=%v", byStatus)
	}

	since := now - 24*3600_000
	failed, err := repo.CountFinishedSince(ctx, WorkerTaskStatusFailed, since)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("failed last 24h=%d, want 1 (24h window boundary)", failed)
	}
	completed, err := repo.CountFinishedSince(ctx, WorkerTaskStatusComplete, since)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed last 24h=%d", completed)
	}

	stale, err := repo.CountStaleRunning(ctx, now-60_000)
	if err != nil {
		t.Fatal(err)
	}
	if stale != 1 {
		t.Fatalf("stale running=%d, want only heartbeat older than 60s", stale)
	}
}

func seedNamedPlanRow(t *testing.T, db *sql.DB, id, name string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plans (id, name, base_currency, valuation_date, config_version, created_at, updated_at)
		VALUES (?,?,?,?,1,?,?)`,
		id, name, "CNY", "2026-01-01", now, now); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func seedJob(t *testing.T, db *sql.DB, id, planID, jobType, status string, createdAt int64, started, finished *int64) {
	t.Helper()
	var plan any
	if planID != "" {
		plan = planID
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO jobs (id, plan_id, type, status, input_hash, progress_current, progress_total,
			phase, cancel_requested, retry_count, created_at, started_at, finished_at)
		VALUES (?,?,?,?,'',0,0,'',0,0,?,?,?)`,
		id, plan, jobType, status, createdAt, started, finished); err != nil {
		t.Fatalf("seed job: %v", err)
	}
}

func TestJobRepo_ListWithPlanJoin(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewJobRepo(db)
	ctx := context.Background()

	seedNamedPlanRow(t, db, "plan_x", "主计划")
	seedJob(t, db, "job_1", "plan_x", JobTypeSimulation, JobStatusSucceeded, 100, i64(110), i64(200))
	seedJob(t, db, "job_2", "plan_x", JobTypeStress, JobStatusRunning, 200, i64(210), nil)
	seedJob(t, db, "job_3", "", JobTypeSimulation, JobStatusQueued, 300, nil, nil)

	items, total, err := repo.List(ctx, JobFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}
	if items[0].ID != "job_3" {
		t.Fatalf("order first=%s, want created_at DESC", items[0].ID)
	}
	if items[0].PlanName != "" || items[0].PlanID != "" {
		t.Fatalf("system job should have empty plan: %+v", items[0])
	}
	if items[2].PlanName != "主计划" {
		t.Fatalf("plan join name=%q", items[2].PlanName)
	}

	// active expansion is service-side; the repo takes a status set.
	_, total, err = repo.List(ctx, JobFilter{
		Statuses: []string{JobStatusQueued, JobStatusRunning}, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("active total=%d", total)
	}

	_, total, err = repo.List(ctx, JobFilter{Type: JobTypeSimulation, PlanID: "plan_x", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("type+plan total=%d", total)
	}
}

func TestJobRepo_Aggregates(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewJobRepo(db)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	seedJob(t, db, "job_q", "", JobTypeSimulation, JobStatusQueued, now, nil, nil)
	seedJob(t, db, "job_r", "", JobTypeSimulation, JobStatusRunning, now, i64(now), nil)
	seedJob(t, db, "job_f_new", "", JobTypeStress, JobStatusFailed, now-7200_000, i64(now-7200_000), i64(now-3600_000))
	seedJob(t, db, "job_f_old", "", JobTypeStress, JobStatusFailed, now-50*3600_000, i64(now-50*3600_000), i64(now-30*3600_000))
	seedJob(t, db, "job_s", "", JobTypeSensitivity, JobStatusSucceeded, now-7200_000, i64(now-7200_000), i64(now-3600_000))

	byStatus, err := repo.CountByStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if byStatus[JobStatusQueued] != 1 || byStatus[JobStatusRunning] != 1 {
		t.Fatalf("byStatus=%v", byStatus)
	}
	failed, err := repo.CountFinishedSince(ctx, JobStatusFailed, now-24*3600_000)
	if err != nil {
		t.Fatal(err)
	}
	if failed != 1 {
		t.Fatalf("failed last 24h=%d", failed)
	}
	succeeded, err := repo.CountFinishedSince(ctx, JobStatusSucceeded, now-24*3600_000)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 {
		t.Fatalf("succeeded last 24h=%d", succeeded)
	}
}

func TestMarketAssetRepo_ListDataVersionsAndHistoryAggregate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewMarketAssetRepo(db)
	ctx := context.Background()

	seed := func(key string, no int64, updatedAt int64) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO market_data_versions (version_key, version_no, task_id, updated_at)
			VALUES (?,?,?,?)`, key, no, "wt_"+key, updatedAt); err != nil {
			t.Fatal(err)
		}
	}
	seed("asset_directory|cn_all", 10, 100)
	seed("asset_history|cn:cn_exchange_fund:sh:510300|none|adjusted_close", 11, 300)
	seed("fx_rate|USDCNY", 12, 200)

	items, total, err := repo.ListDataVersions(ctx, "", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}
	if items[0].VersionKey != "asset_history|cn:cn_exchange_fund:sh:510300|none|adjusted_close" {
		t.Fatalf("order first=%s, want updated_at DESC", items[0].VersionKey)
	}

	items, total, err = repo.ListDataVersions(ctx, "fx_rate", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || items[0].TaskID != "wt_fx_rate|USDCNY" {
		t.Fatalf("prefix filter total=%d items=%+v", total, items)
	}

	// History dimension aggregate.
	now := time.Now().UnixMilli()
	seedHistoryState := func(assetKey string, lastSuccess *int64) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO market_assets (asset_key, market, instrument_type, symbol, name,
				last_seen_at, source_name, refreshed_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			assetKey, "CN", "cn_exchange_fund", assetKey, assetKey, now, "test", now, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO market_asset_history_state
				(asset_key, adjust_policy, point_type, last_success_at, updated_at)
			VALUES (?,?,?,?,?)`,
			assetKey, "none", "adjusted_close", lastSuccess, now); err != nil {
			t.Fatal(err)
		}
	}
	seedHistoryState("a1", i64(now-1000))
	seedHistoryState("a2", i64(now-8*24*3600_000))
	seedHistoryState("a3", nil)

	agg, err := repo.AggregateHistoryStates(ctx, now-7*24*3600_000)
	if err != nil {
		t.Fatal(err)
	}
	if agg.Total != 3 || agg.StaleBefore != 1 || agg.NeverSynced != 1 {
		t.Fatalf("aggregate=%+v", agg)
	}
}
