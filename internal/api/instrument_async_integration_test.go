//go:build integration

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/testutil"
)

func resolveTicket(t *testing.T, client *http.Client, baseURL string, market, instrumentType, code string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{
		"market": market, "instrument_type": instrumentType, "code": code,
	})
	resp, err := client.Post(baseURL+"/api/v1/instruments/resolve", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	data := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	resolved, ok := data["resolved"].(map[string]any)
	if !ok {
		t.Fatalf("resolve missing resolved: %v", data)
	}
	ticketID, ok := resolved["ticket_id"].(string)
	if !ok || ticketID == "" {
		t.Fatalf("resolve missing ticket_id: %v", resolved)
	}
	return ticketID
}

func importAsyncNoWait(t *testing.T, client *http.Client, baseURL, ticketID, region string) string {
	t.Helper()
	assetClass := "equity"
	raw, _ := json.Marshal(map[string]any{"ticket_id": ticketID, "asset_class": assetClass, "region": region})
	resp, err := client.Post(baseURL+"/api/v1/instruments/import-async", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import-async status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	return env["data"].(map[string]any)["instrument_id"].(string)
}

func defaultImportRegionForMarket(market string) string {
	switch strings.ToUpper(market) {
	case "HK", "US":
		return "foreign"
	default:
		return "domestic"
	}
}

func resolveAndImportAsync(t *testing.T, client *http.Client, baseURL, market, instrumentType, code string) string {
	t.Helper()
	ticketID := resolveTicket(t, client, baseURL, market, instrumentType, code)
	return importAsyncNoWait(t, client, baseURL, ticketID, defaultImportRegionForMarket(market))
}

func waitForInstrumentStatus(t *testing.T, client *http.Client, baseURL, instrumentID, wantStatus string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/v1/instruments/" + instrumentID)
		if err != nil {
			t.Fatal(err)
		}
		inst := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
		if inst["status"] == wantStatus {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("instrument %s did not reach status %s", instrumentID, wantStatus)
}

func assertHoldingsNotReady(t *testing.T, client *http.Client, baseURL, planID string, version int, instrumentID string) {
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
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected holdings failure for non-active instrument, got 200 body=%s", readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_not_ready")
}

func assertWizardNotReady(t *testing.T, client *http.Client, baseURL, instrumentID string) {
	t.Helper()
	body := map[string]any{
		"name": "未就绪向导", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id":      "scn_builtin_near_fire",
		"apply_unallocated_to_cash": true,
		"parameters":                wizardParams(1_000_000_00),
		"holdings": []map[string]any{
			{"instrument_id": instrumentID, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": 1_000_000_00, "sort_order": 1},
		},
	}
	resp, raw := postWizard(t, client, baseURL, body)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected wizard failure for non-active instrument, got 200 body=%s", string(raw))
	}
	assertErrorCode(t, raw, "instrument_not_ready")
}

func TestInstrumentNotReadyIntegration(t *testing.T) {
	provider := mockProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)

	assertHoldingsNotReady(t, client, srv.URL, plan.ID, version, instID)
	assertWizardNotReady(t, client, srv.URL, instID)

	if _, err := db.ExecContext(context.Background(), `UPDATE instruments SET status='fetch_failed' WHERE id=?`, instID); err != nil {
		t.Fatal(err)
	}
	assertHoldingsNotReady(t, client, srv.URL, plan.ID, version, instID)
	assertWizardNotReady(t, client, srv.URL, instID)
}

