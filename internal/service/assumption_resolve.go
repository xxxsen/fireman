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
// fallback is identical everywhere (td/061 §5.A.2).
type resolvedAssumption struct {
	Profile  assumptions.Profile
	Scenario string
	Mode     string
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
	scenario := params.ReturnAssumptionScenario
	if scenario == "" {
		scenario = assumptions.ScenarioBaseline
	}

	profile, scenario, err := s.resolveProfileAndScenario(ctx, params, scenario)
	if err != nil {
		return resolvedAssumption{}, err
	}
	return resolvedAssumption{Profile: profile, Scenario: scenario, Mode: mode}, nil
}

func (s *SimulationService) resolveProfileAndScenario(
	ctx context.Context, params repository.PlanParameters, scenario string,
) (assumptions.Profile, string, error) {
	if params.AssumptionSelectionMode == "pinned_profile" && params.ReturnAssumptionSetID != "" {
		p, err := s.assumptions.Get(ctx, params.ReturnAssumptionSetID, params.ReturnAssumptionSetVersion)
		if err != nil {
			return assumptions.Profile{}, "", newErr("assumption_profile_not_found",
				"pinned assumption profile is unavailable", map[string]any{
					"profile_id": params.ReturnAssumptionSetID, "version": params.ReturnAssumptionSetVersion,
				})
		}
		return p, scenario, nil
	}

	pref, err := s.assumptions.GetPreferences(ctx)
	if err != nil {
		return assumptions.Profile{}, "", wrapRepo("get assumption preferences", err)
	}
	if scenario == assumptions.ScenarioBaseline && pref.DefaultScenario != "" {
		scenario = pref.DefaultScenario
	}
	p, err := s.assumptions.Get(ctx, pref.DefaultProfileID, pref.DefaultProfileVersion)
	if err == nil {
		return p, scenario, nil
	}
	if !errors.Is(err, repository.ErrAssumptionProfileNotFound) {
		return assumptions.Profile{}, "", wrapRepo("get default assumption profile", err)
	}
	// The configured default version was removed; fall back to the system default.
	sys, sysErr := s.assumptions.Get(ctx, assumptions.SystemProfileID, assumptions.SystemProfileVersion)
	if sysErr != nil {
		return assumptions.Profile{}, "", wrapRepo("get system assumption profile", sysErr)
	}
	return sys, scenario, nil
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
