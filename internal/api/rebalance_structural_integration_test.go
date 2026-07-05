package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func seedBondPlanWithHoldingAmount(t *testing.T, db *sql.DB, configuredMinor, holdingMinor int64) string {
	t.Helper()
	plan := createTestPlan(t, db)
	planID := plan.ID

	snapRepo := repository.NewSnapshotRepo(db)
	instID := "ins_rebalance_bond"
	now := time.Now().UnixMilli()
	if err := snapRepo.EnsureMarketAsset(context.Background(), repository.MarketAsset{
		AssetKey: instID, Symbol: "BOND001", Name: "测试债券基金",
		Market: "CN", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}
	snapID := "snap_rebalance_bond"
	if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
		ID: snapID, AssetKey: instID, PlanID: &planID,
		InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
		CompleteYearCount: 5, DailyObservationCount: 100, MonthlyReturnCount: 60,
		VolatilityMethod: "monthly_log_return_sample_stddev_annualized",
		MetricsVersion:   "monthly_log_return_v1", HistoryDepth: "five_plus_years",
		HistoricalCAGR: 0.04, ModeledAnnualReturn: 0.04, AnnualVolatility: 0.05, MaxDrawdown: 0.05,
		ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
		SourceMode: "akshare_historical", QualityStatus: "available",
		WarningsJSON: "[]", SourceHash: "fixture_hash", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	holdID := "hold_rebalance_bond"
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plan_holdings (
			id, plan_id, asset_key, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?,?,?,1,'bond','domestic',1.0,?,?,1,?,?)`,
		holdID, planID, instID, holdingMinor, snapID, now, now); err != nil {
		t.Fatal(err)
	}

	stmts := []struct {
		query string
		args  []any
	}{
		{`UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, []any{configuredMinor, planID}},
		{`UPDATE plan_asset_class_targets SET weight=1.0 WHERE plan_id=? AND asset_class='bond'`, []any{planID}},
		{`UPDATE plan_asset_class_targets SET weight=0 WHERE plan_id=? AND asset_class IN ('equity','cash')`, []any{planID}},
		{`UPDATE plan_region_targets SET weight_within_class=1.0
		 WHERE plan_id=? AND asset_class='bond' AND region='domestic'`, []any{planID}},
		{`UPDATE plan_region_targets SET weight_within_class=0
		 WHERE plan_id=? AND asset_class='bond' AND region='foreign'`, []any{planID}},
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(context.Background(), stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	return planID
}

func getRebalanceSummary(t *testing.T, srv *httptest.Server, planID string) map[string]any {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rebalance status=%d body=%s", resp.StatusCode, string(body))
	}
	env := decodeEnvelope(t, body)
	data := env["data"].(map[string]any)
	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary: %+v", data)
	}
	return summary
}

func TestRebalanceAPI_StructuralHoldWhenScaleOver(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedBondPlanWithHoldingAmount(t, db, 450_000_00, 500_000_00)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()

	summary := getRebalanceSummary(t, srv, planID)
	if got := int64(summary["scale_gap_minor"].(float64)); got != 50_000_00 {
		t.Fatalf("scale_gap_minor=%d want 5000000", got)
	}
	if got := int(summary["structural_actionable_count"].(float64)); got != 0 {
		t.Fatalf("structural_actionable_count=%d want 0", got)
	}
	if got := int(summary["actionable_count"].(float64)); got != 0 {
		t.Fatalf("actionable_count=%d want 0", got)
	}
}

func TestRebalanceAPI_StructuralHoldWhenScaleUnder(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedBondPlanWithHoldingAmount(t, db, 450_000_00, 400_000_00)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()

	summary := getRebalanceSummary(t, srv, planID)
	if got := int64(summary["scale_gap_minor"].(float64)); got != -50_000_00 {
		t.Fatalf("scale_gap_minor=%d want -5000000", got)
	}
	if got := int(summary["structural_actionable_count"].(float64)); got != 0 {
		t.Fatalf("structural_actionable_count=%d want 0", got)
	}
}

func TestRebalanceExportCSV_UsesStructuralColumns(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedBondPlanWithHoldingAmount(t, db, 450_000_00, 500_000_00)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/plans/" + planID + "/export/rebalance.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d", resp.StatusCode)
	}
	body := string(mustRead(t, resp))
	if !containsAll(body, "structural_gap_weight", "structural_current_weight", "plan_scale_action") {
		t.Fatalf("export missing structural columns: %s", body)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
