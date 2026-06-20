package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// AssumptionService is the API-facing manager for the global "模拟假设" center: it
// lists/reads/validates/saves/activates version-locked profiles and reads/writes
// the user's global default selection. The read-only system profile is always
// present (td/061 §3.2, §6.6).
type AssumptionService struct {
	repo *repository.AssumptionProfileRepo
}

func NewAssumptionService(db *sql.DB) *AssumptionService {
	return &AssumptionService{repo: repository.NewAssumptionProfileRepo(db)}
}

// AssumptionProfilesView is the list payload: every profile summary plus the
// resolved global default selection so the UI can mark the active default.
type AssumptionProfilesView struct {
	Profiles    []repository.ProfileSummary      `json:"profiles"`
	Preferences repository.AssumptionPreferences `json:"preferences"`
	Scenarios   []string                         `json:"scenarios"`
}

// ListProfiles ensures the system default exists and returns all summaries with
// the current preferences.
func (s *AssumptionService) ListProfiles(ctx context.Context) (AssumptionProfilesView, error) {
	if err := s.repo.EnsureSystemDefault(ctx); err != nil {
		return AssumptionProfilesView{}, wrapRepo("ensure system assumption profile", err)
	}
	profiles, err := s.repo.List(ctx)
	if err != nil {
		return AssumptionProfilesView{}, wrapRepo("list assumption profiles", err)
	}
	pref, err := s.repo.GetPreferences(ctx)
	if err != nil {
		return AssumptionProfilesView{}, wrapRepo("get assumption preferences", err)
	}
	for i := range profiles {
		eligible, err := s.isEligibleForGlobalDefault(ctx, profiles[i].ID, profiles[i].Version, profiles[i].Status)
		if err != nil {
			return AssumptionProfilesView{}, err
		}
		profiles[i].EligibleForGlobalDefault = eligible
	}
	return AssumptionProfilesView{
		Profiles:    profiles,
		Preferences: pref,
		Scenarios: []string{
			assumptions.ScenarioConservative, assumptions.ScenarioBaseline, assumptions.ScenarioOptimistic,
		},
	}, nil
}

// isEligibleForGlobalDefault reports whether a profile may be the user's global
// default: it must be active AND still pass the current publish gate (structure +
// coverage + PSD + tail). The legacy system_cma_v1@1 stays active for replay/pins
// but fails this gate (no tail truncation, stale correlation universe), so it can
// never be re-selected as the default (td/065 R8).
func (s *AssumptionService) isEligibleForGlobalDefault(
	ctx context.Context, id string, version int, status string,
) (bool, error) {
	if status != assumptions.StatusActive {
		return false, nil
	}
	p, err := s.repo.Get(ctx, id, version)
	if err != nil {
		if errors.Is(err, repository.ErrAssumptionProfileNotFound) {
			return false, nil
		}
		return false, wrapRepo("get assumption profile", err)
	}
	// A system-owned profile is only eligible when it is the CURRENT system
	// identity (v3). Frozen historical system profiles (v1/v2) stay active for
	// replay and explicit pins but can never be re-selected as the global default
	// (td/066 R12).
	if p.OwnerScope == assumptions.OwnerSystem &&
		!assumptions.IsCurrentSystemDefaultIdentity(p.ID, p.Version) {
		return false, nil
	}
	return s.assertActivatable(p) == nil, nil
}

// GetProfile returns one full profile id@version.
func (s *AssumptionService) GetProfile(
	ctx context.Context, id string, version int,
) (assumptions.Profile, error) {
	if err := s.repo.EnsureSystemDefault(ctx); err != nil {
		return assumptions.Profile{}, wrapRepo("ensure system assumption profile", err)
	}
	p, err := s.repo.Get(ctx, id, version)
	if err != nil {
		if errors.Is(err, repository.ErrAssumptionProfileNotFound) {
			return assumptions.Profile{}, newErr("assumption_profile_not_found",
				"assumption profile not found", map[string]any{"id": id, "version": version})
		}
		return assumptions.Profile{}, wrapRepo("get assumption profile", err)
	}
	return p, nil
}

// AssumptionValidationView reports structural + PSD validity for the editor.
type AssumptionValidationView struct {
	Valid          bool    `json:"valid"`
	Error          string  `json:"error,omitempty"`
	MinEigenvalue  float64 `json:"min_eigenvalue"`
	MaxRepairDelta float64 `json:"max_repair_delta"`
	PSDRepairHeavy bool    `json:"psd_repair_heavy"`
}

