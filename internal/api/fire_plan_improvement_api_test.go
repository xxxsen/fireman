package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/improvement"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

func TestFirePlanImprovementEndToEnd(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedOneYearSimulationPlan(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE plan_parameters SET current_age=40,
		retirement_age=41,end_age=43,total_assets_minor=10000000,annual_savings_minor=0,
		annual_spending_minor=40000000,annual_retirement_income_minor=0 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE plan_holdings SET current_amount_minor=10000000 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}

	services := buildServices(db)
	worker := newTestTaskWorker(db, services)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go worker.Start(workerCtx, 1)
	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, Services: services}))
	defer srv.Close()

	source := postImprovementJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "11"}, http.StatusOK, "source-simulation")
	waitImprovementTask(t, db, source["task_id"].(string))
	sourceRunID := source["run_id"].(string)

	tasksBefore := countImprovementRows(t, db, "worker_tasks")
	readiness := getImprovementJSON(t, srv,
		"/api/v1/plans/"+planID+"/improvement-readiness?simulation_run_id="+sourceRunID, http.StatusOK)
	if readiness["ready"] != true || readiness["source_run"].(map[string]any)["id"] != sourceRunID {
		t.Fatalf("unexpected readiness: %#v", readiness)
	}
	if got := countImprovementRows(t, db, "worker_tasks"); got != tasksBefore {
		t.Fatalf("readiness wrote tasks: before=%d after=%d", tasksBefore, got)
	}

	request := map[string]any{
		"simulation_run_id": sourceRunID, "target_success_probability": 0.5,
		"savings_increase": map[string]any{"max_increase_minor": 100000000, "step_minor": 25000000},
	}
	created := postImprovementJSON(t, srv, "/api/v1/plans/"+planID+"/improvement-runs",
		request, http.StatusOK, "improvement-create")
	reused := postImprovementJSON(t, srv, "/api/v1/plans/"+planID+"/improvement-runs",
		request, http.StatusOK, "improvement-retry")
	if reused["run_id"] != created["run_id"] || reused["reused"] != true {
		t.Fatalf("active improvement was not reused: created=%#v reused=%#v", created, reused)
	}
	waitImprovementTask(t, db, created["task_id"].(string))
	runID := created["run_id"].(string)

	detail := getImprovementJSON(t, srv, "/api/v1/improvement-runs/"+runID, http.StatusOK)
	if detail["status"] != repository.WorkerTaskStatusComplete {
		t.Fatalf("improvement status=%v", detail["status"])
	}
	result := detail["result"].(map[string]any)
	if result["target_reached"] != true {
		t.Fatalf("expected a feasible proposal: %#v", result)
	}
	proposals := result["proposals"].([]any)
	if len(proposals) == 0 {
		t.Fatal("completed improvement returned no proposals")
	}
	proposal := proposals[0].(map[string]any)
	proposalID := proposal["id"].(string)

	before, err := repository.NewParametersRepo(db).Get(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repository.NewPlanRepo(db).GetByID(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	appsBefore := countImprovementRows(t, db, "fire_plan_improvement_applications")
	preview := postImprovementJSON(t, srv,
		"/api/v1/improvement-runs/"+runID+"/proposals/"+proposalID+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusOK, "")
	if got := countImprovementRows(t, db, "fire_plan_improvement_applications"); got != appsBefore {
		t.Fatalf("preview wrote application rows: before=%d after=%d", appsBefore, got)
	}
	expiresAt := int64(preview["preview_expires_at"].(float64))
	services.Improvements.SetClockForTest(func() time.Time {
		return time.UnixMilli(expiresAt + 1)
	})
	expired := postImprovementJSON(t, srv,
		"/api/v1/improvement-runs/"+runID+"/proposals/"+proposalID+"/apply",
		map[string]any{
			"expected_plan_config_version": plan.ConfigVersion,
			"preview_hash":                 preview["preview_hash"],
			"preview_expires_at":           preview["preview_expires_at"],
		}, http.StatusConflict, "")
	if expired["code"] != "improvement_preview_stale" {
		t.Fatalf("expired preview error=%#v", expired)
	}
	services.Improvements.SetClockForTest(time.Now)
	if _, err := db.ExecContext(ctx, `UPDATE market_asset_simulation_snapshots
		SET source_hash='changed-market-hash' WHERE id='snap_one_year'`); err != nil {
		t.Fatal(err)
	}
	marketChanged := postImprovementJSON(t, srv,
		"/api/v1/improvement-runs/"+runID+"/proposals/"+proposalID+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusConflict, "")
	if marketChanged["code"] != "improvement_source_market_changed" {
		t.Fatalf("market change error=%#v", marketChanged)
	}
	if _, err := db.ExecContext(ctx, `UPDATE market_asset_simulation_snapshots
		SET source_hash='one_year_hash' WHERE id='snap_one_year'`); err != nil {
		t.Fatal(err)
	}
	preview = postImprovementJSON(t, srv,
		"/api/v1/improvement-runs/"+runID+"/proposals/"+proposalID+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusOK, "")
	applyBody := map[string]any{
		"expected_plan_config_version": plan.ConfigVersion,
		"preview_hash":                 preview["preview_hash"],
		"preview_expires_at":           preview["preview_expires_at"],
	}
	postImprovementJSON(t, srv,
		"/api/v1/improvement-runs/"+runID+"/proposals/"+proposalID+"/apply",
		applyBody, http.StatusOK, "")
	after, err := repository.NewParametersRepo(db).Get(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.RetirementAge = int(proposal["result_retirement_age"].(float64))
	want.AnnualSavingsMinor = int64(proposal["result_annual_savings_minor"].(float64))
	want.AnnualSpendingMinor = int64(proposal["result_annual_spending_minor"].(float64))
	want.AnnualRetirementIncomeMinor = int64(proposal["result_annual_retirement_income_minor"].(float64))
	want.UpdatedAt = after.UpdatedAt
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("apply changed fields outside the proposal\nbefore=%#v\nafter=%#v\nwant=%#v", before, after, want)
	}
	updatedPlan, err := repository.NewPlanRepo(db).GetByID(ctx, planID)
	if err != nil || updatedPlan.ConfigVersion != plan.ConfigVersion+1 {
		t.Fatalf("plan version after apply=%d err=%v", updatedPlan.ConfigVersion, err)
	}
	postImprovementJSON(t, srv,
		"/api/v1/improvement-runs/"+runID+"/proposals/"+proposalID+"/apply",
		applyBody, http.StatusConflict, "")
	stale := getImprovementJSON(t, srv, "/api/v1/improvement-runs/"+runID, http.StatusOK)
	if stale["result_stale"] != true {
		t.Fatal("applied result must become stale after the plan version changes")
	}

	verification := postImprovementJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "11"}, http.StatusOK, "verification-simulation")
	waitImprovementTask(t, db, verification["task_id"].(string))
	verified, err := repository.NewSimulationRepo(db).GetByID(ctx, verification["run_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var verifiedSnapshot simulation.InputSnapshot
	if err := json.Unmarshal([]byte(verified.InputSnapshotJSON), &verifiedSnapshot); err != nil {
		t.Fatal(err)
	}
	verifiedHash, err := simulation.HashInput(&verifiedSnapshot)
	if err != nil || verifiedHash != proposal["candidate_snapshot_hash"] {
		t.Fatalf("verification snapshot hash=%s want=%v err=%v", verifiedHash, proposal["candidate_snapshot_hash"], err)
	}
	var verifiedSummary simulation.Summary
	if err := json.Unmarshal([]byte(verified.SummaryJSON), &verifiedSummary); err != nil {
		t.Fatal(err)
	}
	if !closeFloat(verifiedSummary.SuccessProbability, proposal["success_probability"].(float64)) ||
		!closeFloat(verifiedSummary.SuccessWilsonLow, proposal["success_wilson_low"].(float64)) ||
		!closeFloat(verifiedSummary.SuccessWilsonHigh, proposal["success_wilson_high"].(float64)) {
		t.Fatalf("verification result differs: summary=%#v proposal=%#v", verifiedSummary, proposal)
	}
}

func TestDeletingPlanCancelsActiveImprovementTask(t *testing.T) {
	db := testutil.OpenTestDB(t)
	services := buildServices(db)
	plan := createTestPlan(t, db)
	ctx := context.Background()
	taskID := "task_delete_improvement"
	runID := "fpir_delete"
	err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := services.TaskCoordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: taskID, WorkerType: repository.WorkerTypeGo,
			Type: repository.WorkerTaskTypeFirePlanImprovement, Status: repository.WorkerTaskStatusPending,
			ScopeType: "plan", ScopeID: plan.ID, PayloadJSON: `{"run_id":"fpir_delete"}`,
		}); err != nil {
			return err
		}
		return repository.NewFirePlanImprovementRepo(db).CreateTx(ctx, tx, &repository.FirePlanImprovementRun{
			ID: runID, TaskID: taskID, PlanID: plan.ID, SourceSimulationRunID: "sim_old",
			InputHash: "input", AlgorithmVersion: improvement.AlgorithmVersion,
			SourceEngineVersion: simulation.EngineVersion, SourceConfigHash: "config",
			SourceMarketHash: "market", ConfigJSON: `{}`, InputSnapshotJSON: `{}`,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := services.Plans.Delete(ctx, plan.ID); err != nil {
		t.Fatal(err)
	}
	task, err := repository.NewWorkerTaskRepo(db).GetByID(ctx, taskID)
	if err != nil || task.Status != repository.WorkerTaskStatusCanceled || !task.CancelRequested {
		t.Fatalf("task after plan delete=%#v err=%v", task, err)
	}
	if _, err := repository.NewFirePlanImprovementRepo(db).GetByID(ctx, runID); !errors.Is(err, repository.ErrFirePlanImprovementNotFound) {
		t.Fatalf("improvement run survived plan delete: %v", err)
	}
	claimable, err := services.TaskCoordinator.ListClaimable(ctx, repository.WorkerTypeGo,
		[]string{repository.WorkerTaskTypeFirePlanImprovement}, 10, nil, nil, "")
	if err != nil || len(claimable) != 0 {
		t.Fatalf("deleted plan task remains claimable: %#v err=%v", claimable, err)
	}
}

func postImprovementJSON(t *testing.T, srv *httptest.Server, path string, body any, status int, idempotencyKey string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw = mustRead(t, resp)
	if resp.StatusCode != status {
		t.Fatalf("POST %s status=%d want=%d body=%s", path, resp.StatusCode, status, raw)
	}
	env := decodeEnvelope(t, raw)
	if status != http.StatusOK {
		return env
	}
	return env["data"].(map[string]any)
}

func getImprovementJSON(t *testing.T, srv *httptest.Server, path string, status int) map[string]any {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	raw := mustRead(t, resp)
	if resp.StatusCode != status {
		t.Fatalf("GET %s status=%d want=%d body=%s", path, resp.StatusCode, status, raw)
	}
	return decodeEnvelope(t, raw)["data"].(map[string]any)
}

func waitImprovementTask(t *testing.T, db *sql.DB, taskID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		task, err := repository.NewWorkerTaskRepo(db).GetByID(context.Background(), taskID)
		if err != nil {
			t.Fatal(err)
		}
		switch task.Status {
		case repository.WorkerTaskStatusComplete:
			return
		case repository.WorkerTaskStatusFailed, repository.WorkerTaskStatusCanceled:
			t.Fatalf("task %s ended as %s: %s %s", taskID, task.Status, task.ErrorCode, task.ErrorMessage)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("task %s did not complete", taskID)
}

func countImprovementRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	query := ""
	switch table {
	case "worker_tasks":
		query = "SELECT COUNT(*) FROM worker_tasks"
	case "fire_plan_improvement_applications":
		query = "SELECT COUNT(*) FROM fire_plan_improvement_applications"
	default:
		t.Fatalf("unsupported count table %q", table)
	}
	var count int
	if err := db.QueryRowContext(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func closeFloat(a, b float64) bool { return math.Abs(a-b) < 1e-12 }
