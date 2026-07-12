package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestAdminAutoUpdateHistoryPaginationAndFilteredTotal(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	ctx := context.Background()
	assets := repository.NewSnapshotRepo(db)
	rules := repository.NewMarketDataAutoUpdateRepo(db)
	for index := 0; index < 101; index++ {
		assetKey := fmt.Sprintf("CN|test|sh|PAGE%03d", index)
		name := fmt.Sprintf("分页资产 %03d", index)
		if index < 3 {
			name = fmt.Sprintf("筛选专用 %03d", index)
		}
		if err := assets.EnsureMarketAsset(ctx, repository.MarketAsset{
			AssetKey: assetKey, Symbol: fmt.Sprintf("PAGE%03d", index), Name: name,
			Market: "CN", Currency: "CNY",
		}); err != nil {
			t.Fatalf("seed asset %d: %v", index, err)
		}
		if _, err := rules.EnableHistory(ctx, assetKey, "hfq", "adjusted_close", int64(index+1), 2_000); err != nil {
			t.Fatalf("seed rule %d: %v", index, err)
		}
	}

	resp, body := getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates?target_type=asset_history&limit=50&offset=100")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("last page status=%d body=%s", resp.StatusCode, body)
	}
	page := decodeEnvelope(t, body)["data"].(map[string]any)
	if page["total"] != float64(101) || page["limit"] != float64(50) || page["offset"] != float64(100) {
		t.Fatalf("last page metadata=%v", page)
	}
	if items := page["items"].([]any); len(items) != 1 {
		t.Fatalf("last page items=%d, want 1", len(items))
	}

	resp, body = getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates?target_type=asset_history&limit=50&q="+url.QueryEscape("筛选专用"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered status=%d body=%s", resp.StatusCode, body)
	}
	filtered := decodeEnvelope(t, body)["data"].(map[string]any)
	if filtered["total"] != float64(3) || len(filtered["items"].([]any)) != 3 {
		t.Fatalf("filtered page=%v", filtered)
	}
}

func putAutoUpdate(t *testing.T, client *http.Client, url string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, readBody(t, resp)
}

func TestHistoryAutoUpdateEnableAndAdminList(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seedMarketAssetWithHistory(t, db, seed)
	resp, body := getJSON(t, client, srv.URL+"/api/v1/market-assets/by-key?asset_key="+url.QueryEscape(seed.AssetKey))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", resp.StatusCode, body)
	}
	history := decodeEnvelope(t, body)["data"].(map[string]any)["history"].(map[string]any)
	if value, exists := history["auto_update"]; !exists || value != nil {
		t.Fatalf("auto_update before enable=%v exists=%v, want explicit null", value, exists)
	}
	status, body := putAutoUpdate(t, client, srv.URL+"/api/v1/market-assets/history-auto-update", map[string]any{"asset_key": seed.AssetKey, "adjust_policy": "hfq", "point_type": seed.PointType, "enabled": true})
	if status != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", status, body)
	}
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	if data["enabled"] != true || data["interval_hours"] != float64(24) {
		t.Fatalf("unexpected rule %v", data)
	}
	for _, camelCase := range []string{"Enabled", "IntervalHours", "SyncKey", "AssetKey"} {
		if _, exists := data[camelCase]; exists {
			t.Fatalf("response leaked Go field %q: %v", camelCase, data)
		}
	}
	resp, body = getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates?target_type=asset_history")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	items := decodeEnvelope(t, body)["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	page := decodeEnvelope(t, body)["data"].(map[string]any)
	if page["total"] != float64(1) || page["limit"] != float64(50) || page["offset"] != float64(0) {
		t.Fatalf("invalid page contract: %v", page)
	}
	resp, body = getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates?target_type=asset_history&q=%E6%B2%AA%E6%B7%B1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("name search status=%d body=%s", resp.StatusCode, body)
	}
	if got := len(decodeEnvelope(t, body)["data"].(map[string]any)["items"].([]any)); got != 1 {
		t.Fatalf("name search items=%d, want 1", got)
	}
}

