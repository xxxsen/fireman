package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// researchTestNow keeps readiness/staleness deterministic: fixtures end on
// 2024-06-30 and "now" is the same day.
var researchTestNow = time.Date(2024, 6, 30, 12, 0, 0, 0, time.UTC)

func newResearchTestService(t *testing.T) (*ResearchService, *sql.DB) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	tasks := repository.NewWorkerTaskRepo(db)
	assets := repository.NewMarketAssetRepo(db)
	svc := NewResearchService(
		db,
		repository.NewResearchRepo(db),
		assets,
		tasks,
		repository.NewJobRepo(db),
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
		repository.NewPlanRepo(db),
		repository.NewHoldingsRepo(db),
		NewMarketAssetService(db, tasks, assets),
	)
	svc.now = func() time.Time { return researchTestNow }
	return svc, db
}

// insertResearchFixtureAsset creates a directory row and, when days > 0,
// daily points from start plus the matching history state.
func insertResearchFixtureAsset(
	t *testing.T, db *sql.DB, key, name, _ string, start string, days int,
	value func(i int) float64,
) {
	t.Helper()
	ctx := context.Background()
	assets := repository.NewMarketAssetRepo(db)
	now := researchTestNow.UnixMilli()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	asset := repository.MarketAsset{
		AssetKey: key, Market: "CN", InstrumentType: "cn_exchange_fund",
		RegionCode: "sh", Exchange: "SSE",
		Symbol: key, Name: name, Currency: "CNY", Active: true, ListingStatus: "active",
		SourceName: "test_source",
	}
	if err := assets.UpsertAssetTx(ctx, tx, asset, now); err != nil {
		t.Fatalf("upsert asset: %v", err)
	}
	if days > 0 {
		st, err := time.Parse("2006-01-02", start)
		if err != nil {
			t.Fatalf("parse start: %v", err)
		}
		points := make([]repository.MarketAssetPoint, 0, days)
		for i := 0; i < days; i++ {
			points = append(points, repository.MarketAssetPoint{
				AssetKey: key, AdjustPolicy: "hfq", PointType: "adjusted_close",
				TradeDate: st.AddDate(0, 0, i).Format("2006-01-02"),
				Value:     value(i), SourceName: "test_source", FetchedAt: now,
			})
		}
		if err := assets.UpsertPointsTx(ctx, tx, points); err != nil {
			t.Fatalf("upsert points: %v", err)
		}
		last := points[len(points)-1].TradeDate
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO market_asset_history_state
				(asset_key, adjust_policy, point_type, data_as_of, point_count, source_name, updated_at)
			VALUES (?,?,?,?,?,?,?)`,
			key, "hfq", "adjusted_close", last, days, "test_source", now); err != nil {
			t.Fatalf("insert history state: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func growthValue(base float64) func(i int) float64 {
	return func(i int) float64 {
		return base * (1 + 0.03*math.Sin(float64(i)/9)) * math.Pow(1.0002, float64(i))
	}
}

func mustCreateResearchCollection(
	t *testing.T, svc *ResearchService, in ResearchCollectionInput,
) ResearchCollectionDetail {
	t.Helper()
	detail, err := svc.CreateCollection(context.Background(), in)
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	return detail
}

func fptr(v float64) *float64 { return &v }

func TestResearchCollectionCRUDAndNormalize(t *testing.T) {
	svc, _ := newResearchTestService(t)
	ctx := context.Background()
	insertResearchFixtureAsset(t, svc.sql, "A1", "资产一", "CNY", "2020-01-01", 1643, growthValue(100))
	insertResearchFixtureAsset(t, svc.sql, "A2", "资产二", "CNY", "2020-01-01", 1643, growthValue(50))
	insertResearchFixtureAsset(t, svc.sql, "A3", "资产三", "CNY", "2020-01-01", 1643, growthValue(10))
	insertResearchFixtureAsset(t, svc.sql, "A4", "旧口径资产", "CNY", "", 0, nil)

	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name: "组合甲",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "A1", Weight: fptr(0.5), WeightLocked: true},
			{AssetKey: "A2", Weight: fptr(0.3)},
			{AssetKey: "A3", Weight: fptr(0.1)},
		},
	})
	if len(detail.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(detail.Items))
	}
	if detail.BaseCurrency != "CNY" || detail.RebalancePolicy != ResearchRebalanceMonthly {
		t.Fatalf("defaults not applied: %+v", detail.ResearchCollection)
	}

	// Normalize: locked 0.5 untouched, unlocked 0.3/0.1 rescale to 0.375/0.125.
	normalized, err := svc.NormalizeWeights(ctx, detail.ID)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	weights := map[string]float64{}
	sum := 0.0
	for _, item := range normalized.Items {
		weights[item.AssetKey] = item.Weight
		sum += item.Weight
	}
	if math.Abs(sum-1) > 1e-12 {
		t.Fatalf("normalized sum expected 1, got %v", sum)
	}
	if weights["A1"] != 0.5 {
		t.Fatalf("locked weight changed: %v", weights["A1"])
	}
	if math.Abs(weights["A2"]-0.375) > 1e-9 || math.Abs(weights["A3"]-0.125) > 1e-9 {
		t.Fatalf("unexpected normalized weights: %+v", weights)
	}

	// Duplicate dimension rejected.
	if _, err := svc.AddItem(ctx, detail.ID, ResearchCollectionItemInput{AssetKey: "A1"}); err == nil {
		t.Fatal("duplicate item should fail")
	}

	_, err = svc.AddItem(ctx, detail.ID, ResearchCollectionItemInput{
		AssetKey: "A4", AdjustPolicy: "none", PointType: "adjusted_close",
	})
	if err == nil {
		t.Fatal("none + adjusted_close must be rejected instead of canonicalized")
	}

	// Update + delete item.
	itemID := normalized.Items[1].ID
	updated, err := svc.UpdateItem(ctx, detail.ID, itemID, ResearchItemUpdate{
		Weight: fptr(0.2), Note: strptr("备注"),
	})
	if err != nil {
		t.Fatalf("update item: %v", err)
	}
	found := false
	for _, item := range updated.Items {
		if item.ID == itemID {
			found = true
			if item.Weight != 0.2 || item.Note != "备注" {
				t.Fatalf("item not updated: %+v", item.ResearchCollectionItem)
			}
		}
	}
	if !found {
		t.Fatal("updated item missing")
	}
	afterDelete, err := svc.DeleteItem(ctx, detail.ID, itemID)
	if err != nil {
		t.Fatalf("delete item: %v", err)
	}
	if len(afterDelete.Items) != 2 {
		t.Fatalf("expected 2 items after delete, got %d", len(afterDelete.Items))
	}

	// Archive and hard delete.
	if err := svc.DeleteCollection(ctx, detail.ID, false); err != nil {
		t.Fatalf("archive: %v", err)
	}
	archived, err := svc.ListCollections(ctx, repository.ResearchCollectionStatusArchived)
	if err != nil || len(archived) != 1 {
		t.Fatalf("archived listing wrong: %v %d", err, len(archived))
	}
	if err := svc.DeleteCollection(ctx, detail.ID, true); err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	if _, err := svc.GetCollection(ctx, detail.ID); err == nil {
		t.Fatal("collection should be gone")
	}
}

func strptr(s string) *string { return &s }

func TestResearchBacktestEndToEnd(t *testing.T) {
	svc, db := newResearchTestService(t)
	ctx := context.Background()
	insertResearchFixtureAsset(t, db, "B1", "股票基金", "CNY", "2020-01-01", 1643, growthValue(100))
	insertResearchFixtureAsset(t, db, "B2", "债券基金", "CNY", "2020-01-01", 1643,
		func(i int) float64 { return 100 * (1 + 0.01*math.Cos(float64(i)/13)) })

	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name: "端到端组合",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "B1", Weight: fptr(0.6)},
			{AssetKey: "B2", Weight: fptr(0.4)},
		},
	})

	readiness, err := svc.GetReadiness(ctx, detail.ID)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if !readiness.Ready {
		t.Fatalf("expected ready, got %+v", readiness.BlockingReasons)
	}

	created, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("create backtest: %v", err)
	}
	if created.Reused || created.Run.Status != repository.ResearchRunStatusQueued {
		t.Fatalf("expected new queued run, got %+v", created)
	}

	// Idempotency: identical input reuses the active run.
	dup, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("duplicate backtest: %v", err)
	}
	if !dup.Reused || dup.Run.ID != created.Run.ID {
		t.Fatalf("expected reuse of %s, got %+v", created.Run.ID, dup)
	}

	// Execute the job like the worker would.
	var phases []string
	err = svc.ExecuteBacktestJob(ctx, created.Run.JobID, func() bool { return false },
		func(_, _ int, phase string) { phases = append(phases, phase) })
	if err != nil {
		t.Fatalf("execute backtest job: %v", err)
	}
	if len(phases) == 0 || phases[len(phases)-1] != "done" {
		t.Fatalf("unexpected phases: %v", phases)
	}

	run, err := svc.GetRun(ctx, created.Run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != repository.ResearchRunStatusSucceeded {
		t.Fatalf("run status expected succeeded, got %s", run.Status)
	}
	if run.CompletedAt == nil {
		t.Fatal("completed_at missing")
	}
	var summary BacktestSummary
	if err := json.Unmarshal(run.Summary, &summary); err != nil {
		t.Fatalf("summary invalid: %v", err)
	}
	if summary.CAGR == 0 || len(summary.Contributions) != 2 {
		t.Fatalf("summary incomplete: %+v", summary)
	}
	if len(run.Years) < 4 || len(run.Months) < 50 {
		t.Fatalf("years/months missing: %d/%d", len(run.Years), len(run.Months))
	}

	points, err := svc.GetRunPoints(ctx, created.Run.ID, ResearchPointsParams{IncludeWeights: true})
	if err != nil {
		t.Fatalf("run points: %v", err)
	}
	if points.Total < 1600 || len(points.Points) == 0 {
		t.Fatalf("points missing: total=%d", points.Total)
	}
	if points.Points[0].NAV != 1 {
		t.Fatalf("normalized NAV expected 1, got %v", points.Points[0].NAV)
	}
	if len(points.Points[0].Weights) != 2 {
		t.Fatalf("weights not embedded: %+v", points.Points[0])
	}

	csv, filename, err := svc.ExportRunCSV(ctx, created.Run.ID)
	if err != nil {
		t.Fatalf("export csv: %v", err)
	}
	if !strings.HasPrefix(csv, "date,nav,") || !strings.Contains(filename, created.Run.ID) {
		t.Fatalf("csv export malformed: %s %s", filename, csv[:40])
	}

	// Same input after success: reuse the succeeded run.
	again, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("re-create backtest: %v", err)
	}
	if !again.Reused || again.Run.ID != created.Run.ID {
		t.Fatalf("expected succeeded-run reuse, got %+v", again)
	}

	// Changing a weight changes input_hash and produces a fresh run.
	items, err := svc.research.ListItems(ctx, detail.ID)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	if _, err := svc.UpdateItem(ctx, detail.ID, items[0].ID, ResearchItemUpdate{
		Weight: fptr(0.5),
	}); err != nil {
		t.Fatalf("update weight: %v", err)
	}
	if _, err := svc.UpdateItem(ctx, detail.ID, items[1].ID, ResearchItemUpdate{
		Weight: fptr(0.5),
	}); err != nil {
		t.Fatalf("update weight: %v", err)
	}
	fresh, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("create after weight change: %v", err)
	}
	if fresh.Reused || fresh.Run.ID == created.Run.ID {
		t.Fatalf("expected fresh run, got %+v", fresh)
	}
	if fresh.Run.InputHash == created.Run.InputHash {
		t.Fatal("input hash must change with weights")
	}

	runs, err := svc.ListRuns(ctx, detail.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("list runs: %v n=%d", err, len(runs))
	}
	recent, err := svc.ListRecentRuns(ctx, 5)
	if err != nil || len(recent) != 2 {
		t.Fatalf("recent runs: %v n=%d", err, len(recent))
	}
}

// TestResearchSourceHashIncludesForwardFillAnchor covers td/100 Finding 1:
// when a series has no observation on the window start day, the valuation
// forward-fills from the last pre-window point, so that anchor must be part
// of source_hash / input_hash. Points before the anchor stay outside the
// minimal closure and must not affect the hash.
func TestResearchSourceHashIncludesForwardFillAnchor(t *testing.T) {
	buildDataset := func(anchorValue, preAnchorValue float64) *researchDataset {
		a := rdAsset(t, "A", 0.5, "2020-01-01", 1642)
		b := rdAsset(t, "B", 0.5, "2020-06-01", 1490)
		// Drop A's observation on the common window start (B's first day)
		// so day one forward-fills from A's 2020-05-31 point.
		filtered := make([]repository.MarketAssetPoint, 0, len(a.Points))
		for _, p := range a.Points {
			switch p.TradeDate {
			case "2020-06-01":
				continue
			case "2020-05-31":
				p.Value = anchorValue
			case "2020-05-15":
				p.Value = preAnchorValue
			}
			filtered = append(filtered, p)
		}
		a.Points = filtered
		return rdDataset(a, b)
	}

	snapshotFor := func(ds *researchDataset) (researchInputSnapshot, string) {
		readiness := evaluateResearchReadiness(ds, rdNow(t))
		if !readiness.Ready {
			t.Fatalf("expected ready, got %+v", readiness.BlockingReasons)
		}
		if readiness.WindowStart != "2020-06-01" {
			t.Fatalf("window start expected 2020-06-01, got %s", readiness.WindowStart)
		}
		snapshot := buildResearchSnapshot(ds, readiness)
		return snapshot, computeResearchInputHash(snapshot, ds)
	}

	base, baseInput := snapshotFor(buildDataset(100, 100))

	var entryA researchSnapshotAsset
	for _, entry := range base.Assets {
		if entry.AssetKey == "A" {
			entryA = entry
		}
	}
	if entryA.AnchorDate != "2020-05-31" || entryA.FirstDate != "2020-05-31" {
		t.Fatalf("anchor not captured: anchor=%s first=%s", entryA.AnchorDate, entryA.FirstDate)
	}

	// Mutating the forward-fill anchor must change both hashes.
	moved, movedInput := snapshotFor(buildDataset(120, 100))
	if moved.SourceHash == base.SourceHash {
		t.Fatal("source_hash must change when the forward-fill anchor changes")
	}
	if movedInput == baseInput {
		t.Fatal("input_hash must change when the forward-fill anchor changes")
	}

	// Mutating a point strictly before the anchor stays outside the minimal
	// closure and must not change the hashes.
	unrelated, unrelatedInput := snapshotFor(buildDataset(100, 120))
	if unrelated.SourceHash != base.SourceHash {
		t.Fatal("source_hash must ignore points before the forward-fill anchor")
	}
	if unrelatedInput != baseInput {
		t.Fatal("input_hash must ignore points before the forward-fill anchor")
	}

	// A series that does observe the window start day has no anchor.
	withStart := rdDataset(rdAsset(t, "A", 0.5, "2020-01-01", 1642), rdAsset(t, "B", 0.5, "2020-06-01", 1490))
	snapshot := buildResearchSnapshot(withStart, evaluateResearchReadiness(withStart, rdNow(t)))
	for _, entry := range snapshot.Assets {
		if entry.AnchorDate != "" {
			t.Fatalf("unexpected anchor for %s: %s", entry.AssetKey, entry.AnchorDate)
		}
	}
}

func TestResearchTailRiskChangesInputHashButNotSourceHash(t *testing.T) {
	ds := rdDataset(rdAsset(t, "A", 1, "2020-01-01", 1642))
	ds.Collection.TailRiskConfidence = 0.95
	ds.Collection.TailRiskHorizonDays = 20
	readiness := evaluateResearchReadiness(ds, rdNow(t))
	if !readiness.Ready {
		t.Fatalf("fixture not ready: %+v", readiness.BlockingReasons)
	}
	first := buildResearchSnapshot(ds, readiness)
	firstInputHash := computeResearchInputHash(first, ds)

	ds.Collection.TailRiskConfidence = 0.90
	second := buildResearchSnapshot(ds, readiness)
	secondInputHash := computeResearchInputHash(second, ds)
	if first.SourceHash != second.SourceHash {
		t.Fatal("CVaR spec must not change source hash")
	}
	if firstInputHash == secondInputHash {
		t.Fatal("CVaR spec must change input hash")
	}
}

// TestResearchBacktestAnchorChangeCreatesFreshRun is the service-level
// acceptance for td/100 Finding 1: after a successful run, changing only the
// pre-window forward-fill anchor must not reuse the old run.
func TestResearchBacktestAnchorChangeCreatesFreshRun(t *testing.T) {
	svc, db := newResearchTestService(t)
	ctx := context.Background()
	insertResearchFixtureAsset(t, db, "D1", "早资产", "CNY", "2020-01-01", 1643, growthValue(100))
	insertResearchFixtureAsset(t, db, "D2", "晚资产", "CNY", "2020-06-01", 1491,
		func(i int) float64 { return 50 * (1 + 0.02*math.Cos(float64(i)/11)) })
	// Common window starts at D2's first day; drop D1's observation there so
	// its 2020-05-31 point becomes the forward-fill anchor.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM market_asset_points WHERE asset_key='D1' AND trade_date='2020-06-01'`); err != nil {
		t.Fatalf("delete window-start point: %v", err)
	}

	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name: "锚点组合",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "D1", Weight: fptr(0.5)},
			{AssetKey: "D2", Weight: fptr(0.5)},
		},
	})
	created, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("create backtest: %v", err)
	}
	if err := svc.ExecuteBacktestJob(ctx, created.Run.JobID, nil, nil); err != nil {
		t.Fatalf("execute backtest job: %v", err)
	}

	// Only the pre-window anchor changes; every in-window point is intact.
	if _, err := db.ExecContext(ctx, `
		UPDATE market_asset_points SET value = value * 1.1
		WHERE asset_key='D1' AND trade_date='2020-05-31'`); err != nil {
		t.Fatalf("mutate anchor: %v", err)
	}

	again, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("re-create backtest: %v", err)
	}
	if again.Reused || again.Run.ID == created.Run.ID {
		t.Fatalf("anchor change must not reuse the old run: %+v", again)
	}
	if again.Run.SourceHash == created.Run.SourceHash {
		t.Fatal("source_hash must change with the anchor")
	}
	if again.Run.InputHash == created.Run.InputHash {
		t.Fatal("input_hash must change with the anchor")
	}

	// Anchor drift between freeze and execution is caught by the pre-run
	// source verification.
	if _, err := db.ExecContext(ctx, `
		UPDATE market_asset_points SET value = value * 1.1
		WHERE asset_key='D1' AND trade_date='2020-05-31'`); err != nil {
		t.Fatalf("mutate anchor again: %v", err)
	}
	err = svc.ExecuteBacktestJob(ctx, again.Run.JobID, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "source data changed") {
		t.Fatalf("expected source-changed error for anchor drift, got %v", err)
	}
}

