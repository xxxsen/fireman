package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"
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

func TestMarketAssetDirectorySync_ScopeInstrumentTypes(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	cases := map[string][]string{
		"hk_all": {"hk_stock", "hk_etf"},
		"us_all": {"us_stock", "us_etf"},
		"cn_all": {"cn_exchange_stock", "cn_exchange_fund", "cn_mutual_fund"},
	}
	for scope, wantTypes := range cases {
		resp, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
			map[string]any{"scope": scope})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("sync %s status=%d body=%s", scope, resp.StatusCode, body)
		}
		task, _ := taskFromResult(t, body)
		payload := taskPayload(t, db, task["id"].(string))
		got := map[string]bool{}
		for _, v := range payload["instrument_types"].([]any) {
			got[v.(string)] = true
		}
		if len(got) != len(wantTypes) {
			t.Fatalf("scope %s instrument_types = %v, want %v", scope, payload["instrument_types"], wantTypes)
		}
		for _, want := range wantTypes {
			if !got[want] {
				t.Fatalf("scope %s payload is missing instrument type %s (got %v)",
					scope, want, payload["instrument_types"])
			}
		}
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
	// 2 seeded assets plus 3 built-in system cash assets (SYS market).
	if got := len(data["assets"].([]any)); got != 5 {
		t.Fatalf("assets = %d, want 5", got)
	}
	if got := len(data["syncs"].([]any)); got != 3 {
		t.Fatalf("syncs should cover 3 scopes, got %d", got)
	}

	// Local search by symbol substring.
	resp, body = getJSON(t, client, srv.URL+"/api/v1/market-assets?symbol_q=510300")
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

	// Market filter narrows the asset list, but the syncs block stays fixed:
	// the UI sync panel always shows every directory scope.
	resp, body = getJSON(t, client, srv.URL+"/api/v1/market-assets?market=HK")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filter status=%d body=%s", resp.StatusCode, body)
	}
	data = decodeEnvelope(t, body)["data"].(map[string]any)
	if got := len(data["assets"].([]any)); got != 1 {
		t.Fatalf("HK assets = %d, want 1", got)
	}
	syncs := data["syncs"].([]any)
	if len(syncs) != 3 {
		t.Fatalf("syncs must stay fixed across filters, got %v", syncs)
	}
	scopes := map[string]bool{}
	for _, s := range syncs {
		scopes[s.(map[string]any)["scope"].(string)] = true
	}
	for _, want := range []string{"cn_all", "hk_all", "us_all"} {
		if !scopes[want] {
			t.Fatalf("syncs missing scope %s: %v", want, syncs)
		}
	}
}

// TestListMarketAssets_HistorySyncStatusForPendingAndFailed covers the picker
// states: an asset without local history whose latest history sync task failed
// (or is still running) must expose that through the listing API.
func TestListMarketAssets_HistorySyncStatusForPendingAndFailed(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()

	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund:sh:561000"
	seed.Symbol = "561000"
	seed.Points = nil
	seedMarketAssetWithHistory(t, db, seed)

	if _, err := db.ExecContext(ctx, `
		INSERT INTO worker_tasks (id, version_no, type, status, payload_json,
			error_code, error_message, created_at)
		VALUES ('task_failed_1', 1, 'asset_history_sync', 'failed', '{}',
			'provider_unreachable', 'upstream timed out', ?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO market_asset_history_state (asset_key, adjust_policy, point_type,
			last_task_id, last_success_task_id, data_as_of, point_count, source_name, updated_at)
		VALUES (?, 'none', 'adjusted_close', 'task_failed_1', '', '', 0, '', ?)`,
		seed.AssetKey, now); err != nil {
		t.Fatal(err)
	}

	resp, body := getJSON(t, client, srv.URL+"/api/v1/market-assets?symbol_q=561000")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	assets := data["assets"].([]any)
	if len(assets) != 1 {
		t.Fatalf("assets = %d, want 1", len(assets))
	}
	row := assets[0].(map[string]any)
	if row["has_history"] != false {
		t.Fatalf("has_history = %v, want false", row["has_history"])
	}
	if row["history_sync_status"] != "failed" {
		t.Fatalf("history_sync_status = %v, want failed", row["history_sync_status"])
	}
	if row["history_sync_error"] != "upstream timed out" {
		t.Fatalf("history_sync_error = %v", row["history_sync_error"])
	}

	// Flip the task to running: status is exposed, error cleared.
	if _, err := db.ExecContext(ctx,
		`UPDATE worker_tasks SET status='running', error_code='', error_message='' WHERE id='task_failed_1'`); err != nil {
		t.Fatal(err)
	}
	resp, body = getJSON(t, client, srv.URL+"/api/v1/market-assets?symbol_q=561000")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	data = decodeEnvelope(t, body)["data"].(map[string]any)
	row = data["assets"].([]any)[0].(map[string]any)
	if row["history_sync_status"] != "running" {
		t.Fatalf("history_sync_status = %v, want running", row["history_sync_status"])
	}
	if _, hasErr := row["history_sync_error"]; hasErr {
		t.Fatalf("history_sync_error should be omitted, got %v", row["history_sync_error"])
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
	var lastTaskID string
	if err := db.QueryRow(
		`SELECT last_task_id FROM market_asset_sync_state WHERE scope='fx_rates'`).Scan(&lastTaskID); err != nil {
		t.Fatal(err)
	}
	if lastTaskID != task["id"].(string) {
		t.Fatalf("fx sync last_task_id = %s, want %s", lastTaskID, task["id"].(string))
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
