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
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner, analysisRunner, services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: services}))
	defer srv.Close()

	stressBody, _ := json.Marshal(map[string]any{"runs": 1000, "seed": 7})
	stressReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/stress-tests", bytes.NewReader(stressBody))
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
	stressView := decodeEnvelope(t, mustRead(t, stressGet))
	if stressView["data"].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("stress job not succeeded: %+v", stressView)
	}

	sensBody, _ := json.Marshal(map[string]any{"runs": 1000, "seed": 8})
	sensReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/sensitivity-tests", bytes.NewReader(sensBody))
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

	listResp, err := http.Get(srv.URL + "/api/v1/plans/" + planID + "/stress-tests")
	if err != nil {
		t.Fatal(err)
	}
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list stress status=%d", listResp.StatusCode)
	}
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
