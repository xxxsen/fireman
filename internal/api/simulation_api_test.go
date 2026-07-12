package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

func seedSimulationReadyPlan(t *testing.T, db *sql.DB) string {
	t.Helper()
	plan := createTestPlan(t, db)
	planID := plan.ID

	snapRepo := repository.NewSnapshotRepo(db)
	assetKey := "CN|test|sh|SIM001"
	if err := snapRepo.EnsureMarketAsset(context.Background(), repository.MarketAsset{
		AssetKey: assetKey, Symbol: "SIM001", Name: "模拟基金",
		Market: "CN", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}
	snapID := "snap_sim_equity"
	now := time.Now().UnixMilli()
	if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
		ID: snapID, AssetKey: assetKey, PlanID: &planID,
		InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
		CompleteYearCount: 5, DailyObservationCount: 100, MonthlyReturnCount: 60,
		VolatilityMethod: "monthly_log_return_sample_stddev_annualized",
		MetricsVersion:   "monthly_log_return_v1", HistoryDepth: "five_plus_years",
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
			id, plan_id, asset_key, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?,?,?,1,'equity','domestic',1.0,?,?,1,?,?)`,
		holdID, planID, assetKey, 1_000_000_00, snapID, now, now); err != nil {
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
		UPDATE plan_asset_class_targets SET weight=0 WHERE plan_id=? AND asset_class IN ('bond','cash')`,
		planID); err != nil {
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

func seedOneYearSimulationPlan(t *testing.T, db *sql.DB) string {
	t.Helper()
	plan := createTestPlan(t, db)
	planID := plan.ID

	snapRepo := repository.NewSnapshotRepo(db)
	assetKey := "CN|test|sh|ONE001"
	if err := snapRepo.EnsureMarketAsset(context.Background(), repository.MarketAsset{
		AssetKey: assetKey, Symbol: "ONE001", Name: "一年样本基金",
		Market: "CN", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}
	snapID := "snap_one_year"
	now := time.Now().UnixMilli()
	if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
		ID: snapID, AssetKey: assetKey, PlanID: &planID,
		InclusionDate: "2026-06-14", AsOfDate: "2026-06-14",
		CompleteYearCount: 1, DailyObservationCount: 252, MonthlyReturnCount: 12,
		VolatilityMethod: "monthly_log_return_sample_stddev_annualized",
		MetricsVersion:   "monthly_log_return_v1", HistoryDepth: "one_year",
		HistoricalCAGR: 0.05, ModeledAnnualReturn: 0.05, AnnualVolatility: 0.12, MaxDrawdown: 0.1,
		ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
		SourceMode: "akshare_historical", QualityStatus: "available",
		WarningsJSON: `["仅有 1 个完整自然年度，收益与风险估计的不确定性较高"]`,
		SourceHash:   "one_year_hash", CreatedAt: now,
		Years: []repository.SnapshotYear{
			{Year: 2025, AnnualReturn: 0.05, StartDate: "2025-01-01", EndDate: "2025-12-31", Observations: 250},
		},
	}); err != nil {
		t.Fatal(err)
	}

	holdID := "hold_one_year"
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plan_holdings (
			id, plan_id, asset_key, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?,?,?,1,'equity','domestic',1.0,?,?,1,?,?)`,
		holdID, planID, assetKey, 1_000_000_00, snapID, now, now); err != nil {
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
		UPDATE plan_asset_class_targets SET weight=0 WHERE plan_id=? AND asset_class IN ('bond','cash')`,
		planID); err != nil {
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

func TestOneCompleteYearSimulationJobFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedOneYearSimulationPlan(t, db)

	services := buildServices(db)
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner,
		jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), services.Research, services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "11"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/simulations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "one-year-sim")
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

	deadline := time.Now().Add(15 * time.Second)
	jobSucceeded := false
	for time.Now().Before(deadline) {
		job, err := repository.NewJobRepo(db).GetByID(context.Background(), jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == repository.JobStatusSucceeded {
			jobSucceeded = true
			break
		}
		if job.Status == repository.JobStatusFailed {
			t.Fatalf("job failed: %s %s", job.ErrorCode, job.ErrorMessage)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !jobSucceeded {
		t.Fatal("simulation job did not complete")
	}

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get simulation status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	env = decodeEnvelope(t, mustRead(t, resp))
	run := env["data"].(map[string]any)
	summary, ok := run["summary_json"].(map[string]any)
	if !ok {
		t.Fatalf("missing summary_json: %+v", run)
	}
	rawWarnings, ok := summary["model_warnings"].([]any)
	if !ok || len(rawWarnings) == 0 {
		t.Fatalf("expected model_warnings in summary: %+v", summary)
	}
	for _, w := range rawWarnings {
		msg, ok := w.(string)
		if !ok {
			continue
		}
		if strings.Contains(msg, "一年样本基金") && strings.Contains(msg, "ONE001") &&
			strings.Contains(msg, "仅有 1 个完整自然年度") {
			return
		}
	}
	t.Fatalf("model_warnings missing one-year asset warning: %v", rawWarnings)
}

func TestCreateSimulationRejectsPersistedInvalidTransactionCost(t *testing.T) {
	for _, rate := range []float64{-0.01, 1.0} {
		t.Run(strconv.FormatFloat(rate, 'g', -1, 64), func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			planID := seedSimulationReadyPlan(t, db)
			if _, err := db.Exec(`UPDATE plan_parameters SET transaction_cost_rate=? WHERE plan_id=?`, rate, planID); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
			defer srv.Close()
			body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "11"})
			resp, err := http.Post(
				srv.URL+"/api/v1/plans/"+planID+"/simulations",
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				t.Fatal(err)
			}
			raw := mustRead(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
			}
			assertErrorCode(t, raw, "parameters_invalid")
		})
	}
}

