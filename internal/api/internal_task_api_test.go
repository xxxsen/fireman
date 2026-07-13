package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/fireman/fireman/internal/testutil"
)

type internalStack struct {
	srv         *httptest.Server
	db          *sql.DB
	assets      *service.MarketAssetService
	client      *http.Client
	coordinator *taskcore.Coordinator
	resources   *resourcedb.DB
	finalizer   *service.TaskFinalizer
}

func newInternalStack(t *testing.T) internalStack {
	t.Helper()
	db := testutil.OpenTestDB(t)
	resources, err := resourcedb.Open(context.Background(), filepath.Join(t.TempDir(), "resource.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resources.Close() })
	tasks := repository.NewWorkerTaskRepo(db)
	coordinator := taskcore.NewCoordinator(db, tasks, taskcore.DefaultRegistry(), taskcore.NewEventHub())
	assets := repository.NewMarketAssetRepo(db)
	finalizer := service.NewTaskFinalizer(
		db, tasks, assets, repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db), resources,
		repository.NewWorkerTaskFinalizeRecordRepo(db),
	)
	finalizer.SetCoordinator(coordinator)
	srv := httptest.NewServer(NewInternalRouter(context.Background(), InternalDeps{
		Coordinator: coordinator, Resources: resources,
	}))
	t.Cleanup(srv.Close)
	return internalStack{
		srv: srv, db: db, client: srv.Client(), coordinator: coordinator,
		resources: resources, finalizer: finalizer,
		assets: service.NewMarketAssetService(db, tasks, assets, coordinator),
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func createInternalTask(t *testing.T, st internalStack, id string) {
	t.Helper()
	err := fdb.WithTx(context.Background(), st.db, func(tx *sql.Tx) error {
		return st.coordinator.CreateTx(context.Background(), tx, &repository.WorkerTask{
			ID: id, WorkerType: repository.WorkerTypeSidecar,
			Type:   repository.WorkerTaskTypeFXRateSync,
			Status: repository.WorkerTaskStatusPending, PayloadJSON: `{"pairs":["USDCNY"]}`,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func postTaskJSON(t *testing.T, client *http.Client, url string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := client.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, envelope
}

func TestInternalWorkerTaskProtocol(t *testing.T) {
	st := newInternalStack(t)
	createInternalTask(t, st, "task_protocol")

	resp, err := st.client.Get(st.srv.URL + "/internal/worker-tasks?worker_type=sidecar_worker&status=pending&limit=20")
	if err != nil {
		t.Fatal(err)
	}
	var listed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&listed)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d body=%+v", resp.StatusCode, listed)
	}
	items := listed["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list items=%+v", items)
	}

	for name, query := range map[string]string{
		"missing worker type": "",
		"wrong worker type":   "?worker_type=go_worker",
	} {
		t.Run(name, func(t *testing.T) {
			response, getErr := st.client.Get(st.srv.URL + "/internal/worker-tasks/task_protocol" + query)
			if getErr != nil {
				t.Fatal(getErr)
			}
			defer response.Body.Close()
			var body map[string]any
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusForbidden || body["code"] != taskcore.ErrWorkerTypeMismatch {
				t.Fatalf("detail status=%d body=%+v", response.StatusCode, body)
			}
		})
	}
	response, getErr := st.client.Get(st.srv.URL +
		"/internal/worker-tasks/task_protocol?worker_type=sidecar_worker")
	if getErr != nil {
		t.Fatal(getErr)
	}
	defer response.Body.Close()
	var detail map[string]any
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || detail["data"].(map[string]any)["id"] != "task_protocol" {
		t.Fatalf("worker-scoped detail status=%d body=%+v", response.StatusCode, detail)
	}

	owner := map[string]any{
		"worker_type": "sidecar_worker", "worker_id": "sidecar_worker:test",
		"claim_token": "api-claim-token-00000001",
	}
	status, claimed := postTaskJSON(t, st.client,
		st.srv.URL+"/internal/worker-tasks/task_protocol/claim", owner)
	if status != http.StatusOK || claimed["data"].(map[string]any)["status"] != "running" {
		t.Fatalf("claim status=%d body=%+v", status, claimed)
	}

	heartbeat := map[string]any{}
	for key, value := range owner {
		heartbeat[key] = value
	}
	heartbeat["progress_current"] = 1
	heartbeat["progress_total"] = 2
	heartbeat["phase"] = "fetching"
	status, _ = postTaskJSON(t, st.client,
		st.srv.URL+"/internal/worker-tasks/task_protocol/heartbeat", heartbeat)
	if status != http.StatusOK {
		t.Fatalf("heartbeat status=%d", status)
	}

	compressed, err := resourcedb.GzipBytes([]byte(`{"type":"fx_rate_sync","rates":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(compressed)
	req, _ := http.NewRequest(http.MethodPost,
		st.srv.URL+"/internal/worker-tasks/task_protocol/resources", bytes.NewReader(compressed))
	req.Header.Set("X-Fireman-Worker-Type", "sidecar_worker")
	req.Header.Set("X-Fireman-Worker-Id", "sidecar_worker:test")
	req.Header.Set("X-Fireman-Claim-Token", "api-claim-token-00000001")
	req.Header.Set("X-Fireman-Content-Type", "application/json")
	req.Header.Set("X-Fireman-Content-Encoding", "gzip")
	req.Header.Set("X-Fireman-Content-Sha256", hex.EncodeToString(digest[:]))
	resp, err = st.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var upload map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&upload)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d body=%+v", resp.StatusCode, upload)
	}
	resultKey := upload["data"].(map[string]any)["result_key"].(string)
	if resultKey != "resource:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("result key=%s", resultKey)
	}

	result := map[string]any{}
	for key, value := range owner {
		result[key] = value
	}
	result["outcome"] = "success"
	result["result_key"] = resultKey
	status, accepted := postTaskJSON(t, st.client,
		st.srv.URL+"/internal/worker-tasks/task_protocol/result", result)
	if status != http.StatusOK || accepted["data"].(map[string]any)["status"] != "pre_complete" {
		t.Fatalf("result status=%d body=%+v", status, accepted)
	}
}

func TestInternalClaimConflictAndWorkerTypeMismatch(t *testing.T) {
	st := newInternalStack(t)
	createInternalTask(t, st, "task_conflict")
	wrong := map[string]any{
		"worker_type": "go_worker", "worker_id": "go_worker:test",
		"claim_token": "wrong-worker-token-00001",
	}
	status, body := postTaskJSON(t, st.client,
		st.srv.URL+"/internal/worker-tasks/task_conflict/claim", wrong)
	if status != http.StatusForbidden || body["code"] != taskcore.ErrWorkerTypeMismatch {
		t.Fatalf("wrong worker status=%d body=%+v", status, body)
	}
	owner := map[string]any{
		"worker_type": "sidecar_worker", "worker_id": "sidecar_worker:one",
		"claim_token": "owner-token-000000000001",
	}
	status, _ = postTaskJSON(t, st.client,
		st.srv.URL+"/internal/worker-tasks/task_conflict/claim", owner)
	if status != http.StatusOK {
		t.Fatal(fmt.Errorf("first claim status %d", status))
	}
	owner["claim_token"] = "owner-token-000000000002"
	status, body = postTaskJSON(t, st.client,
		st.srv.URL+"/internal/worker-tasks/task_conflict/claim", owner)
	if status != http.StatusConflict || body["code"] != taskcore.ErrClaimConflict {
		t.Fatalf("claim conflict status=%d body=%+v", status, body)
	}
}
