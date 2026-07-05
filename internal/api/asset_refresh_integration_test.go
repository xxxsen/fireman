package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func seedThreeHoldingsRebalancePlan(t *testing.T, db *sql.DB) (string, []string) {
	t.Helper()
	plan := createTestPlan(t, db)
	planID := plan.ID
	now := time.Now().UnixMilli()
	snapRepo := repository.NewSnapshotRepo(db)

	amounts := []int64{120_000_00, 90_000_00, 90_000_00}
	weights := []float64{0.3334, 0.3333, 0.3333}
	instIDs := []string{"ins_rbd_a", "ins_rbd_b", "ins_rbd_c"}

	for i, instID := range instIDs {
		if err := snapRepo.EnsureMarketAsset(context.Background(), repository.MarketAsset{
			AssetKey: instID, Symbol: "RB" + string(rune('A'+i)), Name: "测试标的" + string(rune('A'+i)),
			Market: "CN", Currency: "CNY",
		}); err != nil {
			t.Fatal(err)
		}
		snapID := "snap_" + instID
		if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
			ID: snapID, AssetKey: instID, PlanID: &planID,
			InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
			CompleteYearCount: 5, DailyObservationCount: 100, MonthlyReturnCount: 60,
			VolatilityMethod: "monthly_log_return_sample_stddev_annualized",
			MetricsVersion:   "monthly_log_return_v1", HistoryDepth: "five_plus_years",
			HistoricalCAGR: 0.08, ModeledAnnualReturn: 0.08, AnnualVolatility: 0.15, MaxDrawdown: 0.2,
			ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
			SourceMode: "akshare_historical", QualityStatus: "available",
			WarningsJSON: "[]", SourceHash: "fixture", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO plan_holdings (
				id, plan_id, asset_key, enabled, asset_class, region,
				weight_within_group, current_amount_minor, simulation_snapshot_id,
				sort_order, created_at, updated_at
			) VALUES (?,?,?,1,'equity','domestic',?,?,?,?,?,?)`,
			"hold_"+instID, planID, instID, weights[i], amounts[i], snapID, i*10, now, now); err != nil {
			t.Fatal(err)
		}
	}

	stmts := []struct {
		query string
		args  []any
	}{
		{`UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, []any{300_000_00, planID}},
		{`UPDATE plan_asset_class_targets SET weight=1.0 WHERE plan_id=? AND asset_class='equity'`, []any{planID}},
		{`UPDATE plan_asset_class_targets SET weight=0 WHERE plan_id=? AND asset_class IN ('bond','cash')`, []any{planID}},
		{`UPDATE plan_region_targets SET weight_within_class=1.0
		 WHERE plan_id=? AND asset_class='equity' AND region='domestic'`, []any{planID}},
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(context.Background(), stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, instID := range instIDs {
		seedInstrumentMarketData(t, db, instID)
	}
	return planID, instIDs
}

func seedInstrumentMarketData(t *testing.T, db *sql.DB, assetKey string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	for _, p := range buildTwentyYearFixturePoints() {
		if _, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO market_asset_points (asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at)
			VALUES (?, 'none', 'adjusted_close', ?, ?, 'fixture', ?)`,
			assetKey, p.Date, p.Value, now); err != nil {
			t.Fatal(err)
		}
	}
}

func seedBondInstrumentForPlan(t *testing.T, db *sql.DB, planID string) string {
	t.Helper()
	snapRepo := repository.NewSnapshotRepo(db)
	instID := "ins_rbd_bond"
	now := time.Now().UnixMilli()
	if err := snapRepo.EnsureMarketAsset(context.Background(), repository.MarketAsset{
		AssetKey: instID, Symbol: "RBOND", Name: "测试债券基金",
		Market: "CN", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}
	snapID := "snap_" + instID
	if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
		ID: snapID, AssetKey: instID, PlanID: &planID,
		InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
		CompleteYearCount: 5, DailyObservationCount: 100, MonthlyReturnCount: 60,
		VolatilityMethod: "monthly_log_return_sample_stddev_annualized",
		MetricsVersion:   "monthly_log_return_v1", HistoryDepth: "five_plus_years",
		HistoricalCAGR: 0.04, ModeledAnnualReturn: 0.04, AnnualVolatility: 0.05, MaxDrawdown: 0.05,
		ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
		SourceMode: "akshare_historical", QualityStatus: "available",
		WarningsJSON: "[]", SourceHash: "fixture", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	seedInstrumentMarketData(t, db, instID)
	return instID
}

func TestAssetRefreshPOST(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	newAmounts := []int64{130_000_00, 85_000_00, 85_000_00}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{"asset_key": id, "current_amount_minor": newAmounts[i]}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": false,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh status=%d body=%s", resp.StatusCode, string(respBody))
	}
	env := decodeEnvelope(t, respBody)
	if env["data"].(map[string]any)["after_total_minor"].(float64) != float64(300_000_00) {
		t.Fatal("unexpected after total")
	}
}

func TestAssetRefreshSyncScaleAndAuditEvent(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	newAmounts := []int64{100_000_00, 100_000_00, 100_000_00}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{"asset_key": id, "current_amount_minor": newAmounts[i]}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": true,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh status=%d body=%s", resp.StatusCode, string(respBody))
	}
	env := decodeEnvelope(t, respBody)
	if env["data"].(map[string]any)["synced_scale"].(bool) != true {
		t.Fatal("expected synced_scale true")
	}

	var paramTotal int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT total_assets_minor FROM plan_parameters WHERE plan_id=?`, planID).Scan(&paramTotal); err != nil {
		t.Fatal(err)
	}
	if paramTotal != 300_000_00 {
		t.Fatalf("parameters total: got %d want 30000000", paramTotal)
	}

	var auditCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM asset_refresh_events WHERE plan_id=?`, planID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit event, got %d", auditCount)
	}
	var syncScale, configChanged int
	if err := db.QueryRowContext(context.Background(), `
		SELECT sync_scale, config_changed FROM asset_refresh_events WHERE plan_id=? LIMIT 1`, planID).
		Scan(&syncScale, &configChanged); err != nil {
		t.Fatal(err)
	}
	if syncScale != 1 || configChanged != 1 {
		t.Fatalf("audit flags sync=%d config=%d", syncScale, configChanged)
	}
}

func TestAssetRefreshAtomicRollbackOnSyncFailure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	paramsRepo := repository.NewParametersRepo(db)
	beforeParams, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	beforeScenario := ""
	if beforeParams.SelectedScenarioID != nil {
		beforeScenario = *beforeParams.SelectedScenarioID
	}

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	amounts := []int64{120_000_00, 90_000_00, 90_000_00}
	weights := []float64{0.3334, 0.3333, 0.3333}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{
			"asset_key":            id,
			"current_amount_minor": amounts[i],
			"weight_within_group":  weights[i],
			"sort_order":           i * 10,
		}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"scenario_id":             "scn_builtin_post_fire",
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": true,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected asset refresh to fail, body=%s", string(respBody))
	}
	assertErrorCode(t, respBody, "plan_weights_invalid")

	afterParams, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	afterScenario := ""
	if afterParams.SelectedScenarioID != nil {
		afterScenario = *afterParams.SelectedScenarioID
	}
	if afterScenario != beforeScenario {
		t.Fatalf("scenario changed after failed refresh: before=%q after=%q", beforeScenario, afterScenario)
	}

	var holdingCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM plan_holdings WHERE plan_id=?`, planID).Scan(&holdingCount); err != nil {
		t.Fatal(err)
	}
	if holdingCount != len(instIDs) {
		t.Fatalf("holdings count changed: got %d want %d", holdingCount, len(instIDs))
	}

	var disabled int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM plan_holdings WHERE plan_id=? AND enabled=0`, planID).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled != 0 {
		t.Fatalf("expected no disabled holdings after rollback, got %d", disabled)
	}

	var auditCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM asset_refresh_events WHERE plan_id=?`, planID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("expected no audit event after rollback, got %d", auditCount)
	}
}

