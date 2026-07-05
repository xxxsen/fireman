//go:build integration

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

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func setupInstrumentIntegration(t *testing.T) (*httptest.Server, *sql.DB, *http.Client) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client()
}

// seedFixtureAsset seeds the 510300 market asset with fixture history and
// returns its asset key. Holdings reference market assets directly.
func seedFixtureAsset(t *testing.T, db *sql.DB) string {
	t.Helper()
	return seedAssetCode(t, db, "510300")
}

func seedAssetCode(t *testing.T, db *sql.DB, code string) string {
	t.Helper()
	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund:sh:" + code
	seed.Symbol = code
	seed.Name = "测试ETF" + code
	seedMarketAssetWithHistory(t, db, seed)
	return seed.AssetKey
}

func createPlanWithValuationDate(t *testing.T, db *sql.DB, valuationDate string) service.PlanDetail {
	t.Helper()
	svc := service.NewPlanService(
		db,
		repository.NewPlanRepo(db),
		repository.NewParametersRepo(db),
		repository.NewAllocationRepo(db),
		repository.NewScenarioRepo(db),
		repository.NewHoldingsRepo(db),
		repository.NewMarketAssetRepo(db),
		service.NewConfigHashService(
			repository.NewPlanRepo(db),
			repository.NewParametersRepo(db),
			repository.NewAllocationRepo(db),
			repository.NewHoldingsRepo(db),
			repository.NewReturnOverrideRepo(db),
		),
		marketdata.NewSnapshotService(
			repository.NewSnapshotRepo(db),
			repository.NewMarketAssetRepo(db),
		),
	)
	scn := "scn_builtin_near_fire"
	plan, err := svc.Create(context.Background(), service.CreatePlanRequest{
		Name: "集成测试计划", BaseCurrency: "CNY", ValuationDate: valuationDate,
		SelectedScenarioID: &scn,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}

func planConfigVersion(t *testing.T, db *sql.DB, planID string) int {
	t.Helper()
	plan, err := repository.NewPlanRepo(db).GetByID(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	return plan.ConfigVersion
}

func setEquityOnlyAllocation(t *testing.T, client *http.Client, baseURL, planID string, version int) int {
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allocation status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	_ = readBody(t, resp)
	return version + 1
}

func addEquityHolding(t *testing.T, db *sql.DB, client *http.Client, baseURL, planID, assetKey string,
	version int,
) (holdingID string, newVersion int) {
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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holdings status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	holdings := env["data"].(map[string]any)["holdings"].([]any)
	holding := holdings[0].(map[string]any)
	return holding["id"].(string), planConfigVersion(t, db, planID)
}

func saveEquityHoldingAmount(
	t *testing.T, db *sql.DB, client *http.Client, baseURL, planID, assetKey string,
	version int, amountMinor int64,
) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"asset_key": assetKey, "enabled": true,
				"asset_class": "equity", "region": "domestic",
				"weight_within_group": 1.0, "current_amount_minor": amountMinor, "sort_order": 1,
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/v1/plans/"+planID+"/holdings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("structural holdings save status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	_ = readBody(t, resp)
}

// TestHoldingClassificationUserOwnedIntegration verifies the td-independent
// rule that a holding's asset_class/region are user-chosen and survive a
// structural holdings save unchanged.
func TestHoldingClassificationUserOwnedIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	assetKey := seedFixtureAsset(t, db)

	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	_, version = addEquityHolding(t, db, client, srv.URL, plan.ID, assetKey, version)

	if ac, rg := holdingClassification(t, db, plan.ID, assetKey); ac != "equity" || rg != "domestic" {
		t.Fatalf("seed holding classification=%s/%s", ac, rg)
	}

	saveEquityHoldingAmount(t, db, client, srv.URL, plan.ID, assetKey, version, 9_000_000_00)
	if ac, rg := holdingClassification(t, db, plan.ID, assetKey); ac != "equity" || rg != "domestic" {
		t.Fatalf("structural save changed classification: %s/%s", ac, rg)
	}
}

func holdingClassification(t *testing.T, db *sql.DB, planID, assetKey string) (string, string) {
	t.Helper()
	var assetClass, region string
	err := db.QueryRowContext(context.Background(),
		`SELECT asset_class, region FROM plan_holdings WHERE plan_id=? AND asset_key=?`,
		planID, assetKey).Scan(&assetClass, &region)
	if err != nil {
		t.Fatal(err)
	}
	return assetClass, region
}

func TestHoldingSnapshotOnCreateIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	assetKey := seedFixtureAsset(t, db)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, _ := addEquityHolding(t, db, client, srv.URL, plan.ID, assetKey, version)

	resp, err := client.Get(srv.URL + "/api/v1/plans/" + plan.ID + "/holdings/" + holdingID + "/simulation-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("snapshot status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	snap := env["data"].(map[string]any)
	if got := snap["inclusion_date"].(string); got != valuationDate {
		t.Fatalf("expected inclusion_date=%s on create, got %s", valuationDate, got)
	}
}

func TestSyncHoldingSnapshotUsesSyncDateIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	syncDate := time.Now().Format("2006-01-02")
	assetKey := seedFixtureAsset(t, db)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, version := addEquityHolding(t, db, client, srv.URL, plan.ID, assetKey, version)

	body, _ := json.Marshal(map[string]any{"config_version": version})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+plan.ID+"/holdings/"+holdingID+"/sync-simulation-snapshot",
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	snap := env["data"].(map[string]any)
	if got := snap["inclusion_date"].(string); got != syncDate {
		t.Fatalf("expected inclusion_date=%s on sync, got %s (valuation_date=%s)", syncDate, got, valuationDate)
	}
	if got := snap["inclusion_date"].(string); got == valuationDate {
		t.Fatalf("sync must not reuse plan valuation_date %s", valuationDate)
	}
}

func TestSyncHoldingSnapshotRollbackOnHoldingUpdateFailureIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	assetKey := seedFixtureAsset(t, db)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, version := addEquityHolding(t, db, client, srv.URL, plan.ID, assetKey, version)

	var oldSnapshotID string
	if err := db.QueryRowContext(context.Background(),
		`SELECT simulation_snapshot_id FROM plan_holdings WHERE id=?`, holdingID).Scan(&oldSnapshotID); err != nil {
		t.Fatal(err)
	}

	snapsBefore := countTable(t, db, "market_asset_simulation_snapshots")
	if _, err := db.ExecContext(context.Background(), `
		CREATE TRIGGER IF NOT EXISTS test_fail_holding_snapshot_update
		BEFORE UPDATE OF simulation_snapshot_id ON plan_holdings
		WHEN NEW.simulation_snapshot_id != OLD.simulation_snapshot_id
		BEGIN
			SELECT RAISE(ABORT, 'injected holding update failure');
		END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS test_fail_holding_snapshot_update`)
	})

	body, _ := json.Marshal(map[string]any{"config_version": version})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+plan.ID+"/holdings/"+holdingID+"/sync-simulation-snapshot",
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected sync to fail when holding update is injected, got 200 body=%s", readBody(t, resp))
	}

	if got := countTable(t, db, "market_asset_simulation_snapshots"); got != snapsBefore {
		t.Fatalf("expected no orphan snapshot after rollback, snapshots before=%d after=%d", snapsBefore, got)
	}
	var currentSnapshotID string
	if err := db.QueryRowContext(context.Background(),
		`SELECT simulation_snapshot_id FROM plan_holdings WHERE id=?`, holdingID).Scan(&currentSnapshotID); err != nil {
		t.Fatal(err)
	}
	if currentSnapshotID != oldSnapshotID {
		t.Fatalf("holding snapshot_id changed despite rollback: old=%s new=%s", oldSnapshotID, currentSnapshotID)
	}
}

