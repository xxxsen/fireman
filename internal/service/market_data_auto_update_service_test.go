package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func newAutoUpdateServiceForTest(t *testing.T) (*AutoUpdateService, *repository.WorkerTaskRepo, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	tasks := repository.NewWorkerTaskRepo(db)
	assets := repository.NewMarketAssetRepo(db)
	market := NewMarketAssetService(db, tasks, assets)
	return NewAutoUpdateService(repository.NewMarketDataAutoUpdateRepo(db), assets, market), tasks, db
}

func TestAutoUpdateDirectorySchedulesOncePerPeriod(t *testing.T) {
	svc, tasks, _ := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	rule, err := svc.CreateDirectory(context.Background(), "cn_exchange_stock", 24)
	if err != nil {
		t.Fatal(err)
	}
	if !rule.Enabled || rule.IntervalHours != 24 {
		t.Fatalf("unexpected rule: %+v", rule)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, total, err := tasks.List(context.Background(), repository.WorkerTaskFilter{Type: repository.WorkerTaskTypeAssetDirectorySync, Limit: 10})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("tasks=%d items=%d err=%v", total, len(items), err)
	}
	fullTask, err := tasks.GetByID(context.Background(), items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var payload AssetDirectorySyncPayload
	if err := json.Unmarshal([]byte(fullTask.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SyncKey != "cn_exchange_stock" || payload.Force {
		t.Fatalf("unexpected directory payload: %+v", payload)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, total, err = tasks.List(context.Background(), repository.WorkerTaskFilter{Type: repository.WorkerTaskTypeAssetDirectorySync, Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("same-period tasks=%d err=%v", total, err)
	}
}

func TestAutoUpdateRuleUpdateRequiresCurrentVersion(t *testing.T) {
	svc, _, _ := newAutoUpdateServiceForTest(t)
	rule, err := svc.CreateDirectory(context.Background(), "hk_stock", 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(context.Background(), rule.ID, rule.Version, false, 24); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(context.Background(), rule.ID, rule.Version, true, 24); err == nil {
		t.Fatal("expected stale version conflict")
	}
}

func TestAutoUpdateEmptyDatabaseCreatesNoTasks(t *testing.T) {
	svc, tasks, _ := newAutoUpdateServiceForTest(t)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, total, err := tasks.List(context.Background(), repository.WorkerTaskFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("tasks=%d, want 0", total)
	}
}

func TestAutoUpdateReenableIsDueImmediately(t *testing.T) {
	svc, _, _ := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	rule, err := svc.CreateDirectory(context.Background(), "hk_etf", 24)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	paused, err := svc.Update(context.Background(), rule.ID, rule.Version, false, 6)
	if err != nil {
		t.Fatal(err)
	}
	if paused.NextRunAt != nil {
		t.Fatalf("paused next_run_at=%v, want nil", paused.NextRunAt)
	}
	now = now.Add(time.Hour)
	enabled, err := svc.Update(context.Background(), paused.ID, paused.Version, true, 6)
	if err != nil {
		t.Fatal(err)
	}
	if enabled.NextRunAt == nil || *enabled.NextRunAt != now.UnixMilli() {
		t.Fatalf("reenabled next_run_at=%v, want %d", enabled.NextRunAt, now.UnixMilli())
	}
}

func TestAutoUpdateBindsExistingManualTaskWithoutDuplicate(t *testing.T) {
	svc, tasks, _ := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	rule, err := svc.CreateDirectory(context.Background(), "us_stock", 24)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := svc.market.SyncDirectory(context.Background(), DirectorySyncRequest{SyncKey: "us_stock"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, total, err := tasks.List(context.Background(), repository.WorkerTaskFilter{Type: repository.WorkerTaskTypeAssetDirectorySync, Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("tasks=%d err=%v", total, err)
	}
	updated, err := svc.repo.Get(context.Background(), rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastTaskID != manual.Tasks[0].Task.ID {
		t.Fatalf("last_task_id=%s, want %s", updated.LastTaskID, manual.Tasks[0].Task.ID)
	}
}

func TestAutoUpdateReconcilesTerminalFailure(t *testing.T) {
	svc, _, db := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	created, err := svc.CreateDirectory(context.Background(), "us_etf", 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rule, err := svc.repo.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedAt := now.Add(time.Minute).UnixMilli()
	if _, err := db.Exec(`UPDATE worker_tasks SET status='failed',error_code='provider_down',error_message='provider unavailable',finished_at=? WHERE id=?`, failedAt, rule.LastTaskID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rule, err = svc.repo.Get(context.Background(), rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rule.LastFailedAt == nil || *rule.LastFailedAt != failedAt || rule.LastErrorCode != "provider_down" {
		t.Fatalf("failure was not reconciled: %+v", rule)
	}
}

func TestAutoUpdateConcurrentScansCreateOneTask(t *testing.T) {
	svc, tasks, _ := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if _, err := svc.CreateDirectory(context.Background(), "cn_mutual_fund", 24); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- svc.RunOnce(context.Background()) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	_, total, err := tasks.List(context.Background(), repository.WorkerTaskFilter{Type: repository.WorkerTaskTypeAssetDirectorySync, Limit: 10})
	if err != nil || total != 1 {
		t.Fatalf("tasks=%d err=%v", total, err)
	}
}

func TestAutoUpdateScanProcessesMoreThanOneBatch(t *testing.T) {
	svc, tasks, db := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 101 {
		key := fmt.Sprintf("US|us_stock|nasdaq|AUTO%03d", i)
		if err := svc.assets.UpsertAssetTx(ctx, tx, repository.MarketAsset{
			AssetKey: key, Market: "US", InstrumentType: "us_stock", RegionCode: "nasdaq",
			Symbol: fmt.Sprintf("AUTO%03d", i), Name: fmt.Sprintf("Auto %03d", i), Exchange: "NASDAQ",
			Currency: "USD", Active: true, SourceName: "test",
		}, now.UnixMilli()); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for i := range 101 {
		key := fmt.Sprintf("US|us_stock|nasdaq|AUTO%03d", i)
		if _, err := svc.SetHistory(ctx, key, "none", "adjusted_close", true); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	items, total, err := tasks.List(ctx, repository.WorkerTaskFilter{Type: repository.WorkerTaskTypeAssetHistorySync, Limit: 200})
	if err != nil || total != 101 {
		t.Fatalf("tasks=%d err=%v", total, err)
	}
	fullTask, err := tasks.GetByID(ctx, items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var payload AssetHistorySyncPayload
	if err := json.Unmarshal([]byte(fullTask.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestedRange != "full" || payload.ReplacementMode != "full" || payload.PointType != "adjusted_close" {
		t.Fatalf("unexpected history payload: %+v", payload)
	}
}

func TestAutoUpdateInvalidTargetRecordsFailureAndAdvancesPeriod(t *testing.T) {
	svc, tasks, _ := newAutoUpdateServiceForTest(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	rule, err := svc.repo.EnableHistory(context.Background(), "missing_asset", "none", "adjusted_close", now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rule, err = svc.repo.Get(context.Background(), rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := now.Add(24 * time.Hour).UnixMilli()
	if rule.LastFailedAt == nil || rule.LastErrorCode != "auto_update_target_invalid" || rule.NextRunAt == nil || *rule.NextRunAt != wantNext {
		t.Fatalf("invalid target state=%+v", rule)
	}
	_, total, err := tasks.List(context.Background(), repository.WorkerTaskFilter{Limit: 10})
	if err != nil || total != 0 {
		t.Fatalf("tasks=%d err=%v", total, err)
	}
}

func TestAutoUpdateFailedFilterOnlyReturnsUnrecoveredFailures(t *testing.T) {
	svc, _, db := newAutoUpdateServiceForTest(t)
	rule, err := svc.CreateDirectory(context.Background(), "hk_stock", 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE market_data_auto_update_rules SET last_failed_at=100,last_success_at=200 WHERE id=?`, rule.ID); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(context.Background(), AutoUpdateListParams{Enabled: "failed", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("recovered failure total=%d, want 0", page.Total)
	}
	if _, err := db.Exec(`UPDATE market_data_auto_update_rules SET last_failed_at=300 WHERE id=?`, rule.ID); err != nil {
		t.Fatal(err)
	}
	page, err = svc.List(context.Background(), AutoUpdateListParams{Enabled: "failed", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("current failure total=%d, want 1", page.Total)
	}
}

func TestAutoUpdateSchedulerRunsImmediatelyAndStops(t *testing.T) {
	svc, tasks, _ := newAutoUpdateServiceForTest(t)
	if _, err := svc.CreateDirectory(context.Background(), "hk_etf", 24); err != nil {
		t.Fatal(err)
	}
	scheduler := NewAutoUpdateScheduler(svc, time.Hour)
	scheduler.Start(context.Background())
	deadline := time.Now().Add(time.Second)
	for {
		_, total, err := tasks.List(context.Background(), repository.WorkerTaskFilter{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if total == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not run immediately")
		}
		time.Sleep(10 * time.Millisecond)
	}
	scheduler.Stop()
}
