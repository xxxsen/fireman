package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

func seedAdminWorkerTask(t *testing.T, db *sql.DB, id, taskType, status, dedupe string, createdAt int64) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO worker_tasks
			(id, version_no, type, status, dedupe_key, payload_json, created_at)
		VALUES (?,?,?,?,?,'{"scope":"cn_all"}',?)`,
		id, 1, taskType, status, dedupe, createdAt); err != nil {
		t.Fatalf("seed worker task: %v", err)
	}
}

func adminGetJSON(t *testing.T, client *http.Client, url string, wantStatus int) map[string]any {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	body := readBody(t, resp)
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s status=%d body=%s, want %d", url, resp.StatusCode, body, wantStatus)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return out
}

func dataOf(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	if envelope["code"] != "ok" {
		t.Fatalf("envelope code=%v", envelope["code"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("envelope data=%T", envelope["data"])
	}
	return data
}

func TestAdminOverviewEndpoint(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	now := time.Now().UnixMilli()
	seedAdminWorkerTask(t, db, "wt_run", repository.WorkerTaskTypeAssetHistorySync,
		"running", "asset_history|x", now)

	data := dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/overview", http.StatusOK))
	for _, key := range []string{"worker_tasks", "jobs", "callbacks", "sync_health", "storage"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("overview missing %s: %v", key, data)
		}
	}
	wt := data["worker_tasks"].(map[string]any)
	if wt["active"].(float64) != 1 {
		t.Fatalf("active=%v", wt["active"])
	}
	sync := data["sync_health"].(map[string]any)
	scopes := sync["directory_scopes"].([]any)
	if len(scopes) != 3 {
		t.Fatalf("directory_scopes=%d", len(scopes))
	}
	fxPairs := sync["fx_pairs"].([]any)
	if len(fxPairs) != 2 {
		t.Fatalf("fx_pairs=%d", len(fxPairs))
	}
}

func TestAdminWorkerTaskListEndpoint(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	for i := 0; i < 25; i++ {
		status := "complete"
		if i%5 == 0 {
			status = "failed"
		}
		seedAdminWorkerTask(t, db, fmt.Sprintf("wt_%02d", i),
			repository.WorkerTaskTypeAssetHistorySync, status,
			fmt.Sprintf("asset_history|dim_%02d", i), int64(1000+i))
	}

	// Pagination defaults: limit 20, offset 0, total unaffected.
	data := dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/worker-tasks", http.StatusOK))
	if data["total"].(float64) != 25 || data["limit"].(float64) != 20 || data["offset"].(float64) != 0 {
		t.Fatalf("page meta=%v", data)
	}
	items := data["items"].([]any)
	if len(items) != 20 {
		t.Fatalf("items=%d", len(items))
	}
	first := items[0].(map[string]any)
	if first["id"] != "wt_24" {
		t.Fatalf("first item=%v, want created_at DESC", first["id"])
	}
	if _, hasPayload := first["payload_json"]; hasPayload {
		t.Fatalf("list item leaks payload_json: %v", first)
	}

	// Limit is capped at 100 and status filter applies.
	data = dataOf(t, adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/worker-tasks?status=failed&limit=9999", http.StatusOK))
	if data["total"].(float64) != 5 || data["limit"].(float64) != 100 {
		t.Fatalf("failed page meta=%v", data)
	}

	// q matches dedupe substring.
	data = dataOf(t, adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/worker-tasks?q=dim_07", http.StatusOK))
	if data["total"].(float64) != 1 {
		t.Fatalf("q filter total=%v", data["total"])
	}

	// Invalid enum values are rejected.
	body := adminGetJSON(t, client, srv.URL+"/api/v1/admin/worker-tasks?type=bogus", http.StatusBadRequest)
	if body["code"] != "invalid_request" {
		t.Fatalf("invalid type code=%v", body["code"])
	}
	body = adminGetJSON(t, client, srv.URL+"/api/v1/admin/worker-tasks?status=bogus", http.StatusBadRequest)
	if body["code"] != "invalid_request" {
		t.Fatalf("invalid status code=%v", body["code"])
	}
}

func TestAdminWorkerTaskDetailEndpoint(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO worker_tasks
			(id, version_no, type, status, dedupe_key, payload_json, result_data,
			 error_code, error_message, created_at, started_at, pre_completed_at, finished_at)
		VALUES ('wt_x', 7, ?, 'failed', 'asset_history|x', '{"a":1}', '{"resource_key":"k"}',
			'source_unavailable', 'boom', ?, ?, ?, ?)`,
		repository.WorkerTaskTypeAssetHistorySync, now-4000, now-3000, now-2000, now-1000); err != nil {
		t.Fatal(err)
	}
	records := repository.NewPostProcessRecordRepo(db)
	if err := records.Insert(context.Background(), repository.PostProcessRecord{
		TaskID: "wt_x", TaskType: repository.WorkerTaskTypeAssetHistorySync,
		AttemptNo: 1, Result: "permanent_error", ErrorCode: "invalid_result_data",
		DurationMs: 12, CreatedAt: now - 1500,
	}); err != nil {
		t.Fatal(err)
	}

	data := dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/worker-tasks/wt_x", http.StatusOK))
	task := data["task"].(map[string]any)
	if task["payload_json"] != `{"a":1}` || task["result_data"] != `{"resource_key":"k"}` {
		t.Fatalf("task raw fields=%v", task)
	}
	timeline := data["timeline"].([]any)
	if len(timeline) != 4 {
		t.Fatalf("timeline=%v", timeline)
	}
	last := timeline[3].(map[string]any)
	if last["phase"] != "finished" || last["status"] != "failed" {
		t.Fatalf("finished node=%v", last)
	}
	recs := data["post_process_records"].([]any)
	if len(recs) != 1 {
		t.Fatalf("records=%v", recs)
	}

	// 404 contract.
	body := adminGetJSON(t, client, srv.URL+"/api/v1/admin/worker-tasks/none", http.StatusNotFound)
	if body["code"] != "task_not_found" {
		t.Fatalf("not found code=%v", body["code"])
	}
}

