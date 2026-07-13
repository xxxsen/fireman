package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var marketAssetTaskVersion atomic.Int64

type testResponse struct {
	StatusCode int
}

func postJSON(t *testing.T, client *http.Client, url string, body any) (testResponse, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return testResponse{StatusCode: resp.StatusCode}, readBody(t, resp)
}

func getJSON(t *testing.T, client *http.Client, url string) (testResponse, []byte) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return testResponse{StatusCode: resp.StatusCode}, readBody(t, resp)
}

func taskFromResult(t *testing.T, body []byte) (map[string]any, bool) {
	t.Helper()
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	task, _ := data["task"].(map[string]any)
	if task == nil {
		t.Fatalf("response has no task: %s", body)
	}
	existed, _ := data["existed"].(bool)
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

// syncTasksFromResult decodes the POST /market-assets/sync response: the
// scope plus one entry per directory sync unit.
func syncTasksFromResult(t *testing.T, body []byte) (string, []map[string]any) {
	t.Helper()
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	scope, _ := data["scope"].(string)
	rawTasks, ok := data["tasks"].([]any)
	if !ok {
		t.Fatalf("response has no tasks array: %s", body)
	}
	tasks := make([]map[string]any, 0, len(rawTasks))
	for _, rt := range rawTasks {
		tasks = append(tasks, rt.(map[string]any))
	}
	return scope, tasks
}

func syncTaskID(item map[string]any) string {
	return item["task"].(map[string]any)["id"].(string)
}

func TestMarketAssetDirectorySync_ScopeCreatesUnitTasks(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	resp, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "cn_all"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", resp.StatusCode, body)
	}
	scope, tasks := syncTasksFromResult(t, body)
	if scope != "cn_all" || len(tasks) != 3 {
		t.Fatalf("scope=%s tasks=%d, want cn_all with 3 unit tasks", scope, len(tasks))
	}
	wantUnits := []string{"cn_exchange_stock", "cn_exchange_fund", "cn_mutual_fund"}
	for i, want := range wantUnits {
		item := tasks[i]
		if item["sync_key"] != want {
			t.Fatalf("tasks[%d].sync_key = %v, want %s", i, item["sync_key"], want)
		}
		if item["existed"] != false {
			t.Fatalf("tasks[%d].existed = %v, want false", i, item["existed"])
		}
		if item["label"] == "" {
			t.Fatalf("tasks[%d] has no label", i)
		}
		task := item["task"].(map[string]any)
		if task["type"] != "asset_directory_sync" || task["status"] != "pending" {
			t.Fatalf("tasks[%d].task = %v", i, task)
		}
		// Dedupe key is asset_directory_sync|{sync_key}.
		var dedupeKey string
		if err := db.QueryRow(`SELECT dedupe_key FROM worker_tasks WHERE id=?`,
			task["id"]).Scan(&dedupeKey); err != nil {
			t.Fatal(err)
		}
		if dedupeKey != "asset_directory_sync|"+want {
			t.Fatalf("dedupe_key = %s, want asset_directory_sync|%s", dedupeKey, want)
		}
		// Payload carries sync_key and registry markets/instrument_types.
		payload := taskPayload(t, db, task["id"].(string))
		if payload["sync_key"] != want || payload["scope"] != "cn_all" {
			t.Fatalf("payload = %v", payload)
		}
		types := payload["instrument_types"].([]any)
		if len(types) != 1 || types[0] != want {
			t.Fatalf("payload instrument_types = %v, want [%s]", types, want)
		}
		// Each unit tracks its own sync-state row grouped under the scope.
		var lastTaskID, stateScope string
		if err := db.QueryRow(
			`SELECT last_task_id, scope FROM market_asset_sync_state WHERE sync_key=?`,
			want,
		).Scan(&lastTaskID, &stateScope); err != nil {
			t.Fatal(err)
		}
		if lastTaskID != task["id"] || stateScope != "cn_all" {
			t.Fatalf("sync state for %s = (%s,%s)", want, lastTaskID, stateScope)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("worker_tasks rows = %d, want 3", count)
	}

	// Unknown scope is rejected.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "mars_all"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid scope status=%d body=%s", resp.StatusCode, body)
	}
}

