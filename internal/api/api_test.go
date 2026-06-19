package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func testRouter(t *testing.T) *httptest.Server {
	t.Helper()
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})
	return httptest.NewServer(r)
}

func decodeEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode: %v body=%s", err, string(body))
	}
	return env
}

func decodeErrorBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var errBody map[string]any
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("decode error: %v body=%s", err, string(body))
	}
	return errBody
}

func assertErrorCode(t *testing.T, body []byte, wantCode string) {
	t.Helper()
	errBody := decodeErrorBody(t, body)
	if got := errBody["code"]; got != wantCode {
		t.Fatalf("expected code=%q, got %q body=%s", wantCode, got, string(body))
	}
}

func TestPlansCRUDAndVersionConflict(t *testing.T) {
	srv := testRouter(t)
	defer srv.Close()
	client := srv.Client()

	// Create
	scn := "scn_builtin_near_fire"
	createBody, _ := json.Marshal(map[string]any{
		"name": "集成测试计划", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": scn,
	})
	resp, err := client.Post(srv.URL+"/api/v1/plans", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d", resp.StatusCode)
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	data := env["data"].(map[string]any)
	planID := data["id"].(string)
	version := int(data["config_version"].(float64))

	// List
	resp, err = client.Get(srv.URL + "/api/v1/plans")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	listEnv := decodeEnvelope(t, mustRead(t, resp))
	listData := listEnv["data"].([]any)
	if len(listData) != 1 {
		t.Fatalf("list len=%d, want 1", len(listData))
	}
	listItem := listData[0].(map[string]any)
	if _, ok := listItem["rebalance_actionable_count"]; !ok {
		t.Fatalf("plan list missing rebalance_actionable_count: %+v", listItem)
	}
	if _, ok := listItem["holdings_gap_minor"]; !ok {
		t.Fatalf("plan list missing holdings_gap_minor: %+v", listItem)
	}

	// Update allocation
	allocBody, _ := json.Marshal(map[string]any{
		"config_version": version,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 0.70},
			{"asset_class": "bond", "weight": 0.30},
			{"asset_class": "cash", "weight": 0.00},
		},
		"region_targets": []map[string]any{
			{"asset_class": "equity", "region": "domestic", "weight_within_class": 0.60},
			{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.40},
			{"asset_class": "bond", "region": "domestic", "weight_within_class": 0.50},
			{"asset_class": "bond", "region": "foreign", "weight_within_class": 0.50},
			{"asset_class": "cash", "region": "domestic", "weight_within_class": 1.00},
			{"asset_class": "cash", "region": "foreign", "weight_within_class": 0.00},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+planID+"/allocation", bytes.NewReader(allocBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allocation status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	// Stale version -> 409
	staleBody, _ := json.Marshal(map[string]any{
		"config_version": 1,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 0.50},
			{"asset_class": "bond", "weight": 0.50},
			{"asset_class": "cash", "weight": 0.00},
		},
		"region_targets": []map[string]any{},
	})
	req, _ = http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+planID+"/allocation", bytes.NewReader(staleBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	assertErrorCode(t, mustRead(t, resp), "plan_version_conflict")

	// Targets (read-only)
	resp, err = client.Get(srv.URL + "/api/v1/plans/" + planID + "/targets")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("targets status=%d", resp.StatusCode)
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	// Delete
	req, _ = http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/plans/"+planID, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status=%d", resp.StatusCode)
	}
}

