package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// td/061 §6.2.5/§6.6/§7: the system profile is always present and read-only; the
// preferences default to it; copying it to a user draft + activating works; and
// trying to overwrite the system profile is rejected.
func TestAssumptionProfilesLifecycle(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	// List: system profile present, preferences default to it.
	listEnv := doJSON(t, r, http.MethodGet, "/api/v1/simulation-assumptions/profiles", nil, http.StatusOK)
	data := listEnv["data"].(map[string]any)
	profiles := data["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("expected 1 system profile, got %d", len(profiles))
	}
	sys := profiles[0].(map[string]any)
	if sys["id"] != "system_cma_v2" || sys["owner_scope"] != "system" {
		t.Fatalf("unexpected system profile: %+v", sys)
	}
	pref := data["preferences"].(map[string]any)
	if pref["default_profile_id"] != "system_cma_v2" {
		t.Fatalf("preferences should default to system profile, got %+v", pref)
	}

	// Get full system profile.
	getEnv := doJSON(t, r, http.MethodGet,
		"/api/v1/simulation-assumptions/profiles/system_cma_v2/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	// Attempt to overwrite the system id/owner -> rejected as read-only.
	sysCopy := cloneProfile(profile)
	doRaw(t, r, http.MethodPost, "/api/v1/simulation-assumptions/profiles",
		map[string]any{"profile": sysCopy}, "assumption_profile_read_only")

	// Copy to a user draft, save it.
	draft := cloneProfile(profile)
	draft["id"] = "user_cma_custom"
	draft["version"] = 1
	draft["owner_scope"] = "user"
	draft["status"] = "draft"
	draft["name"] = "我的自定义假设"
	saveEnv := doJSON(t, r, http.MethodPost, "/api/v1/simulation-assumptions/profiles",
		map[string]any{"profile": draft, "source_note": "copy", "reviewed_by": "me", "reviewed_at": "2026-06-20"},
		http.StatusOK)
	saved := saveEnv["data"].(map[string]any)["profile"].(map[string]any)
	if saved["status"] != "draft" {
		t.Fatalf("saved profile should be draft, got %v", saved["status"])
	}

	// Activate it.
	doJSON(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/user_cma_custom/1/activate", nil, http.StatusOK)

	// Set it as the global default.
	setEnv := doJSON(t, r, http.MethodPut, "/api/v1/simulation-assumptions/preferences",
		map[string]any{"preferences": map[string]any{
			"default_profile_id": "user_cma_custom", "default_profile_version": 1, "default_scenario": "conservative",
		}}, http.StatusOK)
	setPref := setEnv["data"].(map[string]any)["preferences"].(map[string]any)
	if setPref["default_profile_id"] != "user_cma_custom" || setPref["default_scenario"] != "conservative" {
		t.Fatalf("preferences not updated: %+v", setPref)
	}
}

func TestAssumptionValidateRejectsBadProfile(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	getEnv := doJSON(t, r, http.MethodGet,
		"/api/v1/simulation-assumptions/profiles/system_cma_v2/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	// A profile missing scenarios must fail validation.
	bad := cloneProfile(profile)
	delete(bad, "scenarios")
	env := doJSON(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/system_cma_v2/1/validate",
		map[string]any{"profile": bad}, http.StatusOK)
	res := env["data"].(map[string]any)
	if res["valid"].(bool) {
		t.Fatalf("expected invalid profile, got %+v", res)
	}
}

// td/063 R4 §1 / R3 §2: a draft whose correlation matrix needs a heavy PSD
// repair can neither be saved nor activated. We copy the system profile and bend
// three pairwise correlations into a non-PSD triangle, then assert both the save
// and the (separately-seeded) activate path reject it.
func TestAssumptionHeavyPSDRepairBlocksSaveAndActivate(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	getEnv := doJSON(t, r, http.MethodGet,
		"/api/v1/simulation-assumptions/profiles/system_cma_v2/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	bent := cloneProfile(profile)
	bent["id"] = "user_cma_psd"
	bent["version"] = 1
	bent["owner_scope"] = "user"
	bent["status"] = "draft"
	bendCorrelationTriangle(t, bent)

	// Validate endpoint surfaces the heavy repair but still parses structurally.
	valEnv := doJSON(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/system_cma_v2/1/validate",
		map[string]any{"profile": bent}, http.StatusOK)
	val := valEnv["data"].(map[string]any)
	if !val["psd_repair_heavy"].(bool) {
		t.Fatalf("expected psd_repair_heavy=true, got %+v", val)
	}

	// Save must be rejected.
	doRaw(t, r, http.MethodPost, "/api/v1/simulation-assumptions/profiles",
		map[string]any{"profile": bent, "source_note": "x", "reviewed_by": "me", "reviewed_at": "2026-06-20"},
		"assumption_profile_invalid")

	// And activate of a directly-seeded heavy-PSD draft must also be rejected.
	repo := repository.NewAssumptionProfileRepo(db)
	var heavy assumptions.Profile
	b, _ := json.Marshal(bent)
	if err := json.Unmarshal(b, &heavy); err != nil {
		t.Fatalf("decode bent profile: %v", err)
	}
	if err := repo.Save(context.Background(), heavy, "x", "me", "2026-06-20"); err != nil {
		t.Fatalf("seed heavy draft: %v", err)
	}
	doRaw(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/user_cma_psd/1/activate",
		nil, "assumption_profile_invalid")
}

// td/065 R8: an active-but-ineligible legacy profile (predating the current
// publish gate) must not be selectable as the global default. The list exposes
// eligible_for_global_default, SetPreferences rejects ineligible targets with
// assumption_profile_not_eligible, and the stored default stays on v2.
func TestSetPreferencesRejectsIneligibleLegacyDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	// Seed an active legacy v1 row directly via SQL: it carries zero tail
	// truncation, which fails the current Validate gate, so it is ineligible even
	// though it is active. Raw SQL bypasses the service gate, mimicking a DB
	// upgraded from a pre-td063 release.
	legacy := assumptions.SystemDefaultProfile()
	legacy.ID = assumptions.SystemLegacyProfileID
	legacy.Version = assumptions.SystemLegacyProfileVersion
	legacy.Name = "系统默认（CMA v1）"
	legacy.ReturnFloor = 0
	legacy.ReturnCeil = 0
	cb, err := legacy.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	h, err := legacy.ContentHash()
	if err != nil {
		t.Fatalf("content hash: %v", err)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		legacy.ID, legacy.Version, "system", legacy.Name, "active", string(cb), h,
		"legacy", "FIRE", "2026-06-20", now, now); err != nil {
		t.Fatalf("seed legacy v1: %v", err)
	}

	// List surfaces eligibility: v2 eligible, legacy v1 not.
	listEnv := doJSON(t, r, http.MethodGet, "/api/v1/simulation-assumptions/profiles", nil, http.StatusOK)
	data := listEnv["data"].(map[string]any)
	elig := map[string]bool{}
	for _, raw := range data["profiles"].([]any) {
		p := raw.(map[string]any)
		elig[p["id"].(string)] = p["eligible_for_global_default"].(bool)
	}
	if !elig["system_cma_v2"] {
		t.Fatalf("v2 must be eligible: %+v", elig)
	}
	if elig["system_cma_v1"] {
		t.Fatalf("legacy v1 must NOT be eligible: %+v", elig)
	}
	// Default resolves to v2 after the fresh-install seed.
	if data["preferences"].(map[string]any)["default_profile_id"] != "system_cma_v2" {
		t.Fatalf("default should be v2, got %+v", data["preferences"])
	}

	// Setting the ineligible legacy v1 as default is rejected.
	doRaw(t, r, http.MethodPut, "/api/v1/simulation-assumptions/preferences",
		map[string]any{"preferences": map[string]any{
			"default_profile_id": "system_cma_v1", "default_profile_version": 1, "default_scenario": "baseline",
		}}, "assumption_profile_not_eligible")

	// The stored default is unchanged (still v2).
	afterEnv := doJSON(t, r, http.MethodGet, "/api/v1/simulation-assumptions/profiles", nil, http.StatusOK)
	after := afterEnv["data"].(map[string]any)["preferences"].(map[string]any)
	if after["default_profile_id"] != "system_cma_v2" {
		t.Fatalf("default must stay on v2 after rejected set, got %+v", after)
	}
}

// bendCorrelationTriangle rewrites three pairwise correlations among domestic
// equity and the two FX factors into a non-positive-semidefinite triangle.
func bendCorrelationTriangle(t *testing.T, p map[string]any) {
	t.Helper()
	priors, ok := p["correlation_priors"].([]any)
	if !ok {
		t.Fatalf("profile missing correlation_priors: %+v", p["correlation_priors"])
	}
	const (
		eqD   = "asset:equity:domestic"
		fxUSD = "fx:USD:CNY"
		fxHKD = "fx:HKD:CNY"
	)
	pairRho := func(a, b string) (float64, bool) {
		switch {
		case (a == eqD && b == fxUSD) || (a == fxUSD && b == eqD):
			return 0.9, true
		case (a == eqD && b == fxHKD) || (a == fxHKD && b == eqD):
			return 0.9, true
		case (a == fxUSD && b == fxHKD) || (a == fxHKD && b == fxUSD):
			return -0.9, true
		default:
			return 0, false
		}
	}
	changed := 0
	for _, raw := range priors {
		c := raw.(map[string]any)
		if rho, ok := pairRho(c["factor_a"].(string), c["factor_b"].(string)); ok {
			c["rho"] = rho
			changed++
		}
	}
	if changed != 3 {
		t.Fatalf("expected to bend 3 correlation pairs, bent %d", changed)
	}
}

func cloneProfile(p map[string]any) map[string]any {
	b, _ := json.Marshal(p)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// doJSON issues a request expecting a 200 envelope and returns it decoded.
func doJSON(t *testing.T, r http.Handler, method, path string, body any, _ int) map[string]any {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s status=%d want=200 body=%s", method, path, w.Code, w.Body.String())
	}
	return decodeEnvelope(t, w.Body.Bytes())
}

// doRaw issues a request expecting a 400 error envelope with the given code.
func doRaw(t *testing.T, r http.Handler, method, path string, body any, wantCode string) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("%s %s status=%d want=400 body=%s", method, path, w.Code, w.Body.String())
	}
	if wantCode != "" {
		assertErrorCode(t, w.Body.Bytes(), wantCode)
	}
}
