package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// DashboardAllocationBar is one asset-class bar for charts.
type DashboardAllocationBar struct {
	AssetClass    string  `json:"asset_class"`
	TargetWeight  float64 `json:"target_weight"`
	CurrentWeight float64 `json:"current_weight"`
}

// DashboardDeviation is a top deviation holding line.
type DashboardDeviation struct {
	InstrumentName  string  `json:"instrument_name"`
	InstrumentCode  string  `json:"instrument_code"`
	DeviationWeight float64 `json:"deviation_weight"`
	DeviationMinor  int64   `json:"deviation_amount_minor"`
	PortfolioWeight float64 `json:"portfolio_target_weight"`
	CurrentWeight   float64 `json:"current_weight"`
}

// DashboardView aggregates the data required for the dashboard first screen.
type DashboardView struct {
	Plan             PlanDetail                    `json:"plan"`
	ScenarioName     string                        `json:"scenario_name,omitempty"`
	Parameters       repository.PlanParameters     `json:"parameters"`
	WeightChecks     domain.WeightValidationResult `json:"weight_checks"`
	HoldingsSumMinor int64                         `json:"holdings_sum_minor"`
	HoldingsGapMinor int64                         `json:"holdings_gap_minor"`
	RebalanceSummary domain.RebalanceSummary       `json:"rebalance_summary"`
	AllocationBars   []DashboardAllocationBar      `json:"allocation_bars"`
	TopDeviations    []DashboardDeviation          `json:"top_deviations"`
	DataWarnings     []string                      `json:"data_warnings"`
	LatestSimulation *SimulationRunView            `json:"latest_simulation,omitempty"`
	StressTest       *DashboardAnalysisSummary     `json:"stress_test,omitempty"`
	SensitivityTest  *DashboardAnalysisSummary     `json:"sensitivity_test,omitempty"`
}

// DashboardAnalysisSummary holds stress/sensitivity summary for the dashboard.
type DashboardAnalysisSummary struct {
	Available         bool            `json:"available"`
	JobID             string          `json:"job_id,omitempty"`
	ResultStale       bool            `json:"result_stale"`
	BaselineSuccess   float64         `json:"baseline_success_probability,omitempty"`
	WorstScenarioID   string          `json:"worst_scenario_id,omitempty"`
	WorstScenarioName string          `json:"worst_scenario_name,omitempty"`
	TopParameters     []string        `json:"top_parameters,omitempty"`
	Result            json.RawMessage `json:"result_json,omitempty"`
	Message           string          `json:"message,omitempty"`
}

// DashboardService builds the dashboard aggregate response.
type DashboardService struct {
	plans       *repository.PlanRepo
	params      *repository.ParametersRepo
	alloc       *repository.AllocationRepo
	scenario    *repository.ScenarioRepo
	holdings    *repository.HoldingsRepo
	instRepo    *repository.InstrumentRepo
	sims        *repository.SimulationRepo
	analysis    *repository.AnalysisRepo
	hash        *ConfigHashService
	targets     *TargetService
	rebalance   *RebalanceService
	simulations *SimulationService
	stress      *StressService
	sensitivity *SensitivityService
}

func NewDashboardService(
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	scenario *repository.ScenarioRepo,
	holdings *repository.HoldingsRepo,
	instRepo *repository.InstrumentRepo,
	sims *repository.SimulationRepo,
	analysis *repository.AnalysisRepo,
	hash *ConfigHashService,
	targets *TargetService,
	rebalance *RebalanceService,
	simulations *SimulationService,
	stress *StressService,
	sensitivity *SensitivityService,
) *DashboardService {
	return &DashboardService{
		plans: plans, params: params, alloc: alloc, scenario: scenario,
		holdings: holdings, instRepo: instRepo, sims: sims, analysis: analysis, hash: hash,
		targets: targets, rebalance: rebalance, simulations: simulations,
		stress: stress, sensitivity: sensitivity,
	}
}

