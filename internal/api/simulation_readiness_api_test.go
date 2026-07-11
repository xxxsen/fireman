package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/testutil"
)

// buildAnomalousFixturePoints produces complete calendar years whose
// month-end values oscillate ×2 / ÷2. Annual returns stay ~0 (CAGR valid)
// but monthly volatility annualizes to ~2.4, beyond the 2.0 admission bound,
// so the snapshot fails with an invalid volatility metric — the same shape
// as a money-market fund fetched under an exchange-traded identity.
func buildAnomalousFixturePoints() []marketdata.HistoricalPoint {
	var out []marketdata.HistoricalPoint
	value := 100.0
	for year := 2018; year <= 2024; year++ {
		out = append(out, marketdata.HistoricalPoint{
			Date: fmt.Sprintf("%d-12-31", year-1), Value: value,
		})
		for month := 1; month <= 12; month++ {
			factor := 2.0
			if month%2 == 0 {
				factor = 0.5
			}
			step := math.Pow(factor, 1.0/11.0)
			for day := 1; day <= 11; day++ {
				value *= step
				out = append(out, marketdata.HistoricalPoint{
					Date: formatFixtureDate(year, month, day), Value: value,
				})
			}
		}
	}
	return out
}

// buildShortFixturePoints yields history with zero complete years (a few
// months only), which blocks simulation without any metric anomaly.
func buildShortFixturePoints() []marketdata.HistoricalPoint {
	var out []marketdata.HistoricalPoint
	value := 100.0
	for month := 1; month <= 3; month++ {
		for day := 1; day <= 11; day++ {
			value *= 1.0004
			out = append(out, marketdata.HistoricalPoint{
				Date: formatFixtureDate(2024, month, day), Value: value,
			})
		}
	}
	return out
}

