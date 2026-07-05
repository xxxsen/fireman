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
	AssetClass         string                       `json:"asset_class"`
	TargetWeight       float64                      `json:"target_weight"`
	CurrentWeight      float64                      `json:"current_weight"`
	CurrentAmountMinor int64                        `json:"current_amount_minor"`
	TargetAmountMinor  int64                        `json:"target_amount_minor"`
	Holdings           []DashboardAllocationHolding `json:"holdings"`
}

// DashboardAllocationHolding is one instrument detail line under an asset-class bar.
type DashboardAllocationHolding struct {
	InstrumentName     string  `json:"instrument_name"`
	InstrumentCode     string  `json:"instrument_code"`
	CurrentAmountMinor int64   `json:"current_amount_minor"`
	TargetAmountMinor  int64   `json:"target_amount_minor"`
	CurrentWeight      float64 `json:"current_weight"`
	TargetWeight       float64 `json:"target_weight"`
}

// assetClassDisplayOrder is the fixed business ordering for allocation charts:
// equity, bond, cash, then any unknown class last.
var assetClassDisplayOrder = map[string]int{
	string(domain.AssetClassEquity): 0,
	string(domain.AssetClassBond):   1,
	string(domain.AssetClassCash):   2,
}

func assetClassOrderIndex(ac string) int {
	if idx, ok := assetClassDisplayOrder[ac]; ok {
		return idx
	}
	return len(assetClassDisplayOrder)
}

// DashboardRegionBar is one region allocation bar, used both for the
// full-portfolio region exposure and for the within-asset-class region split.
type DashboardRegionBar struct {
	Region             string                       `json:"region"`
	TargetWeight       float64                      `json:"target_weight"`
	CurrentWeight      float64                      `json:"current_weight"`
	CurrentAmountMinor int64                        `json:"current_amount_minor"`
	TargetAmountMinor  int64                        `json:"target_amount_minor"`
	Holdings           []DashboardAllocationHolding `json:"holdings"`
}

// DashboardAssetClassRegionGroup is the domestic/foreign split inside one asset class.
type DashboardAssetClassRegionGroup struct {
	AssetClass string               `json:"asset_class"`
	Regions    []DashboardRegionBar `json:"regions"`
}

// regionDisplayOrder fixes the region ordering: domestic, then foreign.
var regionDisplayOrder = map[string]int{
	string(domain.RegionDomestic): 0,
	string(domain.RegionForeign):  1,
}

func regionOrderIndex(region string) int {
	if idx, ok := regionDisplayOrder[region]; ok {
		return idx
	}
	return len(regionDisplayOrder)
}

// PlanDashboardSummary is the lightweight plan-list summary.
type PlanDashboardSummary struct {
	RebalanceActionableCount int   `json:"rebalance_actionable_count"`
	HoldingsGapMinor         int64 `json:"holdings_gap_minor"`
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
	Plan                   PlanDetail                       `json:"plan"`
	ScenarioName           string                           `json:"scenario_name,omitempty"`
	Parameters             repository.PlanParameters        `json:"parameters"`
	WeightChecks           domain.WeightValidationResult    `json:"weight_checks"`
	HoldingsSumMinor       int64                            `json:"holdings_sum_minor"`
	InvestedMinor          int64                            `json:"invested_minor"`
	InvestedRatio          float64                          `json:"invested_ratio"`
	HoldingsGapMinor       int64                            `json:"holdings_gap_minor"`
	RebalanceSummary       domain.RebalanceSummary          `json:"rebalance_summary"`
	ActiveExecution        *ActiveRebalanceExecution        `json:"active_rebalance_execution,omitempty"`
	AllocationBars         []DashboardAllocationBar         `json:"allocation_bars"`
	RegionBars             []DashboardRegionBar             `json:"region_bars"`
	AssetClassRegionGroups []DashboardAssetClassRegionGroup `json:"asset_class_region_groups"`
	TopDeviations          []DashboardDeviation             `json:"top_deviations"`
	DataWarnings           []string                         `json:"data_warnings"`
	LatestSimulation       *SimulationRunView               `json:"latest_simulation,omitempty"`
	StressTest             *DashboardAnalysisSummary        `json:"stress_test,omitempty"`
	SensitivityTest        *DashboardAnalysisSummary        `json:"sensitivity_test,omitempty"`
}

