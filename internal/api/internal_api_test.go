package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

// internalStack wires the internal (sidecar-facing) listener exactly like
// app.Run: main DB + resource DB + post-process service.
type internalStack struct {
	srv    *httptest.Server
	db     *sql.DB
	assets *service.MarketAssetService
	client *http.Client
}

func newInternalStack(t *testing.T) internalStack {
	t.Helper()
	db := testutil.OpenTestDB(t)
	resources, err := resourcedb.Open(context.Background(), filepath.Join(t.TempDir(), "resource.db"))
	if err != nil {
		t.Fatalf("open resource db: %v", err)
	}
	t.Cleanup(func() { _ = resources.Close() })

	taskRepo := repository.NewWorkerTaskRepo(db)
	assetRepo := repository.NewMarketAssetRepo(db)
	postProcess := service.NewPostProcessService(
		db, taskRepo, assetRepo,
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
		resources,
		repository.NewPostProcessRecordRepo(db),
	)
	srv := httptest.NewServer(NewInternalRouter(InternalDeps{
		PostProcess: postProcess, Resources: resources,
	}))
	t.Cleanup(srv.Close)
	return internalStack{
		srv:    srv,
		db:     db,
		assets: service.NewMarketAssetService(db, taskRepo, assetRepo),
		client: srv.Client(),
	}
}

// uploadResult mimics the sidecar upload: gzip the payload, declare its
// sha256 and return the envelope Go handed back.
func uploadResult(t *testing.T, st internalStack, raw []byte) resourcedb.Envelope {
	t.Helper()
	gz, err := resourcedb.GzipBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(gz)
	req, err := http.NewRequest(http.MethodPost, st.srv.URL+"/internal/resources", bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Fireman-Content-Type", "application/json")
	req.Header.Set("X-Fireman-Content-Encoding", "gzip")
	req.Header.Set("X-Fireman-Schema-Version", "1")
	req.Header.Set("X-Fireman-Content-SHA256", hex.EncodeToString(sum[:]))
	resp, err := st.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Data resourcedb.Envelope `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode upload response: %v body=%s", err, body)
	}
	if out.Data.ResourceKey != hex.EncodeToString(sum[:]) {
		t.Fatalf("resource_key = %s, want payload sha256", out.Data.ResourceKey)
	}
	return out.Data
}

// markPreComplete simulates the sidecar finishing execution: stores the
// resource envelope in result_data and flips the task to pre_complete.
func markPreComplete(t *testing.T, db *sql.DB, taskID string, env resourcedb.Envelope) {
	t.Helper()
	envJSON, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		UPDATE worker_tasks SET status='pre_complete', result_data=?, pre_completed_at=?
		WHERE id=?`, string(envJSON), time.Now().UnixMilli(), taskID); err != nil {
		t.Fatal(err)
	}
}

// finishTask simulates the sidecar's terminal CAS after post-process.
func finishTask(t *testing.T, db *sql.DB, taskID, status string) {
	t.Helper()
	if _, err := db.Exec(`
		UPDATE worker_tasks SET status=?, finished_at=? WHERE id=?`,
		status, time.Now().UnixMilli(), taskID); err != nil {
		t.Fatal(err)
	}
}

func notifyPostProcess(t *testing.T, st internalStack, taskID string) map[string]any {
	t.Helper()
	resp, err := st.client.Post(
		st.srv.URL+"/internal/tasks/"+taskID+"/post-process", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-process status=%d body=%s", resp.StatusCode, body)
	}
	return decodeEnvelope(t, body)["data"].(map[string]any)
}