// TestScenarioComparisonEndpoint verifies that comparison is bound to one
// immutable run and remains unchanged after the current plan is edited.
func TestScenarioComparisonEndpoint(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	// Keep all three scenario medians solvent so the endpoint test can verify
	// strict return ordering instead of comparing several legitimate ruin zeros.
	if _, err := db.Exec(`UPDATE plan_parameters SET annual_spending_minor=? WHERE plan_id=?`, 50_000_00, planID); err != nil {
		t.Fatal(err)
	}

	services := buildServices(db)
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner,
		jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), services.Research, services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()
	runID := createSimulationAndWait(t, srv, planID, "42")

	endpoint := srv.URL + "/api/v1/plans/" + planID + "/simulations/" + runID + "/scenario-comparison"
	resp, err := http.DefaultClient.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scenario comparison status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	data := env["data"].(map[string]any)
	if data["base_run_id"] != runID {
		t.Fatalf("comparison base run = %v, want %s", data["base_run_id"], runID)
	}
	scenarios, ok := data["scenarios"].([]any)
	if !ok || len(scenarios) != 3 {
		t.Fatalf("expected 3 scenarios, got %+v", data["scenarios"])
	}

	fwd := map[string]float64{}
	p50 := map[string]float64{}
	for _, raw := range scenarios {
		m := raw.(map[string]any)
		key := m["scenario"].(string)
		fwd[key] = m["forward_return"].(float64)
		p50[key] = m["terminal_p50_minor"].(float64)
		if m["real_terminal_p50_minor"].(float64) > m["terminal_p50_minor"].(float64) {
			t.Fatalf("[%s] real P50 should not exceed nominal P50: %+v", key, m)
		}
	}
	if !(fwd["conservative"] < fwd["baseline"] && fwd["baseline"] < fwd["optimistic"]) {
		t.Fatalf("forward return must increase conservative<baseline<optimistic: %+v", fwd)
	}
	if !(p50["conservative"] < p50["baseline"] && p50["baseline"] < p50["optimistic"]) {
		t.Fatalf("terminal P50 must increase conservative<baseline<optimistic: %+v", p50)
	}

	if _, err := db.Exec(`UPDATE plan_parameters SET annual_spending_minor=? WHERE plan_id=?`, 500_000_00, planID); err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	repeated := decodeEnvelope(t, mustRead(t, resp))["data"]
	if !reflect.DeepEqual(data, repeated) {
		t.Fatalf("current plan edit changed frozen comparison\nfirst=%+v\nsecond=%+v", data, repeated)
	}

	oldJobID, oldRunID := "job_old_compare", "run_old_compare"
	if _, err := db.Exec(`INSERT INTO jobs (id, plan_id, type, status, input_hash, created_at)
		VALUES (?, ?, 'simulation', 'succeeded', 'old', 1)`, oldJobID, planID); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewSimulationRepo(db).CreatePending(context.Background(), nil, repository.SimulationRun{
		ID: oldRunID, JobID: oldJobID, PlanID: planID, InputHash: "old", InputSnapshotJSON: `{}`,
		MarketSnapshotHash: "old", EngineVersion: "3.3.0", Runs: 1, Seed: 1, HorizonMonths: 1,
	}); err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/plans/" + planID + "/simulations/" + oldRunID + "/scenario-comparison")
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("legacy comparison status=%d body=%s", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "scenario_comparison_unsupported")
}