// ActiveRebalanceExecution is the in-progress execution summary for dashboard navigation.
type ActiveRebalanceExecution struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	CashPoolMinor int64  `json:"cash_pool_minor"`
	DoneLineCount int    `json:"done_line_count"`
	LineCount     int    `json:"line_count"`
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
	sims        *repository.SimulationRepo
	analysis    *repository.AnalysisRepo
	hash        *ConfigHashService
	targets     *TargetService
	rebalance   *RebalanceService
	simulations *SimulationService
	stress      *StressService
	sensitivity *SensitivityService
	executions  *repository.RebalanceExecutionRepo
}

func NewDashboardService(
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	scenario *repository.ScenarioRepo,
	holdings *repository.HoldingsRepo,
	sims *repository.SimulationRepo,
	analysis *repository.AnalysisRepo,
	hash *ConfigHashService,
	targets *TargetService,
	rebalance *RebalanceService,
	simulations *SimulationService,
	stress *StressService,
	sensitivity *SensitivityService,
	executions *repository.RebalanceExecutionRepo,
) *DashboardService {
	return &DashboardService{
		plans: plans, params: params, alloc: alloc, scenario: scenario,
		holdings: holdings, sims: sims, analysis: analysis, hash: hash,
		targets: targets, rebalance: rebalance, simulations: simulations,
		stress: stress, sensitivity: sensitivity, executions: executions,
	}
}

func (s *DashboardService) Get(ctx context.Context, planID string) (DashboardView, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return DashboardView{}, newErr("plan_not_found", "plan not found", nil)
		}
		return DashboardView{}, wrapRepo("load plan", err)
	}
	configHash, err := s.hash.Compute(ctx, planID)
	if err != nil {
		return DashboardView{}, wrapRepo("compute config hash", err)
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return DashboardView{}, wrapRepo("load plan parameters", err)
	}

	scenarioName := s.loadScenarioName(ctx, params.SelectedScenarioID)

	targets, err := s.targets.GetTargets(ctx, planID)
	if err != nil {
		return DashboardView{}, err
	}

	reb, err := s.rebalance.GetRebalance(ctx, planID, domain.RebalanceModeFull, 0)
	if err != nil {
		return DashboardView{}, err
	}

	var holdingsSum, investedMinor int64
	holds, _ := s.holdings.ListByPlan(ctx, planID)
	for _, h := range holds {
		if h.Enabled {
			holdingsSum += h.CurrentAmountMinor
			if h.AssetClass != domain.AssetClassCash {
				investedMinor += h.CurrentAmountMinor
			}
		}
	}
	investedRatio := computeInvestedRatio(investedMinor, params.TotalAssetsMinor)

	activeExecution := s.loadActiveExecution(ctx, planID)

	bars := buildAllocationBars(targets)
	regionBars := buildRegionBars(targets)
	regionGroups := buildAssetClassRegionGroups(targets)
	topDev := topDeviations(targets.Holdings, holds, 5)
	warnings := collectDataWarnings(holds)

	latest := s.loadLatestSimulationRun(ctx, planID)

	return DashboardView{
		Plan:                   PlanDetail{Plan: plan, ConfigHash: configHash},
		ScenarioName:           scenarioName,
		Parameters:             params,
		WeightChecks:           targets.WeightChecks,
		HoldingsSumMinor:       holdingsSum,
		InvestedMinor:          investedMinor,
		InvestedRatio:          investedRatio,
		HoldingsGapMinor:       holdingsSum - params.TotalAssetsMinor,
		RebalanceSummary:       reb.Summary,
		ActiveExecution:        activeExecution,
		AllocationBars:         bars,
		RegionBars:             regionBars,
		AssetClassRegionGroups: regionGroups,
		TopDeviations:          topDev,
		DataWarnings:           warnings,
		LatestSimulation:       latest,
		StressTest:             s.stressSummary(ctx, planID),
		SensitivityTest:        s.sensitivitySummary(ctx, planID),
	}, nil
}