func assertOutcome(t *testing.T, got map[string]any, wantResult, wantCode string) {
	t.Helper()
	if got["result"] != wantResult {
		t.Fatalf("result = %v (code=%v message=%v), want %s",
			got["result"], got["error_code"], got["error_message"], wantResult)
	}
	if wantCode != "" && got["error_code"] != wantCode {
		t.Fatalf("error_code = %v, want %s (message=%v)",
			got["error_code"], wantCode, got["error_message"])
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return n
}

func TestInternalResourceUpload_ChecksumAndValidation(t *testing.T) {
	st := newInternalStack(t)

	gz, err := resourcedb.GzipBytes([]byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}

	// Declared checksum that does not match the body is rejected before any
	// write.
	req, _ := http.NewRequest(http.MethodPost, st.srv.URL+"/internal/resources", bytes.NewReader(gz))
	req.Header.Set("X-Fireman-Content-SHA256", "deadbeef")
	resp, err := st.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad checksum status=%d body=%s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "resource_checksum_mismatch")

	// Empty body is rejected.
	resp, err = st.client.Post(st.srv.URL+"/internal/resources", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body status=%d body=%s", resp.StatusCode, body)
	}

	// Valid upload twice: identical envelope, single stored row (idempotent
	// retry).
	env1 := uploadResult(t, st, []byte(`{"ok":true}`))
	env2 := uploadResult(t, st, []byte(`{"ok":true}`))
	if env1.ResourceKey != env2.ResourceKey {
		t.Fatalf("idempotent upload changed key: %s vs %s", env1.ResourceKey, env2.ResourceKey)
	}
}

// syncUnitTask creates (or returns) the directory task for one sync unit and
// returns its task id.
func syncUnitTask(t *testing.T, st internalStack, syncKey string) string {
	t.Helper()
	created, err := st.assets.SyncDirectory(context.Background(),
		service.DirectorySyncRequest{SyncKey: syncKey})
	if err != nil {
		t.Fatalf("create %s sync task: %v", syncKey, err)
	}
	if len(created.Tasks) != 1 || created.Tasks[0].SyncKey != syncKey {
		t.Fatalf("sync %s tasks = %+v, want exactly one", syncKey, created.Tasks)
	}
	return created.Tasks[0].Task.ID
}

func directoryUnitResult(syncKey, scope string, assets []map[string]any) []byte {
	raw, _ := json.Marshal(map[string]any{
		"type": "asset_directory_sync", "sync_key": syncKey, "scope": scope,
		"assets": assets,
	})
	return raw
}