// insertHoldingRow stores one enabled lazy holding (empty snapshot id)
// directly, bypassing the holdings API so tests can pin arbitrary states.
func insertHoldingRow(t *testing.T, db *sql.DB, planID, assetKey string) {
	t.Helper()
	id := "hold_test_" + fmt.Sprintf("%d", time.Now().UnixNano())
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plan_holdings (
			id, plan_id, asset_key, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?, ?, ?, 1, 'equity', 'domestic', 1.0, 1000000, '', 1, ?, ?)`,
		id, planID, assetKey, now, now); err != nil {
		t.Fatal(err)
	}
}

func getReadiness(t *testing.T, client *http.Client, baseURL, planID string) map[string]any {
	t.Helper()
	resp, body := getJSON(t, client, baseURL+"/api/v1/plans/"+planID+"/simulation-readiness")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d body=%s", resp.StatusCode, body)
	}
	return decodeEnvelope(t, body)["data"].(map[string]any)
}

func postSyncMissing(t *testing.T, client *http.Client, baseURL, planID string) map[string]any {
	t.Helper()
	resp, err := client.Post(
		baseURL+"/api/v1/plans/"+planID+"/sync-missing-asset-history", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync-missing status=%d body=%s", resp.StatusCode, body)
	}
	return decodeEnvelope(t, body)["data"].(map[string]any)
}

func TestSimulationReadinessRejectsForeignCash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	insertHoldingRow(t, db, plan.ID, "SYS|cash||USD")
	router := NewRouter(context.Background(), Deps{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plans/"+plan.ID+"/simulation-readiness", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("readiness status=%d body=%s", w.Code, w.Body.String())
	}
	readiness := decodeEnvelope(t, w.Body.Bytes())["data"].(map[string]any)
	if readiness["ready"].(bool) {
		t.Fatal("foreign cash must block simulation readiness")
	}
	blocking := readiness["blocking_assets"].([]any)
	if len(blocking) != 1 || blocking[0].(map[string]any)["reason"] != "foreign_cash_not_supported" {
		t.Fatalf("unexpected blocking assets: %+v", blocking)
	}
}

// TestSimulationReadiness_IdentityConflict reproduces the 150015 case: the
// plan holds the exchange-fund identity whose synced history is anomalous
// while a mutual-fund identity with the same code and name exists. Readiness
// must flag the identity conflict with the mutual-fund candidate, and
// one-click sync must block instead of creating a useless task.
func TestSimulationReadiness_IdentityConflict(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	exchange := marketAssetSeed{
		AssetKey: "CN|cn_exchange_fund|sz|150015", Market: "CN",
		InstrumentType: "cn_exchange_fund", RegionCode: "sz", Symbol: "150015",
		Name: "银河银富货币B", InstrumentKind: "lof", Currency: "CNY",
		PointType: "adjusted_close", Points: buildAnomalousFixturePoints(),
	}
	seedMarketAssetWithHistory(t, db, exchange)
	mutual := marketAssetSeed{
		AssetKey: "CN|cn_mutual_fund||150015", Market: "CN",
		InstrumentType: "cn_mutual_fund", Symbol: "150015",
		Name: "银河银富货币B", InstrumentKind: "货币型-普通货币", Currency: "CNY",
		PointType: "nav", Points: nil,
	}
	seedMarketAssetWithHistory(t, db, mutual)

	plan := createTestPlan(t, db)
	insertHoldingRow(t, db, plan.ID, exchange.AssetKey)

	readiness := getReadiness(t, client, srv.URL, plan.ID)
	if readiness["ready"].(bool) {
		t.Fatal("anomalous holding must not be ready")
	}
	blocking := readiness["blocking_assets"].([]any)
	if len(blocking) != 1 {
		t.Fatalf("blocking_assets=%v want 1 item", blocking)
	}
	item := blocking[0].(map[string]any)
	if item["reason"] != "asset_identity_conflict" {
		t.Fatalf("reason=%v want asset_identity_conflict", item["reason"])
	}
	candidates := item["candidate_asset_keys"].([]any)
	if len(candidates) != 1 || candidates[0] != mutual.AssetKey {
		t.Fatalf("candidate_asset_keys=%v want [%s]", candidates, mutual.AssetKey)
	}
	if msg, _ := item["message"].(string); msg == "" {
		t.Fatal("identity conflict must carry a user-facing message")
	}

	// One-click sync must not create a task for the conflicted asset.
	syncOut := postSyncMissing(t, client, srv.URL, plan.ID)
	if created := syncOut["created"].([]any); len(created) != 0 {
		t.Fatalf("created=%v want empty", created)
	}
	if existing := syncOut["existing"].([]any); len(existing) != 0 {
		t.Fatalf("existing=%v want empty", existing)
	}
	blocked := syncOut["blocked"].([]any)
	if len(blocked) != 1 {
		t.Fatalf("blocked=%v want 1 item", blocked)
	}
	be := blocked[0].(map[string]any)
	if be["reason"] != "asset_identity_conflict" {
		t.Fatalf("blocked reason=%v want asset_identity_conflict", be["reason"])
	}
	var taskCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM worker_tasks WHERE type='asset_history_sync'`).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("worker_tasks count=%d want 0 (blocked asset must not sync)", taskCount)
	}
}

// TestSimulationReadiness_ProviderDataAnomalyWithoutCandidate verifies the
// anomaly reason when no better identity exists for the code.
func TestSimulationReadiness_ProviderDataAnomalyWithoutCandidate(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	seed := cnETFAssetSeed()
	seed.AssetKey = "CN|cn_exchange_fund|sh|561111"
	seed.Symbol = "561111"
	seed.Name = "异常波动ETF"
	seed.Points = buildAnomalousFixturePoints()
	seedMarketAssetWithHistory(t, db, seed)

	plan := createTestPlan(t, db)
	insertHoldingRow(t, db, plan.ID, seed.AssetKey)

	readiness := getReadiness(t, client, srv.URL, plan.ID)
	item := readiness["blocking_assets"].([]any)[0].(map[string]any)
	if item["reason"] != "provider_data_anomaly" {
		t.Fatalf("reason=%v want provider_data_anomaly", item["reason"])
	}
	if _, hasCand := item["candidate_asset_keys"]; hasCand {
		t.Fatalf("no candidates expected, got %v", item["candidate_asset_keys"])
	}

	syncOut := postSyncMissing(t, client, srv.URL, plan.ID)
	blocked := syncOut["blocked"].([]any)
	if len(blocked) != 1 || blocked[0].(map[string]any)["reason"] != "provider_data_anomaly" {
		t.Fatalf("blocked=%v want provider_data_anomaly entry", blocked)
	}
}

