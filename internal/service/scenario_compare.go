package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// scenarioCompareMaxRuns caps the synchronous per-scenario path count while
// preserving the selected run's seed and frozen input.
const scenarioCompareMaxRuns = 3000

var (
	errFrozenFactorCorrelationMissing = errors.New("frozen factor correlation is missing")
	errFrozenFactorReferencesMissing  = errors.New("frozen factor references are incomplete")
	errAssetFactorReferenceRange      = errors.New("asset factor reference is out of range")
	errFXFactorReferenceRange         = errors.New("FX factor reference is out of range")
	errFrozenFactorInputsMissing      = errors.New("frozen factor inputs are incomplete")
	errScenarioCovarianceDecompose    = errors.New("scenario covariance cannot be decomposed")
	errFactorExposureMappingMissing   = errors.New("factor exposure mapping is incomplete")
)

// ScenarioComparisonRow is one scenario's headline outcome in the comparison.
type ScenarioComparisonRow struct {
	Scenario             string  `json:"scenario"`
	ForwardReturn        float64 `json:"forward_return"`
	Volatility           float64 `json:"volatility"`
	SuccessRate          float64 `json:"success_rate"`
	TerminalP00Minor     int64   `json:"terminal_p00_minor"`
	TerminalP50Minor     int64   `json:"terminal_p50_minor"`
	TerminalP95Minor     int64   `json:"terminal_p95_minor"`
	RealTerminalP50Minor int64   `json:"real_terminal_p50_minor"`
	MaxDrawdownP50       float64 `json:"max_drawdown_p50"`
}

// ScenarioComparisonView identifies the immutable run every row was derived
// from. Current mutable plan state never participates in this response.
type ScenarioComparisonView struct {
	PlanID         string                  `json:"plan_id"`
	BaseRunID      string                  `json:"base_run_id"`
	BaseInputHash  string                  `json:"base_input_hash"`
	ProfileID      string                  `json:"profile_id"`
	ProfileVersion int                     `json:"profile_version"`
	Seed           string                  `json:"seed"`
	Runs           int                     `json:"runs"`
	BaselineKey    string                  `json:"baseline_key"`
	Scenarios      []ScenarioComparisonRow `json:"scenarios"`
}

// CompareScenarios derives conservative/baseline/optimistic variants from one
// persisted simulation snapshot. It recalibrates only factor drift/volatility,
// retaining the run's exact PSD correlation, factor order, overrides, seed and
// FIRE cash-flow parameters.
func (s *SimulationService) CompareScenarios(
	ctx context.Context, planID, runID string,
) (*ScenarioComparisonView, error) {
	run, err := s.sims.GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrSimulationNotFound) {
			return nil, newErr("simulation_not_found", "simulation run not found", nil)
		}
		return nil, wrapRepo("get frozen run for scenario comparison", err)
	}
	if run.PlanID != planID {
		return nil, newErr("simulation_not_found", "simulation run not found for plan", nil)
	}

	var base simulation.InputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &base); err != nil {
		return nil, scenarioComparisonUnsupported("simulation snapshot cannot be decoded")
	}
	if !base.ScenarioComparisonReady || base.ReturnAssumptionSetID == "" ||
		base.ReturnAssumptionSetVersion < 1 || len(base.AssetFactorRefs) != len(base.Assets) {
		return nil, scenarioComparisonUnsupported("run predates frozen scenario-comparison inputs")
	}
	profile, contentHash, err := s.assumptions.GetWithHash(
		ctx, base.ReturnAssumptionSetID, base.ReturnAssumptionSetVersion,
	)
	if err != nil || contentHash != base.AssumptionProfileContentHash {
		return nil, scenarioComparisonUnsupported("run's immutable assumption profile is unavailable")
	}
	runs := min(base.Parameters.SimulationRuns, scenarioCompareMaxRuns)
	if runs <= 0 {
		return nil, scenarioComparisonUnsupported("run has no valid path count")
	}

	view := &ScenarioComparisonView{
		PlanID: planID, BaseRunID: run.ID, BaseInputHash: run.InputHash,
		ProfileID: profile.ID, ProfileVersion: profile.Version,
		Seed: base.Parameters.Seed, Runs: runs, BaselineKey: assumptions.ScenarioBaseline,
	}
	for _, scenario := range []string{
		assumptions.ScenarioConservative,
		assumptions.ScenarioBaseline,
		assumptions.ScenarioOptimistic,
	} {
		snap, err := deriveScenarioSnapshot(base, profile, scenario)
		if err != nil {
			return nil, scenarioComparisonUnsupported(err.Error())
		}
		result := simulation.Run(&snap, simulation.RunOptions{Runs: runs})
		forwardReturn, volatility, err := portfolioReturnAndVol(snap)
		if err != nil {
			return nil, scenarioComparisonUnsupported(err.Error())
		}
		view.Scenarios = append(view.Scenarios, ScenarioComparisonRow{
			Scenario: scenario, ForwardReturn: forwardReturn, Volatility: volatility,
			SuccessRate:          float64(result.SuccessCount) / float64(runs),
			TerminalP00Minor:     result.Summary.TerminalQuantiles["p00"],
			TerminalP50Minor:     result.Summary.TerminalQuantiles["p50"],
			TerminalP95Minor:     result.Summary.TerminalQuantiles["p95"],
			RealTerminalP50Minor: result.Summary.RealTerminalQuantiles["p50"],
			MaxDrawdownP50:       result.Summary.MaxDrawdownQuantiles["p50"],
		})
	}
	return view, nil
}

