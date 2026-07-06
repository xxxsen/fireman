package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func TestStressAndSensitivityJobFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	services := buildServices(db)
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	analysisRunner := jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner, analysisRunner, services.Research,
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
	// Regression guard: the run input_hash is a different hash
	// space than the plan config hash, so they differ; despite that, an unedited
	// plan must NOT mark the freshly completed analysis as stale.
	if ih, cc := stressView["input_hash"].(string), stressView["current_config_hash"].(string); ih == cc {
		t.Fatalf("expected run input_hash (%s) to differ from config hash (%s)", ih, cc)
	}
	if stale, _ := stressView["result_stale"].(bool); stale {
		t.Fatalf("freshly completed stress must not be stale on an unedited plan: %+v", stressView)
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

	sensGet, err := http.Get(srv.URL + "/api/v1/sensitivity-tests/" + sensJobID)
	if err != nil {
		t.Fatal(err)
	}
	sensView := decodeEnvelope(t, mustRead(t, sensGet))["data"].(map[string]any)
	if stale, _ := sensView["result_stale"].(bool); stale {
		t.Fatalf("freshly completed sensitivity must not be stale on an unedited plan: %+v", sensView)
	}

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
	if stale, _ := stressList[0].(map[string]any)["result_stale"].(bool); stale {
		t.Fatalf("listed stress for unedited plan must not be stale: %+v", stressList[0])
	}
}

// TestAttachedAnalysisListByRunRejectsForeignRun guards cross-plan isolation: a
// plan must not be able to read another plan's attached analysis by passing a
// foreign simulation_run_id.
func TestAttachedAnalysisListByRunRejectsForeignRun(t *testing.T) {
	db := testutil.OpenTestDB(t)
	// Plan A only needs to exist; plan B is the run owner.
	planA := createTestPlan(t, db).ID
	planB := seedSimulationReadyPlan(t, db)

	services := buildServices(db)
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	analysisRunner := jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner, analysisRunner, services.Research,
		services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()

	// Run a Monte Carlo simulation and a stress test on plan B.
	runB := createSimulationAndWait(t, srv, planB, "11")
	stressBody, _ := json.Marshal(map[string]any{"simulation_run_id": runB})
	stressResp, err := http.Post(srv.URL+"/api/v1/plans/"+planB+"/stress-tests", "application/json",
		bytes.NewReader(stressBody))
	if err != nil {
		t.Fatal(err)
	}
	stressJobID := decodeEnvelope(t, mustRead(t, stressResp))["data"].(map[string]any)["job_id"].(string)
	waitJobSucceeded(t, srv, stressJobID)

	// Plan A asking for plan B's run must NOT receive plan B's results.
	for _, kind := range []string{"stress-tests", "sensitivity-tests"} {
		resp, err := http.Get(srv.URL + "/api/v1/plans/" + planA + "/" + kind + "?simulation_run_id=" + runB)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode == http.StatusOK {
			env := decodeEnvelope(t, mustRead(t, resp))
			list, _ := env["data"].(map[string]any)[underscoreKey(kind)].([]any)
			if len(list) > 0 {
				_ = resp.Body.Close()
				t.Fatalf("plan A leaked plan B %s via foreign run id: %+v", kind, list)
			}
		}
		_ = resp.Body.Close()
	}
}

func underscoreKey(kind string) string {
	if kind == "stress-tests" {
		return "stress_tests"
	}
	return "sensitivity_tests"
}

