package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/frontier"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

func TestFireFrontierFourTypesAndApplyEndToEnd(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedFrontierPlan(t, db)
	ctx := context.Background()
	services := buildServices(db)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go newTestTaskWorker(db, services).Start(workerCtx, 1)
	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, Services: services}))
	defer srv.Close()

	source := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "11"}, http.StatusOK, "source")
	waitImprovementTask(t, db, source["task_id"].(string))
	sourceRunID := source["run_id"].(string)

	requests := []map[string]any{
		frontierRequest(sourceRunID, "retirement_age_max_spending", map[string]any{"min": 41, "max": 42},
			map[string]any{"min_minor": 1, "max_minor": 100, "step_minor": 99}),
		frontierRequest(sourceRunID, "retirement_age_min_savings", map[string]any{"min": 41, "max": 42},
			map[string]any{"min_minor": 0, "max_minor": 100, "step_minor": 100}),
		frontierRequest(sourceRunID, "required_current_assets", nil,
			map[string]any{"min_minor": 1, "max_minor": 20_000_000, "step_minor": 19_999_999}),
		frontierRequest(sourceRunID, "coast_required_assets", nil,
			map[string]any{"min_minor": 1, "max_minor": 20_000_000, "step_minor": 19_999_999}),
	}
	var ageRun map[string]any
	for i, request := range requests {
		beforeTasks := countTableRows(t, db, "worker_tasks")
		readiness := postFrontierJSON(t, srv,
			"/api/v1/plans/"+planID+"/fire-frontier-readiness", request, http.StatusOK, "")
		if readiness["ready"] != true || readiness["money_levels"].(float64) != 2 ||
			readiness["evaluation_budget"].(float64) < 1 {
			t.Fatalf("type %d readiness=%#v", i, readiness)
		}
		if got := countTableRows(t, db, "worker_tasks"); got != beforeTasks {
			t.Fatalf("readiness wrote task: before=%d after=%d", beforeTasks, got)
		}
		created := postFrontierJSON(t, srv,
			"/api/v1/plans/"+planID+"/fire-frontier-runs", request, http.StatusAccepted, "frontier-key-"+string(rune('a'+i)))
		replayed := postFrontierJSON(t, srv,
			"/api/v1/plans/"+planID+"/fire-frontier-runs", request, http.StatusOK,
			"frontier-key-"+string(rune('a'+i)))
		if replayed["reused"] != true || replayed["run_id"] != created["run_id"] {
			t.Fatalf("idempotent frontier replay diverged: created=%#v replayed=%#v", created, replayed)
		}
		waitImprovementTask(t, db, created["task_id"].(string))
		detail := getImprovementJSON(t, srv,
			"/api/v1/fire-frontier-runs/"+created["run_id"].(string), http.StatusOK)
		if detail["status"] != repository.WorkerTaskStatusComplete || detail["phase"] != "complete" || detail["result"] == nil {
			t.Fatalf("frontier detail=%#v", detail)
		}
		basis, ok := detail["frozen_basis"].(map[string]any)
		if !ok || basis["current_age"] != float64(40) || basis["retirement_age"] != float64(41) ||
			basis["total_assets_minor"] != float64(10_000_000) || basis["asset_count"] != float64(1) ||
			basis["source_simulation_runs"] != float64(1000) || basis["seed"] != "11" {
			t.Fatalf("frontier detail omitted frozen calculation basis: %#v", basis)
		}
		result := detail["result"].(map[string]any)
		if result["distinct_evaluations"].(float64) > detail["config"].(map[string]any)["evaluation_budget"].(float64) ||
			len(result["points"].([]any)) == 0 {
			t.Fatalf("frontier result=%#v", result)
		}
		if i == 0 {
			ageRun = detail
		}
	}

	points := ageRun["result"].(map[string]any)["points"].([]any)
	var selected, other map[string]any
	for _, raw := range points {
		point := raw.(map[string]any)
		if point["applicable"] == true {
			if selected == nil && int(point["retirement_age"].(float64)) == 42 {
				selected = point
			} else {
				other = point
			}
		}
	}
	if selected == nil {
		t.Fatalf("expected applicable age-42 point: %#v", points)
	}
	resultBeforeDelete, _ := json.Marshal(ageRun["result"])
	if _, err := db.ExecContext(ctx, `DELETE FROM simulation_runs WHERE id=?`, sourceRunID); err != nil {
		t.Fatal(err)
	}
	previewNow := time.Date(2026, 7, 13, 12, 0, 0, 123_000_000, time.UTC)
	services.Frontiers.SetClockForTest(func() time.Time { return previewNow })
	before, err := repository.NewParametersRepo(db).Get(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := repository.NewPlanRepo(db).GetByID(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	applicationsBefore := countTableRows(t, db, "fire_frontier_applications")
	rebalancesBefore := countTableRows(t, db, "rebalance_executions")
	preview := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusOK, "")
	if countTableRows(t, db, "fire_frontier_applications") != applicationsBefore {
		t.Fatal("frontier preview wrote an application")
	}
	if preview["preview_expires_at"].(float64) != float64(previewNow.Add(15*time.Minute).UnixMilli()) {
		t.Fatalf("preview expiry=%v", preview["preview_expires_at"])
	}
	if preview["source_run_id"] != sourceRunID || preview["target_probability"] == nil ||
		preview["success_wilson_low"] == nil || preview["improved_path_count"] == nil ||
		preview["regressed_path_count"] == nil || preview["before"] == nil || preview["after"] == nil ||
		preview["current_config_hash"] == "" || preview["current_market_hash"] == "" ||
		!strings.HasPrefix(preview["preview_hash"].(string), "sha256:") {
		t.Fatalf("preview omitted required evidence: %#v", preview)
	}
	tasksBeforeApply := countTableRows(t, db, "worker_tasks")
	applyBody := map[string]any{
		"expected_plan_config_version": plan.ConfigVersion,
		"preview_hash":                 preview["preview_hash"], "preview_expires_at": preview["preview_expires_at"],
	}
	badHashBody := map[string]any{
		"expected_plan_config_version": plan.ConfigVersion,
		"preview_hash":                 "sha256:tampered", "preview_expires_at": preview["preview_expires_at"],
	}
	badHash := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/apply",
		badHashBody, http.StatusConflict, "")
	if badHash["code"] != "frontier_preview_stale" {
		t.Fatalf("tampered preview=%#v", badHash)
	}
	services.Frontiers.SetClockForTest(func() time.Time { return previewNow.Add(15 * time.Minute) })
	expired := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/apply",
		applyBody, http.StatusConflict, "")
	if expired["code"] != "frontier_preview_stale" {
		t.Fatalf("expired preview=%#v", expired)
	}
	services.Frontiers.SetClockForTest(func() time.Time { return previewNow })
	wrongVersionBody := map[string]any{
		"expected_plan_config_version": plan.ConfigVersion + 1,
		"preview_hash":                 preview["preview_hash"], "preview_expires_at": preview["preview_expires_at"],
	}
	wrongVersion := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/apply",
		wrongVersionBody, http.StatusConflict, "")
	if wrongVersion["code"] != "frontier_preview_stale" {
		t.Fatalf("wrong-version preview=%#v", wrongVersion)
	}
	var marketSnapshotID, marketSourceHash string
	if err := db.QueryRowContext(ctx, `SELECT s.id,s.source_hash FROM plan_holdings h
		JOIN market_asset_simulation_snapshots s ON s.id=h.simulation_snapshot_id
		WHERE h.plan_id=? LIMIT 1`, planID).Scan(&marketSnapshotID, &marketSourceHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE market_asset_simulation_snapshots
		SET source_hash=source_hash||'-changed' WHERE id=?`, marketSnapshotID); err != nil {
		t.Fatal(err)
	}
	marketStale := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/apply",
		applyBody, http.StatusConflict, "")
	if marketStale["code"] != "frontier_preview_stale" {
		t.Fatalf("market-stale preview=%#v", marketStale)
	}
	if _, err := db.ExecContext(ctx, `UPDATE market_asset_simulation_snapshots SET source_hash=? WHERE id=?`,
		marketSourceHash, marketSnapshotID); err != nil {
		t.Fatal(err)
	}
	firstApply := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/apply",
		applyBody, http.StatusOK, "")
	retryApply := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+selected["id"].(string)+"/apply",
		applyBody, http.StatusOK, "")
	if firstApply["application"].(map[string]any)["id"] != retryApply["application"].(map[string]any)["id"] {
		t.Fatal("same apply retry did not return the existing application")
	}
	if got := countTableRows(t, db, "worker_tasks"); got != tasksBeforeApply {
		t.Fatalf("apply created a task: before=%d after=%d", tasksBeforeApply, got)
	}
	if got := countTableRows(t, db, "rebalance_executions"); got != rebalancesBefore {
		t.Fatalf("apply created rebalance execution: before=%d after=%d", rebalancesBefore, got)
	}
	after, err := repository.NewParametersRepo(db).Get(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.RetirementAge = 42
	want.AnnualSpendingMinor = int64(selected["value_minor"].(float64))
	want.UpdatedAt = after.UpdatedAt
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("frontier apply changed extra fields\nbefore=%#v\nafter=%#v\nwant=%#v", before, after, want)
	}
	updatedPlan, _ := repository.NewPlanRepo(db).GetByID(ctx, planID)
	if updatedPlan.ConfigVersion != plan.ConfigVersion+1 ||
		countTableRows(t, db, "fire_frontier_applications") != applicationsBefore+1 {
		t.Fatalf("apply version/audit mismatch: plan=%#v", updatedPlan)
	}
	if other != nil {
		conflict := postFrontierJSON(t, srv,
			"/api/v1/fire-frontier-runs/"+ageRun["id"].(string)+"/points/"+other["id"].(string)+"/apply",
			applyBody, http.StatusConflict, "")
		if conflict["code"] != "frontier_run_already_applied" {
			t.Fatalf("other point conflict=%#v", conflict)
		}
	}

	afterDelete := getImprovementJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+ageRun["id"].(string), http.StatusOK)
	resultAfterDelete, _ := json.Marshal(afterDelete["result"])
	if afterDelete["source_available"] != false || string(resultBeforeDelete) != string(resultAfterDelete) {
		t.Fatalf("source deletion changed history: %#v", afterDelete)
	}
}

func TestFireFrontierValidationAndIdempotencyErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedFrontierPlan(t, db)
	ctx := context.Background()
	services := buildServices(db)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go newTestTaskWorker(db, services).Start(workerCtx, 1)
	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, Services: services}))
	defer srv.Close()
	source := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "17"}, http.StatusOK, "source")
	waitImprovementTask(t, db, source["task_id"].(string))
	sourceID := source["run_id"].(string)

	invalid := frontierRequest(sourceID, "retirement_age_max_spending", map[string]any{"min": 41, "max": 42},
		map[string]any{"min_minor": 1, "max_minor": 10, "step_minor": 4})
	readiness := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-readiness",
		invalid, http.StatusOK, "")
	if readiness["ready"] == true || readiness["issues"].([]any)[0].(map[string]any)["code"] != "frontier_config_invalid" {
		t.Fatalf("invalid readiness=%#v", readiness)
	}
	request := frontierRequest(sourceID, "required_current_assets", nil,
		map[string]any{"min_minor": 1, "max_minor": 100, "step_minor": 99})
	created := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		request, http.StatusAccepted, "same-key")
	waitImprovementTask(t, db, created["task_id"].(string))
	repeatedRun := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		request, http.StatusAccepted, "same-input-new-key")
	if repeatedRun["reused"] == true || repeatedRun["run_id"] == created["run_id"] ||
		repeatedRun["task_id"] == created["task_id"] {
		t.Fatalf("a new request unexpectedly reused a historical calculation: first=%#v repeated=%#v",
			created, repeatedRun)
	}
	waitImprovementTask(t, db, repeatedRun["task_id"].(string))
	different := frontierRequest(sourceID, "required_current_assets", nil,
		map[string]any{"min_minor": 1, "max_minor": 200, "step_minor": 199})
	conflict := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		different, http.StatusConflict, "same-key")
	if conflict["code"] != "idempotency_conflict" {
		t.Fatalf("idempotency conflict=%#v", conflict)
	}
	differentCreated := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		different, http.StatusAccepted, "different-key")
	waitImprovementTask(t, db, differentCreated["task_id"].(string))
	conflictAfterIndependentRuns := postFrontierJSON(t, srv,
		"/api/v1/plans/"+planID+"/fire-frontier-runs", request, http.StatusConflict, "different-key")
	if conflictAfterIndependentRuns["code"] != "idempotency_conflict" {
		t.Fatalf("idempotency conflict was bypassed after independent runs: %#v", conflictAfterIndependentRuns)
	}
	if _, err := db.ExecContext(ctx, `UPDATE plan_parameters SET annual_spending_minor=annual_spending_minor+1
		WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	stale := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		request, http.StatusConflict, "")
	if stale["code"] != "frontier_source_stale" {
		t.Fatalf("stale source=%#v", stale)
	}
}

