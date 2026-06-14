package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

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
) ([]simulation.SnapshotAsset, error) {
	fxCache := make(map[string]marketdata.SnapshotMetrics)
	assets := make([]simulation.SnapshotAsset, 0, len(lines))
	for _, line := range lines {
		if !line.Enabled {
			continue
		}
		sa, err := s.buildOneSnapshotAsset(ctx, plan, line, fxCache)
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
) (simulation.SnapshotAsset, error) {
	snap, err := s.snapRepo.GetByID(ctx, line.SimulationSnapshotID)
	if err != nil {
		return simulation.SnapshotAsset{}, newErr("snapshot_not_found", "simulation snapshot missing for holding",
			map[string]any{"holding_id": line.HoldingID})
	}
	if snap.SourceMode != "system_cash" && snap.MonthlyReturnCount < 12 {
		return simulation.SnapshotAsset{}, newErr("instrument_insufficient_history",
			"holding snapshot does not meet simulation eligibility",
			map[string]any{
				"holding_id": line.HoldingID,
				"complete_year_count": snap.CompleteYearCount,
				"monthly_return_count": snap.MonthlyReturnCount,
			})
	}
	years := make([]simulation.SnapshotYear, len(snap.Years))
	for i, y := range snap.Years {
		years[i] = simulation.SnapshotYear{
			Year: y.Year, AnnualReturn: y.AnnualReturn, StartDate: y.StartDate,
			EndDate: y.EndDate, Observations: y.Observations,
		}
	}
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
	sa := simulation.SnapshotAsset{
		HoldingID: line.HoldingID, InstrumentID: line.InstrumentID,
		InstrumentName: instrumentName, InstrumentCode: instrumentCode,
		SnapshotID: line.SimulationSnapshotID,
		Currency: currency, AssetClass: line.AssetClass, IsCash: isCash,
		InitialMinor: line.CurrentAmountMinor, TargetWeight: line.PortfolioTargetWeight,
		ModeledAnnualReturn: snap.ModeledAnnualReturn, AnnualVolatility: snap.AnnualVolatility,
		MaxDrawdown: snap.MaxDrawdown, FeeTreatment: snap.FeeTreatment, ExpenseRatio: snap.ExpenseRatio,
		SourceHash: snap.SourceHash, Years: years,
		CompleteYearCount: snap.CompleteYearCount, MonthlyReturnCount: snap.MonthlyReturnCount,
		HistoryDepth: snap.HistoryDepth, MetricsVersion: snap.MetricsVersion,
		DataWarnings: parseSnapshotWarnings(snap.WarningsJSON),
	}
	if currency == plan.BaseCurrency {
		return sa, nil
	}
	return s.enrichSnapshotAssetFX(ctx, plan, line, sa, currency, fxCache)
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
					"complete_year_count": fxMetrics.CompleteYearCount,
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
	return sa, nil
}

func parseSnapshotWarnings(raw string) []string {
	var out []string
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func snapshotCashFlows(flows []repository.PlanCashFlow) []simulation.SnapshotCashFlow {
	cfSnap := make([]simulation.SnapshotCashFlow, 0, len(flows))
	for _, f := range flows {
		cfSnap = append(cfSnap, simulation.SnapshotCashFlow{
			ID: f.ID, Kind: f.Kind, AmountMinor: f.AmountMinor, StartMonthOffset: f.StartMonthOffset,
			EndMonthOffset: f.EndMonthOffset, Recurrence: f.Recurrence, InflationLinked: f.InflationLinked,
			AnnualGrowthRate: f.AnnualGrowthRate, Enabled: f.Enabled,
		})
	}
	return cfSnap
}

func buildInputSnapshotStruct(
	plan repository.Plan,
	params repository.PlanParameters,
	seed int64,
	configHash string,
	assets []simulation.SnapshotAsset,
	cfSnap []simulation.SnapshotCashFlow,
) *simulation.InputSnapshot {
	in := &simulation.InputSnapshot{
		EngineVersion:     simulation.EngineVersion,
		PlanID:            plan.ID,
		BaseCurrency:      plan.BaseCurrency,
		RandomFactorModel: "independent_student_t",
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
		Assets:    assets,
		CashFlows: cfSnap,
	}
	in.MarketSnapshotHash = simulation.MarketHashFromAssets(assets)
	return in
}