func TestInternalPostProcess_DirectoryLifecycle(t *testing.T) {
	st := newInternalStack(t)

	// A previously-known asset that the new listing no longer contains; it
	// must be marked inactive after the sync commits.
	staleSeen := time.Now().Add(-24 * time.Hour).UnixMilli()
	if _, err := st.db.Exec(`
		INSERT INTO market_assets (
			asset_key, market, instrument_type, region_code, symbol, name, exchange,
			instrument_kind, currency, active, listing_status, last_seen_at,
			source_name, source_as_of, refreshed_at, created_at, updated_at
		) VALUES ('hk:hk_stock:09999','HK','hk_stock','','09999','已退市股票','',
			'stock','HKD',1,'active',?, 'ak_hk','',?,?,?)`,
		staleSeen, staleSeen, staleSeen, staleSeen); err != nil {
		t.Fatal(err)
	}

	taskID := syncUnitTask(t, st, "hk_stock")
	raw := directoryUnitResult("hk_stock", "hk_all", []map[string]any{
		{"market": "HK", "instrument_type": "hk_stock", "symbol": "00700",
			"name": "腾讯控股", "instrument_kind": "stock", "currency": "HKD",
			"source_name": "ak_hk", "source_as_of": "2026-07-04"},
		{"market": "HK", "instrument_type": "hk_stock", "symbol": "00005",
			"name": "汇丰控股", "instrument_kind": "stock", "currency": "HKD",
			"source_name": "ak_hk", "source_as_of": "2026-07-04"},
		// Out-of-unit entries must be ignored, never written.
		{"market": "HK", "instrument_type": "hk_etf", "symbol": "02800",
			"name": "盈富基金", "instrument_kind": "etf", "currency": "HKD",
			"source_name": "ak_hk_fund", "source_as_of": "2026-07-04"},
		{"market": "CN", "instrument_type": "cn_exchange_stock", "symbol": "600000",
			"name": "浦发银行", "currency": "CNY", "source_name": "ak_cn", "source_as_of": ""},
	})
	markPreComplete(t, st.db, taskID, uploadResult(t, st, raw))

	assertOutcome(t, notifyPostProcess(t, st, taskID), "success", "")

	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='hk_stock' AND active=1`); n != 2 {
		t.Fatalf("active hk_stock assets = %d, want 2", n)
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE symbol='09999' AND active=0`); n != 1 {
		t.Fatal("unseen asset was not marked inactive")
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='hk_etf'`); n != 0 {
		t.Fatal("out-of-unit hk_etf entry was written")
	}
	if n := countRows(t, st.db, `SELECT COUNT(*) FROM market_assets WHERE market='CN'`); n != 0 {
		t.Fatal("out-of-unit CN entry was written")
	}

	// Sync state and data version live at the unit granularity; no scope
	// aggregate rows are written.
	var lastSuccessTaskID, scope string
	if err := st.db.QueryRow(`
		SELECT last_success_task_id, scope FROM market_asset_sync_state
		WHERE sync_key='hk_stock'`).
		Scan(&lastSuccessTaskID, &scope); err != nil {
		t.Fatal(err)
	}
	if lastSuccessTaskID != taskID || scope != "hk_all" {
		t.Fatalf("sync state = (%s,%s), want (%s,hk_all)", lastSuccessTaskID, scope, taskID)
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_sync_state WHERE sync_key='hk_all'`); n != 0 {
		t.Fatal("scope aggregate row must not be written to sync state")
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_data_versions WHERE version_key='asset_directory|hk_stock'`); n != 1 {
		t.Fatal("unit data version was not written")
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_data_versions WHERE version_key='asset_directory|hk_all'`); n != 0 {
		t.Fatal("scope-level data version must not be written")
	}

	// Duplicate notification while still pre_complete: reentrant success, no
	// duplicate writes.
	assertOutcome(t, notifyPostProcess(t, st, taskID), "success", "")
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='hk_stock' AND active=1`); n != 2 {
		t.Fatalf("re-notify duplicated writes: active hk_stock assets = %d", n)
	}

	// Lost success response: task already complete, re-notify still succeeds.
	finishTask(t, st.db, taskID, "complete")
	assertOutcome(t, notifyPostProcess(t, st, taskID), "success", "")

	// Coverage gate: a later sync returning below 90% of the previous count
	// for a same-source category is rejected without touching the directory.
	shrunkTaskID := syncUnitTask(t, st, "hk_stock")
	shrunk := directoryUnitResult("hk_stock", "hk_all", []map[string]any{
		{"market": "HK", "instrument_type": "hk_stock", "symbol": "00700",
			"name": "腾讯控股", "currency": "HKD", "source_name": "ak_hk", "source_as_of": ""},
	})
	markPreComplete(t, st.db, shrunkTaskID, uploadResult(t, st, shrunk))
	assertOutcome(t, notifyPostProcess(t, st, shrunkTaskID),
		"permanent_error", "directory_data_incomplete")
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='hk_stock' AND active=1`); n != 2 {
		t.Fatalf("failed sync mutated the directory: active hk_stock assets = %d", n)
	}
	finishTask(t, st.db, shrunkTaskID, "failed")

	// Listing-source migration: the same category served by a brand-new
	// source compares against zero previous rows (first-sync semantics), so
	// a smaller snapshot still commits instead of tripping the 90% gate.
	migratedTaskID := syncUnitTask(t, st, "hk_stock")
	migrated := directoryUnitResult("hk_stock", "hk_all", []map[string]any{
		{"market": "HK", "instrument_type": "hk_stock", "symbol": "00700",
			"name": "腾讯控股", "instrument_kind": "stock", "currency": "HKD",
			"source_name": "em.hk_equity_list", "source_as_of": ""},
	})
	markPreComplete(t, st.db, migratedTaskID, uploadResult(t, st, migrated))
	assertOutcome(t, notifyPostProcess(t, st, migratedTaskID), "success", "")
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='hk_stock' AND active=1`); n != 1 {
		t.Fatalf("migrated sync active hk_stock assets = %d, want 1", n)
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE symbol='00005' AND active=0`); n != 1 {
		t.Fatal("asset absent from the migrated listing was not marked inactive")
	}
}

