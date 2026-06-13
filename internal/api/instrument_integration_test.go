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

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func startInstrumentFetchWorker(t *testing.T, db *sql.DB, providerURL string) context.CancelFunc {
	t.Helper()
	fetchProvider := marketdata.NewProviderClient(providerURL).FetchClient()
	fetchRunner := jobs.NewInstrumentFetchRunner(
		db,
		repository.NewJobRepo(db),
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
		repository.NewAnnualReturnsRepo(db),
		fetchProvider,
	)
	worker := jobs.NewWorker(
		db, repository.NewJobRepo(db), repository.NewSimulationRepo(db),
		jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db)),
		nil, fetchRunner, jobs.NewEventHub(), nil, nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	go worker.Start(ctx, 1)
	t.Cleanup(cancel)
	return cancel
}

func waitForInstrumentActive(t *testing.T, client *http.Client, baseURL, instrumentID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/v1/instruments/" + instrumentID)
		if err != nil {
			t.Fatal(err)
		}
		inst := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
		if inst["status"] == "active" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("instrument %s did not become active", instrumentID)
}

func setupInstrumentIntegration(t *testing.T) (*httptest.Server, *sql.DB, *http.Client) {
	t.Helper()
	provider := mockProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client()
}

func importFixtureInstrument(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	id := resolveAndImportAsync(t, client, baseURL, "CN", "cn_exchange_fund", "510300")
	waitForInstrumentActive(t, client, baseURL, id)
	return id
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

	body, _ := json.Marshal(map[string]any{"force": true})
	resp, err = client.Post(srv.URL+"/api/v1/instruments/"+instID+"/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force refresh status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestInstrumentForceRefreshReplacesStaleHistoryIntegration(t *testing.T) {
	fetchCount := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			_ = json.NewEncoder(w).Encode(marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: "sh510300", ProviderSymbol: "sh510300",
						Name: "沪深300ETF", Exchange: "SH", InstrumentKind: "etf",
					},
				},
			})
			return
		case "/v1/instruments/fetch":
		default:
			http.NotFound(w, r)
			return
		}
		fetchCount++
		var req struct {
			StartDate *string `json:"start_date"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		source := "ak.stock_zh_a_hist_tx"
		points := []marketdata.HistoricalPoint{
			{Date: "2023-12-29", Value: 1.0},
			{Date: "2024-12-31", Value: 1.2},
			{Date: "2025-12-31", Value: 1.5},
		}
		if fetchCount == 1 {
			source = "test_fixture"
			points = buildFixturePoints()
		} else if req.StartDate == nil {
			source = "full_refresh_source"
		}
		resp := marketdata.FetchResponse{
			Code: 0, Message: "success",
			Data: marketdata.FetchData{
				Provider: "akshare", ProviderSymbol: "510300", Name: "沪深300ETF",
				AssetClass: "equity", Currency: "CNY", PointType: "adjusted_close",
				ExpenseRatioStatus: "unavailable", ExpenseRatioComponents: map[string]any{"region": "domestic"},
				Points: points, SourceName: source, SourceQuality: "full",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(provider.Close)

	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := importFixtureInstrument(t, client, srv.URL)
	if _, err := db.ExecContext(context.Background(), `
		UPDATE market_data_points SET source_name='ak.fund_etf_hist_sina', value=0.5
		WHERE instrument_id=? AND trade_date='2017-12-31'`, instID); err != nil {
		t.Fatal(err)
	}

	var oldValue float64
	if err := db.QueryRowContext(context.Background(), `
		SELECT value FROM market_data_points WHERE instrument_id=? AND trade_date='2017-12-31'`,
		instID).Scan(&oldValue); err != nil {
		t.Fatal(err)
	}
	if oldValue != 0.5 {
		t.Fatalf("expected seeded sina value 0.5, got %v", oldValue)
	}

	oldFetched := time.Now().Add(-25 * time.Hour).UnixMilli()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE market_data_points SET fetched_at=? WHERE instrument_id=?`, oldFetched, instID); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"force": true})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force refresh status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM market_data_points WHERE instrument_id=?`, instID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 points after full replace, got %d", count)
	}

	var sourceName string
	var legacyValue float64
	err = db.QueryRowContext(context.Background(), `
		SELECT source_name FROM market_data_points WHERE instrument_id=? LIMIT 1`, instID).Scan(&sourceName)
	if err != nil {
		t.Fatal(err)
	}
	if sourceName != "full_refresh_source" {
		t.Fatalf("expected full refresh source, got %q", sourceName)
	}
	err = db.QueryRowContext(context.Background(), `
		SELECT value FROM market_data_points WHERE instrument_id=? AND trade_date='2017-12-31'`, instID).Scan(&legacyValue)
	if err != sql.ErrNoRows {
		t.Fatalf("expected legacy sina date removed, err=%v value=%v", err, legacyValue)
	}
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

func TestSyncHoldingSnapshotRollbackOnHoldingUpdateFailureIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	valuationDate := "2025-06-01"
	instID := importFixtureInstrument(t, client, srv.URL)
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
	srv, _, client := setupInstrumentIntegration(t)
	instID := importFixtureInstrument(t, client, srv.URL)

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
	srv, _, client := setupInstrumentIntegration(t)
	instID := importFixtureInstrument(t, client, srv.URL)

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

	instID := importFixtureInstrument(t, client, srv.URL)
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

func TestInstrumentRefreshSourceConflictKeepsExistingData(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/fetch":
			_ = json.NewEncoder(w).Encode(marketdata.FetchResponse{
				Code: 0, Message: "success",
				Data: marketdata.FetchData{
					Provider: "akshare", ProviderSymbol: "000001", Name: "华夏成长混合",
					AssetClass: "cash", Currency: "CNY", PointType: "nav",
					ExpenseRatioStatus: "unavailable",
					ExpenseRatioComponents: map[string]any{"region": "domestic"},
					Points: []marketdata.HistoricalPoint{{Date: "2024-12-31", Value: 1.0}},
					SourceName: "ak.fund_money_fund_info_em", SourceQuality: "full", SourceKind: "money_fund",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)

	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := "ins_source_conflict_test"
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system, expense_ratio, expense_ratio_status,
			fee_treatment, status, created_at, updated_at
		) VALUES (?, '000001', '华夏成长混合', 'CN', 'cn_mutual_fund', 'equity', 'domestic', 'CNY',
			'akshare', '000001', 'none', 0, NULL, 'unavailable', 'embedded', 'active', ?, ?)`,
		instID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO market_data_points (instrument_id, trade_date, value, point_type, source_name, fetched_at)
		VALUES (?, '2024-12-31', 2.5, 'nav', 'ak.fund_open_fund_info_em:单位净值走势', ?)`,
		instID, now); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"force": true})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	respBody := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("refresh status=%d body=%s", resp.StatusCode, respBody)
	}
	assertErrorCode(t, respBody, "market_data_source_type_conflict")

	var value float64
	var sourceName string
	if err := db.QueryRowContext(context.Background(), `
		SELECT value, source_name FROM market_data_points WHERE instrument_id=? AND trade_date='2024-12-31'`,
		instID).Scan(&value, &sourceName); err != nil {
		t.Fatal(err)
	}
	if value != 2.5 {
		t.Fatalf("expected old value preserved, got %v", value)
	}
	if sourceName != "ak.fund_open_fund_info_em:单位净值走势" {
		t.Fatalf("expected old source preserved, got %q", sourceName)
	}
}
