package service

import (
	"context"
	"errors"
	"strconv"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// scenarioCompareMaxRuns caps the synchronous per-scenario path count so the
// on-demand comparison stays responsive even when the plan's configured run
// count is very large. The comparison is a directional preview, not a stored
// run, so a reduced (but shared) sample is acceptable.
const scenarioCompareMaxRuns = 3000

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

// ScenarioComparisonView compares the same frozen plan input under the three
// global scenarios with one shared seed (td/061 §3.6 / §5.4.6). Because only the
// scenario differs, the rows isolate the effect of the return/volatility shift.
type ScenarioComparisonView struct {
	PlanID         string                  `json:"plan_id"`
	ProfileID      string                  `json:"profile_id"`
	ProfileVersion int                     `json:"profile_version"`
	Seed           string                  `json:"seed"`
	Runs           int                     `json:"runs"`
	BaselineKey    string                  `json:"baseline_key"`
	Scenarios      []ScenarioComparisonRow `json:"scenarios"`
}

// CompareScenarios builds the plan's frozen input once per scenario, sharing a
// single seed, and runs the engine synchronously for each. All three runs use
// blended_prior so the scenario shift is meaningful regardless of the plan's
// own assumption mode.
func (s *SimulationService) CompareScenarios(ctx context.Context, planID string) (*ScenarioComparisonView, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan for scenario comparison", err)
	}

	seed, err := randomSeed()
	if err != nil {
		return nil, err
	}
	seedStr := strconv.FormatInt(seed, 10)
	req := CreateSimulationRequest{PlanID: planID, seedInt: &seed}

	scenarios := []string{
		assumptions.ScenarioConservative,
		assumptions.ScenarioBaseline,
		assumptions.ScenarioOptimistic,
	}

	view := &ScenarioComparisonView{
		PlanID:      planID,
		Seed:        seedStr,
		BaselineKey: assumptions.ScenarioBaseline,
	}
	for _, scenario := range scenarios {
		snap, _, err := s.buildInputSnapshot(ctx, plan, req, scenario)
		if err != nil {
			return nil, err
		}
		runs := snap.Parameters.SimulationRuns
		if runs > scenarioCompareMaxRuns {
			runs = scenarioCompareMaxRuns
		}
		res := simulation.Run(snap, simulation.RunOptions{Runs: runs})

		if view.Runs == 0 {
			view.Runs = runs
			for _, a := range snap.Assets {
				if a.ReturnAssumptionSetID != "" {
					view.ProfileID = a.ReturnAssumptionSetID
					view.ProfileVersion = a.ReturnAssumptionSetVersion
					break
				}
			}
		}
		fwd, vol := weightedReturnAndVol(snap.Assets)
		view.Scenarios = append(view.Scenarios, ScenarioComparisonRow{
			Scenario:             scenario,
			ForwardReturn:        fwd,
			Volatility:           vol,
			SuccessRate:          float64(res.SuccessCount) / float64(runs),
			TerminalP00Minor:     res.Summary.TerminalQuantiles["p00"],
			TerminalP50Minor:     res.Summary.TerminalQuantiles["p50"],
			TerminalP95Minor:     res.Summary.TerminalQuantiles["p95"],
			RealTerminalP50Minor: res.Summary.RealTerminalQuantiles["p50"],
			MaxDrawdownP50:       res.Summary.MaxDrawdownQuantiles["p50"],
		})
	}
	return view, nil
}

// weightedReturnAndVol returns the target-weighted forward return and volatility
// across non-cash assets, the single headline number per scenario.
func weightedReturnAndVol(assets []simulation.SnapshotAsset) (float64, float64) {
	var wSum, rSum, vSum float64
	for _, a := range assets {
		if a.IsCash {
			continue
		}
		wSum += a.TargetWeight
		rSum += a.TargetWeight * a.ModeledAnnualReturn
		vSum += a.TargetWeight * a.AnnualVolatility
	}
	if wSum <= 0 {
		return 0, 0
	}
	return rSum / wSum, vSum / wSum
}