// TestSimulationReadiness_InsufficientHistoryIsNotMissing separates "synced
// but too short to simulate" from "history missing": no task is created and
// the reason is simulation_insufficient_history.
func TestSimulationReadiness_InsufficientHistoryIsNotMissing(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	seed := cnETFAssetSeed()
	seed.AssetKey = "CN|cn_exchange_fund|sh|562222"
	seed.Symbol = "562222"
	seed.Name = "短历史ETF"
	seed.Points = buildShortFixturePoints()
	seedMarketAssetWithHistory(t, db, seed)

	plan := createTestPlan(t, db)
	insertHoldingRow(t, db, plan.ID, seed.AssetKey)

	readiness := getReadiness(t, client, srv.URL, plan.ID)
	item := readiness["blocking_assets"].([]any)[0].(map[string]any)
	if item["reason"] != "simulation_insufficient_history" {
		t.Fatalf("reason=%v want simulation_insufficient_history", item["reason"])
	}

	syncOut := postSyncMissing(t, client, srv.URL, plan.ID)
	if blocked := syncOut["blocked"].([]any); len(blocked) != 1 {
		t.Fatalf("blocked=%v want 1 item", blocked)
	}
	if created := syncOut["created"].([]any); len(created) != 0 {
		t.Fatalf("created=%v want empty", created)
	}
}

// TestSimulationReadiness_MissingThenRunning covers the true missing-history
// path: sync creates a task, after which readiness reports the in-flight
// task and a second sync reuses it instead of duplicating.
func TestSimulationReadiness_MissingThenRunning(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	seed := cnETFAssetSeed()
	seed.AssetKey = "CN|cn_exchange_fund|sh|563333"
	seed.Symbol = "563333"
	seed.Name = "缺历史ETF"
	seed.Points = nil
	seedMarketAssetWithHistory(t, db, seed)

	plan := createTestPlan(t, db)
	insertHoldingRow(t, db, plan.ID, seed.AssetKey)

	readiness := getReadiness(t, client, srv.URL, plan.ID)
	item := readiness["blocking_assets"].([]any)[0].(map[string]any)
	if item["reason"] != "history_missing" {
		t.Fatalf("reason=%v want history_missing", item["reason"])
	}
	if active := readiness["active_tasks"].([]any); len(active) != 0 {
		t.Fatalf("active_tasks=%v want empty before sync", active)
	}

	syncOut := postSyncMissing(t, client, srv.URL, plan.ID)
	created := syncOut["created"].([]any)
	if len(created) != 1 {
		t.Fatalf("created=%v want 1 item", created)
	}
	if blocked := syncOut["blocked"].([]any); len(blocked) != 0 {
		t.Fatalf("blocked=%v want empty", blocked)
	}

	// With the task pending, readiness reports history_sync_running plus the
	// active task, and a repeat sync reuses instead of creating.
	readiness = getReadiness(t, client, srv.URL, plan.ID)
	item = readiness["blocking_assets"].([]any)[0].(map[string]any)
	if item["reason"] != "history_sync_running" {
		t.Fatalf("reason=%v want history_sync_running", item["reason"])
	}
	if active := readiness["active_tasks"].([]any); len(active) != 1 {
		t.Fatalf("active_tasks=%v want 1 item", active)
	}
	syncOut = postSyncMissing(t, client, srv.URL, plan.ID)
	if created := syncOut["created"].([]any); len(created) != 0 {
		t.Fatalf("second sync created=%v want empty", created)
	}
	if existing := syncOut["existing"].([]any); len(existing) != 1 {
		t.Fatalf("second sync existing=%v want 1 item", existing)
	}
}

