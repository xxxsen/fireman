package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	if sys["id"] != "system_cma_v3" || sys["owner_scope"] != "system" {
		t.Fatalf("unexpected system profile: %+v", sys)
	}
	pref := data["preferences"].(map[string]any)
	if pref["default_profile_id"] != "system_cma_v3" {
		t.Fatalf("preferences should default to system profile, got %+v", pref)
	}

	// Get full system profile.
	getEnv := doJSON(t, r, http.MethodGet,
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	// Attempt to overwrite the system id/owner -> rejected as read-only.
	sysCopy := cloneProfile(profile)
	doRaw(t, r, http.MethodPost, "/api/v1/simulation-assumptions/profiles",
		map[string]any{"profile": sysCopy}, "assumption_profile_read_only")

	// Copy to a user draft, save it. The client-proposed id is ignored: the server
	// assigns a fresh user_ id so a user profile can never squat a system id
	// (td/067 R13).
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
	savedID := saved["id"].(string)
	if !strings.HasPrefix(savedID, "user_") || strings.HasPrefix(savedID, assumptions.SystemProfileIDPrefix) {
		t.Fatalf("server must assign a fresh user_ id, got %q", savedID)
	}

	// Activate it (using the server-assigned id).
	doJSON(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/"+savedID+"/1/activate", nil, http.StatusOK)

	// Set it as the global default.
	setEnv := doJSON(t, r, http.MethodPut, "/api/v1/simulation-assumptions/preferences",
		map[string]any{"preferences": map[string]any{
			"default_profile_id": savedID, "default_profile_version": 1, "default_scenario": "conservative",
		}}, http.StatusOK)
	setPref := setEnv["data"].(map[string]any)["preferences"].(map[string]any)
	if setPref["default_profile_id"] != savedID || setPref["default_scenario"] != "conservative" {
		t.Fatalf("preferences not updated: %+v", setPref)
	}
}

// TestCreateUserProfileRejectsReservedID covers td/067 R13 acceptance #1: a user
// profile may never claim a reserved system_cma_ id (create AND validate paths
// reject it), and a normal user profile always receives a server-assigned user_ id.
func TestCreateUserProfileRejectsReservedID(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	getEnv := doJSON(t, r, http.MethodGet,
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	reserved := cloneProfile(profile)
	reserved["id"] = "system_cma_v3"
	reserved["version"] = 1
	reserved["owner_scope"] = "user"
	reserved["status"] = "draft"
	reserved["name"] = "hijack"

	// Create is rejected with the stable reserved-id code.
	doRaw(t, r, http.MethodPost, "/api/v1/simulation-assumptions/profiles",
		map[string]any{"profile": reserved, "source_note": "x", "reviewed_by": "me", "reviewed_at": "2026-06-20"},
		"assumption_profile_reserved_id")
	// Validate is rejected too.
	doRaw(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1/validate",
		map[string]any{"profile": reserved}, "assumption_profile_reserved_id")

	// A normal user profile (even with a client id) gets a server-assigned user_ id.
	ok := cloneProfile(profile)
	ok["id"] = "anything_client_sends"
	ok["version"] = 1
	ok["owner_scope"] = "user"
	ok["status"] = "draft"
	ok["name"] = "正常自定义"
	saveEnv := doJSON(t, r, http.MethodPost, "/api/v1/simulation-assumptions/profiles",
		map[string]any{"profile": ok, "source_note": "x", "reviewed_by": "me", "reviewed_at": "2026-06-20"},
		http.StatusOK)
	savedID := saveEnv["data"].(map[string]any)["profile"].(map[string]any)["id"].(string)
	if !strings.HasPrefix(savedID, "user_") || strings.HasPrefix(savedID, assumptions.SystemProfileIDPrefix) {
		t.Fatalf("normal user profile must get a server-assigned user_ id, got %q", savedID)
	}
}

func TestAssumptionValidateRejectsBadProfile(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	getEnv := doJSON(t, r, http.MethodGet,
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	// A profile missing scenarios must fail validation.
	bad := cloneProfile(profile)
	delete(bad, "scenarios")
	env := doJSON(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1/validate",
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
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1", nil, http.StatusOK)
	profile := getEnv["data"].(map[string]any)["profile"].(map[string]any)

	bent := cloneProfile(profile)
	bent["id"] = "user_cma_psd"
	bent["version"] = 1
	bent["owner_scope"] = "user"
	bent["status"] = "draft"
	bendCorrelationTriangle(t, bent)

	// Validate endpoint surfaces the heavy repair but still parses structurally.
	valEnv := doJSON(t, r, http.MethodPost,
		"/api/v1/simulation-assumptions/profiles/system_cma_v3/1/validate",
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

// td/065 R8 + td/066 R12: a frozen historical system profile (v1 ineligible by the
// publish gate, v2 ineligible because it is no longer the current identity) must
// not be selectable as the global default. The list exposes
// eligible_for_global_default, SetPreferences rejects both with
// assumption_profile_not_eligible, and the stored default stays on v3.
func TestSetPreferencesRejectsIneligibleLegacyDefault(t *testing.T) {
	db := testutil.OpenTestDB(t)
	r := NewRouter(context.Background(), Deps{DB: db})

	// Seed an active legacy v1 row directly via SQL: it carries zero tail
	// truncation, which fails the current Validate gate, so it is ineligible even
	// though it is active. Raw SQL bypasses the service gate, mimicking a DB
	// upgraded from a pre-td063 release.
	seedSystemProfileRow(t, db, assumptions.SystemLegacyProfileID, assumptions.SystemLegacyProfileVersion,
		"系统默认（CMA v1）", func(p *assumptions.Profile) {
			p.ReturnFloor = 0
			p.ReturnCeil = 0
		})
	// Seed the REAL published td/064 v2 content (byte-exact fixture). It is a
	// recognized historical system content (so the upgrade accepts it) but is NOT
	// the current identity, so it must still be ineligible purely on the identity
	// rule (td/066 R12 / td/067 R14), proving the gate is identity-based, not only
	// structural.
	seedRealV2SystemRow(t, db)

	// List surfaces eligibility: v3 eligible; v1 and v2 not.
	listEnv := doJSON(t, r, http.MethodGet, "/api/v1/simulation-assumptions/profiles", nil, http.StatusOK)
	data := listEnv["data"].(map[string]any)
	elig := map[string]bool{}
	for _, raw := range data["profiles"].([]any) {
		p := raw.(map[string]any)
		elig[p["id"].(string)] = p["eligible_for_global_default"].(bool)
	}
	if !elig["system_cma_v3"] {
		t.Fatalf("v3 must be eligible: %+v", elig)
	}
	if elig["system_cma_v2"] {
		t.Fatalf("frozen v2 must NOT be eligible: %+v", elig)
	}
	if elig["system_cma_v1"] {
		t.Fatalf("legacy v1 must NOT be eligible: %+v", elig)
	}
	// Default resolves to v3 after the fresh-install seed.
	if data["preferences"].(map[string]any)["default_profile_id"] != "system_cma_v3" {
		t.Fatalf("default should be v3, got %+v", data["preferences"])
	}

	// Setting the ineligible v1 or v2 as default is rejected.
	doRaw(t, r, http.MethodPut, "/api/v1/simulation-assumptions/preferences",
		map[string]any{"preferences": map[string]any{
			"default_profile_id": "system_cma_v1", "default_profile_version": 1, "default_scenario": "baseline",
		}}, "assumption_profile_not_eligible")
	doRaw(t, r, http.MethodPut, "/api/v1/simulation-assumptions/preferences",
		map[string]any{"preferences": map[string]any{
			"default_profile_id": "system_cma_v2", "default_profile_version": 1, "default_scenario": "baseline",
		}}, "assumption_profile_not_eligible")

	// The stored default is unchanged (still v3).
	afterEnv := doJSON(t, r, http.MethodGet, "/api/v1/simulation-assumptions/profiles", nil, http.StatusOK)
	after := afterEnv["data"].(map[string]any)["preferences"].(map[string]any)
	if after["default_profile_id"] != "system_cma_v3" {
		t.Fatalf("default must stay on v3 after rejected set, got %+v", after)
	}
}

// seedRealV2SystemRow inserts the byte-exact published td/064 system_cma_v2@1
// canonical fixture so a v2 row carries a content hash recognized by the system
// content registry (td/067 R14).
func seedRealV2SystemRow(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, err := os.ReadFile("../repository/testdata/system_cma_v2_canonical.json")
	if err != nil {
		t.Fatalf("read v2 fixture: %v", err)
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		assumptions.SystemProfileV2ID, assumptions.SystemProfileV2Version, "system",
		"系统默认（CMA v2）", "active", string(raw), hash,
		"historical release", "FIRE", "2026-06-20", now, now); err != nil {
		t.Fatalf("seed v2 fixture: %v", err)
	}
}

// seedSystemProfileRow inserts an active system-owned profile row directly via SQL
// (bypassing the service gate) to mimic a database upgraded from an older release.
// mutate optionally bends the profile content before it is frozen.
func seedSystemProfileRow(
	t *testing.T, db *sql.DB, id string, version int, name string, mutate func(*assumptions.Profile),
) {
	t.Helper()
	p := assumptions.SystemDefaultProfile()
	p.ID = id
	p.Version = version
	p.Name = name
	if mutate != nil {
		mutate(&p)
	}
	cb, err := p.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical json: %v", err)
	}
	h, err := p.ContentHash()
	if err != nil {
		t.Fatalf("content hash: %v", err)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO simulation_assumption_profiles
		(id, version, owner_scope, name, status, canonical_json, content_hash,
		 source_note, reviewed_by, reviewed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Version, "system", p.Name, "active", string(cb), h,
		"legacy", "FIRE", "2026-06-20", now, now); err != nil {
		t.Fatalf("seed system profile %s@%d: %v", id, version, err)
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