func TestResearchBacktestSourceChanged(t *testing.T) {
	svc, db := newResearchTestService(t)
	ctx := context.Background()
	insertResearchFixtureAsset(t, db, "C1", "资产", "CNY", "2020-01-01", 1643, growthValue(100))
	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name:  "源变更组合",
		Items: []ResearchCollectionItemInput{{AssetKey: "C1", Weight: fptr(1)}},
	})
	created, err := svc.CreateBacktest(ctx, detail.ID)
	if err != nil {
		t.Fatalf("create backtest: %v", err)
	}

	// Mutate one in-window point before the job runs.
	if _, err := db.ExecContext(ctx, `
		UPDATE market_asset_points SET value = value * 1.1
		WHERE asset_key='C1' AND trade_date='2022-01-05'`); err != nil {
		t.Fatalf("mutate point: %v", err)
	}

	err = svc.ExecuteBacktestJob(ctx, created.Run.JobID, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "source data changed") {
		t.Fatalf("expected source-changed error, got %v", err)
	}
	run, err := svc.GetRun(ctx, created.Run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != repository.ResearchRunStatusFailed {
		t.Fatalf("run should be failed, got %s", run.Status)
	}
}

func TestResearchSyncHistoryCreateReuseSkip(t *testing.T) {
	svc, db := newResearchTestService(t)
	ctx := context.Background()
	// FRESH has current data; BARE has directory entry only.
	insertResearchFixtureAsset(t, db, "FRESH", "新鲜资产", "CNY", "2020-01-01", 1643, growthValue(100))
	insertResearchFixtureAsset(t, db, "BARE", "无历史资产", "CNY", "", 0, nil)

	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name: "同步组合",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "FRESH", Weight: fptr(0.5)},
			{AssetKey: "BARE", Weight: fptr(0.5)},
		},
	})

	out, err := svc.SyncCollectionHistory(ctx, detail.ID, ResearchSyncRequest{})
	if err != nil {
		t.Fatalf("sync history: %v", err)
	}
	statuses := map[string]string{}
	for _, a := range out.Assets {
		statuses[a.AssetKey] = a.Status
	}
	if statuses["BARE"] != "created" {
		t.Fatalf("BARE expected created, got %+v", out.Assets)
	}
	if statuses["FRESH"] != "skipped" {
		t.Fatalf("FRESH expected skipped, got %+v", out.Assets)
	}

	// Second call: the still-pending task is reused.
	out2, err := svc.SyncCollectionHistory(ctx, detail.ID, ResearchSyncRequest{})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	statuses2 := map[string]string{}
	for _, a := range out2.Assets {
		statuses2[a.AssetKey] = a.Status
	}
	if statuses2["BARE"] != "existed" {
		t.Fatalf("BARE expected existed, got %+v", out2.Assets)
	}

	// Force refresh creates a task for the fresh asset too.
	out3, err := svc.SyncCollectionHistory(ctx, detail.ID, ResearchSyncRequest{Force: true})
	if err != nil {
		t.Fatalf("forced sync: %v", err)
	}
	for _, a := range out3.Assets {
		if a.AssetKey == "FRESH" && a.Status == "skipped" {
			t.Fatalf("force must not skip FRESH: %+v", out3.Assets)
		}
	}

	// Single-asset retry narrows the batch.
	out4, err := svc.SyncCollectionHistory(ctx, detail.ID, ResearchSyncRequest{
		AssetKeys: []string{"BARE"},
	})
	if err != nil {
		t.Fatalf("retry sync: %v", err)
	}
	if len(out4.Assets) != 1 || out4.Assets[0].AssetKey != "BARE" {
		t.Fatalf("retry should target BARE only: %+v", out4.Assets)
	}
}