// TestSyncMissingHistory_ReadyAssetCreatesNothing: an asset whose snapshot
// builds fine is reported ready and never gets a new task.
func TestSyncMissingHistory_ReadyAssetCreatesNothing(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	seed := cnETFAssetSeed()
	seed.AssetKey = "CN|cn_exchange_fund|sh|564444"
	seed.Symbol = "564444"
	seed.Name = "正常ETF"
	seedMarketAssetWithHistory(t, db, seed)

	plan := createTestPlan(t, db)
	insertHoldingRow(t, db, plan.ID, seed.AssetKey)

	readiness := getReadiness(t, client, srv.URL, plan.ID)
	if !readiness["ready"].(bool) {
		t.Fatalf("expected ready, got %+v", readiness)
	}

	syncOut := postSyncMissing(t, client, srv.URL, plan.ID)
	if ready := syncOut["ready"].([]any); len(ready) != 1 {
		t.Fatalf("ready=%v want 1 item", ready)
	}
	if created := syncOut["created"].([]any); len(created) != 0 {
		t.Fatalf("created=%v want empty", created)
	}
	var taskCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM worker_tasks WHERE type='asset_history_sync'`).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 {
		t.Fatalf("worker_tasks count=%d want 0", taskCount)
	}
}

// TestHoldingsReplace_DoesNotReuseSnapshotAcrossAssetKeys: replacing a
// holding's asset with a different asset_key must not carry the old
// simulation snapshot over to the new asset.
func TestHoldingsReplace_DoesNotReuseSnapshotAcrossAssetKeys(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	first := cnETFAssetSeed()
	first.AssetKey = "CN|cn_exchange_fund|sh|565555"
	first.Symbol = "565555"
	first.Name = "原持仓ETF"
	seedMarketAssetWithHistory(t, db, first)
	second := cnETFAssetSeed()
	second.AssetKey = "CN|cn_mutual_fund||007777"
	second.InstrumentType = "cn_mutual_fund"
	second.RegionCode = ""
	second.Symbol = "007777"
	second.Name = "替换后场外基金"
	second.PointType = "nav"
	seedMarketAssetWithHistory(t, db, second)

	plan := createTestPlan(t, db)
	version := putEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)

	oldSnapID := putSingleEquityHolding(t, client, srv.URL, plan.ID, first.AssetKey, version)
	if oldSnapID == "" {
		t.Fatal("expected non-empty snapshot for the original asset")
	}

	newSnapID := putSingleEquityHolding(t, client, srv.URL, plan.ID, second.AssetKey, version+1)
	if newSnapID == "" {
		t.Fatal("expected non-empty snapshot for the replacement asset")
	}
	if newSnapID == oldSnapID {
		t.Fatalf("replacement holding reused snapshot %s of the old asset", oldSnapID)
	}

	// Replacing with a history-less asset must save lazily, not reuse.
	lazy := cnETFAssetSeed()
	lazy.AssetKey = "CN|cn_mutual_fund||008888"
	lazy.InstrumentType = "cn_mutual_fund"
	lazy.RegionCode = ""
	lazy.Symbol = "008888"
	lazy.Name = "缺历史场外基金"
	lazy.Points = nil
	seedMarketAssetWithHistory(t, db, lazy)
	lazySnapID := putSingleEquityHolding(t, client, srv.URL, plan.ID, lazy.AssetKey, version+2)
	if lazySnapID != "" {
		t.Fatalf("history-less replacement must be lazy, got snapshot %q", lazySnapID)
	}
}

func putEquityOnlyAllocation(t *testing.T, client *http.Client, baseURL, planID string, version int) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 1.0},
			{"asset_class": "bond", "weight": 0.0},
			{"asset_class": "cash", "weight": 0.0},
		},
		"region_targets": []map[string]any{
			{"asset_class": "equity", "region": "domestic", "weight_within_class": 1.0},
			{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.0},
			{"asset_class": "bond", "region": "domestic", "weight_within_class": 1.0},
			{"asset_class": "bond", "region": "foreign", "weight_within_class": 0.0},
			{"asset_class": "cash", "region": "domestic", "weight_within_class": 1.0},
			{"asset_class": "cash", "region": "foreign", "weight_within_class": 0.0},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/v1/plans/"+planID+"/allocation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allocation status=%d body=%s", resp.StatusCode, respBody)
	}
	return version + 1
}

// putSingleEquityHolding replaces the plan's holdings with one enabled
// equity/domestic row and returns its simulation_snapshot_id.
func putSingleEquityHolding(
	t *testing.T, client *http.Client, baseURL, planID, assetKey string, version int,
) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"asset_key": assetKey, "enabled": true,
				"asset_class": "equity", "region": "domestic",
				"weight_within_group": 1.0, "current_amount_minor": 10_000_000_00, "sort_order": 1,
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/v1/plans/"+planID+"/holdings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	respBody := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holdings status=%d body=%s", resp.StatusCode, respBody)
	}
	holdings := decodeEnvelope(t, respBody)["data"].(map[string]any)["holdings"].([]any)
	if len(holdings) != 1 {
		t.Fatalf("holdings=%v want 1 row", holdings)
	}
	snapID, _ := holdings[0].(map[string]any)["simulation_snapshot_id"].(string)
	return snapID
}