// seedSimulationRun inserts a completed Monte Carlo run (and its job) directly so
// supersede tests can attach prior analysis jobs without running a worker.
func seedSimulationRun(t *testing.T, db *sql.DB, planID string) string {
	t.Helper()
	ctx := context.Background()
	jobsRepo := repository.NewJobRepo(db)
	simsRepo := repository.NewSimulationRepo(db)
	runJobID := "job_run_" + planID
	if err := jobsRepo.Create(ctx, nil, repository.Job{
		ID: runJobID, PlanID: planID, Type: repository.JobTypeSimulation,
		Status: repository.JobStatusSucceeded, InputHash: "run_ih",
	}); err != nil {
		t.Fatal(err)
	}
	runID := "simrun_" + planID
	if err := simsRepo.CreatePending(ctx, nil, repository.SimulationRun{
		ID: runID, JobID: runJobID, PlanID: planID, InputHash: "run_ih",
		InputSnapshotJSON: "{}", MarketSnapshotHash: "msh", EngineVersion: "v1",
		Runs: 1000, Seed: 1, HorizonMonths: 12, SuccessCount: 1, FailureCount: 0,
		SummaryJSON: json.RawMessage(`{"success_probability":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	return runID
}

// TestStressRerunCancelsPriorQueuedJob verifies that re-running stress
// on the same run cancels a still-queued prior job instead of orphaning it.
func TestStressRerunCancelsPriorQueuedJob(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	runID := seedSimulationRun(t, db, planID)
	services := buildServices(db)
	ctx := context.Background()

	jobsRepo := repository.NewJobRepo(db)
	analysisRepo := repository.NewAnalysisRepo(db)
	priorJobID := "job_prior_stress"
	if err := jobsRepo.Create(ctx, nil, repository.Job{
		ID: priorJobID, PlanID: planID, Type: repository.JobTypeStress,
		Status: repository.JobStatusQueued, InputHash: "run_ih", ProgressTotal: 8,
	}); err != nil {
		t.Fatal(err)
	}
	if err := analysisRepo.CreatePending(ctx, nil, repository.AnalysisResult{
		JobID: priorJobID, PlanID: planID, Type: repository.AnalysisTypeStress,
		InputHash: "run_ih", SimulationRunID: runID, ResultJSON: `{"pending":true}`,
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := services.Stress.Create(ctx, service.CreateStressTestRequest{PlanID: planID, SimulationRunID: runID})
	if err != nil {
		t.Fatalf("create stress: %v", err)
	}
	if resp.JobID == priorJobID {
		t.Fatalf("expected a new job id, got the prior one")
	}

	prior, err := jobsRepo.GetByID(ctx, priorJobID)
	if err != nil {
		t.Fatal(err)
	}
	if prior.Status != repository.JobStatusCanceled {
		t.Fatalf("prior queued job should be canceled, got %s", prior.Status)
	}
	if prior.ErrorCode != "superseded_by_newer_analysis" {
		t.Fatalf("prior job error_code = %q", prior.ErrorCode)
	}

	recs, err := analysisRepo.ListBySimulationRun(ctx, runID, repository.AnalysisTypeStress, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].JobID != resp.JobID {
		t.Fatalf("expected only the new analysis record, got %+v", recs)
	}
}

// TestSensitivityRerunRequestsCancelOfRunningJob covers the rerun-cancel behavior for a
// prior job that is already running: it receives a cancel request, and its stale
// analysis record is removed in favor of the new job.
func TestSensitivityRerunRequestsCancelOfRunningJob(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	runID := seedSimulationRun(t, db, planID)
	services := buildServices(db)
	ctx := context.Background()

	jobsRepo := repository.NewJobRepo(db)
	analysisRepo := repository.NewAnalysisRepo(db)
	priorJobID := "job_prior_sens"
	if err := jobsRepo.Create(ctx, nil, repository.Job{
		ID: priorJobID, PlanID: planID, Type: repository.JobTypeSensitivity,
		Status: repository.JobStatusRunning, InputHash: "run_ih", ProgressTotal: 50,
	}); err != nil {
		t.Fatal(err)
	}
	if err := analysisRepo.CreatePending(ctx, nil, repository.AnalysisResult{
		JobID: priorJobID, PlanID: planID, Type: repository.AnalysisTypeSensitivity,
		InputHash: "run_ih", SimulationRunID: runID, ResultJSON: `{"pending":true}`,
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := services.Sensitivity.Create(ctx,
		service.CreateSensitivityTestRequest{PlanID: planID, SimulationRunID: runID})
	if err != nil {
		t.Fatalf("create sensitivity: %v", err)
	}

	prior, err := jobsRepo.GetByID(ctx, priorJobID)
	if err != nil {
		t.Fatal(err)
	}
	if !prior.CancelRequested {
		t.Fatalf("prior running job should have cancel_requested set, got %+v", prior)
	}
	if prior.ErrorCode != "superseded_by_newer_analysis" {
		t.Fatalf("prior running job error_code = %q want superseded_by_newer_analysis", prior.ErrorCode)
	}

	recs, err := analysisRepo.ListBySimulationRun(ctx, runID, repository.AnalysisTypeSensitivity, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].JobID != resp.JobID {
		t.Fatalf("expected only the new analysis record, got %+v", recs)
	}
}

func TestStressTestRequiresSimulationRun(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	services := buildServices(db)
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
