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

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func setupInstrumentIntegration(t *testing.T) (*httptest.Server, *sql.DB, *http.Client) {
	t.Helper()
	provider := mockProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client()
}

func importFixtureInstrument(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund", "code": "510300",
	})
	resp, err := client.Post(baseURL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	return env["data"].(map[string]any)["id"].(string)
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
		service.NewConfigHashService(
			repository.NewPlanRepo(db),
			repository.NewParametersRepo(db),
			repository.NewAllocationRepo(db),
			repository.NewHoldingsRepo(db),
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

func addEquityHolding(t *testing.T, db *sql.DB, client *http.Client, baseURL, planID, instrumentID string, version int) (holdingID string, newVersion int) {
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

	instID := importFixtureInstrument(t, client, srv.URL)

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

	instID = importFixtureInstrument(t, client, srv.URL)
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

func TestInstrumentRefreshThrottleIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	instID := importFixtureInstrument(t, client, srv.URL)
	oldFetched := time.Now().Add(-25 * time.Hour).UnixMilli()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE market_data_points SET fetched_at=? WHERE instrument_id=?`, oldFetched, instID); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/refresh", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first refresh status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	resp, err = client.Post(srv.URL+"/api/v1/instruments/"+instID+"/refresh", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("throttled refresh status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_refresh_throttled")
}

func TestHoldingSnapshotOnCreateIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	instID := importFixtureInstrument(t, client, srv.URL)
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
	instID := importFixtureInstrument(t, client, srv.URL)
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

func TestInstrumentDataStaleWarningIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	instID := importFixtureInstrument(t, client, srv.URL)
	staleDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `DELETE FROM market_data_points WHERE instrument_id=?`, instID); err != nil {
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