// TestInternalPostProcess_DirectoryUnitIsolation covers the 090 split: one
// unit's success only touches its own version key, sync state and
// market+instrument_type rows; sibling units of the same scope are untouched
// and a failed sibling does not block the successful unit.
func TestInternalPostProcess_DirectoryUnitIsolation(t *testing.T) {
	st := newInternalStack(t)

	// Existing assets in two sibling CN units.
	seen := time.Now().Add(-24 * time.Hour).UnixMilli()
	for _, row := range []struct{ key, itype, symbol string }{
		{"CN|cn_exchange_stock|sh|600000", "cn_exchange_stock", "600000"},
		{"CN|cn_mutual_fund||000001", "cn_mutual_fund", "000001"},
	} {
		if _, err := st.db.Exec(`
			INSERT INTO market_assets (
				asset_key, market, instrument_type, region_code, symbol, name, exchange,
				instrument_kind, currency, active, listing_status, last_seen_at,
				source_name, source_as_of, refreshed_at, created_at, updated_at
			) VALUES (?,'CN',?,'',?,?,'','','CNY',1,'active',?,'ak_cn','',?,?,?)`,
			row.key, row.itype, row.symbol, row.symbol, seen, seen, seen, seen); err != nil {
			t.Fatal(err)
		}
	}

	fundTaskID := syncUnitTask(t, st, "cn_exchange_fund")
	fund := directoryUnitResult("cn_exchange_fund", "cn_all", []map[string]any{
		{"market": "CN", "instrument_type": "cn_exchange_fund", "region_code": "sh",
			"symbol": "510300", "name": "沪深300ETF", "exchange": "SH",
			"instrument_kind": "etf", "currency": "CNY",
			"source_name": "em.cn_etf_list", "source_as_of": "2026-07-05"},
	})
	markPreComplete(t, st.db, fundTaskID, uploadResult(t, st, fund))
	assertOutcome(t, notifyPostProcess(t, st, fundTaskID), "success", "")

	// Sibling units' assets keep active=1: MarkUnseenInactive is unit-scoped.
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='cn_exchange_stock' AND active=1`); n != 1 {
		t.Fatal("cn_exchange_stock rows were touched by a cn_exchange_fund sync")
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE instrument_type='cn_mutual_fund' AND active=1`); n != 1 {
		t.Fatal("cn_mutual_fund rows were touched by a cn_exchange_fund sync")
	}
	// Only the unit's version key exists.
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_data_versions WHERE version_key='asset_directory|cn_exchange_fund'`); n != 1 {
		t.Fatal("cn_exchange_fund version key missing")
	}
	if n := countRows(t, st.db, `
		SELECT COUNT(*) FROM market_data_versions
		WHERE version_key IN ('asset_directory|cn_all','asset_directory|cn_exchange_stock')`); n != 0 {
		t.Fatal("sibling/scope version keys must not be written")
	}
	// Only the unit's sync state row exists.
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_sync_state WHERE sync_key='cn_exchange_fund' AND scope='cn_all'`); n != 1 {
		t.Fatal("cn_exchange_fund sync state row missing")
	}
	finishTask(t, st.db, fundTaskID, "complete")

	// A failed sibling unit does not roll back the committed unit.
	stockTaskID := syncUnitTask(t, st, "cn_exchange_stock")
	empty := directoryUnitResult("cn_exchange_stock", "cn_all", []map[string]any{})
	markPreComplete(t, st.db, stockTaskID, uploadResult(t, st, empty))
	assertOutcome(t, notifyPostProcess(t, st, stockTaskID),
		"permanent_error", "directory_data_incomplete")
	finishTask(t, st.db, stockTaskID, "failed")
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_assets WHERE symbol='510300' AND active=1`); n != 1 {
		t.Fatal("failed sibling rolled back the committed unit")
	}

	// sync_key mismatch between result and payload is a permanent error.
	mismatchTaskID := syncUnitTask(t, st, "cn_mutual_fund")
	mismatch := directoryUnitResult("cn_exchange_fund", "cn_all", []map[string]any{
		{"market": "CN", "instrument_type": "cn_mutual_fund", "symbol": "000001",
			"name": "华夏成长", "currency": "CNY",
			"source_name": "ak.fund_name_em", "source_as_of": ""},
	})
	markPreComplete(t, st.db, mismatchTaskID, uploadResult(t, st, mismatch))
	assertOutcome(t, notifyPostProcess(t, st, mismatchTaskID),
		"permanent_error", "result_task_mismatch")
}

func historySyncResult(assetKey, source string, dates []string, values []float64) []byte {
	points := make([]map[string]any, len(dates))
	for i, d := range dates {
		points[i] = map[string]any{"date": d, "value": values[i]}
	}
	raw, _ := json.Marshal(map[string]any{
		"type":          "asset_history_sync",
		"asset_key":     assetKey,
		"adjust_policy": "none",
		"point_type":    "adjusted_close",
		"source_name":   source,
		"points":        points,
	})
	return raw
}

func sequentialDates(start string, n int) []string {
	t0, _ := time.Parse("2006-01-02", start)
	out := make([]string, n)
	for i := range out {
		out[i] = t0.AddDate(0, 0, i).Format("2006-01-02")
	}
	return out
}

func TestInternalPostProcess_HistoryFullMergeAndGap(t *testing.T) {
	st := newInternalStack(t)
	ctx := context.Background()

	seed := cnETFAssetSeed()
	seed.Points = nil // directory entry only; history arrives via the task
	seedMarketAssetWithHistory(t, st.db, seed)
	assetKey := seed.AssetKey

	// 1) Full replacement (first sync).
	created, err := st.assets.SyncHistory(ctx, service.HistorySyncRequest{
		AssetKey: assetKey, Mode: "default_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	fullTaskID := created.Task.ID

	dates := sequentialDates("2024-01-01", 40)
	values := make([]float64, len(dates))
	for i := range values {
		values[i] = 100 * (1 + 0.001*float64(i))
	}
	env := uploadResult(t, st, historySyncResult(assetKey, "ak_primary", dates, values))
	markPreComplete(t, st.db, fullTaskID, env)
	assertOutcome(t, notifyPostProcess(t, st, fullTaskID), "success", "")

	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_points WHERE asset_key=?`, assetKey); n != 40 {
		t.Fatalf("points after full sync = %d, want 40", n)
	}
	var dataAsOf, sourceName string
	var pointCount int
	if err := st.db.QueryRow(`
		SELECT data_as_of, source_name, point_count FROM market_asset_history_state
		WHERE asset_key=? AND adjust_policy='none' AND point_type='adjusted_close'`, assetKey).
		Scan(&dataAsOf, &sourceName, &pointCount); err != nil {
		t.Fatal(err)
	}
	if dataAsOf != dates[len(dates)-1] || sourceName != "ak_primary" || pointCount != 40 {
		t.Fatalf("history state = (%s, %s, %d)", dataAsOf, sourceName, pointCount)
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_detail_projections WHERE asset_key=?`, assetKey); n != 1 {
		t.Fatal("detail projection was not computed")
	}

	// Reentrancy: same notification again is success and changes nothing.
	assertOutcome(t, notifyPostProcess(t, st, fullTaskID), "success", "")
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_points WHERE asset_key=?`, assetKey); n != 40 {
		t.Fatalf("re-notify duplicated points: %d", n)
	}
	finishTask(t, st.db, fullTaskID, "complete")

	// 2) Same-source merge with no_new_data: only success metadata moves.
	created, err = st.assets.SyncHistory(ctx, service.HistorySyncRequest{
		AssetKey: assetKey, Mode: "default_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	mergeTaskID := created.Task.ID
	noNew, _ := json.Marshal(map[string]any{
		"type": "asset_history_sync", "asset_key": assetKey,
		"adjust_policy": "none", "point_type": "adjusted_close",
		"source_name": "ak_primary", "no_new_data": true,
		"points": []any{},
	})
	env = uploadResult(t, st, noNew)
	markPreComplete(t, st.db, mergeTaskID, env)
	assertOutcome(t, notifyPostProcess(t, st, mergeTaskID), "success", "")
	var lastSuccess string
	if err := st.db.QueryRow(`
		SELECT last_success_task_id FROM market_asset_history_state
		WHERE asset_key=? AND adjust_policy='none' AND point_type='adjusted_close'`, assetKey).
		Scan(&lastSuccess); err != nil {
		t.Fatal(err)
	}
	if lastSuccess != mergeTaskID {
		t.Fatalf("no_new_data did not record success task, got %s", lastSuccess)
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_points WHERE asset_key=?`, assetKey); n != 40 {
		t.Fatalf("no_new_data mutated points: %d", n)
	}
	finishTask(t, st.db, mergeTaskID, "complete")

	// 3) Merge from the wrong source is a permanent source_mismatch.
	created, err = st.assets.SyncHistory(ctx, service.HistorySyncRequest{
		AssetKey: assetKey, Mode: "default_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongSourceTaskID := created.Task.ID
	env = uploadResult(t, st, historySyncResult(assetKey, "ak_other",
		sequentialDates(dataAsOf, 3), []float64{104, 104.5, 105}))
	markPreComplete(t, st.db, wrongSourceTaskID, env)
	assertOutcome(t, notifyPostProcess(t, st, wrongSourceTaskID),
		"permanent_error", "source_mismatch")
	finishTask(t, st.db, wrongSourceTaskID, "failed")

	// 4) Merge data that starts after data_as_of leaves a gap and is
	// rejected.
	created, err = st.assets.SyncHistory(ctx, service.HistorySyncRequest{
		AssetKey: assetKey, Mode: "default_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}
	gapTaskID := created.Task.ID
	gapStart, _ := time.Parse("2006-01-02", dataAsOf)
	gapDates := sequentialDates(gapStart.AddDate(0, 0, 5).Format("2006-01-02"), 3)
	env = uploadResult(t, st, historySyncResult(assetKey, "ak_primary", gapDates,
		[]float64{104, 104.5, 105}))
	markPreComplete(t, st.db, gapTaskID, env)
	assertOutcome(t, notifyPostProcess(t, st, gapTaskID),
		"permanent_error", "provider_data_incomplete")
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM market_asset_points WHERE asset_key=?`, assetKey); n != 40 {
		t.Fatalf("rejected merge mutated points: %d", n)
	}
}

func TestInternalPostProcess_FXRates(t *testing.T) {
	st := newInternalStack(t)
	ctx := context.Background()

	created, err := st.assets.SyncFXRates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	taskID := created.Task.ID

	fxRaw, _ := json.Marshal(map[string]any{
		"type": "fx_rate_sync", "pairs": []string{"HKDCNY", "USDCNY"},
		"source_name": "ak_fx",
		"rates": []map[string]any{
			{"date": "2026-07-02", "pair": "USDCNY", "value": 7.21},
			{"date": "2026-07-03", "pair": "USDCNY", "value": 7.22},
			{"date": "2026-07-02", "pair": "HKDCNY", "value": 0.92},
			{"date": "2026-07-03", "pair": "HKDCNY", "value": 0.93},
		},
	})
	env := uploadResult(t, st, fxRaw)
	markPreComplete(t, st.db, taskID, env)
	assertOutcome(t, notifyPostProcess(t, st, taskID), "success", "")

	for _, instID := range []string{"system_fx_usdcny", "system_fx_hkdcny"} {
		if n := countRows(t, st.db,
			`SELECT COUNT(*) FROM market_data_points WHERE instrument_id=? AND point_type='fx_rate'`,
			instID); n != 2 {
			t.Fatalf("%s fx points = %d, want 2", instID, n)
		}
	}
	var lastSuccessTaskID string
	var lastSuccessAt int64
	if err := st.db.QueryRow(`
		SELECT last_success_task_id, last_success_at
		FROM market_asset_sync_state WHERE scope='fx_rates'`).
		Scan(&lastSuccessTaskID, &lastSuccessAt); err != nil {
		t.Fatal(err)
	}
	if lastSuccessTaskID != taskID || lastSuccessAt == 0 {
		t.Fatalf("fx sync success state = task %s at %d, want %s and non-zero time",
			lastSuccessTaskID, lastSuccessAt, taskID)
	}

	// Reentrancy.
	assertOutcome(t, notifyPostProcess(t, st, taskID), "success", "")
	finishTask(t, st.db, taskID, "complete")

	// A result missing one requested pair is permanently incomplete.
	created2, err := st.assets.SyncFXRates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	partial, _ := json.Marshal(map[string]any{
		"type": "fx_rate_sync", "pairs": []string{"HKDCNY", "USDCNY"},
		"source_name": "ak_fx",
		"rates": []map[string]any{
			{"date": "2026-07-04", "pair": "USDCNY", "value": 7.23},
		},
	})
	env = uploadResult(t, st, partial)
	markPreComplete(t, st.db, created2.Task.ID, env)
	assertOutcome(t, notifyPostProcess(t, st, created2.Task.ID),
		"permanent_error", "provider_data_incomplete")
}

// TestInternalPostProcess_ETFSearchableViaPublicAPI covers the
// acceptance path: after a directory sync post-process commits HK/US ETF
// entries, the public market-assets API can find them by market and query.
func TestInternalPostProcess_ETFSearchableViaPublicAPI(t *testing.T) {
	st := newInternalStack(t)
	ctx := context.Background()
	pub := httptest.NewServer(NewRouter(ctx, Deps{DB: st.db, Services: buildServices(st.db)}))
	t.Cleanup(pub.Close)
	client := pub.Client()

	runDirectory := func(syncKey, scope string, assets []map[string]any) {
		taskID := syncUnitTask(t, st, syncKey)
		raw := directoryUnitResult(syncKey, scope, assets)
		markPreComplete(t, st.db, taskID, uploadResult(t, st, raw))
		assertOutcome(t, notifyPostProcess(t, st, taskID), "success", "")
		finishTask(t, st.db, taskID, "complete")
	}

	runDirectory("hk_stock", "hk_all", []map[string]any{
		{"market": "HK", "instrument_type": "hk_stock", "symbol": "00700",
			"name": "腾讯控股", "instrument_kind": "stock", "currency": "HKD",
			"source_name": "em.hk_equity_list", "source_as_of": "2026-07-05"},
	})
	runDirectory("hk_etf", "hk_all", []map[string]any{
		{"market": "HK", "instrument_type": "hk_etf", "symbol": "02800",
			"name": "盈富基金", "instrument_kind": "etf", "currency": "HKD",
			"source_name": "em.hk_fund_list", "source_as_of": "2026-07-05"},
	})
	runDirectory("us_stock", "us_all", []map[string]any{
		{"market": "US", "instrument_type": "us_stock", "symbol": "AAPL",
			"name": "苹果", "instrument_kind": "stock", "currency": "USD",
			"source_name": "em.us_equity_list", "source_as_of": "2026-07-05"},
	})
	runDirectory("us_etf", "us_all", []map[string]any{
		{"market": "US", "instrument_type": "us_etf", "symbol": "SPY",
			"name": "标普500ETF-SPDR", "instrument_kind": "etf", "currency": "USD",
			"source_name": "em.us_etf_list", "source_as_of": "2026-07-05"},
	})

	assertSearchable := func(url, wantType, wantSymbol string) {
		resp, body := getJSON(t, client, url)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", url, resp.StatusCode, body)
		}
		assets := decodeEnvelope(t, body)["data"].(map[string]any)["assets"].([]any)
		for _, a := range assets {
			m := a.(map[string]any)
			if m["instrument_type"] == wantType && m["symbol"] == wantSymbol {
				if m["instrument_kind"] != "etf" && wantType != "hk_stock" && wantType != "us_stock" {
					t.Fatalf("%s %s instrument_kind = %v, want etf", wantType, wantSymbol, m["instrument_kind"])
				}
				return
			}
		}
		t.Fatalf("GET %s did not return %s/%s: %s", url, wantType, wantSymbol, body)
	}

	assertSearchable(pub.URL+"/api/v1/market-assets?market=HK", "hk_etf", "02800")
	assertSearchable(pub.URL+"/api/v1/market-assets?market=US", "us_etf", "SPY")
	// Local search hits the ETF entries by name/symbol as well.
	assertSearchable(pub.URL+"/api/v1/market-assets?name_q=盈富", "hk_etf", "02800")
	assertSearchable(pub.URL+"/api/v1/market-assets?symbol_q=SPY", "us_etf", "SPY")
}