func TestFireFrontierRejectsEveryIneligibleSourceBeforeTaskCreation(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedFrontierPlan(t, db)
	ctx := context.Background()
	services := buildServices(db)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go newTestTaskWorker(db, services).Start(workerCtx, 1)
	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, Services: services}))
	defer srv.Close()
	source := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "29"}, http.StatusOK, "source-eligibility")
	waitImprovementTask(t, db, source["task_id"].(string))
	sourceID := source["run_id"].(string)
	request := frontierRequest(sourceID, "required_current_assets", nil,
		map[string]any{"min_minor": 1, "max_minor": 100, "step_minor": 99})

	assertRejected := func(routePlanID string, body map[string]any, status int, code string) {
		t.Helper()
		before := countTableRows(t, db, "worker_tasks")
		got := postFrontierJSON(t, srv, "/api/v1/plans/"+routePlanID+"/fire-frontier-runs",
			body, status, "")
		if got["code"] != code {
			t.Fatalf("source rejection code=%v want=%s body=%#v", got["code"], code, got)
		}
		if after := countTableRows(t, db, "worker_tasks"); after != before {
			t.Fatalf("ineligible source created a task: before=%d after=%d", before, after)
		}
	}

	missing := frontierRequest("sim_missing", "required_current_assets", nil,
		map[string]any{"min_minor": 1, "max_minor": 100, "step_minor": 99})
	assertRejected(planID, missing, http.StatusNotFound, "frontier_source_not_found")
	otherPlan := createTestPlan(t, db)
	assertRejected(otherPlan.ID, request, http.StatusNotFound, "frontier_source_not_found")

	if _, err := db.ExecContext(ctx, `UPDATE worker_tasks SET status='running' WHERE id=?`, source["task_id"]); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_incomplete")
	if _, err := db.ExecContext(ctx, `UPDATE worker_tasks SET status='complete' WHERE id=?`, source["task_id"]); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET runs=999 WHERE id=?`, sourceID); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_incomplete")
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET runs=1000 WHERE id=?`, sourceID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET engine_version='legacy' WHERE id=?`, sourceID); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_stale")
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET engine_version=? WHERE id=?`, simulation.EngineVersion, sourceID); err != nil {
		t.Fatal(err)
	}

	var originalInput, originalMarket string
	if err := db.QueryRowContext(ctx, `SELECT input_snapshot_json,market_snapshot_hash
		FROM simulation_runs WHERE id=?`, sourceID).Scan(&originalInput, &originalMarket); err != nil {
		t.Fatal(err)
	}
	var snapshot simulation.InputSnapshot
	if err := json.Unmarshal([]byte(originalInput), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Parameters.AnnualSpendingMinor++
	tamperedInput, _ := json.Marshal(snapshot)
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET input_snapshot_json=? WHERE id=?`,
		string(tamperedInput), sourceID); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_incomplete")

	if err := json.Unmarshal([]byte(originalInput), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Parameters.StudentTDf = 4
	invalidSchema, _ := json.Marshal(snapshot)
	invalidSchemaHash, _ := simulation.HashInput(&snapshot)
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET input_snapshot_json=?,input_hash=? WHERE id=?`,
		string(invalidSchema), invalidSchemaHash, sourceID); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_incomplete")

	if err := json.Unmarshal([]byte(originalInput), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.MarketSnapshotHash = "sha256:changed-market"
	changedMarketInput, _ := json.Marshal(snapshot)
	changedMarketInputHash, _ := simulation.HashInput(&snapshot)
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs
		SET input_snapshot_json=?,input_hash=?,market_snapshot_hash=? WHERE id=?`,
		string(changedMarketInput), changedMarketInputHash, snapshot.MarketSnapshotHash, sourceID); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_market_changed")
	if err := json.Unmarshal([]byte(originalInput), &snapshot); err != nil {
		t.Fatal(err)
	}
	originalInputHash, _ := simulation.HashInput(&snapshot)
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs
		SET input_snapshot_json=?,input_hash=?,market_snapshot_hash=? WHERE id=?`,
		originalInput, originalInputHash, originalMarket, sourceID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM simulation_path_index WHERE run_id=? AND path_no=999`, sourceID); err != nil {
		t.Fatal(err)
	}
	assertRejected(planID, request, http.StatusConflict, "frontier_source_incomplete")
}

func TestFireFrontierPreviewRejectsNoOpAssetNoneUnknownAndIncomplete(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedFrontierPlan(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE plan_parameters SET total_assets_minor=1000000000,
		annual_savings_minor=0,annual_spending_minor=1200,retirement_age=41 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE plan_holdings SET current_amount_minor=1000000000 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	services := buildServices(db)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go newTestTaskWorker(db, services).Start(workerCtx, 1)
	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, Services: services}))
	defer srv.Close()
	source := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "31"}, http.StatusOK, "preview-source")
	waitImprovementTask(t, db, source["task_id"].(string))
	sourceID := source["run_id"].(string)
	plan, err := repository.NewPlanRepo(db).GetByID(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}

	createAndLoad := func(key string, request map[string]any) map[string]any {
		t.Helper()
		created := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
			request, http.StatusAccepted, key)
		waitImprovementTask(t, db, created["task_id"].(string))
		return getImprovementJSON(t, srv, "/api/v1/fire-frontier-runs/"+created["run_id"].(string), http.StatusOK)
	}

	noOpRun := createAndLoad("no-op", frontierRequest(sourceID, "retirement_age_min_savings",
		map[string]any{"min": 41, "max": 41}, map[string]any{"min_minor": 0, "max_minor": 0, "step_minor": 1}))
	noOpPoint := noOpRun["result"].(map[string]any)["points"].([]any)[0].(map[string]any)
	applicationsBefore := countTableRows(t, db, "fire_frontier_applications")
	noOp := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+noOpRun["id"].(string)+"/points/"+noOpPoint["id"].(string)+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusBadRequest, "")
	if noOp["code"] != "frontier_point_no_change" ||
		countTableRows(t, db, "fire_frontier_applications") != applicationsBefore {
		t.Fatalf("no-op preview=%#v", noOp)
	}
	unchangedPlan, _ := repository.NewPlanRepo(db).GetByID(ctx, planID)
	if unchangedPlan.ConfigVersion != plan.ConfigVersion {
		t.Fatalf("no-op changed config version: %d -> %d", plan.ConfigVersion, unchangedPlan.ConfigVersion)
	}
	unknown := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+noOpRun["id"].(string)+"/points/fpt_unknown/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusNotFound, "")
	if unknown["code"] != "frontier_point_not_found" {
		t.Fatalf("unknown point=%#v", unknown)
	}

	assetRun := createAndLoad("asset-inapplicable", frontierRequest(sourceID, "required_current_assets", nil,
		map[string]any{"min_minor": 1, "max_minor": 100, "step_minor": 99}))
	assetPoint := assetRun["result"].(map[string]any)["points"].([]any)[0].(map[string]any)
	asset := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+assetRun["id"].(string)+"/points/"+assetPoint["id"].(string)+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusBadRequest, "")
	if asset["code"] != "frontier_point_not_applicable" {
		t.Fatalf("asset preview=%#v", asset)
	}

	noneRun := createAndLoad("none-inapplicable", frontierRequest(sourceID, "retirement_age_max_spending",
		map[string]any{"min": 41, "max": 41}, map[string]any{
			"min_minor": int64(9_000_000_000_000_000), "max_minor": int64(9_000_000_000_000_000), "step_minor": 1,
		}))
	nonePoint := noneRun["result"].(map[string]any)["points"].([]any)[0].(map[string]any)
	if nonePoint["status"] != frontier.StatusNoFeasibleValue {
		t.Fatalf("expected no-feasible point: %#v", nonePoint)
	}
	none := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+noneRun["id"].(string)+"/points/"+nonePoint["id"].(string)+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusBadRequest, "")
	if none["code"] != "frontier_point_not_applicable" {
		t.Fatalf("none-feasible preview=%#v", none)
	}

	tamperedRun := createAndLoad("tampered-candidate", frontierRequest(sourceID, "retirement_age_max_spending",
		map[string]any{"min": 41, "max": 41},
		map[string]any{"min_minor": 1, "max_minor": 2401, "step_minor": 1200}))
	stored, err := repository.NewFireFrontierRepo(db).GetByID(ctx, tamperedRun["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var tamperedResult frontier.Result
	if err := json.Unmarshal(stored.ResultJSON, &tamperedResult); err != nil {
		t.Fatal(err)
	}
	if len(tamperedResult.Points) != 1 || !tamperedResult.Points[0].Applicable {
		t.Fatalf("expected applicable point for candidate-hash test: %+v", tamperedResult.Points)
	}
	tamperedPointID := tamperedResult.Points[0].ID
	tamperedResult.Points[0].Evaluation.CandidateConfigHash = "sha256:tampered"
	tamperedRaw, _ := json.Marshal(tamperedResult)
	if _, err := db.ExecContext(ctx, `UPDATE fire_frontier_runs SET result_json=? WHERE id=?`,
		string(tamperedRaw), stored.ID); err != nil {
		t.Fatal(err)
	}
	tampered := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/"+stored.ID+"/points/"+tamperedPointID+"/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusConflict, "")
	if tampered["code"] != "frontier_result_inconsistent" {
		t.Fatalf("tampered candidate hash preview=%#v", tampered)
	}

	err = fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := services.TaskCoordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: "task_frontier_incomplete", WorkerType: repository.WorkerTypeGo,
			Type: repository.WorkerTaskTypeFireFrontier, Status: repository.WorkerTaskStatusPending,
			ScopeType: "plan", ScopeID: planID, DedupeKey: "preview-incomplete",
			PayloadJSON: `{"run_id":"ffr_incomplete"}`,
		}); err != nil {
			return err
		}
		return repository.NewFireFrontierRepo(db).CreateTx(ctx, tx, &repository.FireFrontierRun{
			ID: "ffr_incomplete", TaskID: "task_frontier_incomplete", PlanID: planID,
			SourceSimulationRunID: sourceID, InputHash: "sha256:incomplete",
			AlgorithmVersion:    frontier.AlgorithmVersion,
			FrontierType:        frontier.TypeRetirementAgeMaxSpending,
			SourceEngineVersion: simulation.EngineVersion, SourceConfigHash: "sha256:config",
			SourceMarketHash: "sha256:market", EvaluationRuns: 1000,
			ConfigJSON: `{}`, InputSnapshotJSON: `{}`,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	incomplete := postFrontierJSON(t, srv,
		"/api/v1/fire-frontier-runs/ffr_incomplete/points/fpt_any/preview",
		map[string]any{"expected_plan_config_version": plan.ConfigVersion}, http.StatusBadRequest, "")
	if incomplete["code"] != "frontier_point_not_applicable" {
		t.Fatalf("incomplete preview=%#v", incomplete)
	}
}

func TestFireFrontierAdmissionBudgetBoundaries(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedFrontierPlan(t, db)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE plan_parameters SET end_age=90 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	services := buildServices(db)
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		newTestTaskWorker(db, services).Start(workerCtx, 1)
	}()
	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, Services: services}))
	defer srv.Close()
	source := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/simulations",
		map[string]any{"runs": 1000, "seed": "37"}, http.StatusOK, "budget-source")
	waitImprovementTask(t, db, source["task_id"].(string))
	stopWorker()
	<-workerDone
	sourceID := source["run_id"].(string)

	var input string
	if err := db.QueryRowContext(ctx, `SELECT input_snapshot_json FROM simulation_runs WHERE id=?`, sourceID).
		Scan(&input); err != nil {
		t.Fatal(err)
	}
	var snapshot simulation.InputSnapshot
	if err := json.Unmarshal([]byte(input), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Parameters.SimulationRuns = 20_000
	input20k, _ := json.Marshal(snapshot)
	input20kHash, _ := simulation.HashInput(&snapshot)
	if _, err := db.ExecContext(ctx, `WITH RECURSIVE seq(path_no) AS (
		SELECT 1000 UNION ALL SELECT path_no+1 FROM seq WHERE path_no<19999
	) INSERT INTO simulation_path_index
		(run_id,path_no,path_seed,succeeded,failure_month,terminal_wealth_minor,max_drawdown,representative_percentile)
		SELECT ?,path_no,path_no,0,NULL,0,1,'' FROM seq`, sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE simulation_runs SET runs=20000,
		failure_count=20000-success_count,input_snapshot_json=?,input_hash=? WHERE id=?`,
		string(input20k), input20kHash, sourceID); err != nil {
		t.Fatal(err)
	}

	exact := frontierRequest(sourceID, "retirement_age_max_spending",
		map[string]any{"min": 50, "max": 52},
		map[string]any{"min_minor": 1, "max_minor": 32, "step_minor": 1})
	exact["evaluation_runs"] = 20_000
	readiness := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-readiness",
		exact, http.StatusOK, "")
	if readiness["ready"] != true || readiness["evaluation_budget"].(float64) != 25 ||
		readiness["path_month_budget"].(float64) != float64(frontier.MaxPathMonthBudget) {
		t.Fatalf("exact path-month boundary readiness=%#v", readiness)
	}

	overCompute := frontierRequest(sourceID, "retirement_age_max_spending",
		map[string]any{"min": 50, "max": 53},
		map[string]any{"min_minor": 1, "max_minor": 32, "step_minor": 1})
	overCompute["evaluation_runs"] = 20_000
	before := countTableRows(t, db, "worker_tasks")
	computeRejected := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		overCompute, http.StatusBadRequest, "")
	if computeRejected["code"] != "frontier_compute_budget_exceeded" ||
		countTableRows(t, db, "worker_tasks") != before {
		t.Fatalf("compute budget rejection=%#v", computeRejected)
	}

	overEvaluations := frontierRequest(sourceID, "retirement_age_max_spending",
		map[string]any{"min": 50, "max": 69},
		map[string]any{"min_minor": 1, "max_minor": 1024, "step_minor": 1})
	overEvaluations["evaluation_runs"] = 1000
	evaluationRejected := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		overEvaluations, http.StatusBadRequest, "")
	if evaluationRejected["code"] != "frontier_budget_exceeded" ||
		countTableRows(t, db, "worker_tasks") != before {
		t.Fatalf("evaluation budget rejection=%#v", evaluationRejected)
	}

	accepted := postFrontierJSON(t, srv, "/api/v1/plans/"+planID+"/fire-frontier-runs",
		exact, http.StatusAccepted, "exact-budget")
	if accepted["status"] != repository.WorkerTaskStatusPending ||
		countTableRows(t, db, "worker_tasks") != before+1 {
		t.Fatalf("exact budget was not admitted: %#v", accepted)
	}
}

