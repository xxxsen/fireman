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

// importFixtureInstrument seeds the 510300 market asset with fixture history
// and imports it synchronously (td/078: import projects already-synced points,
// no fetch job is involved).
func importFixtureInstrument(t *testing.T, db *sql.DB, client *http.Client, baseURL string) string {
	t.Helper()
	return importInstrumentCode(t, db, client, baseURL, "510300")
}

func importInstrumentCode(t *testing.T, db *sql.DB, client *http.Client, baseURL, code string) string {
	t.Helper()
	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund:sh:" + code
	seed.Symbol = code
	seed.Name = "测试ETF" + code
	seedMarketAssetWithHistory(t, db, seed)
	inst := importMarketAsset(t, client, baseURL, seed.AssetKey)
	return inst["id"].(string)
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
		repository.NewInstrumentRepo(db),
		service.NewConfigHashService(
			repository.NewPlanRepo(db),
			repository.NewParametersRepo(db),
			repository.NewAllocationRepo(db),
			repository.NewHoldingsRepo(db),
			repository.NewReturnOverrideRepo(db),
		),
		marketdata.NewSnapshotService(
			repository.NewSnapshotRepo(db),
			repository.NewInstrumentRepo(db),
			repository.NewMarketDataRepo(db),
		),
		repository.NewMarketDataRepo(db),
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

func addEquityHolding(t *testing.T, db *sql.DB, client *http.Client, baseURL, planID, instrumentID string,
	version int,
) (holdingID string, newVersion int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"instrument_id": instrumentID, "enabled": true,
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

func TestInstrumentDeleteIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	instID := importFixtureInstrument(t, db, client, srv.URL)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/instruments/"+instID, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/instruments/system_fx_usdcny", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete system fx status=%d", resp.StatusCode)
	}
	assertErrorCode(t, readBody(t, resp), "instrument_not_deletable")

	instID = importFixtureInstrument(t, db, client, srv.URL)
	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	_, _ = addEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/instruments/"+instID, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("delete in-use status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_in_use")
}

