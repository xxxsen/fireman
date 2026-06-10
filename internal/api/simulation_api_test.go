package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

func seedSimulationReadyPlan(t *testing.T, db *sql.DB) (planID string) {
	t.Helper()
	plan := createTestPlan(t, db)
	planID = plan.ID

	snapRepo := repository.NewSnapshotRepo(db)
	instID := "ins_sim_equity"
	if err := snapRepo.EnsureInstrument(context.Background(), repository.Instrument{
		ID: instID, Code: "SIM001", Name: "模拟基金", Market: "CN",
		AssetClass: "equity", Region: "domestic", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}
	snapID := "snap_sim_equity"
	now := time.Now().UnixMilli()
	if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
		ID: snapID, InstrumentID: instID, PlanID: &planID,
		InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
		CompleteYearCount: 5, ObservationCount: 100,
		HistoricalCAGR: 0.08, ModeledAnnualReturn: 0.08, AnnualVolatility: 0.15, MaxDrawdown: 0.2,
		ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
		SourceMode: "akshare_historical", QualityStatus: "available",
		WarningsJSON: "[]", SourceHash: "fixture_hash", CreatedAt: now,
		Years: []repository.SnapshotYear{
			{Year: 2021, AnnualReturn: 0.10, StartDate: "2021-01-01", EndDate: "2021-12-31", Observations: 250},
			{Year: 2022, AnnualReturn: 0.05, StartDate: "2022-01-01", EndDate: "2022-12-31", Observations: 250},
			{Year: 2023, AnnualReturn: 0.08, StartDate: "2023-01-01", EndDate: "2023-12-31", Observations: 250},
			{Year: 2024, AnnualReturn: 0.07, StartDate: "2024-01-01", EndDate: "2024-12-31", Observations: 250},
			{Year: 2025, AnnualReturn: 0.06, StartDate: "2025-01-01", EndDate: "2025-12-31", Observations: 250},
		},
	}); err != nil {
		t.Fatal(err)
	}

	holdID := "hold_sim_1"
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plan_holdings (
			id, plan_id, instrument_id, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?,?,?,1,'equity','domestic',1.0,?,?,1,?,?)`,
		holdID, planID, instID, 1_000_000_00, snapID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, 1_000_000_00, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_asset_class_targets SET weight=1.0 WHERE plan_id=? AND asset_class='equity'`, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_asset_class_targets SET weight=0 WHERE plan_id=? AND asset_class IN ('bond','cash')`, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_region_targets SET weight_within_class=1.0
		WHERE plan_id=? AND asset_class='equity' AND region='domestic'`, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_region_targets SET weight_within_class=0
		WHERE plan_id=? AND asset_class='equity' AND region='foreign'`, planID); err != nil {
		t.Fatal(err)
	}
	return planID
}

func TestSimulationJobFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	services := buildServices(db, "")
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner, jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), nil, services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: services}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "99"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/simulations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "test-key-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create simulation status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	data := env["data"].(map[string]any)
	jobID := data["job_id"].(string)
	runID := data["run_id"].(string)

	// idempotency
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	env2 := decodeEnvelope(t, mustRead(t, resp2))
	data2 := env2["data"].(map[string]any)
	if data2["job_id"].(string) != jobID {
		t.Fatalf("idempotency should return same job")
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/jobs/" + jobID)
		if err != nil {
			t.Fatal(err)
		}
		env = decodeEnvelope(t, mustRead(t, resp))
		job := env["data"].(map[string]any)
		if job["status"].(string) == "succeeded" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	env = decodeEnvelope(t, mustRead(t, resp))
	run := env["data"].(map[string]any)
	if int(run["success_count"].(float64))+int(run["failure_count"].(float64)) != 1000 {
		t.Fatalf("unexpected run counts: %+v", run)
	}

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/simulations/" + runID + "/paths")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("paths status=%d", resp.StatusCode)
	}

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/simulations/" + runID + "/paths/0")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("path detail status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
}

func TestFailedSimulationJobDoesNotExposeSuccessfulSummary(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	jobsRepo := repository.NewJobRepo(db)
	simsRepo := repository.NewSimulationRepo(db)
	runner := persistFailingRunner{db: db, sims: simsRepo}
	worker := jobs.NewWorker(db, jobsRepo, simsRepo, runner, jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), nil, jobs.NewEventHub(), nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	services := buildServices(db, "")
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: services}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "42"})
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
	jobID := data["job_id"].(string)
	runID := data["run_id"].(string)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := jobsRepo.GetByID(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusFailed {
			break
		}
		if job.Status == repository.JobStatusSucceeded {
			t.Fatal("expected job to fail when simulation persist is injected")
		}
		time.Sleep(50 * time.Millisecond)
	}

	run, err := simsRepo.GetByID(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.SuccessCount != 0 || run.FailureCount != 0 {
		t.Fatalf("expected zero counts after failed persist, got success=%d failure=%d", run.SuccessCount, run.FailureCount)
	}

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	runEnv := decodeEnvelope(t, mustRead(t, resp))
	runView := runEnv["data"].(map[string]any)
	if int(runView["success_count"].(float64)) != 0 || int(runView["failure_count"].(float64)) != 0 {
		t.Fatalf("API must not expose successful run counts: %+v", runView)
	}

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/jobs/" + jobID)
	if err != nil {
		t.Fatal(err)
	}
	jobEnv := decodeEnvelope(t, mustRead(t, resp))
	if jobEnv["data"].(map[string]any)["status"].(string) != "failed" {
		t.Fatalf("expected failed job status, got %+v", jobEnv["data"])
	}
}

type persistFailingRunner struct {
	db   *sql.DB
	sims *repository.SimulationRepo
}

func (r persistFailingRunner) RunSimulation(ctx context.Context, jobID, runID string, snap *simulation.InputSnapshot, cancelCheck func() bool, progress func(done, total int, phase string)) error {
	result := simulation.Run(snap, simulation.RunOptions{
		Runs: snap.Parameters.SimulationRuns, Progress: progress, CancelCheck: cancelCheck,
	})
	summaryJSON, err := json.Marshal(result.Summary)
	if err != nil {
		return err
	}
	return fdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.sims.Complete(ctx, tx, runID, result.SuccessCount, result.FailureCount, summaryJSON); err != nil {
			return err
		}
		return fmt.Errorf("injected persist failure")
	})
}

func TestInputSnapshotHashStable(t *testing.T) {
	in := &simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion,
		PlanID:        "plan_test",
		BaseCurrency:  "CNY",
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 30, RetirementAge: 55, EndAge: 90,
			TotalAssetsMinor: 1_000_000_00, SimulationRuns: 1000, StudentTDf: 7, Seed: "1",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "h1", SnapshotID: "s1", SourceHash: "x",
			InitialMinor: 1_000_000_00, TargetWeight: 1,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15,
		}},
	}
	h1, err := simulation.HashInput(in)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := simulation.HashInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("input hash should be stable")
	}
}
