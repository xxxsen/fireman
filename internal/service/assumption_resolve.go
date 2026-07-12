package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
)

// resolvedAssumption is the frozen-at-resolution global profile, scenario and
// mode a run will calibrate against. Every plan/run path must obtain its
// assumptions only through ResolveAssumptionProfile so the system default
// fallback is identical everywhere.
type resolvedAssumption struct {
	Profile  assumptions.Profile
	Scenario string
	Mode     string
	// ProfileContentHash is the FROZEN stored content hash of the resolved profile
	// row (not a re-canonicalization of the decoded struct). For a legacy system
	// profile whose on-disk canonical predates current struct fields, the stored
	// hash is the only one that matches the immutable registry, so run provenance
	// and the system-content recognition check must use it. Empty
	// for an in-memory profile (unit tests / built-in fallback), in which case the
	// snapshot builder recomputes it.
	ProfileContentHash string
}

// EffectiveAssumptionIdentity is the complete immutable identity that affects a
// forward simulation. It is returned by APIs and included in configuration
// hashes so global-default changes reliably make prior results stale.
type EffectiveAssumptionIdentity struct {
	ProfileID      string `json:"profile_id"`
	ProfileVersion int    `json:"profile_version"`
	ContentHash    string `json:"content_hash"`
	Scenario       string `json:"scenario"`
}

// ResolveAssumptionProfile loads the profile + scenario a plan's parameters
// select, falling back to the read-only system default when the user has not
// configured a global profile.
func (s *SimulationService) ResolveAssumptionProfile(
	ctx context.Context, params repository.PlanParameters,
) (resolvedAssumption, error) {
	if err := s.assumptions.EnsureSystemDefault(ctx); err != nil {
		return resolvedAssumption{}, wrapRepo("ensure system assumption profile", err)
	}

	mode := params.ReturnAssumptionMode
	if mode == "" {
		mode = repository.DefaultReturnAssumptionMode
	}
	profile, scenario, contentHash, err := resolveProfileAndScenario(ctx, s.assumptions, params)
	if err != nil {
		return resolvedAssumption{}, err
	}
	return resolvedAssumption{
		Profile: profile, Scenario: scenario, Mode: mode, ProfileContentHash: contentHash,
	}, nil
}

func resolveProfileAndScenario(
	ctx context.Context, repo *repository.AssumptionProfileRepo, params repository.PlanParameters,
) (assumptions.Profile, string, string, error) {
	scenario := params.ReturnAssumptionScenario
	if scenario == "" {
		scenario = assumptions.ScenarioFollowGlobal
	}
	if params.AssumptionSelectionMode == SelectionPinnedProfile && params.ReturnAssumptionSetID != "" {
		p, hash, err := repo.GetWithHash(ctx, params.ReturnAssumptionSetID, params.ReturnAssumptionSetVersion)
		if err != nil {
			return assumptions.Profile{}, "", "", newErr("assumption_profile_not_found",
				"pinned assumption profile is unavailable", map[string]any{
					"profile_id": params.ReturnAssumptionSetID, "version": params.ReturnAssumptionSetVersion,
				})
		}
		if p.Status == assumptions.StatusDraft {
			return assumptions.Profile{}, "", "", newErr("assumption_profile_draft",
				"draft assumption profiles cannot be used by a simulation", map[string]any{
					"profile_id": params.ReturnAssumptionSetID, "version": params.ReturnAssumptionSetVersion,
				})
		}
		finalScenario, err := resolveEffectiveScenario(ctx, repo, scenario)
		if err != nil {
			return assumptions.Profile{}, "", "", err
		}
		if _, ok := p.Scenarios[finalScenario]; !ok {
			return assumptions.Profile{}, "", "", scenarioNotFound(p, finalScenario)
		}
		return p, finalScenario, hash, nil
	}

	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		return assumptions.Profile{}, "", "", wrapRepo("get assumption preferences", err)
	}
	if scenario == assumptions.ScenarioFollowGlobal {
		scenario = pref.DefaultScenario
	}
	p, hash, err := repo.GetWithHash(ctx, pref.DefaultProfileID, pref.DefaultProfileVersion)
	if errors.Is(err, repository.ErrAssumptionProfileNotFound) {
		return assumptions.Profile{}, "", "", newErr("assumption_profile_not_found",
			"global default assumption profile is unavailable", map[string]any{
				"profile_id": pref.DefaultProfileID, "version": pref.DefaultProfileVersion,
			})
	}
	if err != nil {
		return assumptions.Profile{}, "", "", wrapRepo("get default assumption profile", err)
	}
	if p.Status != assumptions.StatusActive {
		return assumptions.Profile{}, "", "", newErr("assumption_profile_not_active_for_default",
			"global default must reference an active assumption profile", map[string]any{
				"profile_id": p.ID, "version": p.Version, "status": p.Status,
			})
	}
	if p.OwnerScope == assumptions.OwnerSystem &&
		!assumptions.IsCurrentSystemDefaultIdentity(p.ID, p.Version) {
		return assumptions.Profile{}, "", "", newErr("assumption_profile_not_active_for_default",
			"historical system profiles cannot be used as the global default", nil)
	}
	if err := p.ValidateSelfCorrelationCoverage(); err != nil {
		return assumptions.Profile{}, "", "", newErr("assumption_same_type_correlation_missing", err.Error(), nil)
	}
	if _, ok := p.Scenarios[scenario]; !ok {
		return assumptions.Profile{}, "", "", scenarioNotFound(p, scenario)
	}
	return p, scenario, hash, nil
}