// TestInstrumentClassificationFreezeIntegration verifies that editing the
// library classification does not retro-actively change an existing plan's frozen
// holding classification, even when that plan saves a structural holdings update.
func TestInstrumentClassificationFreezeIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	instID := importFixtureInstrument(t, db, client, srv.URL)

	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	_, version = addEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

	if ac, rg := holdingClassification(t, db, plan.ID, instID); ac != "equity" || rg != "domestic" {
		t.Fatalf("seed holding classification=%s/%s", ac, rg)
	}
	updatedAt := instrumentUpdatedAt(t, db, instID)

	resp := patchClassification(t, client, srv.URL, instID, map[string]any{
		"asset_class": "bond", "region": "foreign", "expected_updated_at": updatedAt,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("classification patch status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	data := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if cnt, _ := data["referencing_plan_count"].(float64); cnt < 1 {
		t.Fatalf("referencing_plan_count=%v want >=1", data["referencing_plan_count"])
	}

	// Library now bond/foreign, but the plan holding stays equity/domestic.
	if ac, rg := holdingClassification(t, db, plan.ID, instID); ac != "equity" || rg != "domestic" {
		t.Fatalf("plan holding wrongly adopted library classification: %s/%s", ac, rg)
	}

	// A structural holdings save (rebuild) must keep the frozen classification; a
	// broken freeze would flip it to bond/foreign and fail equity-only validation.
	saveEquityHoldingAmount(t, db, client, srv.URL, plan.ID, instID, version, 9_000_000_00)
	if ac, rg := holdingClassification(t, db, plan.ID, instID); ac != "equity" || rg != "domestic" {
		t.Fatalf("structural save overwrote frozen classification: %s/%s", ac, rg)
	}
}

func holdingClassification(t *testing.T, db *sql.DB, planID, instrumentID string) (string, string) {
	t.Helper()
	var assetClass, region string
	err := db.QueryRowContext(context.Background(),
		`SELECT asset_class, region FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, instrumentID).Scan(&assetClass, &region)
	if err != nil {
		t.Fatal(err)
	}
	return assetClass, region
}

func instrumentUpdatedAt(t *testing.T, db *sql.DB, instrumentID string) int64 {
	t.Helper()
	var updatedAt int64
	err := db.QueryRowContext(context.Background(),
		`SELECT updated_at FROM instruments WHERE id=?`, instrumentID).Scan(&updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return updatedAt
}

func saveEquityHoldingAmount(
	t *testing.T, db *sql.DB, client *http.Client, baseURL, planID, instrumentID string,
	version int, amountMinor int64,
) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"instrument_id": instrumentID, "enabled": true,
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

func countInstrumentRows(t *testing.T, db *sql.DB, table, instrumentID string) int {
	t.Helper()
	var n int
	//nolint:gosec // table is a fixed test-controlled identifier, not user input.
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+table+" WHERE instrument_id=?", instrumentID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func getInstrumentFromList(t *testing.T, client *http.Client, baseURL, instrumentID string) map[string]any {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/v1/instruments")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	insts := env["data"].(map[string]any)["instruments"].([]any)
	for _, raw := range insts {
		m := raw.(map[string]any)
		if m["id"] == instrumentID {
			return m
		}
	}
	t.Fatalf("instrument %s not found in list", instrumentID)
	return nil
}

// TestInstrumentImportProjectionFailureRollsBackIntegration covers transaction
// atomicity of the synchronous market-asset import: if the projection upsert
// SQL fails, the whole import rolls back and no instrument row survives.
func TestInstrumentImportProjectionFailureRollsBackIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	seedMarketAssetWithHistory(t, db, cnETFAssetSeed())

	if _, err := db.ExecContext(context.Background(), `
		CREATE TRIGGER test_fail_library_metrics_insert
		BEFORE INSERT ON instrument_library_metrics
		BEGIN
			SELECT RAISE(ABORT, 'injected projection insert failure');
		END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS test_fail_library_metrics_insert`)
	})

	body, _ := json.Marshal(map[string]any{"asset_key": "cn:cn_exchange_fund:sh:510300"})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected import to fail when projection insert aborts, got 200 body=%s", readBody(t, resp))
	}

	var instCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM instruments WHERE code='510300'`).Scan(&instCount); err != nil {
		t.Fatal(err)
	}
	if instCount != 0 {
		t.Fatalf("instrument row must roll back on projection failure, got %d", instCount)
	}
	var pointCount int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM market_data_points
		WHERE instrument_id NOT LIKE 'system_%'`).Scan(&pointCount); err != nil {
		t.Fatal(err)
	}
	if pointCount != 0 {
		t.Fatalf("market_data_points must roll back on projection failure, got %d", pointCount)
	}
}

func TestHoldingSnapshotOnCreateIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	instID := importFixtureInstrument(t, db, client, srv.URL)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, _ := addEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

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
	instID := importFixtureInstrument(t, db, client, srv.URL)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, version := addEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

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
	instID := importFixtureInstrument(t, db, client, srv.URL)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, version := addEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

	var oldSnapshotID string
	if err := db.QueryRowContext(context.Background(),
		`SELECT simulation_snapshot_id FROM plan_holdings WHERE id=?`, holdingID).Scan(&oldSnapshotID); err != nil {
		t.Fatal(err)
	}

	snapsBefore := countTable(t, db, "instrument_simulation_snapshots")
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

	if got := countTable(t, db, "instrument_simulation_snapshots"); got != snapsBefore {
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

func TestInstrumentDetailReturnsEmptyArraysIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	instID := importFixtureInstrument(t, db, client, srv.URL)

	resp, err := client.Get(srv.URL + "/api/v1/instruments/" + instID + "/detail")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	data := env["data"].(map[string]any)
	for _, key := range []string{"historical_snapshots", "referencing_plans", "annual_returns"} {
		if data[key] == nil {
			t.Fatalf("%s is null", key)
		}
	}
	window, ok := data["simulation_window"].(map[string]any)
	if !ok {
		t.Fatal("simulation_window missing")
	}
	if window["excluded_years"] == nil {
		t.Fatal("excluded_years is null")
	}
}

func TestInstrumentDataSourceNameIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	instID := importFixtureInstrument(t, db, client, srv.URL)

	resp, err := client.Get(srv.URL + "/api/v1/instruments/" + instID)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, readBody(t, resp))
	inst := env["data"].(map[string]any)
	if inst["data_source_name"] != "test_fixture" {
		t.Fatalf("data_source_name=%v want test_fixture", inst["data_source_name"])
	}
	if inst["point_type"] != "adjusted_close" {
		t.Fatalf("point_type=%v", inst["point_type"])
	}
}

func TestInstrumentDataStaleWarningIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	instID := importFixtureInstrument(t, db, client, srv.URL)
	staleDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `DELETE FROM market_data_points WHERE instrument_id=?`,
		instID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO market_data_points (instrument_id, trade_date, value, point_type, source_name, fetched_at)
		VALUES (?, ?, 100.0, 'adjusted_close', 'stale_fixture', ?)`,
		instID, staleDate, now); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Get(srv.URL + "/api/v1/instruments/" + instID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	inst := env["data"].(map[string]any)
	if inst["data_stale"] != true {
		t.Fatalf("expected data_stale=true, got %+v", inst)
	}
	if inst["stale_warning"] != "数据可能过期" {
		t.Fatalf("expected stale_warning, got %+v", inst["stale_warning"])
	}
}

type holdingSnapshotFields struct {
	CompleteYearCount int
	HistoryDepth      string
	MetricsVersion    string
	Warnings          []string
}

func getPlanHoldingsSnapshotFields(t *testing.T, client *http.Client, baseURL, planID string) holdingSnapshotFields {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/v1/plans/" + planID + "/holdings")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holdings status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	holdings := env["data"].(map[string]any)["holdings"].([]any)
	if len(holdings) == 0 {
		t.Fatal("expected at least one holding")
	}
	h := holdings[0].(map[string]any)
	var warnings []string
	if raw, ok := h["snapshot_warnings"]; ok && raw != nil {
		for _, w := range raw.([]any) {
			warnings = append(warnings, w.(string))
		}
	}
	return holdingSnapshotFields{
		CompleteYearCount: int(h["snapshot_complete_year_count"].(float64)),
		HistoryDepth:      h["snapshot_history_depth"].(string),
		MetricsVersion:    h["snapshot_metrics_version"].(string),
		Warnings:          warnings,
	}
}

func TestHistoricalSnapshotDTOContractIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	instID := importFixtureInstrument(t, db, client, srv.URL)
	plan := createPlanWithValuationDate(t, db, valuationDate)
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, version := addEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

	var snapshotID string
	if err := db.QueryRowContext(context.Background(),
		`SELECT simulation_snapshot_id FROM plan_holdings WHERE id=?`, holdingID).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	warnings := `["仅有 1 个完整自然年度，收益与风险估计的不确定性较高"]`
	if _, err := db.ExecContext(context.Background(),
		`UPDATE instrument_simulation_snapshots SET warnings_json=? WHERE id=?`, warnings, snapshotID); err != nil {
		t.Fatal(err)
	}

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

	resp, err = client.Get(srv.URL + "/api/v1/instruments/" + instID + "/detail")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	data := env["data"].(map[string]any)
	snapsRaw, ok := data["historical_snapshots"].([]any)
	if !ok || len(snapsRaw) == 0 {
		t.Fatalf("expected non-empty historical_snapshots, got %#v", data["historical_snapshots"])
	}

	required := []string{
		"id", "plan_id", "inclusion_date", "complete_year_count", "monthly_return_count",
		"history_depth", "metrics_version", "warnings", "created_at",
	}
	foundWarnings := false
	for _, snapAny := range snapsRaw {
		snap, ok := snapAny.(map[string]any)
		if !ok {
			t.Fatalf("snapshot is not an object: %#v", snapAny)
		}
		for _, key := range required {
			if _, exists := snap[key]; !exists {
				t.Fatalf("historical_snapshots missing field %q: %+v", key, snap)
			}
		}
		if snap["warnings"] == nil {
			t.Fatalf("warnings must not be null: %+v", snap)
		}
		warns, ok := snap["warnings"].([]any)
		if !ok {
			t.Fatalf("warnings must be array, got %T", snap["warnings"])
		}
		if len(warns) > 0 {
			foundWarnings = true
			if _, ok := warns[0].(string); !ok {
				t.Fatalf("warnings items must be strings, got %T", warns[0])
			}
		}
	}
	if !foundWarnings {
		t.Fatal("expected at least one snapshot with non-empty warnings array")
	}
}
