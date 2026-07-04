package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
)

func postJSON(t *testing.T, client *http.Client, url string, body any) (*http.Response, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return resp, readBody(t, resp)
}

func getJSON(t *testing.T, client *http.Client, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return resp, readBody(t, resp)
}

func taskFromResult(t *testing.T, body []byte) (task map[string]any, existed bool) {
	t.Helper()
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	task, _ = data["task"].(map[string]any)
	if task == nil {
		t.Fatalf("response has no task: %s", body)
	}
	existed, _ = data["existed"].(bool)
	return task, existed
}

func taskPayload(t *testing.T, db *sql.DB, taskID string) map[string]any {
	t.Helper()
	var payloadJSON string
	if err := db.QueryRowContext(context.Background(),
		`SELECT payload_json FROM worker_tasks WHERE id=?`, taskID).Scan(&payloadJSON); err != nil {
		t.Fatalf("load task payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode task payload: %v", err)
	}
	return payload
}

func TestMarketAssetDirectorySync_CreatesAndDedupes(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	resp, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "cn_all"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", resp.StatusCode, body)
	}
	task, existed := taskFromResult(t, body)
	if existed {
		t.Fatal("first sync reported existed=true")
	}
	if task["type"] != "asset_directory_sync" || task["status"] != "pending" {
		t.Fatalf("unexpected task: %v", task)
	}
	taskID := task["id"].(string)

	payload := taskPayload(t, db, taskID)
	if payload["scope"] != "cn_all" {
		t.Fatalf("payload scope = %v", payload["scope"])
	}
	types := payload["instrument_types"].([]any)
	if len(types) != 3 {
		t.Fatalf("cn_all should require 3 instrument types, got %v", types)
	}

	// Duplicate request returns the existing active task without creating a
	// second one.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "cn_all"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dup sync status=%d body=%s", resp.StatusCode, body)
	}
	dupTask, dupExisted := taskFromResult(t, body)
	if !dupExisted || dupTask["id"] != taskID {
		t.Fatalf("dedupe failed: existed=%v id=%v want %s", dupExisted, dupTask["id"], taskID)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("worker_tasks rows = %d, want 1", count)
	}

	// sync state records the last task id for the scope.
	var lastTaskID string
	if err := db.QueryRow(
		`SELECT last_task_id FROM market_asset_sync_state WHERE scope='cn_all'`).Scan(&lastTaskID); err != nil {
		t.Fatal(err)
	}
	if lastTaskID != taskID {
		t.Fatalf("sync state last_task_id = %s, want %s", lastTaskID, taskID)
	}

	// Unknown scope is rejected.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "mars_all"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid scope status=%d body=%s", resp.StatusCode, body)
	}
}

func TestGetWorkerTask(t *testing.T) {
	srv, _, client := testRouterWithDB(t)

	_, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "hk_all"})
	task, _ := taskFromResult(t, body)
	taskID := task["id"].(string)

	resp, body := getJSON(t, client, srv.URL+"/api/v1/tasks/"+taskID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get task status=%d body=%s", resp.StatusCode, body)
	}
	got := decodeEnvelope(t, body)["data"].(map[string]any)
	if got["id"] != taskID || got["status"] != "pending" {
		t.Fatalf("unexpected task view: %v", got)
	}

	resp, body = getJSON(t, client, srv.URL+"/api/v1/tasks/wt_missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing task status=%d body=%s", resp.StatusCode, body)
	}
	if errBody := decodeErrorBody(t, body); errBody["code"] != "task_not_found" {
		t.Fatalf("missing task code = %v", errBody["code"])
	}
}

func TestListMarketAssets_SearchAndSyncBlock(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seedMarketAssetWithHistory(t, db, cnETFAssetSeed())
	seedMarketAssetWithHistory(t, db, hkStockAssetSeed())

	resp, body := getJSON(t, client, srv.URL+"/api/v1/market-assets")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	if got := len(data["assets"].([]any)); got != 2 {
		t.Fatalf("assets = %d, want 2", got)
	}
	if got := len(data["syncs"].([]any)); got != 3 {
		t.Fatalf("syncs should cover 3 scopes, got %d", got)
	}

	// Local search by name substring.
	resp, body = getJSON(t, client, srv.URL+"/api/v1/market-assets?q=510300")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search status=%d body=%s", resp.StatusCode, body)
	}
	data = decodeEnvelope(t, body)["data"].(map[string]any)
	assets := data["assets"].([]any)
	if len(assets) != 1 {
		t.Fatalf("search hits = %d, want 1", len(assets))
	}
	if assets[0].(map[string]any)["symbol"] != "510300" {
		t.Fatalf("unexpected search hit: %v", assets[0])
	}

	// Market filter narrows both assets and the syncs block.
	resp, body = getJSON(t, client, srv.URL+"/api/v1/market-assets?market=HK")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filter status=%d body=%s", resp.StatusCode, body)
	}
	data = decodeEnvelope(t, body)["data"].(map[string]any)
	if got := len(data["assets"].([]any)); got != 1 {
		t.Fatalf("HK assets = %d, want 1", got)
	}
	syncs := data["syncs"].([]any)
	if len(syncs) != 1 || syncs[0].(map[string]any)["scope"] != "hk_all" {
		t.Fatalf("HK syncs = %v", syncs)
	}
}