func TestDeletingPlanCancelsActiveFrontierTaskWithoutOrphan(t *testing.T) {
	db := testutil.OpenTestDB(t)
	services := buildServices(db)
	plan := createTestPlan(t, db)
	ctx := context.Background()
	taskID := "task_delete_frontier"
	runID := "ffr_delete"
	err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := services.TaskCoordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: taskID, WorkerType: repository.WorkerTypeGo,
			Type: repository.WorkerTaskTypeFireFrontier, Status: repository.WorkerTaskStatusPending,
			ScopeType: "plan", ScopeID: plan.ID, PayloadJSON: `{"run_id":"ffr_delete"}`,
		}); err != nil {
			return err
		}
		repo := repository.NewFireFrontierRepo(db)
		if err := repo.CreateTx(ctx, tx, &repository.FireFrontierRun{
			ID: runID, TaskID: taskID, PlanID: plan.ID, SourceSimulationRunID: "sim_source",
			InputHash: "sha256:input", AlgorithmVersion: frontier.AlgorithmVersion,
			FrontierType:        frontier.TypeRetirementAgeMaxSpending,
			SourceEngineVersion: simulation.EngineVersion, SourceConfigHash: "sha256:config",
			SourceMarketHash: "sha256:market", EvaluationRuns: 1000,
			ConfigJSON: `{}`, InputSnapshotJSON: `{}`,
		}); err != nil {
			return err
		}
		return repo.CreateApplicationTx(ctx, tx, repository.FireFrontierApplication{
			ID: "ffa_delete", FrontierRunID: runID, PointID: "fpt_delete", PlanID: plan.ID,
			BeforeConfigVersion: 1, AfterConfigVersion: 2, PreviewHash: "sha256:preview",
			BeforeJSON: `{}`, AfterJSON: `{}`,
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
	if _, err := repository.NewFireFrontierRepo(db).GetByID(ctx, runID); !errors.Is(err, repository.ErrFireFrontierNotFound) {
		t.Fatalf("frontier run survived plan delete: %v", err)
	}
	if countTableRows(t, db, "fire_frontier_applications") != 0 {
		t.Fatal("frontier application survived plan delete")
	}
	claimable, err := services.TaskCoordinator.ListClaimable(ctx, repository.WorkerTypeGo,
		[]string{repository.WorkerTaskTypeFireFrontier}, 10, nil, nil, "")
	if err != nil || len(claimable) != 0 {
		t.Fatalf("deleted plan task remains claimable: %#v err=%v", claimable, err)
	}
}

func seedFrontierPlan(t *testing.T, db *sql.DB) string {
	t.Helper()
	planID := seedOneYearSimulationPlan(t, db)
	if _, err := db.ExecContext(context.Background(), `UPDATE plan_parameters SET current_age=40,
		retirement_age=41,end_age=43,total_assets_minor=10000000,annual_savings_minor=0,
		annual_spending_minor=40000000,annual_retirement_income_minor=0 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(),
		`UPDATE plan_holdings SET current_amount_minor=10000000 WHERE plan_id=?`, planID); err != nil {
		t.Fatal(err)
	}
	return planID
}

func frontierRequest(sourceID, frontierType string, age any, search map[string]any) map[string]any {
	out := map[string]any{
		"source_simulation_run_id": sourceID, "frontier_type": frontierType,
		"target_success_probability": 0.5, "evaluation_runs": 1000, "search": search,
	}
	if age != nil {
		out["retirement_age_range"] = age
	}
	return out
}

func postFrontierJSON(t *testing.T, srv *httptest.Server, path string, body any, status int,
	idempotencyKey string,
) map[string]any {
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
	envelope := decodeEnvelope(t, raw)
	if status >= 300 {
		return envelope
	}
	return envelope["data"].(map[string]any)
}

func countTableRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	allowed := map[string]bool{
		"worker_tasks": true, "fire_frontier_applications": true, "rebalance_executions": true,
	}
	if !allowed[table] {
		t.Fatalf("unsupported table %q", table)
	}
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