func (s *DashboardService) GetPlanSummary(ctx context.Context, planID string) (PlanDashboardSummary, error) {
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return PlanDashboardSummary{}, wrapRepo("load plan parameters", err)
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return PlanDashboardSummary{}, wrapRepo("list plan holdings", err)
	}
	var holdingsSum int64
	for _, holding := range holds {
		if holding.Enabled {
			holdingsSum += holding.CurrentAmountMinor
		}
	}
	rebalance, err := s.rebalance.GetRebalance(ctx, planID, domain.RebalanceModeFull, 0)
	if err != nil {
		return PlanDashboardSummary{}, err
	}
	return PlanDashboardSummary{
		RebalanceActionableCount: rebalance.Summary.ActionableCount,
		HoldingsGapMinor:         holdingsSum - params.TotalAssetsMinor,
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
	type barAgg struct {
		bar      DashboardAllocationBar
		holdings []DashboardAllocationHolding
	}
	byClass := map[string]*barAgg{}
	order := []string{}
	for _, line := range targets.Holdings {
		if !line.Enabled {
			continue
		}
		agg := byClass[line.AssetClass]
		if agg == nil {
			agg = &barAgg{bar: DashboardAllocationBar{AssetClass: line.AssetClass}}
			byClass[line.AssetClass] = agg
			order = append(order, line.AssetClass)
		}
		agg.bar.TargetWeight += line.PortfolioTargetWeight
		agg.bar.CurrentWeight += line.StructuralCurrentWeight
		agg.bar.CurrentAmountMinor += line.CurrentAmountMinor
		agg.bar.TargetAmountMinor += line.TargetAmountMinor
		agg.holdings = append(agg.holdings, DashboardAllocationHolding{
			InstrumentName:     line.InstrumentName,
			InstrumentCode:     line.InstrumentCode,
			CurrentAmountMinor: line.CurrentAmountMinor,
			TargetAmountMinor:  line.TargetAmountMinor,
			CurrentWeight:      line.StructuralCurrentWeight,
			TargetWeight:       line.PortfolioTargetWeight,
		})
	}

	out := make([]DashboardAllocationBar, 0, len(order))
	for _, ac := range order {
		agg := byClass[ac]
		// Largest holdings first so the front-end "top N" truncation stays meaningful.
		sort.SliceStable(agg.holdings, func(i, j int) bool {
			ai := agg.holdings[i].TargetAmountMinor + agg.holdings[i].CurrentAmountMinor
			aj := agg.holdings[j].TargetAmountMinor + agg.holdings[j].CurrentAmountMinor
			return ai > aj
		})
		agg.bar.Holdings = agg.holdings
		out = append(out, agg.bar)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return assetClassOrderIndex(out[i].AssetClass) < assetClassOrderIndex(out[j].AssetClass)
	})
	return out
}

func holdingDetail(line domain.HoldingTargetLine) DashboardAllocationHolding {
	return DashboardAllocationHolding{
		InstrumentName:     line.InstrumentName,
		InstrumentCode:     line.InstrumentCode,
		CurrentAmountMinor: line.CurrentAmountMinor,
		TargetAmountMinor:  line.TargetAmountMinor,
		CurrentWeight:      line.StructuralCurrentWeight,
		TargetWeight:       line.PortfolioTargetWeight,
	}
}

func sortHoldingsByAmount(holdings []DashboardAllocationHolding) {
	// Largest holdings first so the front-end "top N" truncation stays meaningful.
	sort.SliceStable(holdings, func(i, j int) bool {
		ai := holdings[i].TargetAmountMinor + holdings[i].CurrentAmountMinor
		aj := holdings[j].TargetAmountMinor + holdings[j].CurrentAmountMinor
		return ai > aj
	})
}

// buildRegionBars aggregates full-portfolio domestic/foreign exposure: target uses
// portfolio target weights, current uses structural current weights.
func buildRegionBars(targets TargetView) []DashboardRegionBar {
	type regionAgg struct {
		bar      DashboardRegionBar
		holdings []DashboardAllocationHolding
	}
	byRegion := map[string]*regionAgg{}
	order := []string{}
	for _, line := range targets.Holdings {
		if !line.Enabled {
			continue
		}
		agg := byRegion[line.Region]
		if agg == nil {
			agg = &regionAgg{bar: DashboardRegionBar{Region: line.Region}}
			byRegion[line.Region] = agg
			order = append(order, line.Region)
		}
		agg.bar.TargetWeight += line.PortfolioTargetWeight
		agg.bar.CurrentWeight += line.StructuralCurrentWeight
		agg.bar.CurrentAmountMinor += line.CurrentAmountMinor
		agg.bar.TargetAmountMinor += line.TargetAmountMinor
		agg.holdings = append(agg.holdings, holdingDetail(line))
	}
	out := make([]DashboardRegionBar, 0, len(order))
	for _, region := range order {
		agg := byRegion[region]
		sortHoldingsByAmount(agg.holdings)
		agg.bar.Holdings = agg.holdings
		out = append(out, agg.bar)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return regionOrderIndex(out[i].Region) < regionOrderIndex(out[j].Region)
	})
	return out
}

