//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/testutil"
)

// fetchKindRecorder captures the instrument_kind carried by each fetch request
// so tests can assert the refresh path threads resolved identity to the sidecar.
type fetchKindRecorder struct {
	mu    sync.Mutex
	kinds []string
}

func (r *fetchKindRecorder) record(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = append(r.kinds, kind)
}

func (r *fetchKindRecorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = nil
}

func (r *fetchKindRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.kinds))
	copy(out, r.kinds)
	return out
}

func identityFetchData() marketdata.FetchData {
	return marketdata.FetchData{
		Provider: "akshare", ProviderSymbol: "510300", Name: "沪深300ETF",
		AssetClass: "equity", Currency: "CNY", PointType: "adjusted_close",
		ExpenseRatioStatus: "unavailable", ExpenseRatioComponents: map[string]any{"region": "domestic"},
		Points: buildFixturePoints(), SourceName: "test_fixture", SourceQuality: "full",
	}
}

// TestInstrumentRefreshThreadsResolvedKindIntegration proves that a kind resolved
// at import time is persisted and re-sent on the refresh fetch, so the sidecar
// can pick an identity-consistent history source (td/038 P1-1).
func TestInstrumentRefreshThreadsResolvedKindIntegration(t *testing.T) {
	rec := &fetchKindRecorder{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			var req marketdata.ResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			code := req.Code
			if !marketdata.HasCNExchangePrefix(code) {
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
			var req struct {
				InstrumentKind string `json:"instrument_kind"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			rec.record(req.InstrumentKind)
			_ = json.NewEncoder(w).Encode(marketdata.FetchResponse{
				Code: 0, Message: "success", Data: identityFetchData(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)

	db := testutil.OpenTestDB(t)
	startInstrumentFetchWorker(t, db, provider.URL)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	client := srv.Client()

	instID := resolveAndImportAsync(t, client, srv.URL, "CN", "cn_exchange_fund", "510300")
	waitForInstrumentActive(t, client, srv.URL, instID)

	var storedKind string
	if err := db.QueryRowContext(context.Background(),
		`SELECT instrument_kind FROM instruments WHERE id=?`, instID).Scan(&storedKind); err != nil {
		t.Fatal(err)
	}
	if storedKind != "etf" {
		t.Fatalf("instrument_kind not persisted on import: got %q want etf", storedKind)
	}

	// Only inspect the refresh fetch, not the import fetch.
	rec.reset()
	body, _ := json.Marshal(map[string]any{"force": true})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/"+instID+"/refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force refresh status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	kinds := rec.snapshot()
	if len(kinds) == 0 {
		t.Fatal("refresh did not issue a fetch request")
	}
	for _, k := range kinds {
		if k != "etf" {
			t.Fatalf("refresh fetch carried instrument_kind=%q want etf", k)
		}
	}
}

// TestInstrumentRefreshBackfillsMissingKindIntegration proves that a legacy asset
// missing instrument_kind is healed by a controlled resolve before the refresh
// fetch, then refreshes with the resolved kind (td/038 P1-1 #4).
func TestInstrumentRefreshBackfillsMissingKindIntegration(t *testing.T) {
	rec := &fetchKindRecorder{}
	var resolveCalls int
	var mu sync.Mutex
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			mu.Lock()
			resolveCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: "sh510300", ProviderSymbol: "sh510300",
						Name: "沪深300LOF", Exchange: "SH", InstrumentKind: "lof",
					},
				},
			})
		case "/v1/instruments/fetch":
			var req struct {
				InstrumentKind string `json:"instrument_kind"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			rec.record(req.InstrumentKind)
			_ = json.NewEncoder(w).Encode(marketdata.FetchResponse{
				Code: 0, Message: "success", Data: identityFetchData(),
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

	instID := "ins_legacy_no_kind"
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, instrument_kind, is_system,
			expense_ratio, expense_ratio_status, fee_treatment, status, created_at, updated_at
		) VALUES (?, '510300', '沪深300LOF', 'CN', 'cn_exchange_fund', 'equity', 'domestic', 'CNY',
			'akshare', 'sh510300', 'none', '', 0, NULL, 'unavailable', 'embedded', 'active', ?, ?)`,
		instID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO market_data_points (instrument_id, trade_date, value, point_type, source_name, fetched_at)
		VALUES (?, '2024-12-31', 1.0, 'adjusted_close', 'test_fixture', ?)`, instID, now); err != nil {
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

	mu.Lock()
	gotResolveCalls := resolveCalls
	mu.Unlock()
	if gotResolveCalls == 0 {
		t.Fatal("expected a backfill resolve call for missing instrument_kind")
	}

	var storedKind string
	if err := db.QueryRowContext(context.Background(),
		`SELECT instrument_kind FROM instruments WHERE id=?`, instID).Scan(&storedKind); err != nil {
		t.Fatal(err)
	}
	if storedKind != "lof" {
		t.Fatalf("instrument_kind not backfilled: got %q want lof", storedKind)
	}

	kinds := rec.snapshot()
	if len(kinds) == 0 {
		t.Fatal("refresh did not issue a fetch request")
	}
	for _, k := range kinds {
		if k != "lof" {
			t.Fatalf("refresh fetch carried instrument_kind=%q want lof", k)
		}
	}
}
