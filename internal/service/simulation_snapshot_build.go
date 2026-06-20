package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

func applyCreateSimOverrides(params *repository.PlanParameters, req CreateSimulationRequest) error {
	if req.Runs != nil {
		params.SimulationRuns = *req.Runs
	}
	if req.seedInt != nil {
		params.Seed = req.seedInt
		return nil
	}
	if req.Seed == nil {
		return nil
	}
	parsed, err := ParseSeedString(req.Seed)
	if err != nil && !errors.Is(err, errSeedNotProvided) {
		return newErr("parameters_invalid", err.Error(), nil)
	}
	params.Seed = parsed
	return nil
}

func validateSnapshotHoldings(holds []repository.PlanHolding, totalAssetsMinor int64) error {
	enabledSum := int64(0)
	enabledCount := 0
	for _, h := range holds {
		if h.Enabled {
			enabledSum += h.CurrentAmountMinor
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return newErr("simulation_input_invalid", "at least one enabled holding is required", nil)
	}
	if abs64(enabledSum-totalAssetsMinor) > 100 {
		return newErr("simulation_input_invalid", "total assets must match enabled holdings within 1 CNY",
			map[string]any{
				"total_assets_minor": totalAssetsMinor, "holdings_sum_minor": enabledSum,
			})
	}
	return nil
}

func (s *SimulationService) buildSnapshotAssets(
	ctx context.Context,
	plan repository.Plan,
	lines []domain.HoldingTargetLine,
	resolved resolvedAssumption,
	customByInstrument map[string]float64,
	overrides map[string]repository.PlanReturnOverride,
) ([]simulation.SnapshotAsset, error) {
	fxCache := make(map[string]marketdata.SnapshotMetrics)
	assets := make([]simulation.SnapshotAsset, 0, len(lines))
	for _, line := range lines {
		if !line.Enabled {
			continue
		}
		sa, err := s.buildOneSnapshotAsset(ctx, plan, line, fxCache, resolved, customByInstrument, overrides)
		if err != nil {
			return nil, err
		}
		assets = append(assets, sa)
	}
	return assets, nil
}

func (s *SimulationService) buildOneSnapshotAsset(
	ctx context.Context,
	plan repository.Plan,
	line domain.HoldingTargetLine,
	fxCache map[string]marketdata.SnapshotMetrics,
	resolved resolvedAssumption,
	customByInstrument map[string]float64,
	overrides map[string]repository.PlanReturnOverride,
) (simulation.SnapshotAsset, error) {
	snap, err := s.snapRepo.GetByID(ctx, line.SimulationSnapshotID)
	if err != nil {
		return simulation.SnapshotAsset{}, newErr("snapshot_not_found", "simulation snapshot missing for holding",
			map[string]any{"holding_id": line.HoldingID})
	}
	if snap.SourceMode != "system_cash" {
		if err := marketdata.ValidateSimulationSnapshot(snap); err != nil {
			return simulation.SnapshotAsset{}, newErr("instrument_insufficient_history",
				"holding snapshot does not meet simulation eligibility",
				map[string]any{
					"holding_id":           line.HoldingID,
					"complete_year_count":  snap.CompleteYearCount,
					"monthly_return_count": snap.MonthlyReturnCount,
					"quality_status":       snap.QualityStatus,
					"metrics_version":      snap.MetricsVersion,
					"volatility_method":    snap.VolatilityMethod,
					"validation_error":     err.Error(),
				})
		}
	}
	years := toSimSnapshotYears(snap.Years)
	inst, err := s.holdings.GetInstrument(ctx, line.InstrumentID)
	currency := plan.BaseCurrency
	instrumentName := ""
	instrumentCode := ""
	if err == nil {
		currency = inst.Currency
		instrumentName = inst.Name
		instrumentCode = inst.Code
	}
	isCash := snap.SourceMode == "system_cash" || line.AssetClass == domain.AssetClassCash
	region := line.Region
	if isCash {
		region = domain.RegionDomestic
	}
	cal, err := calibrateAsset(resolved, line.InstrumentID, line.AssetClass, region, currency,
		snap.ModeledAnnualReturn, snap.AnnualVolatility, snap.CompleteYearCount, customByInstrument)
	if err != nil {
		return simulation.SnapshotAsset{}, newErr("assumption_unavailable",
			"no forward-return assumption covers this holding; configure it in 模拟假设",
			map[string]any{
				"holding_id": line.HoldingID, "instrument_id": line.InstrumentID,
				"asset_class": line.AssetClass, "region": region, "currency": currency,
				"mode": resolved.Mode, "error": err.Error(),
			})
	}
	sa := simulation.SnapshotAsset{
		HoldingID: line.HoldingID, InstrumentID: line.InstrumentID,
		InstrumentName: instrumentName, InstrumentCode: instrumentCode,
		SnapshotID: line.SimulationSnapshotID,
		Currency:   currency, AssetClass: line.AssetClass, Region: region, IsCash: isCash,
		InitialMinor: line.CurrentAmountMinor, TargetWeight: line.PortfolioTargetWeight,
		MaxDrawdown: snap.MaxDrawdown, FeeTreatment: snap.FeeTreatment, ExpenseRatio: snap.ExpenseRatio,
		SourceHash: snap.SourceHash, Years: years,
		CompleteYearCount: snap.CompleteYearCount, MonthlyReturnCount: snap.MonthlyReturnCount,
		HistoryDepth: snap.HistoryDepth, MetricsVersion: snap.MetricsVersion,
		DataWarnings: parseSnapshotWarnings(snap.WarningsJSON),
	}
	applyReturnCalibration(&sa, cal, resolved.Scenario)
	if !isCash {
		if ov, ok := overrides[line.InstrumentID]; ok {
			applyReturnOverride(&sa, ov)
		}
		if months, err := s.snapRepo.ListSnapshotMonths(ctx, line.SimulationSnapshotID); err == nil {
			sa.Months = monthSeriesToMap(months)
		}
	}
	if currency == plan.BaseCurrency {
		return sa, nil
	}
	return s.enrichSnapshotAssetFX(ctx, plan, line, sa, currency, fxCache)
}

func toSimSnapshotYears(years []repository.SnapshotYear) []simulation.SnapshotYear {
	out := make([]simulation.SnapshotYear, len(years))
	for i, y := range years {
		out[i] = simulation.SnapshotYear{
			Year: y.Year, AnnualReturn: y.AnnualReturn, StartDate: y.StartDate,
			EndDate: y.EndDate, Observations: y.Observations,
		}
	}
	return out
}

// monthSeriesToMap keys a frozen monthly series by "YYYY-MM" for correlation use.
func monthSeriesToMap(months []repository.SnapshotMonth) map[string]float64 {
	if len(months) == 0 {
		return nil
	}
	out := make(map[string]float64, len(months))
	for _, m := range months {
		out[monthKey(m.Year, m.Month)] = m.LogReturn
	}
	return out
}

func monthKey(year, month int) string {
	return fmt.Sprintf("%04d-%02d", year, month)
}

// applyReturnCalibration freezes the td/061 forward-return audit fields onto the
// asset. ModeledAnnualReturn (the value the engine consumes) is always the
// calibrated forward geometric return, kept identical to the explicit
// ForwardAnnualGeometricReturn field.
func applyReturnCalibration(sa *simulation.SnapshotAsset, cal assumptions.CalibrationResult, scenario string) {
	sa.ModeledAnnualReturn = cal.ForwardAnnualGeometricReturn
	sa.AnnualVolatility = cal.AnnualVolatilityUsed
	sa.HistoricalAnnualGeometricReturn = cal.HistoricalAnnualGeometricReturn
	sa.ForwardAnnualGeometricReturn = cal.ForwardAnnualGeometricReturn
	sa.ForwardLogReturn = cal.ForwardLogReturn
	sa.AnnualVolatilityUsed = cal.AnnualVolatilityUsed
	sa.ReturnAssumptionSource = cal.Source
	sa.ReturnAssumptionSetID = cal.AssumptionSetID
	sa.ReturnAssumptionSetVersion = cal.AssumptionSetVersion
	sa.ReturnAssumptionScenario = scenario
	sa.ReturnSampleYears = cal.SampleYears
	sa.ReturnHistoricalWeight = cal.HistoricalWeight
	sa.ReturnWarnings = cal.Warnings
}

// applyReturnOverride applies an asset-level plan-specific override on top of the
// calibrated forward values (td/061 §4.1.5). It only touches the forward
// geometric return and/or volatility — historical facts, correlation and the FX
// factor are untouched. The override is recorded in the audit (source +
// warning) so the run's assumption view explains why the number differs from the
// global profile.
func applyReturnOverride(sa *simulation.SnapshotAsset, ov repository.PlanReturnOverride) {
	if ov.ForwardReturn != nil {
		r := *ov.ForwardReturn
		sa.ForwardAnnualGeometricReturn = r
		sa.ModeledAnnualReturn = r
		sa.ForwardLogReturn = math.Log1p(r)
	}
	if ov.AnnualVolatility != nil {
		v := *ov.AnnualVolatility
		sa.AnnualVolatility = v
		sa.AnnualVolatilityUsed = v
	}
	sa.ReturnAssumptionSource = "plan_override"
	sa.ReturnWarnings = append(sa.ReturnWarnings, "plan_override: "+ov.Reason)
}

func (s *SimulationService) enrichSnapshotAssetFX(
	ctx context.Context,
	plan repository.Plan,
	line domain.HoldingTargetLine,
	sa simulation.SnapshotAsset,
	currency string,
	fxCache map[string]marketdata.SnapshotMetrics,
) (simulation.SnapshotAsset, error) {
	fxMetrics, ok := fxCache[currency]
	if !ok {
		var err error
		fxMetrics, err = s.fx.Metrics(ctx, currency, plan.BaseCurrency, plan.ValuationDate)
		if err != nil {
			return simulation.SnapshotAsset{}, newErr("fx_snapshot_missing", "FX data unavailable for foreign holding",
				map[string]any{
					"holding_id": line.HoldingID, "currency": currency, "error": err.Error(),
				})
		}
		if fxMetrics.CompleteYearCount < 1 || !fxMetrics.SimulationEligible {
			return simulation.SnapshotAsset{}, newErr(
				"fx_insufficient_history",
				"FX snapshot does not meet simulation eligibility",
				map[string]any{
					"holding_id": line.HoldingID, "currency": currency,
					"complete_year_count":  fxMetrics.CompleteYearCount,
					"monthly_return_count": fxMetrics.MonthlyReturnCount,
				},
			)
		}
		fxCache[currency] = fxMetrics
	}
	sa.FXSnapshotID = fxMetrics.SourceHash
	sa.FXModeledReturn = marketdata.MetricFloat(fxMetrics.ModeledAnnualReturn)
	sa.FXAnnualVolatility = marketdata.MetricFloat(fxMetrics.AnnualVolatility)
	sa.FXCompleteYearCount = fxMetrics.CompleteYearCount
	sa.FXMonthlyReturnCount = fxMetrics.MonthlyReturnCount
	sa.FXHistoryDepth = fxMetrics.HistoryDepth
	sa.FXMetricsVersion = fxMetrics.MetricsVersion
	sa.FXDataWarnings = fxMetrics.Warnings
	sa.FXMonths = fxMonthSeriesToMap(fxMetrics.MonthlyReturns)
	return sa, nil
}

func fxMonthSeriesToMap(months []marketdata.MonthlyReturn) map[string]float64 {
	if len(months) == 0 {
		return nil
	}
	out := make(map[string]float64, len(months))
	for _, m := range months {
		out[monthKey(m.Year, m.Month)] = m.LogReturn
	}
	return out
}

func parseSnapshotWarnings(raw string) []string {
	var out []string
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func buildInputSnapshotStruct(
	plan repository.Plan,
	params repository.PlanParameters,
	seed int64,
	configHash string,
	assets []simulation.SnapshotAsset,
	resolved resolvedAssumption,
) *simulation.InputSnapshot {
	in := &simulation.InputSnapshot{
		EngineVersion:     simulation.LegacyEngineVersion,
		PlanID:            plan.ID,
		BaseCurrency:      plan.BaseCurrency,
		RandomFactorModel: simulation.FactorModelIndependent,
		ConfigHash:        configHash,
		Parameters: simulation.SnapshotParameters{
			CurrentAge: params.CurrentAge, RetirementAge: params.RetirementAge, EndAge: params.EndAge,
			TotalAssetsMinor: params.TotalAssetsMinor, AnnualSavingsMinor: params.AnnualSavingsMinor,
			AnnualSavingsGrowthRate: params.AnnualSavingsGrowthRate, AnnualSpendingMinor: params.AnnualSpendingMinor,
			TerminalWealthFloorMinor: params.TerminalWealthFloorMinor,
			InflationMode:            params.InflationMode, FixedInflationRate: params.FixedInflationRate,
			InflationMu: params.InflationMu, InflationPhi: params.InflationPhi, InflationSigma: params.InflationSigma,
			WithdrawalType: params.WithdrawalType, WithdrawalRate: params.WithdrawalRate,
			WithdrawalFloorRatio: params.WithdrawalFloorRatio, WithdrawalCeilingRatio: params.WithdrawalCeilingRatio,
			WithdrawalTaxRate: params.WithdrawalTaxRate, TaxableWithdrawalRatio: params.TaxableWithdrawalRatio,
			RebalanceFrequency: params.RebalanceFrequency, RebalanceThreshold: params.RebalanceThreshold,
			TransactionCostRate: params.TransactionCostRate, SimulationRuns: params.SimulationRuns,
			StudentTDf: params.StudentTDf, Seed: strconv.FormatInt(seed, 10),
		},
		Assets: assets,
	}
	// Forward-looking modes (blended_prior / custom) run the joint, correlated
	// engine; historical_cagr keeps the legacy independent path so migrated plans
	// reproduce their old numbers exactly (td/061 §4.2).
	if resolved.Mode != assumptions.SourceHistoricalCAGR {
		if fm, refs := buildFrozenFactorModel(assets, plan.BaseCurrency, resolved.Profile); fm != nil {
			in.EngineVersion = simulation.EngineVersion
			in.RandomFactorModel = simulation.FactorModelMultivariate
			in.FactorModel = fm
			in.AssetFactorRefs = refs
		}
	}
	in.MarketSnapshotHash = simulation.MarketHashFromAssets(assets)
	return in
}
