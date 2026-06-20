package service

import (
	"context"
	"database/sql"
	"errors"

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
	return AssumptionProfilesView{
		Profiles:    profiles,
		Preferences: pref,
		Scenarios: []string{
			assumptions.ScenarioConservative, assumptions.ScenarioBaseline, assumptions.ScenarioOptimistic,
		},
	}, nil
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

// SaveDraft validates then persists a new draft version. The system profile id
// is read-only; an attempt to overwrite it (or any existing active version) is
// rejected by the repository's unique (id, version) constraint.
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
	p.Status = assumptions.StatusDraft
	if v := s.ValidateProfile(p); !v.Valid {
		return assumptions.Profile{}, newErr("assumption_profile_invalid", v.Error, nil)
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
	if v := s.ValidateProfile(p); !v.Valid {
		return newErr("assumption_profile_invalid", v.Error, nil)
	}
	if err := s.repo.Activate(ctx, id, version); err != nil {
		return wrapRepo("activate assumption profile", err)
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
	if err := s.repo.SetPreferences(ctx, pref); err != nil {
		return repository.AssumptionPreferences{}, wrapRepo("set assumption preferences", err)
	}
	return pref, nil
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
