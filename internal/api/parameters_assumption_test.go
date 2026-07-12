package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// getPlanParams fetches the API parameters object for a plan.
func getPlanParams(t *testing.T, baseURL, planID string) map[string]any {
	t.Helper()
	resp, err := http.DefaultClient.Get(baseURL + "/api/v1/plans/" + planID + "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	return env["data"].(map[string]any)["parameters"].(map[string]any)
}

// getPlanVersion reads the current config_version of a plan.
func getPlanVersion(t *testing.T, baseURL, planID string) int {
	t.Helper()
	resp, err := http.DefaultClient.Get(baseURL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	return int(env["data"].(map[string]any)["config_version"].(float64))
}

// getPlanConfigHash reads the current config_hash of a plan.
func getPlanConfigHash(t *testing.T, baseURL, planID string) string {
	t.Helper()
	resp, err := http.DefaultClient.Get(baseURL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	return env["data"].(map[string]any)["config_hash"].(string)
}

// putPlanParams sends a parameters update with the current config version.
func putPlanParams(t *testing.T, baseURL, planID string, params map[string]any) (int, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": getPlanVersion(t, baseURL, planID),
		"parameters":     params,
	})
	req, _ := http.NewRequest(http.MethodPut,
		baseURL+"/api/v1/plans/"+planID+"/parameters", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(mustRead(t, resp))
}

// TestPlanParametersAssumptionRoundTrip verifies that the six
// return-assumption fields must round-trip through the API DTO. New plans persist
// blended_prior/follow_global/baseline; an update of an unrelated field keeps the
// selection verbatim; and a pin to the active system profile survives a save.
func TestPlanParametersAssumptionRoundTrip(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if err := repository.NewAssumptionProfileRepo(db).EnsureSystemDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A plan whose holdings already match total assets, so an unrelated parameter
	// PUT does not trip the unallocated-gap guard.
	planID := seedSimulationReadyPlan(t, db)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	defer srv.Close()

	got := getPlanParams(t, srv.URL, planID)
	if got["return_assumption_mode"] != "blended_prior" ||
		got["assumption_selection_mode"] != "follow_global" ||
		got["return_assumption_scenario"] != "baseline" {
		t.Fatalf("new plan assumption selection wrong: %+v", got)
	}

	got["assumption_selection_mode"] = "pinned_profile"
	got["return_assumption_set_id"] = assumptions.SystemProfileID
	got["return_assumption_set_version"] = assumptions.SystemProfileVersion
	got["annual_savings_minor"] = 123_456_00
	if status, body := putPlanParams(t, srv.URL, planID, got); status != http.StatusOK {
		t.Fatalf("pin update status=%d body=%s", status, body)
	}

	got = getPlanParams(t, srv.URL, planID)
	if got["assumption_selection_mode"] != "pinned_profile" ||
		got["return_assumption_set_id"] != assumptions.SystemProfileID ||
		int(got["return_assumption_set_version"].(float64)) != assumptions.SystemProfileVersion {
		t.Fatalf("pin not persisted: %+v", got)
	}
	if int64(got["annual_savings_minor"].(float64)) != 123_456_00 {
		t.Fatalf("unrelated field not persisted: %+v", got["annual_savings_minor"])
	}
	if got["return_assumption_mode"] != "blended_prior" {
		t.Fatalf("mode lost on unrelated update: %+v", got)
	}
}

// TestStudentTDfReadOnlyOnForwardPlan verifies that the plan-level
// student_t_df is a read-only legacy field on forward plans. Sending a new value
// must neither change the persisted value nor the config hash (so existing runs
// are not marked stale for a field with no forward modeling effect).
func TestStudentTDfReadOnlyOnForwardPlan(t *testing.T) {
	db := testutil.OpenTestDB(t)
	if err := repository.NewAssumptionProfileRepo(db).EnsureSystemDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	planID := seedSimulationReadyPlan(t, db)

	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	defer srv.Close()

	hashBefore := getPlanConfigHash(t, srv.URL, planID)
	params := getPlanParams(t, srv.URL, planID)
	if int(params["student_t_df"].(float64)) != repository.DefaultStudentTDf {
		t.Fatalf("new forward plan df = %v, want %d", params["student_t_df"], repository.DefaultStudentTDf)
	}

	params["student_t_df"] = 25
	if status, body := putPlanParams(t, srv.URL, planID, params); status != http.StatusOK {
		t.Fatalf("update status=%d body=%s", status, body)
	}

	after := getPlanParams(t, srv.URL, planID)
	if int(after["student_t_df"].(float64)) != repository.DefaultStudentTDf {
		t.Fatalf("student_t_df must be read-only, got %v", after["student_t_df"])
	}
	if got := getPlanConfigHash(t, srv.URL, planID); got != hashBefore {
		t.Fatalf("read-only student_t_df update must not change config hash: before=%s after=%s", hashBefore, got)
	}
}

// TestPlanParametersAssumptionRejections covers rejection paths: unknown enums, a
// draft/non-existent pin, unparseable custom JSON and a floor>=ceiling guardrail
// must all be rejected with parameters_invalid and leave the record unchanged.
func TestPlanParametersAssumptionRejections(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := repository.NewAssumptionProfileRepo(db)
	if err := repo.EnsureSystemDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A user-owned draft profile so a draft pin can be exercised.
	draft := assumptions.SystemDefaultProfile()
	draft.ID = "user_draft_profile"
	draft.OwnerScope = assumptions.OwnerUser
	draft.Status = assumptions.StatusDraft
	if err := repo.Save(context.Background(), draft, "note", "reviewer", "2026-06-20"); err != nil {
		t.Fatal(err)
	}

	plan := createTestPlan(t, db)
	planID := plan.ID
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	defer srv.Close()

	cases := []struct {
		name  string
		apply func(p map[string]any)
	}{
		{"unknown mode", func(p map[string]any) { p["return_assumption_mode"] = "magic" }},
		{"unknown selection", func(p map[string]any) { p["assumption_selection_mode"] = "weird" }},
		{"unknown scenario", func(p map[string]any) { p["return_assumption_scenario"] = "rosy" }},
		{"draft pin", func(p map[string]any) {
			p["assumption_selection_mode"] = "pinned_profile"
			p["return_assumption_set_id"] = "user_draft_profile"
			p["return_assumption_set_version"] = 1
		}},
		{"missing pin", func(p map[string]any) {
			p["assumption_selection_mode"] = "pinned_profile"
			p["return_assumption_set_id"] = ""
			p["return_assumption_set_version"] = 0
		}},
		{"bad custom json", func(p map[string]any) {
			p["return_assumption_mode"] = "custom"
			p["custom_return_assumptions_json"] = "{not-json"
		}},
		{"floor >= ceiling", func(p map[string]any) {
			p["withdrawal_type"] = "guardrail"
			p["withdrawal_floor_ratio"] = 1.0
			p["withdrawal_ceiling_ratio"] = 1.0
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := getPlanParams(t, srv.URL, planID)
			tc.apply(params)
			status, body := putPlanParams(t, srv.URL, planID, params)
			if status == http.StatusOK {
				t.Fatalf("expected rejection, got 200 body=%s", body)
			}
			assertErrorCode(t, []byte(body), "parameters_invalid")
			after := getPlanParams(t, srv.URL, planID)
			if after["return_assumption_mode"] != "blended_prior" ||
				after["assumption_selection_mode"] != "follow_global" {
				t.Fatalf("record changed after rejection: %+v", after)
			}
		})
	}
}