func scenarioComparisonUnsupported(message string) error {
	return newErr("scenario_comparison_unsupported", message, nil)
}

func deriveScenarioSnapshot(
	base simulation.InputSnapshot, profile assumptions.Profile, scenario string,
) (simulation.InputSnapshot, error) {
	raw, err := json.Marshal(base)
	if err != nil {
		return simulation.InputSnapshot{}, err
	}
	var out simulation.InputSnapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return simulation.InputSnapshot{}, err
	}
	resolved := resolvedAssumption{
		Profile: profile, Mode: assumptions.SourceBlendedPrior, Scenario: scenario,
	}
	for i := range out.Assets {
		asset := &out.Assets[i]
		cal, err := calibrateAsset(
			resolved, asset.AssetKey, asset.AssetClass, asset.Region, asset.Currency,
			asset.HistoricalAnnualGeometricReturn, asset.HistoricalAnnualVolatility,
			asset.CompleteYearCount, nil,
		)
		if err != nil {
			return simulation.InputSnapshot{}, err
		}
		applyReturnCalibration(asset, cal, scenario)
		if asset.OverrideForwardReturn != nil || asset.OverrideAnnualVolatility != nil {
			applyReturnOverride(asset, repository.PlanReturnOverride{
				ForwardReturn: asset.OverrideForwardReturn, AnnualVolatility: asset.OverrideAnnualVolatility,
				Reason: asset.OverrideReason,
			})
		}
		if asset.FXSnapshotID != "" && asset.Currency != out.BaseCurrency {
			fxCal, err := profile.CalibrateFX(assumptions.FXCalibrationInput{
				FromCurrency: asset.Currency, BaseCurrency: out.BaseCurrency,
				HistoricalAnnualGeometricReturn: asset.FXHistoricalReturn,
				HistoricalAnnualVolatility:      asset.FXHistoricalVolatility,
				CompleteYearCount:               asset.FXCompleteYearCount, Scenario: scenario,
			})
			if err != nil {
				return simulation.InputSnapshot{}, fmt.Errorf("calibrate scenario FX: %w", err)
			}
			asset.FXModeledReturn = fxCal.ForwardAnnualGeometricReturn
			asset.FXAnnualVolatility = fxCal.AnnualVolatilityUsed
			asset.FXPriorReturn = fxCal.PriorAnnualGeometricReturn
			asset.FXHistoricalWeight = fxCal.HistoricalWeight
			asset.FXReturnSource = fxCal.Source
			asset.FXReturnScenario = scenario
			asset.FXReturnWarnings = fxCal.Warnings
		}
	}
	out.ReturnAssumptionMode = assumptions.SourceBlendedPrior
	out.ReturnAssumptionScenario = scenario
	out.DeterministicCashReturn = true
	if err := rebuildScenarioFactorModel(&out); err != nil {
		return simulation.InputSnapshot{}, err
	}
	return out, nil
}