// ValidateProfile runs the structural validation plus the correlation PSD check.
// A heavy PSD repair (> threshold) is surfaced but does not by itself fail
// validation; the caller decides whether to block activation.
func (s *AssumptionService) ValidateProfile(p assumptions.Profile) AssumptionValidationView {
	out := AssumptionValidationView{Valid: true}
	if err := p.Validate(); err != nil {
		out.Valid = false
		out.Error = err.Error()
		return out
	}
	keys, rRaw := correlationMatrixFromProfile(p)
	if len(keys) >= 2 {
		res := simulation.CheckCorrelationPSD(rRaw)
		out.MinEigenvalue = res.MinEigenvalue
		out.MaxRepairDelta = res.MaxRepairDelta
		out.PSDRepairHeavy = res.MaxRepairDelta > simulation.PSDRepairWarnThreshold
	}
	return out
}

// SaveDraft validates then persists a new draft version. User profiles are always
// owner_scope=user with a server-assigned id: a brand-new profile (version 1) is
// given a fresh user_<uuid> id (the client id is never trusted), and the reserved
// system_cma_ namespace is rejected outright, so a user profile can never shadow a
// system identity and steal its evidence provenance (td/067 R13). A new version
// (version > 1) of an existing user profile keeps its id.
func (s *AssumptionService) SaveDraft(
	ctx context.Context, p assumptions.Profile, sourceNote, reviewedBy, reviewedAt string,
) (assumptions.Profile, error) {
	if p.OwnerScope == "" {
		p.OwnerScope = assumptions.OwnerUser
	}
	if p.OwnerScope == assumptions.OwnerSystem {
		return assumptions.Profile{}, newErr("assumption_profile_read_only",
			"system profile is read-only; copy it to a custom profile first", nil)
	}
	// Reserved namespace: a user profile must never use a system_cma_ id (td/067 R13).
	if assumptions.HasReservedSystemID(p.ID) {
		return assumptions.Profile{}, newErr("assumption_profile_reserved_id",
			"profile id uses the reserved 'system_cma_' namespace; user profiles receive a server-assigned id",
			map[string]any{"id": p.ID})
	}
	// Server-authoritative ids: a brand-new profile gets a fresh user id regardless
	// of what the client sent; only an explicit new version keeps the existing id.
	if p.Version <= 1 {
		p.ID = "user_" + uuid.New().String()
		p.Version = 1
	}
	if err := validateProfileAudit(sourceNote, reviewedBy, reviewedAt); err != nil {
		return assumptions.Profile{}, err
	}
	p.Status = assumptions.StatusDraft
	if err := s.assertActivatable(p); err != nil {
		return assumptions.Profile{}, err
	}
	if err := s.repo.Save(ctx, p, sourceNote, reviewedBy, reviewedAt); err != nil {
		return assumptions.Profile{}, wrapRepo("save assumption profile", err)
	}
	return p, nil
}

// Activate promotes a draft to active and supersedes other active versions.
func (s *AssumptionService) Activate(ctx context.Context, id string, version int) error {
	p, err := s.GetProfile(ctx, id, version)
	if err != nil {
		return err
	}
	if err := s.assertActivatable(p); err != nil {
		return err
	}
	if err := s.repo.Activate(ctx, id, version); err != nil {
		return wrapRepo("activate assumption profile", err)
	}
	return nil
}

// assertActivatable rejects a profile that cannot be safely saved or activated:
// any structural validation failure, or a correlation matrix that needs a heavy
// (> threshold) PSD repair. Both save and activate share this rule so a draft
// that fails completeness/duplicate/PSD checks can neither be persisted nor
// promoted (td/063 R3 §2, R4 §1).
func (s *AssumptionService) assertActivatable(p assumptions.Profile) error {
	v := s.ValidateProfile(p)
	if !v.Valid {
		return newErr("assumption_profile_invalid", v.Error, nil)
	}
	if v.PSDRepairHeavy {
		return newErr("assumption_profile_invalid", fmt.Sprintf(
			"correlation matrix needs a heavy PSD repair (max_repair_delta=%.4f > %.4f, "+
				"min_eigenvalue=%.4f); revise correlation priors",
			v.MaxRepairDelta, simulation.PSDRepairWarnThreshold, v.MinEigenvalue), nil)
	}
	return nil
}

// GetPreferences returns the user's resolved global default selection.
func (s *AssumptionService) GetPreferences(ctx context.Context) (repository.AssumptionPreferences, error) {
	if err := s.repo.EnsureSystemDefault(ctx); err != nil {
		return repository.AssumptionPreferences{}, wrapRepo("ensure system assumption profile", err)
	}
	pref, err := s.repo.GetPreferences(ctx)
	if err != nil {
		return repository.AssumptionPreferences{}, wrapRepo("get assumption preferences", err)
	}
	return pref, nil
}

