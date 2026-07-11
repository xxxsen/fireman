package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func getSettingsFixtureState(t *testing.T, r http.Handler, planID string) (
	map[string]any, map[string]any, map[string]any,
) {
	t.Helper()
	get := func(path string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
		}
		env := decodeEnvelope(t, w.Body.Bytes())
		return env["data"].(map[string]any)
	}
	plan := get("/api/v1/plans/" + planID)
	params := get("/api/v1/plans/" + planID + "/parameters")["parameters"].(map[string]any)
	alloc := get("/api/v1/plans/" + planID + "/allocation")
	return plan, params, alloc
}

func putSettings(t *testing.T, r http.Handler, planID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/plans/"+planID+"/settings", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

var settingsTestAllocation = map[string]any{
	"asset_class_targets": []map[string]any{
		{"asset_class": "equity", "weight": 0.65},
		{"asset_class": "bond", "weight": 0.25},
		{"asset_class": "cash", "weight": 0.10},
	},
	"region_targets": []map[string]any{
		{"asset_class": "equity", "region": "domestic", "weight_within_class": 0.60},
		{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.40},
		{"asset_class": "bond", "region": "domestic", "weight_within_class": 1.00},
		{"asset_class": "bond", "region": "foreign", "weight_within_class": 0.00},
		{"asset_class": "cash", "region": "domestic", "weight_within_class": 1.00},
		{"asset_class": "cash", "region": "foreign", "weight_within_class": 0.00},
	},
}

func TestPlanSettingsUpdate_SingleCallAtomicSave(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)})

	_, params, _ := getSettingsFixtureState(t, r, plan.ID)
	params["retirement_age"] = float64(60)

	w := putSettings(t, r, plan.ID, map[string]any{
		"config_version":            plan.ConfigVersion,
		"plan":                      map[string]any{"name": "改名后的计划"},
		"allocation":                settingsTestAllocation,
		"parameters":                params,
		"apply_unallocated_to_cash": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", w.Code, w.Body.String())
	}

	planAfter, paramsAfter, allocAfter := getSettingsFixtureState(t, r, plan.ID)
	if got := int(planAfter["config_version"].(float64)); got != plan.ConfigVersion+1 {
		t.Fatalf("config_version = %d, want exactly +1 (%d)", got, plan.ConfigVersion+1)
	}
	if planAfter["name"] != "改名后的计划" {
		t.Fatalf("plan name = %v, want 改名后的计划", planAfter["name"])
	}
	if got := paramsAfter["retirement_age"].(float64); got != 60 {
		t.Fatalf("retirement_age = %v, want 60", got)
	}
	targets := allocAfter["asset_class_targets"].([]any)
	weights := map[string]float64{}
	for _, raw := range targets {
		item := raw.(map[string]any)
		weights[item["asset_class"].(string)] = item["weight"].(float64)
	}
	if weights["equity"] != 0.65 || weights["bond"] != 0.25 || weights["cash"] != 0.10 {
		t.Fatalf("allocation not applied: %+v", weights)
	}
}

func TestPlanSettingsUpdate_InvalidParametersRollsBackEverything(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)})

	planBefore, params, allocBefore := getSettingsFixtureState(t, r, plan.ID)
	// retirement_age below current_age violates the age chain.
	params["retirement_age"] = float64(20)

	w := putSettings(t, r, plan.ID, map[string]any{
		"config_version":            plan.ConfigVersion,
		"plan":                      map[string]any{"name": "不应生效的名字"},
		"allocation":                settingsTestAllocation,
		"parameters":                params,
		"apply_unallocated_to_cash": true,
	})
	if w.Code == http.StatusOK {
		t.Fatalf("expected failure, got 200 body=%s", w.Body.String())
	}
	assertErrorCode(t, w.Body.Bytes(), "parameters_invalid")

	planAfter, paramsAfter, allocAfter := getSettingsFixtureState(t, r, plan.ID)
	if planAfter["name"] != planBefore["name"] {
		t.Fatalf("plan name changed on failed save: %v -> %v", planBefore["name"], planAfter["name"])
	}
	if planAfter["config_version"] != planBefore["config_version"] {
		t.Fatalf("config_version changed on failed save")
	}
	if paramsAfter["retirement_age"].(float64) == 20 {
		t.Fatal("invalid parameters were persisted")
	}
	beforeJSON, _ := json.Marshal(allocBefore["asset_class_targets"])
	afterJSON, _ := json.Marshal(allocAfter["asset_class_targets"])
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatalf("allocation changed on failed save: %s -> %s", beforeJSON, afterJSON)
	}
}

