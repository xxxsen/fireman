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

	"github.com/fireman/fireman/internal/jobs"
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
				AssetKey: key, AdjustPolicy: "none", PointType: "adjusted_close",
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
			key, "none", "adjusted_close",
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
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db),
		jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db)),
		jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), services.Research,
		services.EventHub, nil, nil)
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
		"name": "接口组合",
		"items": []map[string]any{
			{"asset_key": "RA1", "weight": 0.5},
			{"asset_key": "RA2", "weight": 0.3},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create collection status=%d body=%s", resp.StatusCode, body)
	}
	collection := envData(t, body)
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
	jobID := run["job_id"].(string)
	waitJobSucceeded(t, srv, jobID)

	// Run detail carries summary, years, months and the input snapshot.
	resp, body = researchGet(t, srv, "/api/v1/research/runs/"+runID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get run status=%d", resp.StatusCode)
	}
	runDetail := envData(t, body)
	if runDetail["status"].(string) != "succeeded" {
		t.Fatalf("run not succeeded: %s", body)
	}
	summary := runDetail["summary"].(map[string]any)
	if _, ok := summary["cagr"]; !ok {
		t.Fatalf("summary missing cagr: %s", body)
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
		map[string]any{"rebalance_policy": "quarterly", "risk_free_rate": 0.02})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch collection status=%d body=%s", resp.StatusCode, body)
	}
	if got := envData(t, body)["rebalance_policy"].(string); got != "quarterly" {
		t.Fatalf("rebalance policy not updated: %s", got)
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

	// Missing asset_class/region → rejected with details.
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/copy-to-plan",
		map[string]any{"plan_id": plan.ID})
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("incomplete items must fail: %s", body)
	}
	assertErrorCode(t, body, "research_items_incomplete")

	// Fill the fields and retry.
	resp, body = researchPatch(t, srv,
		"/api/v1/research/collections/"+collectionID+"/items/"+itemID,
		map[string]any{"asset_class": "equity", "region": "cn"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch item status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = researchPost(t, srv,
		"/api/v1/research/collections/"+collectionID+"/copy-to-plan",
		map[string]any{"plan_id": plan.ID})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("copy-to-plan status=%d body=%s", resp.StatusCode, body)
	}
	draft := envData(t, body)
	holdings := draft["holdings"].([]any)
	if len(holdings) != 1 || draft["plan_id"].(string) != plan.ID {
		t.Fatalf("draft wrong: %s", body)
	}

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