// SetPreferences validates the target profile exists and is active, then stores
// it as the user's global default.
func (s *AssumptionService) SetPreferences(
	ctx context.Context, pref repository.AssumptionPreferences,
) (repository.AssumptionPreferences, error) {
	if pref.DefaultScenario == "" {
		pref.DefaultScenario = assumptions.ScenarioBaseline
	}
	if !validScenario(pref.DefaultScenario) {
		return repository.AssumptionPreferences{}, newErr("assumption_scenario_invalid",
			"scenario must be conservative, baseline or optimistic", nil)
	}
	p, err := s.GetProfile(ctx, pref.DefaultProfileID, pref.DefaultProfileVersion)
	if err != nil {
		return repository.AssumptionPreferences{}, err
	}
	if p.Status != assumptions.StatusActive {
		return repository.AssumptionPreferences{}, newErr("assumption_profile_not_active",
			"default profile must be an active version", nil)
	}
	// A frozen historical system profile (system_cma_v1@1 / v2@1) is active for
	// replay and explicit pins but is NOT the current default-able system identity,
	// so it can never be re-selected as the global default — which would otherwise
	// silently undo the system-default migration (td/066 R12).
	if p.OwnerScope == assumptions.OwnerSystem &&
		!assumptions.IsCurrentSystemDefaultIdentity(p.ID, p.Version) {
		return repository.AssumptionPreferences{}, newErr("assumption_profile_not_eligible",
			"this system profile version is retained only for historical replay and cannot be the global default; "+
				"use the current system default",
			map[string]any{"id": pref.DefaultProfileID, "version": pref.DefaultProfileVersion})
	}
	// The global default must also still pass the current publish gate (structure,
	// coverage, PSD, tail). The legacy system profiles fail this gate, so they can
	// never be re-selected as the default (td/065 R8 / td/066 R12).
	if err := s.assertActivatable(p); err != nil {
		var ae *AppError
		if errors.As(err, &ae) {
			return repository.AssumptionPreferences{}, newErr("assumption_profile_not_eligible",
				"profile is not eligible as a global default: "+ae.Message,
				map[string]any{"id": pref.DefaultProfileID, "version": pref.DefaultProfileVersion})
		}
		return repository.AssumptionPreferences{}, err
	}
	if err := s.repo.SetPreferences(ctx, pref); err != nil {
		return repository.AssumptionPreferences{}, wrapRepo("set assumption preferences", err)
	}
	return pref, nil
}

// validateProfileAudit enforces named, sourced, dated review metadata on every
// saved profile version (td/063 N1): a non-empty source note, a named reviewer,
// and an ISO (YYYY-MM-DD) review date.
func validateProfileAudit(sourceNote, reviewedBy, reviewedAt string) error {
	if strings.TrimSpace(sourceNote) == "" {
		return newErr("assumption_profile_invalid", "source_note (CMA provenance) is required", nil)
	}
	if strings.TrimSpace(reviewedBy) == "" {
		return newErr("assumption_profile_invalid", "reviewed_by (named reviewer) is required", nil)
	}
	if _, err := time.Parse("2006-01-02", reviewedAt); err != nil {
		return newErr("assumption_profile_invalid", "reviewed_at must be an ISO date (YYYY-MM-DD)", nil)
	}
	return nil
}

func validScenario(s string) bool {
	switch s {
	case assumptions.ScenarioConservative, assumptions.ScenarioBaseline, assumptions.ScenarioOptimistic:
		return true
	default:
		return false
	}
}

// correlationMatrixFromProfile builds the symmetric correlation matrix implied by
// a profile's pairwise priors over the union of named factors (diagonal 1, pairs
// filled both ways, unspecified off-diagonals 0).
func correlationMatrixFromProfile(p assumptions.Profile) ([]string, [][]float64) {
	idx := map[string]int{}
	var keys []string
	add := func(k string) {
		if _, ok := idx[k]; !ok {
			idx[k] = len(keys)
			keys = append(keys, k)
		}
	}
	for _, c := range p.CorrelationPriors {
		add(c.FactorA)
		add(c.FactorB)
	}
	n := len(keys)
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n)
		m[i][i] = 1
	}
	for _, c := range p.CorrelationPriors {
		i, j := idx[c.FactorA], idx[c.FactorB]
		m[i][j] = c.Rho
		m[j][i] = c.Rho
	}
	return keys, m
}
