package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// seedResearchAsset inserts a directory row plus optional daily history
// ending today, so readiness staleness checks pass against the real clock.
func seedResearchAsset(t *testing.T, db *sql.DB, key, name string, days int, base float64) {
	t.Helper()
	ctx := context.Background()
	assets := repository.NewMarketAssetRepo(db)
	now := time.Now().UnixMilli()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := assets.UpsertAssetTx(ctx, tx, repository.MarketAsset{
		AssetKey: key, Market: "CN", InstrumentType: "cn_exchange_fund",
		RegionCode: "sh", Exchange: "SSE", Symbol: key, Name: name,
		Currency: "CNY", Active: true, ListingStatus: "active", SourceName: "test_source",
	}, now); err != nil {
		t.Fatalf("upsert asset: %v", err)
	}
	if days > 0 {
		end := time.Now().UTC().Truncate(24 * time.Hour)
		points := make([]repository.MarketAssetPoint, 0, days)
		for i := 0; i < days; i++ {
			d := end.AddDate(0, 0, i-days+1)
			points = append(points, repository.MarketAssetPoint{
				AssetKey: key, AdjustPolicy: "hfq", PointType: "adjusted_close",
				TradeDate:  d.Format("2006-01-02"),
				Value:      base * (1 + 0.03*math.Sin(float64(i)/9)) * math.Pow(1.0002, float64(i)),
				SourceName: "test_source", FetchedAt: now,
			})
		}
		if err := assets.UpsertPointsTx(ctx, tx, points); err != nil {
			t.Fatalf("upsert points: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO market_asset_history_state
				(asset_key, adjust_policy, point_type, data_as_of, point_count, source_name, updated_at)
			VALUES (?,?,?,?,?,?,?)`,
			key, "hfq", "adjusted_close",
			points[len(points)-1].TradeDate, days, "test_source", now); err != nil {
			t.Fatalf("insert history state: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

type researchHTTPResponse struct {
	StatusCode int
	Header     http.Header
}

func researchPost(t *testing.T, srv *httptest.Server, path string, payload any) (researchHTTPResponse, []byte) {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	responseBody := mustRead(t, resp)
	resp.Body = http.NoBody
	return researchHTTPResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, responseBody
}

func researchPatch(t *testing.T, srv *httptest.Server, path string, payload any) (researchHTTPResponse, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	responseBody := mustRead(t, resp)
	resp.Body = http.NoBody
	return researchHTTPResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, responseBody
}

func researchGet(t *testing.T, srv *httptest.Server, path string) (researchHTTPResponse, []byte) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	resp.Body = http.NoBody
	return researchHTTPResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, body
}

func envData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	env := decodeEnvelope(t, body)
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data envelope: %s", string(body))
	}
	return data
}

func TestResearchAPIFullBacktestFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	seedResearchAsset(t, db, "RA1", "股票基金", 1500, 100)
	seedResearchAsset(t, db, "RA2", "债券基金", 1500, 50)

	services := buildServices(db)
	worker := newTestTaskWorker(db, services)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()

	// Screener sees both assets with metrics.
	resp, body := researchGet(t, srv, "/api/v1/research/assets?history_status=synced")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("screener status=%d body=%s", resp.StatusCode, body)
	}
	screener := envData(t, body)
	if int(screener["total"].(float64)) != 2 {
		t.Fatalf("screener total expected 2: %s", body)
	}
	firstAsset := screener["assets"].([]any)[0].(map[string]any)
	if firstAsset["metrics"] == nil {
		t.Fatalf("screener metrics missing: %v", firstAsset)
	}

	// Create collection with invalid weights: readiness must block.
	resp, body = researchPost(t, srv, "/api/v1/research/collections", map[string]any{
		"name": "接口组合", "tail_risk_confidence": 0.99, "tail_risk_horizon_days": 1,
		"items": []map[string]any{
			{"asset_key": "RA1", "weight": 0.5},
			{"asset_key": "RA2", "weight": 0.3},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create collection status=%d body=%s", resp.StatusCode, body)
	}
	collection := envData(t, body)
	if collection["tail_risk_confidence"] != 0.99 || collection["tail_risk_horizon_days"] != float64(1) {
		t.Fatalf("tail-risk create round trip failed: %s", body)
	}
	collectionID := collection["id"].(string)
	items := collection["items"].([]any)

	resp, body = researchGet(t, srv, "/api/v1/research/collections/"+collectionID+"/readiness")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d", resp.StatusCode)
	}
	readiness := envData(t, body)
	if readiness["ready"].(bool) {
		t.Fatalf("expected not ready: %s", body)
	}

	// Backtest creation is gated on readiness.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/backtests", nil)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("backtest must be blocked: %s", body)
	}
	assertErrorCode(t, body, "research_collection_not_ready")

	// Fix weights via normalize (unlocked items rescale to sum 1).
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/normalize-weights", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("normalize status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = researchGet(t, srv, "/api/v1/research/collections/"+collectionID+"/readiness")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d", resp.StatusCode)
	}
	if ready := envData(t, body)["ready"].(bool); !ready {
		t.Fatalf("expected ready after normalize: %s", body)
	}

	// sync-history: everything is fresh, so assets are skipped.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/sync-history", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync-history status=%d body=%s", resp.StatusCode, body)
	}
	sync := envData(t, body)
	for _, a := range sync["assets"].([]any) {
		if a.(map[string]any)["status"].(string) != "skipped" {
			t.Fatalf("fresh asset should be skipped: %s", body)
		}
	}

	// Create the backtest and wait for the worker.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/backtests", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create backtest status=%d body=%s", resp.StatusCode, body)
	}
	backtest := envData(t, body)
	run := backtest["run"].(map[string]any)
	runID := run["id"].(string)
	jobID := run["task_id"].(string)
	waitJobSucceeded(t, srv, jobID)

	// Run detail carries summary, years, months and the input snapshot.
	resp, body = researchGet(t, srv, "/api/v1/research/runs/"+runID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get run status=%d", resp.StatusCode)
	}
	runDetail := envData(t, body)
	if runDetail["status"].(string) != "complete" {
		t.Fatalf("run not succeeded: %s", body)
	}
	summary := runDetail["summary"].(map[string]any)
	if _, ok := summary["cagr"]; !ok {
		t.Fatalf("summary missing cagr: %s", body)
	}
	tailRisk := summary["tail_risk"].(map[string]any)
	if tailRisk["confidence"] != 0.99 || tailRisk["horizon_days"] != float64(1) ||
		tailRisk["algorithm_version"] != "empirical_cvar_v1" {
		t.Fatalf("summary missing frozen CVaR contract: %s", body)
	}
	if len(runDetail["years"].([]any)) < 3 || len(runDetail["months"].([]any)) < 40 {
		t.Fatalf("years/months missing: %s", body)
	}
	if runDetail["input_snapshot"] == nil {
		t.Fatalf("input snapshot missing")
	}

	// Points with range narrowing.
	resp, body = researchGet(t, srv,
		"/api/v1/research/runs/"+runID+"/points?limit=10&include_weights=true")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("points status=%d", resp.StatusCode)
	}
	points := envData(t, body)
	if len(points["points"].([]any)) != 10 {
		t.Fatalf("points limit ignored: %s", body)
	}
	if int(points["total"].(float64)) < 1400 {
		t.Fatalf("points total wrong: %v", points["total"])
	}
	first := points["points"].([]any)[0].(map[string]any)
	if first["weights"] == nil {
		t.Fatalf("weights missing in points: %v", first)
	}

	// CSV export.
	resp, csvBody := researchGet(t, srv, "/api/v1/research/runs/"+runID+"/export.csv")
	if resp.StatusCode != http.StatusOK ||
		!strings.HasPrefix(string(csvBody), "date,nav,") {
		t.Fatalf("csv export failed: %d %s", resp.StatusCode, string(csvBody[:40]))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("csv content type wrong: %s", ct)
	}

	// Identical input reuses the succeeded run.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/backtests", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-create backtest status=%d", resp.StatusCode)
	}
	reuse := envData(t, body)
	if !reuse["reused"].(bool) || reuse["run"].(map[string]any)["id"].(string) != runID {
		t.Fatalf("expected run reuse: %s", body)
	}

	// Runs listing includes the succeeded run; recent runs sees it too.
	resp, body = researchGet(t, srv, "/api/v1/research/collections/"+collectionID+"/runs")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list runs status=%d", resp.StatusCode)
	}
	if runs := envData(t, body)["runs"].([]any); len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	resp, body = researchGet(t, srv, "/api/v1/research/runs?limit=5")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recent runs status=%d", resp.StatusCode)
	}
	if runs := envData(t, body)["runs"].([]any); len(runs) != 1 {
		t.Fatalf("expected 1 recent run, got %d", len(runs))
	}

	// Collections listing carries the latest run summary.
	resp, body = researchGet(t, srv, "/api/v1/research/collections")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list collections status=%d", resp.StatusCode)
	}
	collections := envData(t, body)["collections"].([]any)
	if len(collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(collections))
	}
	entry := collections[0].(map[string]any)
	if entry["latest_run"] == nil || entry["latest_run_summary"] == nil {
		t.Fatalf("latest run annotations missing: %s", body)
	}

	// CVaR optimization runs through readiness, worker, result and apply APIs.
	resp, body = researchGet(t, srv,
		"/api/v1/research/collections/"+collectionID+
			"/optimization-readiness?weight_step=0.1&cvar_confidence=0.95&cvar_horizon_days=20")
	if resp.StatusCode != http.StatusOK || !envData(t, body)["ready"].(bool) {
		t.Fatalf("optimization readiness failed: status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/optimizations", map[string]any{
			"weight_step": 0.1, "top_k": 5,
			"tail_risk": map[string]any{"confidence": 0.95, "horizon_days": 20},
		})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create optimization status=%d body=%s", resp.StatusCode, body)
	}
	optimization := envData(t, body)["optimization"].(map[string]any)
	optimizationID := optimization["id"].(string)
	waitJobSucceeded(t, srv, optimization["task_id"].(string))
	resp, body = researchGet(t, srv, "/api/v1/research/optimizations/"+optimizationID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get optimization status=%d body=%s", resp.StatusCode, body)
	}
	optimization = envData(t, body)
	optimizationResult := optimization["result"].(map[string]any)
	bestByCVaR := optimizationResult["best_by_cvar"].([]any)
	if len(bestByCVaR) == 0 || optimizationResult["cvar_eligible_count"].(float64) == 0 {
		t.Fatalf("CVaR optimization result incomplete: %s", body)
	}
	resp, body = researchGet(t, srv, "/api/v1/research/collections/"+collectionID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get collection before apply status=%d body=%s", resp.StatusCode, body)
	}
	expectedUpdatedAt := envData(t, body)["updated_at"].(float64)
	resp, body = researchPost(t, srv, "/api/v1/research/optimizations/"+optimizationID+"/apply", map[string]any{
		"objective": "min_cvar", "rank": 1, "expected_collection_updated_at": expectedUpdatedAt,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply CVaR optimization status=%d body=%s", resp.StatusCode, body)
	}
	appliedCollection := envData(t, body)
	if appliedCollection["tail_risk_confidence"] != 0.95 ||
		appliedCollection["tail_risk_horizon_days"] != float64(20) {
		t.Fatalf("applied CVaR spec missing: %s", body)
	}

	// PATCH item weight (rebalances hash) and item update path.
	itemID := items[0].(map[string]any)["id"].(string)
	resp, body = researchPatch(t, srv,
		"/api/v1/research/collections/"+collectionID+"/items/"+itemID,
		map[string]any{"weight": 0.5})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch item status=%d body=%s", resp.StatusCode, body)
	}

	// Update collection params via PATCH.
	resp, body = researchPatch(t, srv, "/api/v1/research/collections/"+collectionID,
		map[string]any{
			"rebalance_policy": "quarterly", "risk_free_rate": 0.02,
			"tail_risk_confidence": 0.9, "tail_risk_horizon_days": 20,
		})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch collection status=%d body=%s", resp.StatusCode, body)
	}
	if got := envData(t, body)["rebalance_policy"].(string); got != "quarterly" {
		t.Fatalf("rebalance policy not updated: %s", got)
	}
	patched := envData(t, body)
	if patched["tail_risk_confidence"] != 0.9 || patched["tail_risk_horizon_days"] != float64(20) {
		t.Fatalf("tail-risk patch round trip failed: %s", body)
	}

	// Archive then hard-delete.
	req, _ := http.NewRequest(http.MethodDelete,
		srv.URL+"/api/v1/research/collections/"+collectionID, nil)
	rawResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, rawResp)
	if rawResp.StatusCode != http.StatusOK || envData(t, body)["archived"] != true {
		t.Fatalf("archive failed: %d %s", rawResp.StatusCode, body)
	}
	req, _ = http.NewRequest(http.MethodDelete,
		srv.URL+"/api/v1/research/collections/"+collectionID+"?hard=true", nil)
	rawResp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, rawResp)
	if rawResp.StatusCode != http.StatusOK {
		t.Fatalf("hard delete failed: %d %s", rawResp.StatusCode, body)
	}
	resp, _ = researchGet(t, srv, "/api/v1/research/collections/"+collectionID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted collection should 404, got %d", resp.StatusCode)
	}
}

func TestOptimizationReadinessRejectsMalformedQuery(t *testing.T) {
	db := testutil.OpenTestDB(t)
	services := buildServices(db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()

	for _, query := range []string{
		"weight_step=abc",
		"cvar_confidence=abc",
		"cvar_horizon_days=1.5",
	} {
		resp, body := researchGet(t, srv, "/api/v1/research/collections/missing/optimization-readiness?"+query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", query, resp.StatusCode, body)
		}
		assertErrorCode(t, body, "invalid_request")
	}
	resp, body := researchPost(t, srv, "/api/v1/research/collections", map[string]any{"name": "CVaR query"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create collection status=%d body=%s", resp.StatusCode, body)
	}
	collectionID := envData(t, body)["id"].(string)
	for _, tc := range []struct {
		query, code string
	}{
		{"cvar_confidence=0.94", "cvar_confidence_invalid"},
		{"cvar_horizon_days=2", "cvar_horizon_invalid"},
	} {
		resp, body = researchGet(t, srv,
			"/api/v1/research/collections/"+collectionID+"/optimization-readiness?"+tc.query)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("query %q status=%d body=%s", tc.query, resp.StatusCode, body)
		}
		assertErrorCode(t, body, tc.code)
	}
}

func TestResearchAPISyncHistory(t *testing.T) {
	srv, db, _ := testRouterWithDB(t)
	seedResearchAsset(t, db, "RB1", "有历史", 1200, 100)
	seedResearchAsset(t, db, "RB2", "无历史", 0, 0)

	// sync-history creates a task for the asset without history and reuses it
	// on the second call.
	resp, body := researchPost(t, srv, "/api/v1/research/collections", map[string]any{
		"name": "同步组合",
		"items": []map[string]any{
			{"asset_key": "RB1", "weight": 0.5},
			{"asset_key": "RB2", "weight": 0.5},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create collection status=%d body=%s", resp.StatusCode, body)
	}
	collectionID := envData(t, body)["id"].(string)

	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/sync-history", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync-history status=%d body=%s", resp.StatusCode, body)
	}
	statuses := map[string]string{}
	var taskID string
	for _, raw := range envData(t, body)["assets"].([]any) {
		a := raw.(map[string]any)
		statuses[a["asset_key"].(string)] = a["status"].(string)
		if a["asset_key"] == "RB2" && a["task"] != nil {
			taskID = a["task"].(map[string]any)["id"].(string)
		}
	}
	if statuses["RB2"] != "created" || statuses["RB1"] != "skipped" {
		t.Fatalf("sync statuses wrong: %+v", statuses)
	}
	if taskID == "" {
		t.Fatal("task id missing for created sync")
	}

	// A read-only status request restores the active batch after navigation and
	// does not enqueue another task.
	resp, body = researchGet(t, srv,
		"/api/v1/research/collections/"+collectionID+"/sync-history")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", resp.StatusCode, body)
	}
	activeAssets := envData(t, body)["assets"].([]any)
	if len(activeAssets) != 1 {
		t.Fatalf("active sync assets=%s", body)
	}
	active := activeAssets[0].(map[string]any)
	if active["asset_key"] != "RB2" ||
		active["task"].(map[string]any)["id"] != taskID {
		t.Fatalf("active sync task mismatch: %s", body)
	}

	// The shared task endpoint serves the research task panel polling.
	resp, body = researchGet(t, srv, "/api/v1/tasks/"+taskID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task poll status=%d body=%s", resp.StatusCode, body)
	}

	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/sync-history", map[string]any{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second sync status=%d", resp.StatusCode)
	}
	for _, raw := range envData(t, body)["assets"].([]any) {
		a := raw.(map[string]any)
		if a["asset_key"] == "RB2" && a["status"] != "existed" {
			t.Fatalf("expected existed for RB2: %s", body)
		}
	}

	// Readiness reports the missing history as syncing (active task).
	resp, body = researchGet(t, srv, "/api/v1/research/collections/"+collectionID+"/readiness")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness status=%d", resp.StatusCode)
	}
	readiness := envData(t, body)
	if readiness["ready"].(bool) {
		t.Fatalf("collection with missing history must not be ready: %s", body)
	}
}

func TestResearchAPICopyToPlanValidation(t *testing.T) {
	srv, db, _ := testRouterWithDB(t)
	seedResearchAsset(t, db, "RC1", "股票", 1200, 100)

	plan := createTestPlan(t, db)

	resp, body := researchPost(t, srv, "/api/v1/research/collections", map[string]any{
		"name":  "复制组合",
		"items": []map[string]any{{"asset_key": "RC1", "weight": 1.0}},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create collection status=%d body=%s", resp.StatusCode, body)
	}
	collection := envData(t, body)
	collectionID := collection["id"].(string)
	itemID := collection["items"].([]any)[0].(map[string]any)["id"].(string)

	// Missing asset_class/region is rejected during preview.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/plan-preview",
		map[string]any{"plan_id": plan.ID})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("incomplete items must fail: %s", body)
	}
	assertErrorCode(t, body, "research_item_classification_incomplete")

	// Fill the fields and retry.
	resp, body = researchPatch(t, srv,
		"/api/v1/research/collections/"+collectionID+"/items/"+itemID,
		map[string]any{"asset_class": "equity", "region": "domestic"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch item status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/plan-preview",
		map[string]any{"plan_id": plan.ID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("plan-preview status=%d body=%s", resp.StatusCode, body)
	}
	preview := envData(t, body)
	holdings := preview["holdings"].([]any)
	if len(holdings) != 1 || preview["plan_id"].(string) != plan.ID {
		t.Fatalf("preview wrong: %s", body)
	}
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/apply-to-plan",
		map[string]any{
			"plan_id":                   plan.ID,
			"expected_config_version":   int(preview["expected_config_version"].(float64)),
			"expected_replacement_hash": preview["replacement_hash"].(string),
			"mode":                      "replace_all",
		})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply-to-plan status=%d body=%s", resp.StatusCode, body)
	}
	applied := envData(t, body)
	if int(applied["config_version"].(float64)) != plan.ConfigVersion+1 ||
		int(applied["holding_count"].(float64)) != 1 {
		t.Fatalf("apply result wrong: %s", body)
	}
	resp, body = researchPost(t, srv,
		"/api/v1/plans/"+plan.ID+"/simulations",
		map[string]any{"runs": 1000, "seed": "117"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("simulation after research apply status=%d body=%s", resp.StatusCode, body)
	}
	simulation := envData(t, body)
	if simulation["status"] != "pending" || simulation["run_id"] == "" {
		t.Fatalf("simulation after research apply wrong: %s", body)
	}

	// The legacy draft endpoint is explicitly retired instead of silently
	// returning a payload that is not persisted.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/copy-to-plan",
		map[string]any{"plan_id": plan.ID})
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("legacy copy-to-plan status=%d body=%s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "copy_to_plan_deprecated")

	// Copy from plan is covered at the service layer; here verify the
	// from_collection path clones items.
	resp, body = researchPost(t, srv, "/api/v1/research/collections", map[string]any{
		"name": "克隆组合", "from_collection_id": collectionID,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clone collection status=%d body=%s", resp.StatusCode, body)
	}
	clone := envData(t, body)
	if len(clone["items"].([]any)) != 1 {
		t.Fatalf("clone items missing: %s", body)
	}
}
