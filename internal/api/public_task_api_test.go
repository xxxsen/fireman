package api

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func seedPublicTask(
	t *testing.T, db *sql.DB, id, status string,
) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewWorkerTaskRepo(db)
	err = repo.CreateTx(context.Background(), tx, &repository.WorkerTask{
		ID: id, WorkerType: repository.WorkerTypeGo,
		Type: repository.WorkerTaskTypeSimulation, Status: status,
		ScopeType: "plan", ScopeID: "plan_public_tasks",
		DedupeKey: id, PayloadJSON: `{}`, ProgressTotal: 10,
	})
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicTaskListExpandsActiveAndValidatesStatus(t *testing.T) {
	db := testutil.OpenTestDB(t)
	for _, status := range []string{
		repository.WorkerTaskStatusPending,
		repository.WorkerTaskStatusRunning,
		repository.WorkerTaskStatusPreComplete,
		repository.WorkerTaskStatusComplete,
		repository.WorkerTaskStatusFailed,
		repository.WorkerTaskStatusCanceled,
	} {
		seedPublicTask(t, db, "task_public_"+status, status)
	}
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{
		DB: db, Services: buildServices(db),
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks?worker_type=go_worker&type=simulation" +
		"&scope_type=plan&scope_id=plan_public_tasks&status=active&limit=20")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Items []service.TaskView `json:"items"`
			Total int                `json:"total"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Total != 3 || len(body.Data.Items) != 3 {
		t.Fatalf("active page=%+v want exactly three active tasks", body.Data)
	}
	for _, item := range body.Data.Items {
		if !repository.IsActiveWorkerTaskStatus(item.Status) {
			t.Fatalf("terminal task leaked into active list: %+v", item)
		}
	}

	invalid, err := http.Get(srv.URL + "/api/v1/tasks?status=unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = invalid.Body.Close() }()
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid status code=%d want 400", invalid.StatusCode)
	}
	var errBody struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(invalid.Body).Decode(&errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Code != "invalid_request" {
		t.Fatalf("code=%q want invalid_request", errBody.Code)
	}
}

func TestTaskEventsStartsWithPersistedSnapshot(t *testing.T) {
	db := testutil.OpenTestDB(t)
	seedPublicTask(t, db, "task_sse_complete", repository.WorkerTaskStatusComplete)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{
		DB: db, Services: buildServices(db),
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/task_sse_complete/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type=%q", got)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("first frame=%q want data frame", line)
	}
	var event struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event); err != nil {
		t.Fatal(err)
	}
	if event.TaskID != "task_sse_complete" || event.Status != repository.WorkerTaskStatusComplete {
		t.Fatalf("event=%+v", event)
	}
}

func TestTaskEventsActiveSnapshotAndKeepalive(t *testing.T) {
	db := testutil.OpenTestDB(t)
	seedPublicTask(t, db, "task_sse_active", repository.WorkerTaskStatusPreComplete)
	previous := taskSSEKeepaliveInterval
	taskSSEKeepaliveInterval = 10 * time.Millisecond
	t.Cleanup(func() { taskSSEKeepaliveInterval = previous })
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{
		DB: db, Services: buildServices(db),
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/tasks/task_sse_active/events")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	reader := bufio.NewReader(resp.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, `"status":"pre_complete"`) {
		t.Fatalf("initial frame=%q", first)
	}
	if _, err := reader.ReadString('\n'); err != nil { // blank frame separator
		t.Fatal(err)
	}
	keepalive, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if keepalive != ": keepalive\n" {
		t.Fatalf("keepalive frame=%q", keepalive)
	}
}
