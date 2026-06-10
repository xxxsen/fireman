//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/testutil"
)

func importAsyncNoWait(t *testing.T, client *http.Client, baseURL string, payload map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(payload)
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

	instID := importAsyncNoWait(t, client, srv.URL, map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund",
		"code": "sh510300", "provider_symbol": "sh510300",
	})
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
	instID := importAsyncNoWait(t, client, srv.URL, map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund",
		"code": "sh510300", "provider_symbol": "sh510300",
	})
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

	shID := importAsyncNoWait(t, client, srv.URL, map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund",
		"code": "sh000510", "provider_symbol": "sh000510",
	})
	szID := importAsyncNoWait(t, client, srv.URL, map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_stock",
		"code": "sz000510", "provider_symbol": "sz000510",
	})
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
		name    string
		payload map[string]any
	}{
		{
			name: "sh000510",
			payload: map[string]any{
				"market": "CN", "instrument_type": "cn_exchange_fund",
				"code": "sh000510", "provider_symbol": "sh000510",
			},
		},
		{
			name: "sz000510",
			payload: map[string]any{
				"market": "CN", "instrument_type": "cn_exchange_stock",
				"code": "sz000510", "provider_symbol": "sz000510",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.payload)
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
