package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestStressAndSensitivityJobFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	services := buildServices(db, "")
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	analysisRunner := jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner, analysisRunner, nil,
		services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()

	// Stress / sensitivity now require an existing Monte Carlo run to attach to.
	runID := createSimulationAndWait(t, srv, planID, "7")

	stressBody, _ := json.Marshal(map[string]any{"simulation_run_id": runID})
	stressReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/stress-tests",
		bytes.NewReader(stressBody))
	stressReq.Header.Set("Content-Type", "application/json")
	stressResp, err := http.DefaultClient.Do(stressReq)
	if err != nil {
		t.Fatal(err)
	}
	if stressResp.StatusCode != http.StatusOK {
		t.Fatalf("create stress status=%d body=%s", stressResp.StatusCode, string(mustRead(t, stressResp)))
	}
	stressEnv := decodeEnvelope(t, mustRead(t, stressResp))
	stressJobID := stressEnv["data"].(map[string]any)["job_id"].(string)

	waitJobSucceeded(t, srv, stressJobID)

	stressGet, err := http.Get(srv.URL + "/api/v1/stress-tests/" + stressJobID)
	if err != nil {
		t.Fatal(err)
	}
	stressView := decodeEnvelope(t, mustRead(t, stressGet))["data"].(map[string]any)
	if stressView["status"] != "succeeded" {
		t.Fatalf("stress job not succeeded: %+v", stressView)
	}
	if stressView["simulation_run_id"].(string) != runID {
		t.Fatalf("stress should be bound to run %s, got %+v", runID, stressView["simulation_run_id"])
	}

	sensBody, _ := json.Marshal(map[string]any{"simulation_run_id": runID})
	sensReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/sensitivity-tests",
		bytes.NewReader(sensBody))
	sensReq.Header.Set("Content-Type", "application/json")
	sensResp, err := http.DefaultClient.Do(sensReq)
	if err != nil {
		t.Fatal(err)
	}
	if sensResp.StatusCode != http.StatusOK {
		t.Fatalf("create sensitivity status=%d", sensResp.StatusCode)
	}
	sensEnv := decodeEnvelope(t, mustRead(t, sensResp))
	sensJobID := sensEnv["data"].(map[string]any)["job_id"].(string)
	waitJobSucceeded(t, srv, sensJobID)

	// List filtered by run returns exactly the latest result for that run.
	listResp, err := http.Get(srv.URL + "/api/v1/plans/" + planID + "/stress-tests?simulation_run_id=" + runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listResp.Body.Close() }()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list stress status=%d", listResp.StatusCode)
	}
	listEnv := decodeEnvelope(t, mustRead(t, listResp))
	stressList := listEnv["data"].(map[string]any)["stress_tests"].([]any)
	if len(stressList) != 1 {
		t.Fatalf("expected exactly 1 stress test for run, got %d", len(stressList))
	}
}

func TestStressTestRequiresSimulationRun(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	services := buildServices(db, "")
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/plans/"+planID+"/stress-tests", "application/json",
		bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected error creating stress test without any simulation run")
	}
}

func createSimulationAndWait(t *testing.T, srv *httptest.Server, planID, seed string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": seed})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/simulations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create simulation status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	data := env["data"].(map[string]any)
	waitJobSucceeded(t, srv, data["job_id"].(string))
	return data["run_id"].(string)
}

func waitJobSucceeded(t *testing.T, srv *httptest.Server, jobID string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.URL + "/api/v1/jobs/" + jobID)
		if err != nil {
			t.Fatal(err)
		}
		env := decodeEnvelope(t, mustRead(t, resp))
		if env["data"].(map[string]any)["status"].(string) == "succeeded" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("job %s did not succeed in time", jobID)
}