func TestAssetRefreshAtomicScenarioAndStructure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	bondInstID := seedBondInstrumentForPlan(t, db, planID)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	equityAmounts := []int64{82_500_00, 41_250_00, 41_250_00}
	equityWeights := []float64{0.5, 0.25, 0.25}
	holdings := make([]map[string]any, 0, len(instIDs)+2)
	for i, id := range instIDs {
		holdings = append(holdings, map[string]any{
			"asset_key":            id,
			"current_amount_minor": equityAmounts[i],
			"weight_within_group":  equityWeights[i],
			"sort_order":           i * 10,
		})
	}
	holdings = append(
		holdings,
		map[string]any{
			"asset_key":            bondInstID,
			"asset_class":          "bond",
			"region":               "domestic",
			"current_amount_minor": 105_000_00,
			"weight_within_group":  1.0,
			"sort_order":           30,
		},
		map[string]any{
			"asset_key":            repository.SystemCashAssetKey,
			"asset_class":          "cash",
			"region":               "domestic",
			"current_amount_minor": 30_000_00,
			"weight_within_group":  1.0,
			"sort_order":           40,
		},
	)
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"scenario_id":             "scn_builtin_post_fire",
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": true,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh status=%d body=%s", resp.StatusCode, string(respBody))
	}

	paramsRepo := repository.NewParametersRepo(db)
	params, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if params.SelectedScenarioID == nil || *params.SelectedScenarioID != "scn_builtin_post_fire" {
		got := ""
		if params.SelectedScenarioID != nil {
			got = *params.SelectedScenarioID
		}
		t.Fatalf("expected scenario scn_builtin_post_fire, got %q", got)
	}

	var weight float64
	if err := db.QueryRowContext(context.Background(), `
		SELECT weight_within_group FROM plan_holdings
		WHERE plan_id=? AND asset_key=?`, planID, instIDs[0]).Scan(&weight); err != nil {
		t.Fatal(err)
	}
	if weight != 0.5 {
		t.Fatalf("expected first holding weight 0.5, got %v", weight)
	}

	var auditCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM asset_refresh_events WHERE plan_id=?`, planID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit event, got %d", auditCount)
	}
}

func TestAssetRefreshScenarioSwitchRejectsMismatchedHoldings(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	amounts := []int64{120_000_00, 90_000_00, 90_000_00}
	weights := []float64{0.3334, 0.3333, 0.3333}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{
			"asset_key":            id,
			"current_amount_minor": amounts[i],
			"weight_within_group":  weights[i],
			"sort_order":           i * 10,
		}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"scenario_id":             "scn_builtin_post_fire",
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": false,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected asset refresh to fail on scenario switch with equity-only holdings, body=%s", string(respBody))
	}
	assertErrorCode(t, respBody, "plan_weights_invalid")

	paramsRepo := repository.NewParametersRepo(db)
	params, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if params.SelectedScenarioID != nil && *params.SelectedScenarioID == "scn_builtin_post_fire" {
		t.Fatal("scenario must not change after failed refresh")
	}
}
