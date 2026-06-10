package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireman/fireman/internal/marketdata"
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
	r := NewRouter(Deps{DB: db, Services: buildServices(db, providerURL)})
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