// TestReturnOverrideEndpoint verifies that an asset-level override is
// validated, persisted, and applied to the next run's frozen forward return
// (source = plan_override), and only held instruments may be overridden.
func TestReturnOverrideEndpoint(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	services := buildServices(db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
	defer srv.Close()
	base := srv.URL + "/api/v1/plans/" + planID + "/return-overrides"

	heldKey := url.PathEscape("CN|test|sh|SIM001")
	unheldKey := url.PathEscape("CN|test|sh|NOTHELD")

	// Missing reason is rejected.
	if status := putJSON(t, base+"/"+heldKey,
		map[string]any{"forward_return": 0.2, "expires_at": "2099-12-31"}); status == http.StatusOK {
		t.Fatal("override without reason must be rejected")
	}
	// Override for an asset not held by the plan is rejected.
	if status := putJSON(t, base+"/"+unheldKey,
		map[string]any{"forward_return": 0.2, "reason": "x", "expires_at": "2099-12-31"}); status == http.StatusOK {
		t.Fatal("override for unheld asset must be rejected")
	}

	// Valid override is accepted.
	if status := putJSON(t, base+"/"+heldKey, map[string]any{
		"forward_return": 0.25, "reason": "锁定到期收益率", "expires_at": "2099-12-31",
	}); status != http.StatusOK {
		t.Fatalf("valid override status=%d", status)
	}

	// It shows up in the list.
	resp, err := http.DefaultClient.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	listEnv := decodeEnvelope(t, mustRead(t, resp))
	overrides := listEnv["data"].(map[string]any)["overrides"].([]any)
	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %+v", overrides)
	}

	// The next run freezes the override as the forward return.
	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "7"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/simulations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create run status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	runID := decodeEnvelope(t, mustRead(t, resp))["data"].(map[string]any)["run_id"].(string)

	resp, err = http.DefaultClient.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	run := decodeEnvelope(t, mustRead(t, resp))["data"].(map[string]any)
	assumption := run["assumption"].(map[string]any)
	assets := assumption["assets"].([]any)
	var found bool
	for _, raw := range assets {
		a := raw.(map[string]any)
		if a["is_cash"].(bool) {
			continue
		}
		if a["source"].(string) != "plan_override" {
			t.Fatalf("expected plan_override source, got %+v", a)
		}
		if got := a["forward_annual_geometric_return"].(float64); got < 0.2499 || got > 0.2501 {
			t.Fatalf("forward return not overridden: %v", got)
		}
		found = true
	}
	if !found {
		t.Fatalf("no non-cash asset in assumption view: %+v", assets)
	}

	// Delete clears it.
	delReq, _ := http.NewRequest(http.MethodDelete, base+"/"+heldKey, nil)
	resp, err = http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatal(err)
	}
	delStatus := resp.StatusCode
	_ = mustRead(t, resp)
	if delStatus != http.StatusOK {
		t.Fatalf("delete override status=%d", delStatus)
	}
	resp, err = http.DefaultClient.Get(base)
	if err != nil {
		t.Fatal(err)
	}
	listEnv = decodeEnvelope(t, mustRead(t, resp))
	if got := listEnv["data"].(map[string]any)["overrides"].([]any); len(got) != 0 {
		t.Fatalf("override should be deleted, got %+v", got)
	}
}

func putJSON(t *testing.T, url string, payload map[string]any) int {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	status := resp.StatusCode
	_ = mustRead(t, resp)
	return status
}

func TestSimulationJobFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	services := buildServices(db)
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner,
		jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), services.Research, services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
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
	pathsEnv := decodeEnvelope(t, mustRead(t, resp))
	pathList := pathsEnv["data"].(map[string]any)["paths"].([]any)
	// Pick a representative path that ran the full horizon (succeeded) so the
	// snake_case contract check (monthly == 12×yearly) is exercised against a
	// full-horizon path. The exact path numbers depend on the active assumption
	// model (system_cma_v3), so select by outcome rather than a hardcoded index.
	fullHorizonPathNo := -1
	for _, raw := range pathList {
		p := raw.(map[string]any)
		if p["succeeded"].(bool) {
			fullHorizonPathNo = int(p["path_no"].(float64))
			break
		}
	}
	if fullHorizonPathNo < 0 {
		t.Fatalf("no succeeded representative path to verify path-detail contract: %+v", pathList)
	}

	resp, err = http.DefaultClient.Get(
		srv.URL + "/api/v1/simulations/" + runID + "/paths/" + strconv.Itoa(fullHorizonPathNo))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("path detail status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	detailBody := mustRead(t, resp)
	assertPathDetailSnakeCaseContract(t, detailBody)
}