func TestResearchCopyFromAndToPlan(t *testing.T) {
	svc, db := newResearchTestService(t)
	ctx := context.Background()
	insertResearchFixtureAsset(t, db, "P1", "股票", "CNY", "2020-01-01", 1643, growthValue(100))
	insertResearchFixtureAsset(t, db, "P2", "债券", "CNY", "2020-01-01", 1643, growthValue(50))

	now := researchTestNow.UnixMilli()
	plans := repository.NewPlanRepo(db)
	if err := plans.Create(ctx, repository.Plan{
		ID: "plan_1", Name: "退休计划", BaseCurrency: "CNY", ValuationDate: "2024-06-30",
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	for i, h := range []struct {
		id, key, class, region string
		amount                 int64
	}{
		{"h1", "P1", "equity", "cn", 600000},
		{"h2", "P2", "bond", "cn", 400000},
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO plan_holdings (
				id, plan_id, asset_key, enabled, asset_class, region,
				weight_within_group, current_amount_minor, simulation_snapshot_id,
				sort_order, created_at, updated_at
			) VALUES (?,?,?,1,?,?,0,?, '', ?, ?, ?)`,
			h.id, "plan_1", h.key, h.class, h.region, h.amount, i, now, now); err != nil {
			t.Fatalf("insert holding: %v", err)
		}
	}

	// Copy from plan: amounts become weights.
	detail, err := svc.CreateCollection(ctx, ResearchCollectionInput{
		Name: "来自计划", FromPlanID: "plan_1",
	})
	if err != nil {
		t.Fatalf("create from plan: %v", err)
	}
	weights := map[string]float64{}
	for _, item := range detail.Items {
		weights[item.AssetKey] = item.Weight
	}
	if math.Abs(weights["P1"]-0.6) > 1e-9 || math.Abs(weights["P2"]-0.4) > 1e-9 {
		t.Fatalf("plan weights wrong: %+v", weights)
	}

	// Copy to plan succeeds because asset_class/region came from the plan.
	result, err := svc.CopyToPlan(ctx, detail.ID, ResearchCopyToPlanRequest{PlanID: "plan_1"})
	if err != nil {
		t.Fatalf("copy to plan: %v", err)
	}
	if len(result.Holdings) != 2 || result.PlanName != "退休计划" {
		t.Fatalf("draft payload wrong: %+v", result)
	}
	var totalAmount int64
	for _, h := range result.Holdings {
		totalAmount += h.CurrentAmountMinor
	}
	if totalAmount != detail.InitialAmountMinor {
		t.Fatalf("draft amounts expected %d, got %d", detail.InitialAmountMinor, totalAmount)
	}

	// Items missing asset_class/region are rejected with details.
	bare := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name:  "缺字段",
		Items: []ResearchCollectionItemInput{{AssetKey: "P1", Weight: fptr(1)}},
	})
	_, err = svc.CopyToPlan(ctx, bare.ID, ResearchCopyToPlanRequest{PlanID: "plan_1"})
	var appErr *AppError
	if err == nil || !errorsAsAppError(err, &appErr) || appErr.Code != "research_items_incomplete" {
		t.Fatalf("expected research_items_incomplete, got %v", err)
	}
}

func errorsAsAppError(err error, target **AppError) bool {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		return false
	}
	*target = appErr
	return true
}

func TestResearchScreenerFiltersAndMetrics(t *testing.T) {
	svc, db := newResearchTestService(t)
	ctx := context.Background()
	insertResearchFixtureAsset(t, db, "S1", "沪深基金", "CNY", "2020-01-01", 1643, growthValue(100))
	insertResearchFixtureAsset(t, db, "S2", "无历史", "CNY", "", 0, nil)
	insertResearchFixtureAsset(t, db, "S3", "仅旧未复权历史", "CNY", "", 0, nil)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO market_asset_history_state
			(asset_key, adjust_policy, point_type, data_as_of, point_count, source_name, updated_at)
		VALUES ('S3', 'none', 'adjusted_close', '2024-06-30', 1000, 'legacy_raw', ?)`,
		researchTestNow.UnixMilli()); err != nil {
		t.Fatalf("insert legacy raw history: %v", err)
	}

	// Lazy backfill computes metrics for S1 during the first listing.
	out, err := svc.ListResearchAssets(ctx, ResearchAssetListParams{})
	if err != nil {
		t.Fatalf("list assets: %v", err)
	}
	byKey := map[string]ResearchAssetView{}
	for _, a := range out.Assets {
		byKey[a.AssetKey] = a
	}
	s1, ok := byKey["S1"]
	if !ok || !s1.HasHistory || s1.Metrics == nil {
		t.Fatalf("S1 metrics missing: %+v", s1)
	}
	if s1.Metrics.CAGR == nil || s1.Metrics.HistoryYears < 4 {
		t.Fatalf("S1 metrics incomplete: %+v", s1.Metrics)
	}
	s2, ok := byKey["S2"]
	if !ok || s2.HasHistory {
		t.Fatalf("S2 should lack history: %+v", s2)
	}
	if len(s2.QualityBadges) == 0 || s2.QualityBadges[0] != "missing_history" {
		t.Fatalf("S2 badges wrong: %+v", s2.QualityBadges)
	}
	s3, ok := byKey["S3"]
	if !ok || s3.HasHistory || s3.BacktestReady {
		t.Fatalf("legacy raw history must not be advertised as research-ready: %+v", s3)
	}

	// history_status filter.
	synced, err := svc.ListResearchAssets(ctx, ResearchAssetListParams{HistoryStatus: "synced"})
	if err != nil {
		t.Fatalf("synced filter: %v", err)
	}
	if synced.Total != 1 || synced.Assets[0].AssetKey != "S1" {
		t.Fatalf("synced filter wrong: %+v", synced)
	}

	// Metric bound filter: min CAGR above S1's excludes it.
	high := 10.0
	none, err := svc.ListResearchAssets(ctx, ResearchAssetListParams{MinCAGR: &high})
	if err != nil {
		t.Fatalf("cagr filter: %v", err)
	}
	if none.Total != 0 {
		t.Fatalf("expected empty result, got %d", none.Total)
	}

	// Invalid enum rejected.
	if _, err := svc.ListResearchAssets(ctx, ResearchAssetListParams{HistoryStatus: "bogus"}); err == nil {
		t.Fatal("invalid history_status should fail")
	}

	// Derived return/drawdown ratio is exposed on the view.
	if s1.Metrics.DownsideVolatility == nil || s1.Metrics.ReturnDrawdownRatio == nil {
		t.Fatalf("derived risk metrics missing: %+v", s1.Metrics)
	}

	// Downside volatility cap: the oscillating fixture has downside vol > 0.
	tiny := 1e-9
	if got, err := svc.ListResearchAssets(ctx,
		ResearchAssetListParams{MaxDownsideVolatility: &tiny}); err != nil || got.Total != 0 {
		t.Fatalf("downside filter: err=%v total=%d", err, got.Total)
	}
	loose := 100.0
	if got, err := svc.ListResearchAssets(ctx,
		ResearchAssetListParams{MaxDownsideVolatility: &loose}); err != nil || got.Total != 1 {
		t.Fatalf("loose downside filter: err=%v total=%d", err, got.Total)
	}

	// Return/drawdown ratio bound and sort key.
	huge := 1000.0
	if got, err := svc.ListResearchAssets(ctx,
		ResearchAssetListParams{MinReturnDrawdownRatio: &huge}); err != nil || got.Total != 0 {
		t.Fatalf("ratio filter: err=%v total=%d", err, got.Total)
	}
	floor := *s1.Metrics.ReturnDrawdownRatio - 0.01
	got, err := svc.ListResearchAssets(ctx, ResearchAssetListParams{
		MinReturnDrawdownRatio: &floor, SortBy: "return_drawdown", SortDesc: true,
	})
	if err != nil || got.Total != 1 || got.Assets[0].AssetKey != "S1" {
		t.Fatalf("ratio floor filter: err=%v %+v", err, got)
	}
}