// TestLazyHoldingThenReadinessSyncIntegration covers the one-click
// missing-history flow: a holding saved without local history has an empty
// snapshot id, readiness reports not_ready with the missing asset, and once
// history arrives EnsureHoldingSnapshots heals the snapshot on simulate.
func TestLazyHoldingThenReadinessSyncIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund:sh:512999"
	seed.Symbol = "512999"
	seed.Points = nil // directory row only, no history yet
	seedMarketAssetWithHistory(t, db, seed)

	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, _ := addEquityHolding(t, db, client, srv.URL, plan.ID, seed.AssetKey, version)
	if _, err := db.ExecContext(context.Background(),
		`UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, 10_000_000_00, plan.ID); err != nil {
		t.Fatal(err)
	}

	var snapID string
	if err := db.QueryRowContext(context.Background(),
		`SELECT simulation_snapshot_id FROM plan_holdings WHERE id=?`, holdingID).Scan(&snapID); err != nil {
		t.Fatal(err)
	}
	if snapID != "" {
		t.Fatalf("expected lazy holding with empty snapshot id, got %q", snapID)
	}

	resp, err := client.Get(srv.URL + "/api/v1/plans/" + plan.ID + "/simulation-readiness")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	readiness := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if readiness["ready"].(bool) {
		t.Fatal("plan with missing history must not be ready")
	}
	missing := readiness["missing_history"].([]any)
	if len(missing) != 1 {
		t.Fatalf("missing_history=%v want 1 item", missing)
	}

	// Creating a simulation is blocked while history is missing.
	simBody, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "1"})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+plan.ID+"/simulations", "application/json", bytes.NewReader(simBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("simulation must be blocked by readiness gate, body=%s", readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "market_asset_history_missing")

	// One-click sync creates a history task for the missing asset.
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+plan.ID+"/sync-missing-asset-history", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync-missing status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	syncOut := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if created := syncOut["created"].([]any); len(created) != 1 {
		t.Fatalf("expected 1 created sync task, got %v", syncOut)
	}

	// Simulate the worker finishing: history lands locally.
	seed.Points = buildFixturePoints()
	seedMarketAssetWithHistory(t, db, seed)

	// Readiness turns ready once history is available.
	resp, err = client.Get(srv.URL + "/api/v1/plans/" + plan.ID + "/simulation-readiness")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	readiness = decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if !readiness["ready"].(bool) {
		t.Fatalf("plan must be ready after history sync: %+v", readiness)
	}

	// Creating a simulation now heals the lazy snapshot before freezing input.
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+plan.ID+"/simulations", "application/json", bytes.NewReader(simBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create simulation status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if err := db.QueryRowContext(context.Background(),
		`SELECT simulation_snapshot_id FROM plan_holdings WHERE id=?`, holdingID).Scan(&snapID); err != nil {
		t.Fatal(err)
	}
	if snapID == "" {
		t.Fatal("expected lazy snapshot healed by simulation readiness gate")
	}
}