// assertPathDetailSnakeCaseContract guards the path detail API contract: the
// frontend reads snake_case fields, so MonthRecord/YearRecord must serialize
// with snake_case JSON tags.
func assertPathDetailSnakeCaseContract(t *testing.T, body []byte) {
	t.Helper()
	env := decodeEnvelope(t, body)
	detail := env["data"].(map[string]any)

	monthly, ok := detail["monthly"].([]any)
	if !ok || len(monthly) == 0 {
		t.Fatalf("path detail monthly missing or empty: %+v", detail["monthly"])
	}
	m0 := monthly[0].(map[string]any)
	for _, key := range []string{"month_offset", "total_wealth_minor", "spending_minor", "income_minor", "tax_minor", "transaction_cost", "drawdown", "rebalanced"} {
		if _, present := m0[key]; !present {
			t.Fatalf("monthly[0] missing snake_case field %q: %+v", key, m0)
		}
	}
	for _, pascal := range []string{"MonthOffset", "TotalWealthMinor", "StartWealthMinor"} {
		if _, present := m0[pascal]; present {
			t.Fatalf("monthly[0] leaked PascalCase field %q: %+v", pascal, m0)
		}
	}

	// Monthly rows must be real (not fake zero spacers). For a path
	// that runs the full horizon, monthly length equals 12×yearly and the
	// first/last month_offset are parseable, increasing month indices.
	mLast := monthly[len(monthly)-1].(map[string]any)
	firstOffset, ok1 := m0["month_offset"].(float64)
	lastOffset, ok2 := mLast["month_offset"].(float64)
	if !ok1 || !ok2 || lastOffset <= firstOffset {
		t.Fatalf("monthly month_offset not increasing: first=%v last=%v", m0["month_offset"], mLast["month_offset"])
	}
	if _, ok := mLast["total_wealth_minor"].(float64); !ok {
		t.Fatalf("monthly last row total_wealth_minor not numeric: %+v", mLast)
	}

	yearly, ok := detail["yearly"].([]any)
	if !ok || len(yearly) == 0 {
		t.Fatalf("path detail yearly missing or empty: %+v", detail["yearly"])
	}
	if len(monthly) != len(yearly)*12 {
		t.Fatalf("monthly length %d != 12×yearly %d (full-horizon path expected)", len(monthly), len(yearly)*12)
	}
	y0 := yearly[0].(map[string]any)
	for _, key := range []string{"year", "start_wealth_minor", "income_minor", "spending_minor", "tax_minor", "transaction_cost", "investment_gain_loss", "end_wealth_minor", "year_end_drawdown", "max_intra_year_dd", "annual_return", "rebalanced"} {
		if _, present := y0[key]; !present {
			t.Fatalf("yearly[0] missing snake_case field %q: %+v", key, y0)
		}
	}
	for _, pascal := range []string{"Year", "StartWealthMinor", "EndWealthMinor"} {
		if _, present := y0[pascal]; present {
			t.Fatalf("yearly[0] leaked PascalCase field %q: %+v", pascal, y0)
		}
	}

	// Path detail must carry frozen-snapshot asset labels so the UI
	// renders instrument names instead of internal holding IDs in weight rows.
	labels, ok := detail["asset_labels"].(map[string]any)
	if !ok || len(labels) == 0 {
		t.Fatalf("path detail asset_labels missing or empty: %+v", detail["asset_labels"])
	}
	var sawNamedLabel bool
	for _, raw := range labels {
		label, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("asset_labels entry not an object: %+v", raw)
		}
		for _, key := range []string{"instrument_name", "instrument_code", "asset_class", "is_cash"} {
			if _, present := label[key]; !present {
				t.Fatalf("asset_labels entry missing field %q: %+v", key, label)
			}
		}
		if name, _ := label["instrument_name"].(string); name != "" {
			sawNamedLabel = true
		}
	}
	if !sawNamedLabel {
		t.Fatalf("asset_labels carried no instrument names: %+v", labels)
	}
}

func TestFailedSimulationJobDoesNotExposeSuccessfulSummary(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)

	jobsRepo := repository.NewJobRepo(db)
	simsRepo := repository.NewSimulationRepo(db)
	runner := persistFailingRunner{db: db, sims: simsRepo}
	worker := jobs.NewWorker(db, jobsRepo, simsRepo, runner, jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), nil,
		jobs.NewEventHub(), nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	services := buildServices(db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: services}))
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
	if runView["job_status"] != "failed" {
		t.Fatalf("simulation run must expose failed job status: %+v", runView)
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

func (r persistFailingRunner) RunSimulation(ctx context.Context, _, runID string, snap *simulation.InputSnapshot,
	cancelCheck func() bool, progress func(done, total int, phase string),
) error {
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
		return errors.New("injected persist failure")
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