func TestInternalPostProcess_ErrorClassification(t *testing.T) {
	st := newInternalStack(t)

	// Unknown task.
	assertOutcome(t, notifyPostProcess(t, st, "wt_missing"), "permanent_error", "task_not_found")

	// Task not yet pre_complete.
	taskID := syncUnitTask(t, st, "us_stock")
	assertOutcome(t, notifyPostProcess(t, st, taskID),
		"permanent_error", "task_status_invalid")

	// pre_complete without result_data.
	if _, err := st.db.Exec(
		`UPDATE worker_tasks SET status='pre_complete' WHERE id=?`, taskID); err != nil {
		t.Fatal(err)
	}
	assertOutcome(t, notifyPostProcess(t, st, taskID),
		"permanent_error", "invalid_result_data")

	// Envelope referencing a missing (e.g. expired) resource.
	ghost := resourcedb.Envelope{
		ResourceKey: "0000000000000000000000000000000000000000000000000000000000000000",
		ContentType: "application/json", ContentEncoding: "gzip",
		SchemaVersion: 1,
		SHA256:        "0000000000000000000000000000000000000000000000000000000000000000",
		SizeBytes:     1,
	}
	markPreComplete(t, st.db, taskID, ghost)
	assertOutcome(t, notifyPostProcess(t, st, taskID),
		"permanent_error", "resource_not_found")
}