func TestMarketAssetDirectorySync_SingleUnitAndDedupe(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	// sync_key creates exactly one unit task.
	resp, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"sync_key": "cn_exchange_fund"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unit sync status=%d body=%s", resp.StatusCode, body)
	}
	scope, tasks := syncTasksFromResult(t, body)
	if scope != "cn_all" || len(tasks) != 1 || tasks[0]["sync_key"] != "cn_exchange_fund" {
		t.Fatalf("scope=%s tasks=%v", scope, tasks)
	}
	fundTaskID := syncTaskID(tasks[0])

	// Re-syncing the active unit returns existed=true without inserting.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"sync_key": "cn_exchange_fund"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dup unit sync status=%d body=%s", resp.StatusCode, body)
	}
	_, tasks = syncTasksFromResult(t, body)
	if tasks[0]["existed"] != true || syncTaskID(tasks[0]) != fundTaskID {
		t.Fatalf("unit dedupe failed: %v", tasks[0])
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("worker_tasks rows = %d, want 1", count)
	}

	// A force request changes the task input and must wait for the active
	// non-force request rather than silently changing its semantics.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"sync_key": "cn_exchange_fund", "force": true})
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "task_already_active") {
		t.Fatalf("different-input sync status=%d body=%s", resp.StatusCode, body)
	}

	// Batch sync while one unit is active: the active unit is returned as
	// existed, the sibling units still get new tasks.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "cn_all"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("batch sync status=%d body=%s", resp.StatusCode, body)
	}
	_, tasks = syncTasksFromResult(t, body)
	if len(tasks) != 3 {
		t.Fatalf("batch tasks = %d, want 3", len(tasks))
	}
	byKey := map[string]map[string]any{}
	for _, item := range tasks {
		byKey[item["sync_key"].(string)] = item
	}
	if byKey["cn_exchange_fund"]["existed"] != true ||
		syncTaskID(byKey["cn_exchange_fund"]) != fundTaskID {
		t.Fatalf("active unit not deduped in batch: %v", byKey["cn_exchange_fund"])
	}
	if byKey["cn_exchange_stock"]["existed"] != false ||
		byKey["cn_mutual_fund"]["existed"] != false {
		t.Fatalf("sibling units were not created: %v", tasks)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_tasks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("worker_tasks rows after batch = %d, want 3", count)
	}

	// Unknown sync_key and scope mismatch are rejected.
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"sync_key": "moon_etf"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid sync_key status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"scope": "hk_all", "sync_key": "cn_exchange_fund"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("scope mismatch status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = postJSON(t, client, srv.URL+"/api/v1/market-assets/sync", map[string]any{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty request status=%d body=%s", resp.StatusCode, body)
	}
}