func TestPlanSettingsUpdate_RejectsTransactionCostBoundaries(t *testing.T) {
	for _, rate := range []float64{-0.01, 1.0} {
		t.Run(strconv.FormatFloat(rate, 'g', -1, 64), func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			plan := createTestPlan(t, db)
			router := NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)})
			_, params, _ := getSettingsFixtureState(t, router, plan.ID)
			params["transaction_cost_rate"] = rate
			writer := putSettings(t, router, plan.ID, map[string]any{
				"config_version": plan.ConfigVersion,
				"parameters":     params,
			})
			if writer.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", writer.Code, writer.Body.String())
			}
			assertErrorCode(t, writer.Body.Bytes(), "parameters_invalid")
		})
	}
}

func TestPlanSettingsUpdate_StaleVersionConflictChangesNothing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)})

	planBefore, params, allocBefore := getSettingsFixtureState(t, r, plan.ID)
	params["retirement_age"] = float64(60)

	w := putSettings(t, r, plan.ID, map[string]any{
		"config_version":            plan.ConfigVersion + 5,
		"plan":                      map[string]any{"name": "不应生效的名字"},
		"allocation":                settingsTestAllocation,
		"parameters":                params,
		"apply_unallocated_to_cash": true,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	assertErrorCode(t, w.Body.Bytes(), "plan_version_conflict")

	planAfter, paramsAfter, allocAfter := getSettingsFixtureState(t, r, plan.ID)
	if planAfter["name"] != planBefore["name"] || planAfter["config_version"] != planBefore["config_version"] {
		t.Fatalf("plan changed on version conflict")
	}
	if paramsAfter["retirement_age"].(float64) == 60 {
		t.Fatal("parameters were persisted despite version conflict")
	}
	beforeJSON, _ := json.Marshal(allocBefore["asset_class_targets"])
	afterJSON, _ := json.Marshal(allocAfter["asset_class_targets"])
	if !bytes.Equal(beforeJSON, afterJSON) {
		t.Fatalf("allocation changed on version conflict")
	}
}

// The settings save that omits plan and allocation behaves like a pure
// parameters update but still bumps the version exactly once.
func TestPlanSettingsUpdate_ParametersOnly(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plan := createTestPlan(t, db)
	r := NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)})

	_, params, _ := getSettingsFixtureState(t, r, plan.ID)
	params["end_age"] = float64(95)

	w := putSettings(t, r, plan.ID, map[string]any{
		"config_version":            plan.ConfigVersion,
		"parameters":                params,
		"apply_unallocated_to_cash": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", w.Code, w.Body.String())
	}
	planAfter, paramsAfter, _ := getSettingsFixtureState(t, r, plan.ID)
	if got := int(planAfter["config_version"].(float64)); got != plan.ConfigVersion+1 {
		t.Fatalf("config_version = %d, want %d", got, plan.ConfigVersion+1)
	}
	if paramsAfter["end_age"].(float64) != 95 {
		t.Fatalf("end_age = %v, want 95", paramsAfter["end_age"])
	}
	// StudentTDf must remain the stored value even if the client tampers.
	stored, err := repository.NewParametersRepo(db).Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if int(paramsAfter["student_t_df"].(float64)) != stored.StudentTDf {
		t.Fatalf("student_t_df drifted: %v != %d", paramsAfter["student_t_df"], stored.StudentTDf)
	}
}
