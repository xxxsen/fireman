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

func buildServices(db *sql.DB) Services {
	return NewServices(db, "", nil)
}

func testRouterWithDB(t *testing.T) (*httptest.Server, *sql.DB, *http.Client) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client()
}

// marketAssetSeed describes one directory entry plus its synced history used
// by import tests. The td/078 import path performs no remote calls: it only
// projects already-synced market_asset_points into the instrument tables.
type marketAssetSeed struct {
	AssetKey       string
	Market         string
	InstrumentType string
	RegionCode     string
	Symbol         string
	Name           string
	InstrumentKind string
	Currency       string
	PointType      string
	Points         []marketdata.HistoricalPoint
}

func cnETFAssetSeed() marketAssetSeed {
	return marketAssetSeed{
		AssetKey: "cn:cn_exchange_fund:sh:510300", Market: "CN",
		InstrumentType: "cn_exchange_fund", RegionCode: "sh", Symbol: "510300",
		Name: "沪深300ETF", InstrumentKind: "etf", Currency: "CNY",
		PointType: "adjusted_close", Points: buildFixturePoints(),
	}
}

func hkStockAssetSeed() marketAssetSeed {
	return marketAssetSeed{
		AssetKey: "hk:hk_stock:00700", Market: "HK",
		InstrumentType: "hk_stock", Symbol: "00700",
		Name: "腾讯控股", InstrumentKind: "stock", Currency: "HKD",
		PointType: "adjusted_close", Points: buildTwentyYearFixturePoints(),
	}
}

// seedMarketAssetWithHistory is idempotent so a test can delete an imported
// instrument and import the same asset again.
func seedMarketAssetWithHistory(t *testing.T, db *sql.DB, seed marketAssetSeed) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO market_assets (
			asset_key, market, instrument_type, region_code, symbol, name, exchange,
			instrument_kind, currency, active, listing_status, last_seen_at,
			source_name, source_as_of, refreshed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, 1, 'active', ?, 'test_directory', '', ?, ?, ?)`,
		seed.AssetKey, seed.Market, seed.InstrumentType, seed.RegionCode, seed.Symbol,
		seed.Name, seed.InstrumentKind, seed.Currency, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if len(seed.Points) == 0 {
		return
	}
	for _, p := range seed.Points {
		if _, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO market_asset_points (
				asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at
			) VALUES (?, 'none', ?, ?, ?, 'test_fixture', ?)`,
			seed.AssetKey, seed.PointType, p.Date, p.Value, now); err != nil {
			t.Fatal(err)
		}
	}
	last := seed.Points[len(seed.Points)-1]
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO market_asset_history_state (
			asset_key, adjust_policy, point_type, last_task_id, last_success_task_id,
			last_success_at, data_as_of, point_count, source_name, updated_at
		) VALUES (?, 'none', ?, 'task_seed', 'task_seed', ?, ?, ?, 'test_fixture', ?)`,
		seed.AssetKey, seed.PointType, now, last.Date, len(seed.Points), now); err != nil {
		t.Fatal(err)
	}
}

func importMarketAsset(t *testing.T, client *http.Client, baseURL, assetKey string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"asset_key": assetKey})
	resp, err := client.Post(baseURL+"/api/v1/instruments/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, raw)
	}
	return decodeEnvelope(t, raw)["data"].(map[string]any)
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

func TestInstrumentImportFieldsReadOnly(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seedMarketAssetWithHistory(t, db, cnETFAssetSeed())

	body, _ := json.Marshal(map[string]any{
		"asset_key": "cn:cn_exchange_fund:sh:510300",
		"name":      "hack",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	assertErrorCode(t, raw, "instrument_fields_read_only")
}

func TestInstrumentImportFromMarketAsset(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seedMarketAssetWithHistory(t, db, cnETFAssetSeed())
	client = srv.Client()

	inst := importMarketAsset(t, client, srv.URL, "cn:cn_exchange_fund:sh:510300")
	id := inst["id"].(string)
	if inst["status"] != "active" {
		t.Fatalf("import status=%v want active", inst["status"])
	}
	if inst["asset_key"] != "cn:cn_exchange_fund:sh:510300" {
		t.Fatalf("asset_key=%v", inst["asset_key"])
	}
	if inst["code"] != "510300" || inst["name"] != "沪深300ETF" {
		t.Fatalf("identity=%v/%v", inst["code"], inst["name"])
	}

	resp, err := client.Get(srv.URL + "/api/v1/instruments/" + id)
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
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("annual status=%d", resp.StatusCode)
	}
	returns := decodeEnvelope(t, raw)["data"].(map[string]any)["annual_returns"].([]any)
	if len(returns) == 0 {
		t.Fatal("expected annual returns projected at import time")
	}
}

func TestInstrumentImportDuplicateRejected(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seedMarketAssetWithHistory(t, db, hkStockAssetSeed())

	first := importMarketAsset(t, client, srv.URL, "hk:hk_stock:00700")
	if first["code"] != "00700" {
		t.Fatalf("imported code=%v want 00700", first["code"])
	}
	if first["currency"] != "HKD" {
		t.Fatalf("imported currency=%v want HKD", first["currency"])
	}
	if first["region"] != "foreign" {
		t.Fatalf("imported region=%v want foreign", first["region"])
	}

	body, _ := json.Marshal(map[string]any{"asset_key": "hk:hk_stock:00700"})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate import status=%d body=%s", resp.StatusCode, raw)
	}
	assertErrorCode(t, raw, "instrument_already_exists")
}

func TestInstrumentImportUnknownAssetRejected(t *testing.T) {
	srv, _, client := testRouterWithDB(t)

	body, _ := json.Marshal(map[string]any{"asset_key": "cn:cn_exchange_fund:sh:999999"})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	assertErrorCode(t, raw, "market_asset_not_found")
}

func TestInstrumentImportWithoutHistoryRejected(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seed.Points = nil
	seedMarketAssetWithHistory(t, db, seed)

	body, _ := json.Marshal(map[string]any{"asset_key": seed.AssetKey})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	assertErrorCode(t, raw, "market_asset_history_empty")
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
	srv, db, client := testRouterWithDB(t)

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
