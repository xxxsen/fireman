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
		if r.URL.Path != "/v1/instruments/fetch" {
			http.NotFound(w, r)
			return
		}
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
		if r.URL.Path != "/v1/instruments/fetch" {
			http.NotFound(w, r)
			return
		}
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

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	return mustRead(t, resp)
}