// buildAssetClassRegionGroups produces the within-asset-class domestic/foreign
// split. Target weights use RegionTarget.weight_within_class; current weights use
// the holding's current amount over that asset class's current total.
func buildAssetClassRegionGroups(targets TargetView) []DashboardAssetClassRegionGroup {
	type regionKey struct{ class, region string }
	currentByCR := map[regionKey]int64{}
	targetAmtByCR := map[regionKey]int64{}
	holdingsByCR := map[regionKey][]DashboardAllocationHolding{}
	classCurrentTotal := map[string]int64{}
	for _, line := range targets.Holdings {
		if !line.Enabled {
			continue
		}
		k := regionKey{line.AssetClass, line.Region}
		currentByCR[k] += line.CurrentAmountMinor
		targetAmtByCR[k] += line.TargetAmountMinor
		classCurrentTotal[line.AssetClass] += line.CurrentAmountMinor
		holdingsByCR[k] = append(holdingsByCR[k], holdingDetail(line))
	}

	classWeight := map[string]float64{}
	for _, ac := range targets.AssetClass {
		classWeight[ac.AssetClass] = ac.Weight
	}

	groups := map[string]*DashboardAssetClassRegionGroup{}
	classOrder := []string{}
	for _, rt := range targets.RegionTargets {
		// Only surface asset classes the plan actually allocates to or holds.
		if classWeight[rt.AssetClass] <= 0 && classCurrentTotal[rt.AssetClass] == 0 {
			continue
		}
		g := groups[rt.AssetClass]
		if g == nil {
			g = &DashboardAssetClassRegionGroup{AssetClass: rt.AssetClass}
			groups[rt.AssetClass] = g
			classOrder = append(classOrder, rt.AssetClass)
		}
		k := regionKey{rt.AssetClass, rt.Region}
		var currentWeight float64
		if total := classCurrentTotal[rt.AssetClass]; total > 0 {
			currentWeight = float64(currentByCR[k]) / float64(total)
		}
		holdings := holdingsByCR[k]
		sortHoldingsByAmount(holdings)
		g.Regions = append(g.Regions, DashboardRegionBar{
			Region:             rt.Region,
			TargetWeight:       rt.WeightWithinClass,
			CurrentWeight:      currentWeight,
			CurrentAmountMinor: currentByCR[k],
			TargetAmountMinor:  targetAmtByCR[k],
			Holdings:           holdings,
		})
	}

	out := make([]DashboardAssetClassRegionGroup, 0, len(classOrder))
	for _, ac := range classOrder {
		g := groups[ac]
		sort.SliceStable(g.Regions, func(i, j int) bool {
			return regionOrderIndex(g.Regions[i].Region) < regionOrderIndex(g.Regions[j].Region)
		})
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return assetClassOrderIndex(out[i].AssetClass) < assetClassOrderIndex(out[j].AssetClass)
	})
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
		absAmt := absFloat(float64(l.StructuralGapAmountMinor))
		if absAmt <= 100 {
			continue
		}
		rows = append(rows, row{line: l, abs: absAmt})
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
			DeviationWeight: r.line.StructuralGapWeight,
			DeviationMinor:  r.line.StructuralGapAmountMinor,
			PortfolioWeight: r.line.PortfolioTargetWeight,
			CurrentWeight:   r.line.StructuralCurrentWeight,
		}
	}
	return out
}

// collectDataWarnings surfaces holdings whose market asset history has not
// been synced yet (empty simulation snapshot): simulation is blocked until a
// history sync makes their snapshot buildable.
func collectDataWarnings(holds []repository.PlanHolding) []string {
	var warnings []string
	for _, h := range holds {
		if !h.Enabled || h.SimulationSnapshotID != "" {
			continue
		}
		name := h.InstrumentName
		if name == "" {
			name = h.AssetKey
		}
		warnings = append(warnings, name+"（"+h.InstrumentCode+"）：历史数据未同步，模拟前需要同步")
	}
	return warnings
}

func (s *DashboardService) loadActiveExecution(ctx context.Context, planID string) *ActiveRebalanceExecution {
	if s.executions == nil {
		return nil
	}
	active, err := s.executions.GetActiveByPlan(ctx, planID)
	if err != nil {
		return nil
	}
	summaries, err := s.executions.ListByPlan(ctx, planID)
	if err != nil {
		return &ActiveRebalanceExecution{
			ID: active.ID, Status: active.Status, CashPoolMinor: active.CashPoolMinor,
		}
	}
	for _, summary := range summaries {
		if summary.ID == active.ID {
			return &ActiveRebalanceExecution{
				ID: summary.ID, Status: summary.Status, CashPoolMinor: summary.CashPoolMinor,
				DoneLineCount: summary.DoneLineCount, LineCount: summary.LineCount,
			}
		}
	}
	return &ActiveRebalanceExecution{
		ID: active.ID, Status: active.Status, CashPoolMinor: active.CashPoolMinor,
	}
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
