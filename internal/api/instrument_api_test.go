package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func mockProviderServer(t *testing.T) *httptest.Server {
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
			if req.Market == "HK" {
				code = marketdata.NormalizeHKCode(code)
			}
			resp := marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: code, ProviderSymbol: code,
						Name: "沪深300ETF", Exchange: "SH", InstrumentKind: "etf",
					},
				},
			}
			if req.Market == "HK" {
				resp.Data.Resolved = &marketdata.ResolveCandidate{
					Code: code, ProviderSymbol: code,
					Name: "腾讯控股", Exchange: "HK", InstrumentKind: "stock",
				}
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/v1/instruments/fetch":
			resp := marketdata.FetchResponse{
				Code: 0, Message: "success",
				Data: marketdata.FetchData{
					Provider: "akshare", ProviderSymbol: "510300", Name: "沪深300ETF",
					AssetClass: "equity", Currency: "CNY", PointType: "adjusted_close",
					ExpenseRatioStatus: "unavailable", ExpenseRatioComponents: map[string]any{"region": "domestic"},
					Points:     buildFixturePoints(),
					SourceName: "test_fixture", SourceQuality: "full",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
}

func buildTwentyYearFixturePoints() []marketdata.HistoricalPoint {
	var out []marketdata.HistoricalPoint
	value := 100.0
	for year := 2005; year <= 2024; year++ {
		out = append(out, marketdata.HistoricalPoint{
			Date: fmt.Sprintf("%d-12-31", year-1), Value: value,
		})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				value *= 1.0004
				out = append(out, marketdata.HistoricalPoint{
					Date: formatFixtureDate(year, month, day), Value: value,
				})
			}
		}
	}
	return out
}

func mockHKProviderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/instruments/resolve":
			var req marketdata.ResolveRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			code := marketdata.NormalizeHKCode(req.Code)
			resp := marketdata.ResolveResponse{
				Code: 0, Message: "success",
				Data: marketdata.ResolveData{
					Ambiguous: false,
					Resolved: &marketdata.ResolveCandidate{
						Code: code, ProviderSymbol: code,
						Name: "腾讯控股", Exchange: "HK", InstrumentKind: "stock",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/v1/instruments/fetch":
			resp := marketdata.FetchResponse{
				Code: 0, Message: "success",
				Data: marketdata.FetchData{
					Provider: "akshare", ProviderSymbol: "00700", Name: "腾讯控股",
					AssetClass: "equity", Currency: "HKD", PointType: "adjusted_close",
					ExpenseRatioStatus: "unavailable",
					Points:             buildTwentyYearFixturePoints(),
					SourceName:         "test_hk_fixture", SourceQuality: "full",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
}

func buildFixturePoints() []marketdata.HistoricalPoint {
	var out []marketdata.HistoricalPoint
	value := 100.0
	for year := 2018; year <= 2024; year++ {
		out = append(out, marketdata.HistoricalPoint{
			Date: fmt.Sprintf("%d-12-31", year-1), Value: value,
		})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				value *= 1.0004
				out = append(out, marketdata.HistoricalPoint{
					Date: formatFixtureDate(year, month, day), Value: value,
				})
			}
		}
	}
	return out
}

func formatFixtureDate(y, m, d int) string {
	return sprintf2(y) + "-" + sprintf2(m) + "-" + sprintf2(d)
}

func sprintf2(n int) string {
	return fmt.Sprintf("%02d", n)
}

func testRouterWithProvider(t *testing.T, providerURL string) *httptest.Server {
	t.Helper()
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, providerURL)})
	return httptest.NewServer(r)
}

func buildServices(db *sql.DB, providerURL string) Services {
	return NewServices(db, "", providerURL, nil)
}

func TestInstrumentFieldsReadOnly(t *testing.T) {
	provider := mockProviderServer(t)
	defer provider.Close()
	srv := testRouterWithProvider(t, provider.URL)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund", "code": "510300",
		"name": "hack",
	})
	resp, err := srv.Client().Post(srv.URL+"/api/v1/instruments/import/preview", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestInstrumentImportPreviewAndImport(t *testing.T) {
	provider := mockProviderServer(t)
	defer provider.Close()
	srv := testRouterWithProvider(t, provider.URL)
	defer srv.Close()
	client := srv.Client()

	payload, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund", "code": "510300",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import/preview", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	resp, err = client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	inst := env["data"].(map[string]any)
	id := inst["id"].(string)
	if inst["status"] != "pending_fetch" {
		t.Fatalf("import status=%v want pending_fetch", inst["status"])
	}

	resp, err = client.Get(srv.URL + "/api/v1/instruments/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status=%d", resp.StatusCode)
	}

	resp, err = client.Get(srv.URL + "/api/v1/instruments/" + id + "/annual-returns")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("annual status=%d", resp.StatusCode)
	}
}

func TestHKInstrumentDuplicateNormalizedCode(t *testing.T) {
	provider := mockHKProviderServer(t)
	defer provider.Close()
	srv := testRouterWithProvider(t, provider.URL)
	defer srv.Close()
	client := srv.Client()

	payload700, _ := json.Marshal(map[string]any{
		"market": "HK", "instrument_type": "hk_stock", "code": "700",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload700))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import 700 status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	first := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if first["code"] != "00700" {
		t.Fatalf("imported code=%v want 00700", first["code"])
	}

	payload00700, _ := json.Marshal(map[string]any{
		"market": "HK", "instrument_type": "hk_stock", "code": "00700",
	})
	resp, err = client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload00700))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate import status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	body := decodeEnvelope(t, readBody(t, resp))
	if body["code"] != "instrument_fetch_in_progress" && body["code"] != "instrument_already_exists" {
		t.Fatalf("duplicate code=%v want instrument_fetch_in_progress or instrument_already_exists", body["code"])
	}
}