func TestAdminJobsEndpoint(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	plan := createTestPlan(t, db)
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO jobs (id, plan_id, type, status, input_hash, progress_current, progress_total,
			phase, cancel_requested, retry_count, created_at, started_at)
		VALUES ('job_1', ?, 'simulation', 'running', '', 42, 100, 'mc_paths', 0, 0, ?, ?)`,
		plan.ID, now, now); err != nil {
		t.Fatal(err)
	}

	data := dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/jobs", http.StatusOK))
	items := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", items)
	}
	job := items[0].(map[string]any)
	if job["plan_name"] != plan.Name || job["phase"] != "mc_paths" {
		t.Fatalf("job=%v", job)
	}
	if job["progress_current"].(float64) != 42 {
		t.Fatalf("progress=%v", job["progress_current"])
	}

	body := adminGetJSON(t, client, srv.URL+"/api/v1/admin/jobs?type=bogus", http.StatusBadRequest)
	if body["code"] != "invalid_request" {
		t.Fatalf("invalid type code=%v", body["code"])
	}
	// active pseudo status accepted.
	data = dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/jobs?status=active", http.StatusOK))
	if data["total"].(float64) != 1 {
		t.Fatalf("active jobs=%v", data["total"])
	}
}

func TestAdminPostProcessRecordsEndpoint(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	records := repository.NewPostProcessRecordRepo(db)
	for i, result := range []string{"success", "retryable_error", "permanent_error"} {
		if err := records.Insert(context.Background(), repository.PostProcessRecord{
			TaskID: fmt.Sprintf("wt_%d", i), TaskType: repository.WorkerTaskTypeFXRateSync,
			Result: result, CreatedAt: int64(100 + i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	data := dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/post-process-records", http.StatusOK))
	if data["total"].(float64) != 3 {
		t.Fatalf("total=%v", data["total"])
	}
	data = dataOf(t, adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/post-process-records?result=retryable_error", http.StatusOK))
	if data["total"].(float64) != 1 {
		t.Fatalf("filtered total=%v", data["total"])
	}
	body := adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/post-process-records?result=bogus", http.StatusBadRequest)
	if body["code"] != "invalid_request" {
		t.Fatalf("invalid result code=%v", body["code"])
	}

	// Empty result pages keep the items array shape (never null).
	data = dataOf(t, adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/post-process-records?task_id=missing", http.StatusOK))
	if items, ok := data["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("empty items=%v (%T), want []", data["items"], data["items"])
	}
}

func TestAdminDataVersionsEndpoint(t *testing.T) {
	srv, db, client := testRouterWithDB(t)
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO market_data_versions (version_key, version_no, task_id, updated_at)
		VALUES ('asset_directory|cn_all', 812, 'wt_1', 100),
		       ('fx_rate|USDCNY', 3, 'wt_2', 200)`); err != nil {
		t.Fatal(err)
	}

	data := dataOf(t, adminGetJSON(t, client, srv.URL+"/api/v1/admin/data-versions", http.StatusOK))
	if data["total"].(float64) != 2 {
		t.Fatalf("total=%v", data["total"])
	}
	items := data["items"].([]any)
	first := items[0].(map[string]any)
	if first["version_key"] != "fx_rate|USDCNY" {
		t.Fatalf("order first=%v, want updated_at DESC", first["version_key"])
	}

	data = dataOf(t, adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/data-versions?prefix=asset_directory", http.StatusOK))
	if data["total"].(float64) != 1 {
		t.Fatalf("prefix total=%v", data["total"])
	}
	items = data["items"].([]any)
	if items[0].(map[string]any)["version_no"].(float64) != 812 {
		t.Fatalf("version_no=%v", items[0])
	}

	// Empty result pages keep the items array shape (never null).
	data = dataOf(t, adminGetJSON(t, client,
		srv.URL+"/api/v1/admin/data-versions?prefix=asset_history", http.StatusOK))
	if items, ok := data["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("empty items=%v (%T), want []", data["items"], data["items"])
	}
}