func (s *DashboardService) Get(ctx context.Context, planID string) (DashboardView, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return DashboardView{}, newErr("plan_not_found", "plan not found", nil)
		}
		return DashboardView{}, err
	}
	configHash, err := s.hash.Compute(ctx, planID)
	if err != nil {
		return DashboardView{}, err
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return DashboardView{}, err
	}

	var scenarioName string
	if params.SelectedScenarioID != nil && *params.SelectedScenarioID != "" {
		if scn, err := s.scenario.GetByID(ctx, *params.SelectedScenarioID); err == nil {
			scenarioName = scn.Name
		}
	}

	targets, err := s.targets.GetTargets(ctx, planID)
	if err != nil {
		return DashboardView{}, err
	}

	reb, err := s.rebalance.GetRebalance(ctx, planID, domain.RebalanceModeFull, 0)
	if err != nil {
		return DashboardView{}, err
	}

	var holdingsSum int64
	holds, _ := s.holdings.ListByPlan(ctx, planID)
	for _, h := range holds {
		if h.Enabled {
			holdingsSum += h.CurrentAmountMinor
		}
	}

	bars := buildAllocationBars(targets)
	topDev := topDeviations(targets.Holdings, holds, 5)
	warnings := collectDataWarnings(ctx, s.instRepo, holds)

	var latest *SimulationRunView
	runs, err := s.sims.ListByPlan(ctx, planID, 1)
	if err == nil && len(runs) > 0 && runs[0].SuccessCount+runs[0].FailureCount > 0 {
		view, err := s.simulations.GetRun(ctx, runs[0].ID)
		if err == nil {
			latest = &view
		}
	}

	return DashboardView{
		Plan:             PlanDetail{Plan: plan, ConfigHash: configHash},
		ScenarioName:     scenarioName,
		Parameters:       params,
		WeightChecks:     targets.WeightChecks,
		HoldingsSumMinor: holdingsSum,
		HoldingsGapMinor: params.TotalAssetsMinor - holdingsSum,
		RebalanceSummary: reb.Summary,
		AllocationBars:   bars,
		TopDeviations:    topDev,
		DataWarnings:     warnings,
		LatestSimulation: latest,
		StressTest:       s.stressSummary(ctx, planID),
		SensitivityTest:  s.sensitivitySummary(ctx, planID),
	}, nil
}