func TestHistorySync_ModesAndDedupe(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	// Unknown asset key is rejected before task creation.
	resp, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": "cn|cn_exchange_fund|sh|999999", "mode": "default_refresh"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown asset status=%d body=%s", resp.StatusCode, body)
	}

	// Asset without history: default_refresh derives a full replacement task.
	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund:sh:560000"
	seed.Symbol = "560000"
	seed.Points = nil
	seedMarketAssetWithHistory(t, db, seed)

	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": seed.AssetKey, "mode": "default_refresh"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty-history sync status=%d body=%s", resp.StatusCode, body)
	}
	task, _ := taskFromResult(t, body)
	payload := taskPayload(t, db, task["id"].(string))
	if payload["requested_range"] != "full" || payload["replacement_mode"] != "full" {
		t.Fatalf("empty history should trigger full refresh, payload=%v", payload)
	}
	if payload["allow_source_switch"] != true || payload["required_source_name"] != "" {
		t.Fatalf("full refresh should allow source fallback, payload=%v", payload)
	}

	// Asset with history: default_refresh derives a source-pinned merge with
	// start_date = data_as_of - 10 days.
	withHistory := cnETFAssetSeed()
	seedMarketAssetWithHistory(t, db, withHistory)

	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": withHistory.AssetKey, "mode": "default_refresh"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merge sync status=%d body=%s", resp.StatusCode, body)
	}
	mergeTask, _ := taskFromResult(t, body)
	mergeTaskID := mergeTask["id"].(string)
	payload = taskPayload(t, db, mergeTaskID)
	if payload["requested_range"] != "incremental" || payload["replacement_mode"] != "merge" {
		t.Fatalf("existing history should trigger merge, payload=%v", payload)
	}
	if payload["required_source_name"] != "test_fixture" {
		t.Fatalf("merge must pin the stored source, payload=%v", payload)
	}
	if payload["allow_source_switch"] != false {
		t.Fatalf("merge must not allow source switch, payload=%v", payload)
	}
	// Seed history ends at 2024-12-11; start date backs off 10 calendar days.
	if payload["start_date"] != "2024-12-01" {
		t.Fatalf("merge start_date = %v, want 2024-12-01", payload["start_date"])
	}

	// Duplicate default_refresh returns the same active task.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": withHistory.AssetKey, "mode": "default_refresh"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dup merge status=%d body=%s", resp.StatusCode, body)
	}
	dupTask, dupExisted := taskFromResult(t, body)
	if !dupExisted || dupTask["id"] != mergeTaskID {
		t.Fatalf("history dedupe failed: existed=%v id=%v", dupExisted, dupTask["id"])
	}

	// switch_source_full is rejected while no source_unavailable failure
	// exists.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": withHistory.AssetKey, "mode": "switch_source_full"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("premature switch status=%d body=%s", resp.StatusCode, body)
	}

	// Simulate the sidecar failing the merge task with source_unavailable;
	// the escape hatch then unlocks.
	if _, err := db.Exec(`
		UPDATE worker_tasks SET status='failed', error_code='source_unavailable',
			error_message='pinned source rejected the symbol'
		WHERE id=?`, mergeTaskID); err != nil {
		t.Fatal(err)
	}
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": withHistory.AssetKey, "mode": "switch_source_full"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch sync status=%d body=%s", resp.StatusCode, body)
	}
	switchTask, existed := taskFromResult(t, body)
	if existed {
		t.Fatal("switch task should be new")
	}
	payload = taskPayload(t, db, switchTask["id"].(string))
	if payload["requested_range"] != "full" || payload["replacement_mode"] != "full" ||
		payload["allow_source_switch"] != true || payload["required_source_name"] != "" {
		t.Fatalf("switch payload = %v", payload)
	}

	// Invalid mode is rejected.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/history-sync",
		map[string]any{"asset_key": withHistory.AssetKey, "mode": "yolo"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid mode status=%d body=%s", resp.StatusCode, body)
	}
}

func TestFXSync_CreatesTask(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	resp, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/fx-sync", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fx sync status=%d body=%s", resp.StatusCode, body)
	}
	task, existed := taskFromResult(t, body)
	if existed || task["type"] != "fx_rate_sync" {
		t.Fatalf("unexpected fx task: existed=%v task=%v", existed, task)
	}
	payload := taskPayload(t, db, task["id"].(string))
	pairs := payload["pairs"].([]any)
	if len(pairs) != 2 || pairs[0] != "HKDCNY" || pairs[1] != "USDCNY" {
		t.Fatalf("fx pairs = %v", pairs)
	}

	// Duplicate returns the existing active task.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/fx-sync", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dup fx status=%d body=%s", resp.StatusCode, body)
	}
	if _, dupExisted := taskFromResult(t, body); !dupExisted {
		t.Fatal("fx dedupe failed")
	}
}

func TestMarketAssetByKey_DetailAndErrors(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seedMarketAssetWithHistory(t, db, seed)

	resp, body := getJSON(t, client,
		srv.URL+"/api/v1/market-assets/by-key?asset_key="+seed.AssetKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("by-key status=%d body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	asset := data["asset"].(map[string]any)
	if asset["asset_key"] != seed.AssetKey {
		t.Fatalf("asset key = %v", asset["asset_key"])
	}
	history := data["history"].(map[string]any)
	if history["source_name"] != "test_fixture" {
		t.Fatalf("history source = %v", history["source_name"])
	}
	if int(history["point_count"].(float64)) != len(seed.Points) {
		t.Fatalf("point_count = %v, want %d", history["point_count"], len(seed.Points))
	}
	points := data["points"].([]any)
	if len(points) != len(seed.Points) {
		t.Fatalf("points = %d, want %d", len(points), len(seed.Points))
	}

	resp, body = getJSON(t, client, srv.URL+"/api/v1/market-assets/by-key?asset_key=missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing by-key status=%d body=%s", resp.StatusCode, body)
	}
	if errBody := decodeErrorBody(t, body); errBody["code"] != "market_asset_not_found" {
		t.Fatalf("missing by-key code = %v", errBody["code"])
	}
}