func rebuildScenarioFactorModel(in *simulation.InputSnapshot) error {
	base := in.ScenarioComparisonFactorModel
	if base == nil {
		for _, asset := range in.Assets {
			if !asset.IsCash {
				return errFrozenFactorCorrelationMissing
			}
		}
		in.RandomFactorModel = simulation.FactorModelIndependent
		in.FactorModel = nil
		return nil
	}
	if len(in.AssetFactorRefs) != len(in.Assets) {
		return errFrozenFactorReferencesMissing
	}
	mu := make([]float64, len(base.Factors))
	sigma := make([]float64, len(base.Factors))
	assigned := make([]bool, len(base.Factors))
	for i, asset := range in.Assets {
		ref := in.AssetFactorRefs[i]
		if ref.AssetFactorIndex >= 0 {
			if ref.AssetFactorIndex >= len(mu) {
				return errAssetFactorReferenceRange
			}
			params := simulation.ParamsFromAnnual(asset.ModeledAnnualReturn, asset.AnnualVolatility)
			mu[ref.AssetFactorIndex], sigma[ref.AssetFactorIndex] = params.MonthlyMu, params.MonthlySigma
			assigned[ref.AssetFactorIndex] = true
		}
		if ref.FXFactorIndex >= 0 {
			if ref.FXFactorIndex >= len(mu) {
				return errFXFactorReferenceRange
			}
			params := simulation.ParamsFromAnnual(asset.FXModeledReturn, asset.FXAnnualVolatility)
			mu[ref.FXFactorIndex], sigma[ref.FXFactorIndex] = params.MonthlyMu, params.MonthlySigma
			assigned[ref.FXFactorIndex] = true
		}
	}
	for _, ok := range assigned {
		if !ok {
			return errFrozenFactorInputsMissing
		}
	}
	model, ok := simulation.RebuildFactorModelWithFrozenCorrelation(*base, mu, sigma)
	if !ok {
		return errScenarioCovarianceDecompose
	}
	in.FactorModel = &model
	in.ScenarioComparisonFactorModel = &model
	in.RandomFactorModel = simulation.FactorModelMultivariate
	return nil
}

// portfolioReturnAndVol returns target-weight base-currency forward return and
// annualized log volatility from the rebuilt factor covariance.
func portfolioReturnAndVol(in simulation.InputSnapshot) (float64, float64, error) {
	forwardReturn := 0.0
	exposure := []float64(nil)
	if in.FactorModel != nil {
		exposure = make([]float64, len(in.FactorModel.Factors))
	}
	for i, asset := range in.Assets {
		baseReturn := asset.ModeledAnnualReturn
		if asset.FXSnapshotID != "" && asset.Currency != in.BaseCurrency {
			baseReturn = simulation.CompositeBaseReturn(baseReturn, asset.FXModeledReturn)
		}
		forwardReturn += asset.TargetWeight * baseReturn
		if len(exposure) == 0 {
			continue
		}
		if i >= len(in.AssetFactorRefs) {
			return 0, 0, errFactorExposureMappingMissing
		}
		ref := in.AssetFactorRefs[i]
		if ref.AssetFactorIndex >= 0 {
			exposure[ref.AssetFactorIndex] += asset.TargetWeight
		}
		if ref.FXFactorIndex >= 0 {
			exposure[ref.FXFactorIndex] += asset.TargetWeight
		}
	}
	if in.FactorModel == nil {
		return forwardReturn, 0, nil
	}
	variance := 0.0
	for i := range exposure {
		for j := range exposure {
			variance += exposure[i] * in.FactorModel.Sigma[i][j] * exposure[j]
		}
	}
	return forwardReturn, math.Sqrt(12 * math.Max(variance, 0)), nil
}