func (s *DashboardService) stressSummary(ctx context.Context, planID string) *DashboardAnalysisSummary {
	recs, err := s.analysis.ListByPlan(ctx, planID, repository.AnalysisTypeStress, 1)
	if err != nil || len(recs) == 0 || isPendingResult(recs[0].ResultJSON) {
		return &DashboardAnalysisSummary{Available: false, Message: "尚未运行压力测试"}
	}
	view, err := s.stress.GetByJobID(ctx, recs[0].JobID)
	if err != nil {
		return &DashboardAnalysisSummary{Available: false, Message: "尚未运行压力测试"}
	}
	sum := &DashboardAnalysisSummary{
		Available: true, JobID: view.JobID, ResultStale: view.ResultStale, Result: view.Result,
	}
	var report struct {
		BaselineSuccessProbability float64 `json:"baseline_success_probability"`
		WorstScenarioID            string  `json:"worst_scenario_id"`
		Scenarios                  []struct {
			ScenarioID         string  `json:"scenario_id"`
			ScenarioName       string  `json:"scenario_name"`
			SuccessProbability float64 `json:"success_probability"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(view.Result, &report); err == nil {
		sum.BaselineSuccess = report.BaselineSuccessProbability
		sum.WorstScenarioID = report.WorstScenarioID
		for _, sc := range report.Scenarios {
			if sc.ScenarioID == report.WorstScenarioID {
				sum.WorstScenarioName = sc.ScenarioName
				break
			}
		}
	}
	return sum
}

func (s *DashboardService) sensitivitySummary(ctx context.Context, planID string) *DashboardAnalysisSummary {
	recs, err := s.analysis.ListByPlan(ctx, planID, repository.AnalysisTypeSensitivity, 1)
	if err != nil || len(recs) == 0 || isPendingResult(recs[0].ResultJSON) {
		return &DashboardAnalysisSummary{Available: false, Message: "尚未运行敏感性测试"}
	}
	view, err := s.sensitivity.GetByJobID(ctx, recs[0].JobID)
	if err != nil {
		return &DashboardAnalysisSummary{Available: false, Message: "尚未运行敏感性测试"}
	}
	sum := &DashboardAnalysisSummary{
		Available: true, JobID: view.JobID, ResultStale: view.ResultStale, Result: view.Result,
	}
	var report struct {
		BaselineSuccessProbability float64 `json:"baseline_success_probability"`
		Tornado                    []struct {
			ParameterName string `json:"parameter_name"`
		} `json:"tornado"`
	}
	if err := json.Unmarshal(view.Result, &report); err == nil {
		sum.BaselineSuccess = report.BaselineSuccessProbability
		for i := 0; i < len(report.Tornado) && i < 3; i++ {
			sum.TopParameters = append(sum.TopParameters, report.Tornado[i].ParameterName)
		}
	}
	return sum
}

func buildAllocationBars(targets TargetView) []DashboardAllocationBar {
	type agg struct {
		target, current float64
	}
	byClass := map[string]*agg{}
	for _, line := range targets.Holdings {
		if !line.Enabled {
			continue
		}
		a := byClass[line.AssetClass]
		if a == nil {
			a = &agg{}
			byClass[line.AssetClass] = a
		}
		a.target += line.PortfolioTargetWeight
		a.current += line.CurrentWeight
	}
	out := make([]DashboardAllocationBar, 0, len(byClass))
	for ac, a := range byClass {
		out = append(out, DashboardAllocationBar{
			AssetClass: ac, TargetWeight: a.target, CurrentWeight: a.current,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssetClass < out[j].AssetClass })
	return out
}

func topDeviations(lines []domain.HoldingTargetLine, holds []repository.PlanHolding, n int) []DashboardDeviation {
	nameByHolding := map[string]repository.PlanHolding{}
	for _, h := range holds {
		nameByHolding[h.ID] = h
	}
	type row struct {
		line domain.HoldingTargetLine
		abs  float64
	}
	var rows []row
	for _, l := range lines {
		if !l.Enabled {
			continue
		}
		rows = append(rows, row{line: l, abs: absFloat(l.DeviationWeight)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].abs > rows[j].abs })
	if len(rows) > n {
		rows = rows[:n]
	}
	out := make([]DashboardDeviation, len(rows))
	for i, r := range rows {
		meta := nameByHolding[r.line.HoldingID]
		out[i] = DashboardDeviation{
			InstrumentName:  meta.InstrumentName,
			InstrumentCode:  meta.InstrumentCode,
			DeviationWeight: r.line.DeviationWeight,
			DeviationMinor:  r.line.DeviationAmountMinor,
			PortfolioWeight: r.line.PortfolioTargetWeight,
			CurrentWeight:   r.line.CurrentWeight,
		}
	}
	return out
}

func collectDataWarnings(ctx context.Context, instRepo *repository.InstrumentRepo, holds []repository.PlanHolding) []string {
	var warnings []string
	seen := map[string]bool{}
	for _, h := range holds {
		if !h.Enabled {
			continue
		}
		inst, err := instRepo.GetByID(ctx, h.InstrumentID)
		if err != nil {
			continue
		}
		key := inst.QualityStatus + inst.Status
		if seen[key] {
			continue
		}
		switch inst.QualityStatus {
		case "insufficient_history", "classification_failed", "data_anomaly", "pending_sync":
			warnings = append(warnings, inst.Name+"（"+inst.Code+"）：数据状态 "+inst.QualityStatus)
			seen[key] = true
		}
		if inst.DataStale && inst.StaleWarning != "" {
			warnings = append(warnings, inst.StaleWarning)
		}
	}
	return warnings
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ParseSimulationSummary unmarshals summary_json for dashboard charts.
func ParseSimulationSummary(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