func TestDirectoryAutoUpdateCreateSixHoursAndList(t *testing.T) {
	srv, _, client := testRouterWithDB(t)
	resp, body := postJSON(t, client, srv.URL+"/api/v1/admin/auto-updates/directories", map[string]any{
		"sync_key": "cn_exchange_stock", "interval_hours": 6,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, body)
	}
	created := decodeEnvelope(t, body)["data"].(map[string]any)
	if created["sync_key"] != "cn_exchange_stock" || created["enabled"] != true || created["interval_hours"] != float64(6) {
		t.Fatalf("unexpected created rule: %v", created)
	}

	resp, body = getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates?target_type=directory_unit")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	items := decodeEnvelope(t, body)["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", items)
	}
	listed := items[0].(map[string]any)
	if listed["enabled"] != true || listed["interval_hours"] != float64(6) {
		t.Fatalf("unexpected listed rule: %v", listed)
	}
}

func TestDirectoryAutoUpdateCatalogListsAllSupportedUnits(t *testing.T) {
	srv, _, client := testRouterWithDB(t)
	resp, body := getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates/directories")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	items := decodeEnvelope(t, body)["data"].([]any)
	if len(items) != 7 {
		t.Fatalf("units=%d, want 7", len(items))
	}
	first := items[0].(map[string]any)
	if first["sync_key"] != "cn_exchange_stock" || first["label"] != "A 股股票" {
		t.Fatalf("unexpected first unit: %v", first)
	}
}

func TestDirectoryAutoUpdateValidationAndVersionConflict(t *testing.T) {
	srv, _, client := testRouterWithDB(t)
	for _, body := range []map[string]any{
		{"sync_key": "unknown", "interval_hours": 24},
		{"sync_key": "hk_stock", "interval_hours": 0},
	} {
		resp, raw := postJSON(t, client, srv.URL+"/api/v1/admin/auto-updates/directories", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body=%v status=%d response=%s", body, resp.StatusCode, raw)
		}
	}
	resp, raw := postJSON(t, client, srv.URL+"/api/v1/admin/auto-updates/directories", map[string]any{"sync_key": "hk_stock", "interval_hours": 24})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, raw)
	}
	rule := decodeEnvelope(t, raw)["data"].(map[string]any)
	status, raw := putAutoUpdate(t, client, srv.URL+"/api/v1/admin/auto-updates/"+rule["id"].(string), map[string]any{
		"enabled": false, "interval_hours": 24, "version": 0,
	})
	if status != http.StatusConflict {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	if code := decodeErrorBody(t, raw)["code"]; code != "rule_version_conflict" {
		t.Fatalf("code=%v", code)
	}
}

func TestHistoryAutoUpdateRejectsUnsupportedDimension(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seedMarketAssetWithHistory(t, db, seed)
	status, body := putAutoUpdate(t, client, srv.URL+"/api/v1/market-assets/history-auto-update", map[string]any{
		"asset_key": seed.AssetKey, "adjust_policy": "none", "point_type": "nav", "enabled": true,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", status, body)
	}
	if code := decodeErrorBody(t, body)["code"]; code != "invalid_request" {
		t.Fatalf("code=%v", code)
	}

	status, body = putAutoUpdate(t, client, srv.URL+"/api/v1/market-assets/history-auto-update", map[string]any{
		"asset_key": seed.AssetKey, "adjust_policy": "qfq", "point_type": "adjusted_close", "enabled": true,
	})
	if status != http.StatusBadRequest {
		t.Fatalf("qfq status=%d body=%s", status, body)
	}
	if code := decodeErrorBody(t, body)["code"]; code != "unsupported_adjust_policy" {
		t.Fatalf("qfq code=%v", code)
	}
}

func TestHistoryAutoUpdateDimensionsAreUniqueAndIdempotent(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seedMarketAssetWithHistory(t, db, seed)
	endpoint := srv.URL + "/api/v1/market-assets/history-auto-update"
	for _, dimension := range []struct{ adjust, point string }{
		{adjust: "none", point: "close"},
		{adjust: "hfq", point: "adjusted_close"},
		{adjust: "none", point: "close"},
	} {
		status, body := putAutoUpdate(t, client, endpoint, map[string]any{
			"asset_key": seed.AssetKey, "adjust_policy": dimension.adjust,
			"point_type": dimension.point, "enabled": true,
		})
		if status != http.StatusOK {
			t.Fatalf("adjust=%s status=%d body=%s", dimension.adjust, status, body)
		}
	}
	resp, body := getJSON(t, client, srv.URL+"/api/v1/admin/auto-updates?target_type=asset_history")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	if data["total"] != float64(2) {
		t.Fatalf("total=%v, want 2", data["total"])
	}
}