func TestHKInstrumentPreviewNormalizesCode(t *testing.T) {
	provider := mockHKProviderServer(t)
	defer provider.Close()
	srv := testRouterWithProvider(t, provider.URL)
	defer srv.Close()
	client := srv.Client()

	for _, code := range []string{"700", "00700"} {
		payload, _ := json.Marshal(map[string]any{
			"market": "HK", "instrument_type": "hk_stock", "code": code,
		})
		resp, err := client.Post(srv.URL+"/api/v1/instruments/import/preview", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("preview %s status=%d body=%s", code, resp.StatusCode, readBody(t, resp))
		}
		preview := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
		if preview["deprecated"] != true {
			t.Fatalf("preview should be deprecated")
		}
		resolve := preview["resolve"].(map[string]any)
		if resolve["ambiguous"] != false {
			t.Fatalf("preview resolve should be unambiguous for %s", code)
		}
		resolved := resolve["resolved"].(map[string]any)
		if resolved["code"] != "00700" {
			t.Fatalf("preview code for %s=%v want 00700", code, resolved["code"])
		}
	}
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	return mustRead(t, resp)
}

func insertLibraryInstrument(t *testing.T, db *sql.DB, assetClass, region string) (string, int64) {
	t.Helper()
	id := "ins_classification_test"
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system, expense_ratio, expense_ratio_status,
			fee_treatment, status, created_at, updated_at
		) VALUES (?, '510300', '测试ETF', 'CN', 'cn_exchange_fund', ?, ?, 'CNY',
			'akshare', '510300', 'none', 0, NULL, 'unavailable', 'embedded', 'active', ?, ?)`,
		id, assetClass, region, now, now); err != nil {
		t.Fatal(err)
	}
	return id, now
}

func patchClassification(
	t *testing.T, client *http.Client, baseURL, instID string, body map[string]any,
) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPatch,
		baseURL+"/api/v1/instruments/"+instID+"/classification", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestUpdateInstrumentClassificationAPI covers classification editing: valid edit, optimistic
// lock conflict, enum rejection and system-asset protection.
func TestUpdateInstrumentClassificationAPI(t *testing.T) {
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, "")}))
	defer srv.Close()
	client := srv.Client()

	instID, updatedAt := insertLibraryInstrument(t, db, "equity", "domestic")

	resp := patchClassification(t, client, srv.URL, instID, map[string]any{
		"asset_class": "bond", "region": "foreign", "expected_updated_at": updatedAt,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("classification patch status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	data := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	inst := data["instrument"].(map[string]any)
	if inst["asset_class"] != "bond" || inst["region"] != "foreign" {
		t.Fatalf("instrument classification=%v/%v", inst["asset_class"], inst["region"])
	}
	if data["classification_sync_scope"] != "future_only" {
		t.Fatalf("sync scope=%v", data["classification_sync_scope"])
	}

	resp = patchClassification(t, client, srv.URL, instID, map[string]any{
		"asset_class": "cash", "region": "domestic", "expected_updated_at": updatedAt,
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_version_conflict")

	resp = patchClassification(t, client, srv.URL, instID, map[string]any{
		"asset_class": "crypto", "region": "domestic", "expected_updated_at": 0,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid enum status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_classification_unsupported")

	resp = patchClassification(t, client, srv.URL, repository.SystemCashInstrumentID, map[string]any{
		"asset_class": "cash", "region": "domestic", "expected_updated_at": 0,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("system asset status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_not_editable")
}

// TestUpdateInstrumentClassificationRejectedDuringFetch verifies that
// a pending_fetch asset cannot have its classification edited, since the in-flight
// fetch would later overwrite it with the import-time payload.
func TestUpdateInstrumentClassificationRejectedDuringFetch(t *testing.T) {
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db, "")}))
	defer srv.Close()
	client := srv.Client()

	id := "ins_pending_fetch"
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system, expense_ratio, expense_ratio_status,
			fee_treatment, status, created_at, updated_at
		) VALUES (?, '510500', '抓取中ETF', 'CN', 'cn_exchange_fund', 'equity', 'domestic', 'CNY',
			'akshare', '510500', 'none', 0, NULL, 'unavailable', 'embedded', 'pending_fetch', ?, ?)`,
		id, now, now); err != nil {
		t.Fatal(err)
	}

	resp := patchClassification(t, client, srv.URL, id, map[string]any{
		"asset_class": "bond", "region": "foreign", "expected_updated_at": now,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("pending fetch patch status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	assertErrorCode(t, readBody(t, resp), "instrument_fetch_in_progress")

	var assetClass, region string
	if err := db.QueryRowContext(context.Background(),
		`SELECT asset_class, region FROM instruments WHERE id=?`, id).Scan(&assetClass, &region); err != nil {
		t.Fatal(err)
	}
	if assetClass != "equity" || region != "domestic" {
		t.Fatalf("pending fetch classification must be unchanged, got %s/%s", assetClass, region)
	}
}
