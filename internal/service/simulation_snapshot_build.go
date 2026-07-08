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
	if absInt64(enabledSum-totalAssetsMinor) > 100 {
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
	customByAsset map[string]float64,
	overrides map[string]repository.PlanReturnOverride,
) ([]simulation.SnapshotAsset, error) {
	fxCache := make(map[string]marketdata.SnapshotMetrics)
	assets := make([]simulation.SnapshotAsset, 0, len(lines))
	for _, line := range lines {
		if !line.Enabled {
			continue
		}
		sa, err := s.buildOneSnapshotAsset(ctx, plan, line, fxCache, resolved, customByAsset, overrides)
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
	customByAsset map[string]float64,
	overrides map[string]repository.PlanReturnOverride,
) (simulation.SnapshotAsset, error) {
	snap, err := s.snapRepo.GetByID(ctx, line.SimulationSnapshotID)
	if err != nil {
		return simulation.SnapshotAsset{}, newErr("snapshot_not_found", "simulation snapshot missing for holding",
			map[string]any{"holding_id": line.HoldingID})
	}
	if err := validateHoldingSnapshot(line, snap); err != nil {
		return simulation.SnapshotAsset{}, err
	}
	years := toSimSnapshotYears(snap.Years)
	currency, instrumentName, instrumentCode := s.snapshotAssetIdentity(ctx, plan, line)
	isCash := snap.SourceMode == "system_cash" || line.AssetClass == domain.AssetClassCash
	region := line.Region
	if isCash {
		region = domain.RegionDomestic
	}
	cal, err := calibrateAsset(resolved, line.AssetKey, line.AssetClass, region, currency,
		snap.ModeledAnnualReturn, snap.AnnualVolatility, snap.CompleteYearCount, customByAsset)
	if err != nil {
		return simulation.SnapshotAsset{}, newErr("assumption_unavailable",
			"no forward-return assumption covers this holding; configure it in 模拟假设",
			map[string]any{
				"holding_id": line.HoldingID, "asset_key": line.AssetKey,
				"asset_class": line.AssetClass, "region": region, "currency": currency,
				"mode": resolved.Mode, "error": err.Error(),
			})
	}
	sa := simulation.SnapshotAsset{
		HoldingID: line.HoldingID, AssetKey: line.AssetKey,
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
		if ov, ok := overrides[line.AssetKey]; ok {
			applyReturnOverride(&sa, ov)
		}
		if months, err := s.snapRepo.ListSnapshotMonths(ctx, line.SimulationSnapshotID); err == nil {
			sa.Months = monthSeriesToMap(months)
		}
	}
	if currency == plan.BaseCurrency {
		return sa, nil
	}
	return s.enrichSnapshotAssetFX(ctx, plan, line, sa, currency, fxCache, resolved)
}

func validateHoldingSnapshot(line domain.HoldingTargetLine, snap repository.SimulationSnapshot) error {
	if snap.SourceMode == "system_cash" {
		return nil
	}
	if err := marketdata.ValidateSimulationSnapshot(snap); err != nil {
		return newErr("instrument_insufficient_history",
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
	return nil
}

func (s *SimulationService) snapshotAssetIdentity(
	ctx context.Context,
	plan repository.Plan,
	line domain.HoldingTargetLine,
) (string, string, string) {
	asset, err := s.assetRepo.GetByKey(ctx, line.AssetKey)
	if err != nil {
		return plan.BaseCurrency, "", ""
	}
	currency := plan.BaseCurrency
	if asset.Currency != "" {
		currency = asset.Currency
	}
	return currency, asset.Name, asset.Symbol
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

// applyReturnCalibration freezes the forward-return audit fields onto the
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
// calibrated forward values. It only touches the forward
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
	resolved resolvedAssumption,
) (simulation.SnapshotAsset, error) {
	fxMetrics, err := s.fxMetricsCached(ctx, plan, line, currency, fxCache)
	if err != nil {
		return simulation.SnapshotAsset{}, err
	}
	hist := marketdata.MetricFloat(fxMetrics.ModeledAnnualReturn)
	histVol := marketdata.MetricFloat(fxMetrics.AnnualVolatility)
	sa.FXSnapshotID = fxMetrics.SourceHash
	sa.FXHistoricalReturn = hist
	sa.FXModeledReturn = hist
	sa.FXAnnualVolatility = histVol
	sa.FXCompleteYearCount = fxMetrics.CompleteYearCount
	sa.FXMonthlyReturnCount = fxMetrics.MonthlyReturnCount
	sa.FXHistoryDepth = fxMetrics.HistoryDepth
	sa.FXMetricsVersion = fxMetrics.MetricsVersion
	sa.FXDataWarnings = fxMetrics.Warnings
	sa.FXMonths = fxMonthSeriesToMap(fxMetrics.MonthlyReturns)
	sa.FXReturnSource = resolved.Mode
	if err := applyFXCalibration(&sa, resolved, currency, plan.BaseCurrency, hist, histVol, fxMetrics); err != nil {
		return simulation.SnapshotAsset{}, newErr("assumption_unavailable",
			"no forward FX assumption covers this currency pair; configure it in 模拟假设",
			map[string]any{
				"holding_id": line.HoldingID, "from_currency": currency,
				"base_currency": plan.BaseCurrency, "mode": resolved.Mode, "error": err.Error(),
			})
	}
	return sa, nil
}

func (s *SimulationService) fxMetricsCached(
	ctx context.Context,
	plan repository.Plan,
	line domain.HoldingTargetLine,
	currency string,
	fxCache map[string]marketdata.SnapshotMetrics,
) (marketdata.SnapshotMetrics, error) {
	if fxMetrics, ok := fxCache[currency]; ok {
		return fxMetrics, nil
	}
	fxMetrics, err := s.fx.Metrics(ctx, currency, plan.BaseCurrency, plan.ValuationDate)
	if err != nil {
		return marketdata.SnapshotMetrics{}, newErr("fx_snapshot_missing", "FX data unavailable for foreign holding",
			map[string]any{
				"holding_id": line.HoldingID, "currency": currency, "error": err.Error(),
			})
	}
	if fxMetrics.CompleteYearCount < 1 || !fxMetrics.SimulationEligible {
		return marketdata.SnapshotMetrics{}, newErr(
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
	return fxMetrics, nil
}

// applyFXCalibration replaces the raw historical FX drift with the forward,
// profile-driven FX calibration for blended_prior and custom modes (custom only
// overrides the asset's local return, never FX, so its FX still uses the prior).
// historical_cagr keeps the historical FX drift so legacy runs are unchanged. A
// missing FX prior under a forward mode is a hard error.
func applyFXCalibration(
	sa *simulation.SnapshotAsset,
	resolved resolvedAssumption,
	from, base string,
	hist, histVol float64,
	fxMetrics marketdata.SnapshotMetrics,
) error {
	sa.FXReturnScenario = resolved.Scenario
	if resolved.Mode == assumptions.SourceHistoricalCAGR {
		return nil
	}
	cal, err := resolved.Profile.CalibrateFX(assumptions.FXCalibrationInput{
		FromCurrency:                    from,
		BaseCurrency:                    base,
		HistoricalAnnualGeometricReturn: hist,
		HistoricalAnnualVolatility:      histVol,
		CompleteYearCount:               fxMetrics.CompleteYearCount,
		Scenario:                        resolved.Scenario,
	})
	if err != nil {
		return fmt.Errorf("calibrate forward FX %s->%s: %w", from, base, err)
	}
	sa.FXModeledReturn = cal.ForwardAnnualGeometricReturn
	sa.FXAnnualVolatility = cal.AnnualVolatilityUsed
	sa.FXPriorReturn = cal.PriorAnnualGeometricReturn
	sa.FXHistoricalWeight = cal.HistoricalWeight
	sa.FXReturnSource = cal.Source
	sa.FXReturnWarnings = cal.Warnings
	return nil
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
) (*simulation.InputSnapshot, error) {
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
	// Freeze the resolved assumption provenance so a run is always explainable by a
	// specific immutable model identity + content/evidence hash.
	// The CMA evidence hash is recorded ONLY when the resolved profile is genuinely
	// system-owned AND its content matches a recognized published system identity by
	// exact (id, version, content_hash). A user profile (even one that squats on a
	// reserved id) never inherits official evidence provenance, and an unrecognized
	// system content is refused outright so it can never run with forged provenance.
	in.AssumptionProfileID = resolved.Profile.ID
	in.AssumptionProfileVersion = resolved.Profile.Version
	contentHash := resolved.ProfileContentHash
	if contentHash == "" {
		// In-memory profile (no stored row, e.g. a unit test or built-in fallback):
		// recompute from the decoded struct.
		h, err := resolved.Profile.ContentHash()
		if err != nil {
			return nil, newErr("simulation_input_invalid",
				"resolved assumption profile content hash unavailable",
				map[string]any{"profile_id": resolved.Profile.ID, "error": err.Error()})
		}
		contentHash = h
	}
	in.AssumptionProfileContentHash = contentHash
	if resolved.Profile.OwnerScope == assumptions.OwnerSystem {
		variant, ok := assumptions.LookupSystemContent(
			resolved.Profile.ID, resolved.Profile.Version, contentHash,
		)
		if !ok {
			return nil, newErr("system_profile_identity_conflict",
				"system assumption profile content is not a recognized published identity",
				map[string]any{
					"profile_id": resolved.Profile.ID, "version": resolved.Profile.Version,
					"content_hash": contentHash,
				})
		}
		in.AssumptionEvidenceHash = variant.EvidenceHash
	}
	// Forward-looking modes (blended_prior / custom) run the joint, correlated
	// engine and apply the deterministic cash return; historical_cagr keeps the
	// legacy independent path with implicit 0% cash so migrated plans reproduce
	// their old numbers exactly.
	if resolved.Mode != assumptions.SourceHistoricalCAGR {
		// A forward run is a 3.0.0 run even when it has no risk factor (an all-cash
		// plan): cash must still grow at its frozen forward return.
		in.EngineVersion = simulation.EngineVersion
		in.DeterministicCashReturn = true
		freezeTailRiskParams(in, resolved.Profile)
		fm, refs, err := buildFrozenFactorModel(assets, plan.BaseCurrency, resolved.Profile)
		if err != nil {
			// The forward engine must block, never silently fall back to the
			// independent 2.0.0 path.
			return nil, newErr("assumption_unavailable",
				"forward risk model could not be built; check the global assumption profile",
				map[string]any{"error": err.Error()})
		}
		if fm != nil {
			in.RandomFactorModel = simulation.FactorModelMultivariate
			in.FactorModel = fm
			in.AssetFactorRefs = refs
		}
	}
	in.MarketSnapshotHash = simulation.MarketHashFromAssets(assets)
	return in, nil
}

// freezeTailRiskParams freezes the active profile's Student-t df and per-month
// return truncation into the forward (3.0.0) snapshot so the sampler reads frozen
// values, a plan can no longer change them, and changing the profile only affects
// new runs. Profiles predating these fields (zero/invalid bounds) fall
// back to the engine defaults so a legacy profile can never collapse the band.
func freezeTailRiskParams(in *simulation.InputSnapshot, profile assumptions.Profile) {
	if profile.StudentTDf > 2 {
		in.TailStudentTDf = profile.StudentTDf
		in.Parameters.StudentTDf = profile.StudentTDf
	}
	floor, ceil := profile.ReturnFloor, profile.ReturnCeil
	if !(ceil > 0 && floor > -1 && floor < 0 && floor < ceil) {
		floor, ceil = simulation.ReturnFloor, simulation.ReturnCeil
	}
	in.TailReturnFloor = &floor
	in.TailReturnCeil = &ceil
}