func mockRetryFetchProvider(t *testing.T) *httptest.Server {
	t.Helper()
	var fetchCount atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			var req marketdata.ResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			code := req.Code
			if req.Market == "CN" && req.InstrumentType == "cn_exchange_fund" && !marketdata.HasCNExchangePrefix(code) {
				code = "sh" + code
			}
			_ = json.NewEncoder(w).Encode(marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: code, ProviderSymbol: code,
						Name: "沪深300ETF", Exchange: "SH", InstrumentKind: "etf",
					},
				},
			})
		case "/v1/instruments/fetch":
			if fetchCount.Add(1) == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code":1,"message":"mock fetch failed"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(marketdata.FetchResponse{
				Code: 0, Message: "success",
				Data: marketdata.FetchData{
					Provider: "akshare", ProviderSymbol: "510300", Name: "沪深300ETF",
					AssetClass: "equity", Currency: "CNY", PointType: "adjusted_close",
					ExpenseRatioStatus: "unavailable", ExpenseRatioComponents: map[string]any{"region": "domestic"},
					Points: buildFixturePoints(), SourceName: "test_fixture", SourceQuality: "full",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstrumentRetryFetchIntegration(t *testing.T) {
	provider := mockRetryFetchProvider(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	beforeCount := countTable(t, db, "instruments")
	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	waitForInstrumentStatus(t, client, srv.URL, instID, "fetch_failed")
	if countTable(t, db, "instruments") != beforeCount+1 {
		t.Fatalf("expected exactly one new instrument row after failed fetch")
	}

	resp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/retry-fetch", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retry-fetch status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	waitForInstrumentActive(t, client, srv.URL, instID)
	if countTable(t, db, "instruments") != beforeCount+1 {
		t.Fatal("retry-fetch must not create a new instrument row")
	}

	var status string
	if err := db.QueryRowContext(context.Background(), `SELECT status FROM instruments WHERE id=?`, instID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "active" {
		t.Fatalf("instrument status=%q want active", status)
	}
}

func mock510ProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			var req marketdata.ResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			code := marketdata.NormalizeCNExchangeCode(req.Code)
			name := "中证A500"
			kind := "index_etf"
			if code == "sz000510" {
				name = "新金路"
				kind = "stock"
			}
			_ = json.NewEncoder(w).Encode(marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: code, ProviderSymbol: code,
						Name: name, Exchange: code[:2], InstrumentKind: kind,
					},
				},
			})
		case "/v1/instruments/fetch":
			var req marketdata.FetchRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			symbol := req.SourceCode
			name := "中证A500"
			if symbol == "sz000510" {
				name = "新金路"
			}
			_ = json.NewEncoder(w).Encode(marketdata.FetchResponse{
				Code: 0, Message: "success",
				Data: marketdata.FetchData{
					Provider: "akshare", ProviderSymbol: symbol, Name: name,
					AssetClass: "equity", Currency: "CNY", PointType: "adjusted_close",
					ExpenseRatioStatus: "unavailable", ExpenseRatioComponents: map[string]any{"region": "domestic"},
					Points: buildFixturePoints(), SourceName: "test_fixture", SourceQuality: "full",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstrument510DualImportIntegration(t *testing.T) {
	provider := mock510ProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	shID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "sh000510")
	szID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_stock", "sz000510")
	if shID == szID {
		t.Fatal("sh000510 and sz000510 must be separate instrument records")
	}

	waitForInstrumentActive(t, client, srv.URL, shID)
	waitForInstrumentActive(t, client, srv.URL, szID)

	var shCode, szCode string
	if err := db.QueryRowContext(context.Background(), `SELECT code FROM instruments WHERE id=?`, shID).Scan(&shCode); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(context.Background(), `SELECT code FROM instruments WHERE id=?`, szID).Scan(&szCode); err != nil {
		t.Fatal(err)
	}
	if shCode != "sh000510" || szCode != "sz000510" {
		t.Fatalf("codes sh=%q sz=%q", shCode, szCode)
	}

	for _, tc := range []struct {
		name                         string
		market, instrumentType, code string
	}{
		{name: "sh000510", market: "CN", instrumentType: "cn_exchange_fund", code: "sh000510"},
		{name: "sz000510", market: "CN", instrumentType: "cn_exchange_stock", code: "sz000510"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ticketID := resolveTicket(t, client, srv.URL, tc.market, tc.instrumentType, tc.code)
			raw, _ := json.Marshal(map[string]any{"ticket_id": ticketID, "asset_class": "equity", "region": "domestic"})
			resp, err := client.Post(srv.URL+"/api/v1/instruments/import-async", "application/json", bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("duplicate import status=%d body=%s", resp.StatusCode, readBody(t, resp))
			}
			assertErrorCode(t, readBody(t, resp), "instrument_already_exists")
		})
	}
}

func TestConcurrentImportAsyncIntegration(t *testing.T) {
	provider := mockProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	ticketID := resolveTicket(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	const workers = 20
	results := make(chan int, workers)
	for i := 0; i < workers; i++ {
		go func() {
			raw, _ := json.Marshal(map[string]any{"ticket_id": ticketID, "asset_class": "equity", "region": "domestic"})
			resp, err := client.Post(srv.URL+"/api/v1/instruments/import-async", "application/json", bytes.NewReader(raw))
			if err != nil {
				results <- 0
				return
			}
			results <- resp.StatusCode
		}()
	}
	okCount := 0
	rejectCount := 0
	for i := 0; i < workers; i++ {
		status := <-results
		switch status {
		case http.StatusOK:
			okCount++
		case http.StatusBadRequest:
			rejectCount++
		default:
			t.Fatalf("unexpected status=%d", status)
		}
	}
	if okCount != 1 {
		t.Fatalf("okCount=%d want 1", okCount)
	}
	if rejectCount != workers-1 {
		t.Fatalf("rejectCount=%d want %d", rejectCount, workers-1)
	}
	var instCount int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM instruments WHERE code='sh510300'`).Scan(&instCount); err != nil {
		t.Fatal(err)
	}
	if instCount != 1 {
		t.Fatalf("instrument rows=%d want 1", instCount)
	}
}

func TestConcurrentRetryFetchIntegration(t *testing.T) {
	provider := mockRetryFetchProvider(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	waitForInstrumentStatus(t, client, srv.URL, instID, "fetch_failed")

	const workers = 20
	results := make(chan int, workers)
	for i := 0; i < workers; i++ {
		go func() {
			resp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/retry-fetch", "application/json", nil)
			if err != nil {
				results <- 0
				return
			}
			results <- resp.StatusCode
		}()
	}
	okCount := 0
	inProgressCount := 0
	for i := 0; i < workers; i++ {
		status := <-results
		switch status {
		case http.StatusOK:
			okCount++
		case http.StatusBadRequest:
			inProgressCount++
		default:
			t.Fatalf("unexpected status=%d", status)
		}
	}
	if okCount != 1 {
		t.Fatalf("okCount=%d want 1", okCount)
	}
	if inProgressCount != workers-1 {
		t.Fatalf("inProgressCount=%d want %d", inProgressCount, workers-1)
	}
}

func mockSlowFetchProvider(t *testing.T) *httptest.Server {
	t.Helper()
	return mockBlockingFetchProvider(t, nil)
}

func mockSlowThenSuccessFetchProvider(t *testing.T) *httptest.Server {
	t.Helper()
	var fetchCount atomic.Int32
	return mockBlockingFetchProvider(t, &fetchCount)
}

func mockBlockingFetchProvider(t *testing.T, fetchCount *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			var req marketdata.ResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			code := req.Code
			if req.Market == "CN" && req.InstrumentType == "cn_exchange_fund" && !marketdata.HasCNExchangePrefix(code) {
				code = "sh" + code
			}
			_ = json.NewEncoder(w).Encode(marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: code, ProviderSymbol: code,
						Name: "沪深300ETF", Exchange: "SH", InstrumentKind: "etf",
					},
				},
			})
		case "/v1/instruments/fetch":
			delay := 5 * time.Second
			if fetchCount != nil && fetchCount.Add(1) > 1 {
				delay = 0
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
			_ = json.NewEncoder(w).Encode(marketdata.FetchResponse{
				Code: 0, Message: "success",
				Data: marketdata.FetchData{
					Provider: "akshare", ProviderSymbol: "510300", Name: "沪深300ETF",
					AssetClass: "equity", Currency: "CNY", PointType: "adjusted_close",
					ExpenseRatioStatus: "unavailable", ExpenseRatioComponents: map[string]any{"region": "domestic"},
					Points: buildFixturePoints(), SourceName: "test_fixture", SourceQuality: "full",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func waitForJobStatus(t *testing.T, db *sql.DB, jobID, wantStatus string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := db.QueryRowContext(context.Background(), `SELECT status FROM jobs WHERE id=?`, jobID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == wantStatus {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %s", jobID, wantStatus)
}

func waitForFetchStatus(t *testing.T, client *http.Client, baseURL, instrumentID string, check func(map[string]any) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/v1/instruments/" + instrumentID + "/fetch-status")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("fetch-status status=%d body=%s", resp.StatusCode, readBody(t, resp))
		}
		status := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
		if check(status) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("fetch-status did not reach expected state")
}

func closeProvider(t *testing.T, provider *httptest.Server) {
	t.Helper()
	t.Cleanup(func() {
		provider.CloseClientConnections()
		provider.Close()
	})
}

func TestInstrumentFetchRunningCancelIntegration(t *testing.T) {
	provider := mockSlowThenSuccessFetchProvider(t)
	closeProvider(t, provider)
	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	var jobID string
	if err := db.QueryRowContext(context.Background(), `
		SELECT id FROM jobs WHERE json_extract(payload_json, '$.instrument_id')=? ORDER BY created_at DESC LIMIT 1`,
		instID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, db, jobID, "running")

	resp, err := client.Post(srv.URL+"/api/v1/jobs/"+jobID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	waitForJobStatus(t, db, jobID, "canceled")
	var jobStatus, errorCode string
	if err := db.QueryRowContext(context.Background(), `SELECT status, COALESCE(error_code, '') FROM jobs WHERE id=?`, jobID).Scan(&jobStatus, &errorCode); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "canceled" {
		t.Fatalf("job status=%q want canceled", jobStatus)
	}
	if errorCode != "fetch_canceled" {
		t.Fatalf("job error_code=%q want fetch_canceled", errorCode)
	}

	var instStatus string
	if err := db.QueryRowContext(context.Background(), `SELECT status FROM instruments WHERE id=?`, instID).Scan(&instStatus); err != nil {
		t.Fatal(err)
	}
	if instStatus != "fetch_failed" {
		t.Fatalf("instrument status=%q want fetch_failed", instStatus)
	}

	waitForFetchStatus(t, client, srv.URL, instID, func(st map[string]any) bool {
		return st["job_status"] == "canceled" && st["error_code"] == "fetch_canceled" && st["instrument_status"] == "fetch_failed"
	})

	retryResp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/retry-fetch", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if retryResp.StatusCode != http.StatusOK {
		t.Fatalf("retry-fetch status=%d body=%s", retryResp.StatusCode, readBody(t, retryResp))
	}
	waitForInstrumentActive(t, client, srv.URL, instID)
}

func TestInstrumentFetchShutdownRequeueIntegration(t *testing.T) {
	provider := mockSlowFetchProvider(t)
	closeProvider(t, provider)
	db := testutil.OpenTestDB(t)
	stopWorker := startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	var jobID string
	if err := db.QueryRowContext(context.Background(), `
		SELECT id FROM jobs WHERE json_extract(payload_json, '$.instrument_id')=? ORDER BY created_at DESC LIMIT 1`,
		instID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	waitForJobStatus(t, db, jobID, "running")

	stopWorker()
	waitForJobStatus(t, db, jobID, "queued")

	var instStatus string
	if err := db.QueryRowContext(context.Background(), `SELECT status FROM instruments WHERE id=?`, instID).Scan(&instStatus); err != nil {
		t.Fatal(err)
	}
	if instStatus != "pending_fetch" {
		t.Fatalf("instrument status=%q want pending_fetch after shutdown requeue", instStatus)
	}
}

func TestInstrumentFetchQueuedCancelIntegration(t *testing.T) {
	provider := mockProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	var jobID string
	if err := db.QueryRowContext(context.Background(), `
		SELECT id FROM jobs WHERE json_extract(payload_json, '$.instrument_id')=? ORDER BY created_at DESC LIMIT 1`,
		instID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Post(srv.URL+"/api/v1/jobs/"+jobID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	var status string
	if err := db.QueryRowContext(context.Background(), `SELECT status FROM instruments WHERE id=?`, instID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "fetch_failed" {
		t.Fatalf("instrument status=%q want fetch_failed", status)
	}

	retryResp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/retry-fetch", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if retryResp.StatusCode != http.StatusOK {
		t.Fatalf("retry-fetch status=%d body=%s", retryResp.StatusCode, readBody(t, retryResp))
	}
}

func TestHoldingsWithSystemCashIntegration(t *testing.T) {
	provider := mockProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instEquity := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	waitForInstrumentActive(t, client, srv.URL, instEquity)
	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	allocBody, _ := json.Marshal(map[string]any{
		"config_version": plan.ConfigVersion,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 0.7},
			{"asset_class": "bond", "weight": 0.0},
			{"asset_class": "cash", "weight": 0.3},
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
	allocReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+plan.ID+"/allocation", bytes.NewReader(allocBody))
	allocReq.Header.Set("Content-Type", "application/json")
	allocResp, err := client.Do(allocReq)
	if err != nil {
		t.Fatal(err)
	}
	if allocResp.StatusCode != http.StatusOK {
		t.Fatalf("allocation status=%d body=%s", allocResp.StatusCode, readBody(t, allocResp))
	}
	_ = readBody(t, allocResp)
	version := plan.ConfigVersion + 1

	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"instrument_id": instEquity, "enabled": true,
				"weight_within_group": 1.0, "current_amount_minor": 7_000_000_00, "sort_order": 1,
			},
			{
				"instrument_id": "system_cash_cny", "enabled": true,
				"weight_within_group": 1.0, "current_amount_minor": 3_000_000_00, "sort_order": 2,
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+plan.ID+"/holdings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holdings status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func mockAmbiguousFundProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/instruments/resolve" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(marketdata.ResolveResponse{
			Code: 0, Message: "success",
			Data: marketdata.ResolveData{
				Ambiguous: true,
				Candidates: []marketdata.ResolveCandidate{
					{Code: "sh000510", ProviderSymbol: "sh000510", Name: "中证A500", Exchange: "SH", InstrumentKind: "index_etf"},
					{Code: "sz000510", ProviderSymbol: "sz000510", Name: "新金路", Exchange: "SZ", InstrumentKind: "stock"},
				},
			},
		})
	}))
}

func TestResolveStockCandidateNoTicketForFundTypeIntegration(t *testing.T) {
	provider := mockAmbiguousFundProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	raw, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund", "code": "000510",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/resolve", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	data := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	cands := data["candidates"].([]any)
	var stockCand map[string]any
	for _, c := range cands {
		row := c.(map[string]any)
		if row["instrument_kind"] == "stock" {
			stockCand = row
			break
		}
	}
	if stockCand == nil {
		t.Fatal("expected stock candidate")
	}
	if stockCand["is_importable"] != false {
		t.Fatalf("stock candidate is_importable=%v want false", stockCand["is_importable"])
	}
	if _, ok := stockCand["ticket_id"]; ok {
		t.Fatal("stock candidate must not include ticket_id")
	}
	candidateID, ok := stockCand["candidate_id"].(string)
	if !ok || candidateID == "" {
		t.Fatalf("stock candidate missing candidate_id: %v", stockCand["candidate_id"])
	}
	if candidateID != "sz000510|sz000510|stock|SZ" {
		t.Fatalf("stock candidate_id=%q want composite identity", candidateID)
	}
}