func TestHoldingsReadOnlyFields(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	seedEquityInstrument(t, db, "ins_test_equity")

	r := NewRouter(context.Background(), Deps{DB: db})
	body, _ := json.Marshal(map[string]any{
		"config_version": plan.ConfigVersion,
		"holdings": []map[string]any{
			{
				"instrument_id": "ins_test_equity", "enabled": true,
				"weight_within_group": 1.0, "current_amount_minor": 10000000,
				"sort_order": 1, "asset_class": "equity",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/plans/"+plan.ID+"/holdings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w.Body.Bytes(), "holding_fields_read_only")
}

func TestPlanVersionConflictOnStalePUT(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	seedEquityInstrument(t, db, "ins_vc_equity")
	r := NewRouter(context.Background(), Deps{DB: db})

	// Bump config_version from 1 to 2 via a successful allocation update.
	allocBody, _ := json.Marshal(map[string]any{
		"config_version": 1,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 0.7},
			{"asset_class": "bond", "weight": 0.3},
		},
		"region_targets": []map[string]any{
			{"asset_class": "equity", "region": "domestic", "weight_within_class": 1.0},
			{"asset_class": "bond", "region": "domestic", "weight_within_class": 1.0},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/plans/"+plan.ID+"/allocation", bytes.NewReader(allocBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup allocation bump: expected 200, got %d %s", w.Code, w.Body.String())
	}

	assertVersionConflict := func(t *testing.T, method, path string, body []byte) {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("%s %s expected 409, got %d body=%s", method, path, w.Code, w.Body.String())
		}
		assertErrorCode(t, w.Body.Bytes(), "plan_version_conflict")
	}

	staleVersion := plan.ConfigVersion - 1
	planBody, _ := json.Marshal(map[string]any{
		"config_version": staleVersion, "name": "过期版本",
	})
	assertVersionConflict(t, http.MethodPut, "/api/v1/plans/"+plan.ID, planBody)

	params, err := repository.NewParametersRepo(db).Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	paramsBody, _ := json.Marshal(map[string]any{
		"config_version": staleVersion,
		"parameters":     params,
	})
	assertVersionConflict(t, http.MethodPut, "/api/v1/plans/"+plan.ID+"/parameters", paramsBody)

	holdingsBody, _ := json.Marshal(map[string]any{
		"config_version": staleVersion,
		"holdings": []map[string]any{
			{
				"instrument_id": "ins_vc_equity", "enabled": true,
				"weight_within_group": 1.0, "current_amount_minor": 10000000, "sort_order": 1,
			},
		},
	})
	assertVersionConflict(t, http.MethodPut, "/api/v1/plans/"+plan.ID+"/holdings", holdingsBody)
}

func TestBuiltinScenarioCannotDelete(t *testing.T) {
	srv := testRouter(t)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/allocation-scenarios/scn_builtin_accumulation", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestApplyScenarioDryRun(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db})

	body, _ := json.Marshal(map[string]any{
		"scenario_id": "scn_builtin_post_fire", "config_version": plan.ConfigVersion, "dry_run": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+plan.ID+"/apply-scenario", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	data := env["data"].(map[string]any)
	if data["applied"].(bool) {
		t.Fatal("dry run should not apply")
	}
	if data["before"] == nil || data["after"] == nil {
		t.Fatal("expected before/after")
	}
}

func TestApplyScenarioSyncsSelectedScenarioID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db})

	paramsRepo := repository.NewParametersRepo(db)
	params, err := paramsRepo.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if params.SelectedScenarioID == nil || *params.SelectedScenarioID != "scn_builtin_near_fire" {
		t.Fatalf("expected initial scenario scn_builtin_near_fire, got %v", params.SelectedScenarioID)
	}

	planRepo := repository.NewPlanRepo(db)
	planRow, err := planRepo.GetByID(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	applyBody, _ := json.Marshal(map[string]any{
		"scenario_id": "scn_builtin_post_fire", "config_version": planRow.ConfigVersion, "dry_run": false,
	})
	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+plan.ID+"/apply-scenario", bytes.NewReader(applyBody))
	applyReq.Header.Set("Content-Type", "application/json")
	applyW := httptest.NewRecorder()
	r.ServeHTTP(applyW, applyReq)
	if applyW.Code != http.StatusOK {
		t.Fatalf("apply scenario status=%d %s", applyW.Code, applyW.Body.String())
	}

	params, err = paramsRepo.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if params.SelectedScenarioID == nil || *params.SelectedScenarioID != "scn_builtin_post_fire" {
		t.Fatalf("expected selected_scenario_id scn_builtin_post_fire, got %v", params.SelectedScenarioID)
	}
}

func TestScenarioRegionTargetsRoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	createBody, _ := json.Marshal(map[string]any{
		"name":        "区域测试",
		"description": "custom region split",
		"weights": []map[string]any{
			{"asset_class": "equity", "weight": 0.7},
			{"asset_class": "bond", "weight": 0.3},
			{"asset_class": "cash", "weight": 0},
		},
		"region_targets": []map[string]any{
			{"asset_class": "equity", "region": "domestic", "weight_within_class": 0.6},
			{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.4},
			{"asset_class": "bond", "region": "domestic", "weight_within_class": 1},
			{"asset_class": "bond", "region": "foreign", "weight_within_class": 0},
			{"asset_class": "cash", "region": "domestic", "weight_within_class": 1},
			{"asset_class": "cash", "region": "foreign", "weight_within_class": 0},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/allocation-scenarios", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create scenario status=%d %s", w.Code, w.Body.String())
	}
	env := decodeEnvelope(t, w.Body.Bytes())
	scn := env["data"].(map[string]any)
	scenarioID := scn["id"].(string)
	regions := scn["region_targets"].([]any)
	if len(regions) != 6 {
		t.Fatalf("expected 6 region targets, got %d", len(regions))
	}

	plan := createTestPlan(t, db)
	planRepo := repository.NewPlanRepo(db)
	planRow, err := planRepo.GetByID(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}

	// The plan sets its own domestic/foreign split (equity domestic 0.8).
	planAllocBody, _ := json.Marshal(map[string]any{
		"config_version": planRow.ConfigVersion,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 0.6},
			{"asset_class": "bond", "weight": 0.4},
			{"asset_class": "cash", "weight": 0},
		},
		"region_targets": []map[string]any{
			{"asset_class": "equity", "region": "domestic", "weight_within_class": 0.8},
			{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.2},
			{"asset_class": "bond", "region": "domestic", "weight_within_class": 1},
			{"asset_class": "bond", "region": "foreign", "weight_within_class": 0},
			{"asset_class": "cash", "region": "domestic", "weight_within_class": 1},
			{"asset_class": "cash", "region": "foreign", "weight_within_class": 0},
		},
	})
	planAllocReq := httptest.NewRequest(http.MethodPut, "/api/v1/plans/"+plan.ID+"/allocation",
		bytes.NewReader(planAllocBody))
	planAllocReq.Header.Set("Content-Type", "application/json")
	planAllocW := httptest.NewRecorder()
	r.ServeHTTP(planAllocW, planAllocReq)
	if planAllocW.Code != http.StatusOK {
		t.Fatalf("set plan allocation status=%d %s", planAllocW.Code, planAllocW.Body.String())
	}
	planRow, err = planRepo.GetByID(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}

	applyBody, _ := json.Marshal(map[string]any{
		"scenario_id": scenarioID, "config_version": planRow.ConfigVersion, "dry_run": false,
	})
	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+plan.ID+"/apply-scenario", bytes.NewReader(applyBody))
	applyReq.Header.Set("Content-Type", "application/json")
	applyW := httptest.NewRecorder()
	r.ServeHTTP(applyW, applyReq)
	if applyW.Code != http.StatusOK {
		t.Fatalf("apply scenario status=%d %s", applyW.Code, applyW.Body.String())
	}

	allocReq := httptest.NewRequest(http.MethodGet, "/api/v1/plans/"+plan.ID+"/allocation", nil)
	allocW := httptest.NewRecorder()
	r.ServeHTTP(allocW, allocReq)
	if allocW.Code != http.StatusOK {
		t.Fatalf("get allocation status=%d %s", allocW.Code, allocW.Body.String())
	}
	allocEnv := decodeEnvelope(t, allocW.Body.Bytes())
	alloc := allocEnv["data"].(map[string]any)
	gotRegions := alloc["region_targets"].([]any)
	var equityDomestic float64
	for _, item := range gotRegions {
		row := item.(map[string]any)
		if row["asset_class"] == "equity" && row["region"] == "domestic" {
			equityDomestic = row["weight_within_class"].(float64)
		}
	}
	// td/049 §3: applying a scenario must NOT overwrite the plan's region split.
	if equityDomestic != 0.8 {
		t.Fatalf("expected plan equity domestic 0.8 preserved after apply, got %v", equityDomestic)
	}
	// Asset-class weights, however, switch to the scenario's structure (equity 0.7).
	gotWeights := alloc["asset_class_targets"].([]any)
	var equityWeight float64
	for _, item := range gotWeights {
		row := item.(map[string]any)
		if row["asset_class"] == "equity" {
			equityWeight = row["weight"].(float64)
		}
	}
	if equityWeight != 0.7 {
		t.Fatalf("expected equity asset-class weight 0.7 from scenario, got %v", equityWeight)
	}
}

func TestRebalanceModes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db})

	for _, path := range []string{
		"/api/v1/plans/" + plan.ID + "/rebalance",
		"/api/v1/plans/" + plan.ID + "/rebalance?mode=new_cash&new_cash_minor=5000000",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestConfigHashChangesOnUpdate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	hashSvc := newConfigHashService(db)
	h1, err := hashSvc.Compute(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	paramsRepo := repository.NewParametersRepo(db)
	params, err := paramsRepo.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	params.TotalAssetsMinor = 2_000_000_00
	planRepo := repository.NewPlanRepo(db)
	if err := paramsRepo.Upsert(context.Background(), nil, params); err != nil {
		t.Fatal(err)
	}
	if _, err := planRepo.BumpVersion(context.Background(), plan.ID, plan.ConfigVersion); err != nil {
		t.Fatal(err)
	}
	h2, err := hashSvc.Compute(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatal("config hash should change when parameters change")
	}
}

func newConfigHashService(db *sql.DB) *service.ConfigHashService {
	plans := repository.NewPlanRepo(db)
	params := repository.NewParametersRepo(db)
	alloc := repository.NewAllocationRepo(db)
	holdings := repository.NewHoldingsRepo(db)
	return service.NewConfigHashService(plans, params, alloc, holdings)
}

func mustRead(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