func TestGetWorkerTask(t *testing.T) {
	srv, _, client := testRouterWithDB(t)

	_, body := postJSON(t, client, srv.URL+"/api/v1/market-assets/sync",
		map[string]any{"sync_key": "hk_stock"})
	_, tasks := syncTasksFromResult(t, body)
	taskID := syncTaskID(tasks[0])

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

// The backend is the single source of truth for instrument-type presentation:
// every listed asset must carry the Chinese label and ordering priority so the
// web pickers never re-derive them.
func TestListMarketAssets_InstrumentTypeLabelAndPriority(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	seedMarketAssetWithHistory(t, db, cnETFAssetSeed())
	seedMarketAssetWithHistory(t, db, hkStockAssetSeed())

	resp, body := getJSON(t, client, srv.URL+"/api/v1/market-assets")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelope(t, body)["data"].(map[string]any)
	want := map[string]struct {
		label    string
		priority float64
	}{
		"cn_exchange_fund": {"场内 ETF / LOF", 1},
		"hk_stock":         {"港股", 3},
	}
	checked := 0
	for _, raw := range data["assets"].([]any) {
		asset := raw.(map[string]any)
		typ := asset["instrument_type"].(string)
		label, ok := asset["instrument_type_label"].(string)
		if !ok || label == "" {
			t.Fatalf("asset %v missing instrument_type_label", asset["asset_key"])
		}
		priority, ok := asset["instrument_type_priority"].(float64)
		if !ok {
			t.Fatalf("asset %v missing instrument_type_priority", asset["asset_key"])
		}
		expect, known := want[typ]
		if !known {
			continue
		}
		if label != expect.label || priority != expect.priority {
			t.Fatalf("type %s got (%q, %v), want (%q, %v)",
				typ, label, priority, expect.label, expect.priority)
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("expected both seeded asset types checked, got %d", checked)
	}
}

// TestListMarketAssets_ScopeAggregation covers the directory scope status rules:
// partial while only some units succeeded, complete (with min success time)
// once every unit succeeded, running with an active unit task and failed when
// nothing ever succeeded and the latest task failed.
func TestListMarketAssets_ScopeAggregation(t *testing.T) {
	srv, db, client := testRouterWithDB(t)

	seedState := func(syncKey, scope, lastTaskID string, lastSuccessAt *int64) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO market_asset_sync_state
				(sync_key, scope, last_task_id, last_success_task_id, last_success_at, updated_at)
			VALUES (?,?,?,?,?,1)
			ON CONFLICT(sync_key) DO UPDATE SET
				last_task_id=excluded.last_task_id,
				last_success_task_id=excluded.last_success_task_id,
				last_success_at=excluded.last_success_at`,
			syncKey, scope, lastTaskID, lastTaskID, lastSuccessAt); err != nil {
			t.Fatal(err)
		}
	}
	seedTask := func(id, status string) {
		t.Helper()
		if _, err := db.Exec(`
			INSERT INTO worker_tasks (id, version_no, worker_type, type, status, dedupe_key,
				payload_json, available_at, created_at, updated_at)
			VALUES (?,?,'sidecar_worker','asset_directory_sync',?,?, '{}', 1,1,1)`,
			id, marketAssetTaskVersion.Add(1), status, "asset_directory_sync|"+id); err != nil {
			t.Fatal(err)
		}
	}
	scopeView := func(scope string) map[string]any {
		t.Helper()
		resp, body := getJSON(t, client, srv.URL+"/api/v1/market-assets")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list status=%d body=%s", resp.StatusCode, body)
		}
		for _, s := range decodeEnvelope(t, body)["data"].(map[string]any)["syncs"].([]any) {
			view := s.(map[string]any)
			if view["scope"] == scope {
				return view
			}
		}
		t.Fatalf("scope %s missing from syncs", scope)
		return nil
	}

	// Nothing seeded: never.
	if got := scopeView("cn_all"); got["status"] != "never" {
		t.Fatalf("empty scope status = %v, want never", got["status"])
	}

	// 2 of 3 CN units succeeded: partial, no aggregate last_success_at.
	t100, t200 := int64(100), int64(200)
	seedState("cn_exchange_stock", "cn_all", "", &t200)
	seedState("cn_exchange_fund", "cn_all", "", &t100)
	view := scopeView("cn_all")
	if view["status"] != "partial" {
		t.Fatalf("2/3 success status = %v, want partial", view["status"])
	}
	if _, ok := view["last_success_at"]; ok {
		t.Fatalf("partial scope must not expose last_success_at, got %v", view["last_success_at"])
	}
	units := view["units"].([]any)
	if len(units) != 3 {
		t.Fatalf("cn_all units = %d, want 3", len(units))
	}
	first := units[0].(map[string]any)
	if first["sync_key"] != "cn_exchange_stock" || first["label"] == "" {
		t.Fatalf("units[0] = %v", first)
	}

	// All units succeeded: complete with the minimum success time.
	t300 := int64(300)
	seedState("cn_mutual_fund", "cn_all", "", &t300)
	view = scopeView("cn_all")
	if view["status"] != "complete" {
		t.Fatalf("3/3 success status = %v, want complete", view["status"])
	}
	if view["last_success_at"].(float64) != 100 {
		t.Fatalf("aggregate last_success_at = %v, want min(100)", view["last_success_at"])
	}

	// An active unit task flips the scope to running.
	seedTask("wt_run", "running")
	seedState("cn_exchange_fund", "cn_all", "wt_run", &t100)
	view = scopeView("cn_all")
	if view["status"] != "running" {
		t.Fatalf("active unit status = %v, want running", view["status"])
	}

	// Failed with no successes at all: hk_all with one failed latest task.
	seedTask("wt_fail", "failed")
	seedState("hk_stock", "hk_all", "wt_fail", nil)
	view = scopeView("hk_all")
	if view["status"] != "failed" {
		t.Fatalf("failed-only scope status = %v, want failed", view["status"])
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
		INSERT INTO worker_tasks (id, version_no, worker_type, type, status, payload_json,
			error_code, error_message, available_at, created_at, updated_at)
		VALUES ('task_failed_1', ?, 'sidecar_worker', 'asset_history_sync', 'failed', '{}',
			'provider_unreachable', 'upstream timed out', ?,?,?)`,
		marketAssetTaskVersion.Add(1), now, now, now); err != nil {
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
		`SELECT last_task_id FROM market_asset_sync_state WHERE scope='fx_rates'`,
	).Scan(&lastTaskID); err != nil {
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