func resolveEffectiveScenario(
	ctx context.Context, repo *repository.AssumptionProfileRepo, scenario string,
) (string, error) {
	if scenario != assumptions.ScenarioFollowGlobal {
		return scenario, nil
	}
	pref, err := repo.GetPreferences(ctx)
	if err != nil {
		return "", wrapRepo("get assumption preferences", err)
	}
	return pref.DefaultScenario, nil
}

func scenarioNotFound(profile assumptions.Profile, scenario string) error {
	return newErr("assumption_scenario_not_found", "scenario is not defined by the selected profile",
		map[string]any{"profile_id": profile.ID, "version": profile.Version, "scenario": scenario})
}

func identityFromResolved(res resolvedAssumption) EffectiveAssumptionIdentity {
	return EffectiveAssumptionIdentity{
		ProfileID: res.Profile.ID, ProfileVersion: res.Profile.Version,
		ContentHash: res.ProfileContentHash, Scenario: res.Scenario,
	}
}

// calibrateAsset derives forward return + volatility for one asset using the
// resolved profile. custom values are read from the plan's per-instrument
// custom map; a missing prior under blended_prior is a hard error.
func calibrateAsset(
	res resolvedAssumption,
	instrumentID, assetClass, region, valuationCurrency string,
	historicalReturn, historicalVol float64,
	completeYears int,
	customByInstrument map[string]float64,
) (assumptions.CalibrationResult, error) {
	in := assumptions.CalibrationInput{
		Source:                          res.Mode,
		AssetClass:                      assetClass,
		Region:                          region,
		ValuationCurrency:               valuationCurrency,
		HistoricalAnnualGeometricReturn: historicalReturn,
		HistoricalAnnualVolatility:      historicalVol,
		CompleteYearCount:               completeYears,
		Scenario:                        res.Scenario,
	}
	if res.Mode == assumptions.SourceCustom {
		if v, ok := customByInstrument[instrumentID]; ok {
			in.CustomAnnualGeometricReturn = &v
		}
	}
	out, err := res.Profile.CalibrateForwardReturn(in)
	if err != nil {
		return assumptions.CalibrationResult{}, fmt.Errorf("calibrate forward return: %w", err)
	}
	return out, nil
}

func parseCustomReturnAssumptions(raw string) map[string]float64 {
	out := map[string]float64{}
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
