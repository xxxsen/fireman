package assumptions

import (
	"errors"
	"fmt"
	"math"
)

// Return assumption sources (td/061 §3.1).
const (
	SourceBlendedPrior   = "blended_prior"
	SourceCustom         = "custom"
	SourceHistoricalCAGR = "historical_cagr"
)

var (
	errScenarioUnknown    = errors.New("unknown scenario for profile")
	errHistoricalInvalid  = errors.New("historical annual return must be finite and > -100%")
	errCustomMissing      = errors.New("custom source requires a custom annual geometric return")
	errCustomInvalid      = errors.New("custom annual geometric return must be finite and > -100%")
	errReturnPriorMissing = errors.New("no return prior covers this asset_class/region/valuation_currency")
	errFXPriorMissing     = errors.New("no fx prior covers this from/base currency pair")
	errUnknownSource      = errors.New("unknown return assumption source")
)

// CalibrationInput is the per-asset request to derive forward simulation inputs.
type CalibrationInput struct {
	Source                          string
	AssetClass                      string
	Region                          string
	ValuationCurrency               string
	HistoricalAnnualGeometricReturn float64
	HistoricalAnnualVolatility      float64
	CompleteYearCount               int
	// CustomAnnualGeometricReturn is required only when Source == custom.
	CustomAnnualGeometricReturn *float64
	Scenario                    string
}

// CalibrationResult is the frozen, auditable output that feeds the engine. Only
// ForwardAnnualGeometricReturn may be presented in the UI as "前瞻年化".
type CalibrationResult struct {
	HistoricalAnnualGeometricReturn float64
	PriorAnnualGeometricReturn      float64
	HistoricalWeight                float64
	ForwardAnnualGeometricReturn    float64
	ForwardLogReturn                float64
	MonthlyMu                       float64
	AnnualVolatilityUsed            float64
	MonthlyVolatility               float64
	Source                          string
	AssumptionSetID                 string
	AssumptionSetVersion            int
	SampleYears                     int
	Warnings                        []string
}

// LookupReturnPrior finds the prior matching (asset_class, region, currency).
func (p *Profile) LookupReturnPrior(assetClass, region, currency string) (ReturnPrior, bool) {
	for _, rp := range p.ReturnPriors {
		if rp.AssetClass == assetClass && rp.Region == region && rp.ValuationCurrency == currency {
			return rp, true
		}
	}
	return ReturnPrior{}, false
}

// LookupFXPrior finds the FX prior for (from, base) currencies.
func (p *Profile) LookupFXPrior(from, base string) (FXPrior, bool) {
	for _, fx := range p.FXPriors {
		if fx.FromCurrency == from && fx.BaseCurrency == base {
			return fx, true
		}
	}
	return FXPrior{}, false
}

// CalibrateForwardReturn derives the forward geometric return, monthly log drift
// and reviewed volatility for one asset (td/061 §3.3). It never silently falls
// back to the raw historical CAGR for a blended_prior asset: a missing prior is
// a hard error so the caller blocks the simulation and routes the user to add a
// mapping in the global assumption center.
func (p *Profile) CalibrateForwardReturn(in CalibrationInput) (CalibrationResult, error) {
	scenario, ok := p.Scenarios[in.Scenario]
	if !ok {
		return CalibrationResult{}, fmt.Errorf("%w: %s", errScenarioUnknown, in.Scenario)
	}
	if math.IsNaN(in.HistoricalAnnualGeometricReturn) ||
		math.IsInf(in.HistoricalAnnualGeometricReturn, 0) ||
		in.HistoricalAnnualGeometricReturn <= -1 {
		return CalibrationResult{}, errHistoricalInvalid
	}

	res := CalibrationResult{
		HistoricalAnnualGeometricReturn: in.HistoricalAnnualGeometricReturn,
		Source:                          in.Source,
		AssumptionSetID:                 p.ID,
		AssumptionSetVersion:            p.Version,
		SampleYears:                     in.CompleteYearCount,
	}

	prior, hasPrior := p.LookupReturnPrior(in.AssetClass, in.Region, in.ValuationCurrency)

	switch in.Source {
	case SourceBlendedPrior:
		if !hasPrior {
			return CalibrationResult{}, fmt.Errorf("%w: %s/%s/%s",
				errReturnPriorMissing, in.AssetClass, in.Region, in.ValuationCurrency)
		}
		p.applyBlended(&res, in, prior, scenario)
	case SourceCustom:
		if in.CustomAnnualGeometricReturn == nil {
			return CalibrationResult{}, errCustomMissing
		}
		custom := *in.CustomAnnualGeometricReturn
		if math.IsNaN(custom) || math.IsInf(custom, 0) || custom <= -1 {
			return CalibrationResult{}, errCustomInvalid
		}
		res.PriorAnnualGeometricReturn = custom
		res.HistoricalWeight = 0
		res.ForwardAnnualGeometricReturn = custom
		res.ForwardLogReturn = math.Log(1 + custom)
		res.MonthlyMu = res.ForwardLogReturn / 12
		res.Warnings = append(res.Warnings, "custom_return_assumption")
	case SourceHistoricalCAGR:
		res.PriorAnnualGeometricReturn = in.HistoricalAnnualGeometricReturn
		res.HistoricalWeight = 1
		res.ForwardAnnualGeometricReturn = in.HistoricalAnnualGeometricReturn
		res.ForwardLogReturn = math.Log(1 + in.HistoricalAnnualGeometricReturn)
		res.MonthlyMu = res.ForwardLogReturn / 12
	default:
		return CalibrationResult{}, fmt.Errorf("%w: %s", errUnknownSource, in.Source)
	}

	applyVolatility(&res, in, prior, hasPrior, scenario)
	return res, nil
}

// FXCalibrationInput is the per-currency-pair request for an FX factor.
type FXCalibrationInput struct {
	FromCurrency                    string
	BaseCurrency                    string
	HistoricalAnnualGeometricReturn float64
	HistoricalAnnualVolatility      float64
	CompleteYearCount               int
	Scenario                        string
}

// CalibrateFX derives the forward FX factor drift and reviewed volatility. The
// scenario applies return_shift_log_fx (default 0) rather than the asset shift,
// so an equity-optimistic scenario does not mechanically imply a currency view.
func (p *Profile) CalibrateFX(in FXCalibrationInput) (CalibrationResult, error) {
	scenario, ok := p.Scenarios[in.Scenario]
	if !ok {
		return CalibrationResult{}, fmt.Errorf("%w: %s", errScenarioUnknown, in.Scenario)
	}
	if math.IsNaN(in.HistoricalAnnualGeometricReturn) ||
		math.IsInf(in.HistoricalAnnualGeometricReturn, 0) ||
		in.HistoricalAnnualGeometricReturn <= -1 {
		return CalibrationResult{}, errHistoricalInvalid
	}
	prior, ok := p.LookupFXPrior(in.FromCurrency, in.BaseCurrency)
	if !ok {
		return CalibrationResult{}, fmt.Errorf("%w: %s->%s",
			errFXPriorMissing, in.FromCurrency, in.BaseCurrency)
	}

	gHist := math.Log(1 + in.HistoricalAnnualGeometricReturn)
	gPrior := math.Log(1 + prior.AnnualGeometricReturn)
	n := in.CompleteYearCount
	if n < 0 {
		n = 0
	}
	if n > p.PriorStrengthYears {
		n = p.PriorStrengthYears
	}
	w := float64(n) / float64(n+p.PriorStrengthYears)
	gFwd := w*gHist + (1-w)*gPrior + scenario.ReturnShiftLogFX

	res := CalibrationResult{
		HistoricalAnnualGeometricReturn: in.HistoricalAnnualGeometricReturn,
		PriorAnnualGeometricReturn:      prior.AnnualGeometricReturn,
		HistoricalWeight:                w,
		ForwardAnnualGeometricReturn:    math.Exp(gFwd) - 1,
		ForwardLogReturn:                gFwd,
		MonthlyMu:                       gFwd / 12,
		Source:                          SourceBlendedPrior,
		AssumptionSetID:                 p.ID,
		AssumptionSetVersion:            p.Version,
		SampleYears:                     in.CompleteYearCount,
	}
	vol := in.HistoricalAnnualVolatility
	if vol < 0 || math.IsNaN(vol) {
		vol = 0
	}
	if vol < prior.AnnualVolatilityFloor {
		vol = prior.AnnualVolatilityFloor
	}
	if prior.AnnualVolatilityCeiling > 0 && vol > prior.AnnualVolatilityCeiling {
		vol = prior.AnnualVolatilityCeiling
	}
	vol *= scenario.VolatilityMultiplier
	res.AnnualVolatilityUsed = vol
	res.MonthlyVolatility = vol / math.Sqrt(12)
	return res, nil
}

// applyBlended performs the log-space shrinkage of historical CAGR toward the
// long-run prior, plus the scenario log shift (td/061 §3.3):
//
//	n      = min(complete_year_count, prior_strength_years)
//	w      = n / (n + prior_strength_years)
//	g_base = w*ln(1+hist) + (1-w)*ln(1+prior)
//	g_fwd  = g_base + scenario.return_shift_log
func (p *Profile) applyBlended(res *CalibrationResult, in CalibrationInput, prior ReturnPrior, scenario Scenario) {
	gHist := math.Log(1 + in.HistoricalAnnualGeometricReturn)
	gPrior := math.Log(1 + prior.AnnualGeometricReturn)
	n := in.CompleteYearCount
	if n < 0 {
		n = 0
	}
	if n > p.PriorStrengthYears {
		n = p.PriorStrengthYears
	}
	w := float64(n) / float64(n+p.PriorStrengthYears)
	gBase := w*gHist + (1-w)*gPrior
	gFwd := gBase + scenario.ReturnShiftLog
	res.PriorAnnualGeometricReturn = prior.AnnualGeometricReturn
	res.HistoricalWeight = w
	res.ForwardLogReturn = gFwd
	res.ForwardAnnualGeometricReturn = math.Exp(gFwd) - 1
	res.MonthlyMu = gFwd / 12
}

// applyVolatility clips the historical volatility into the reviewed band and
// applies the scenario multiplier (td/061 §3.3). Clipping and the scenario
// multiplier only apply to blended_prior: the custom and historical_cagr
// (legacy-compat) sources keep the raw historical volatility so old plans and
// explicit user assumptions reproduce their prior behavior exactly.
func applyVolatility(res *CalibrationResult, in CalibrationInput, prior ReturnPrior, hasPrior bool, scenario Scenario) {
	vol := in.HistoricalAnnualVolatility
	if vol < 0 || math.IsNaN(vol) {
		vol = 0
	}
	if in.Source == SourceBlendedPrior && hasPrior {
		if vol < prior.AnnualVolatilityFloor {
			vol = prior.AnnualVolatilityFloor
		}
		if prior.AnnualVolatilityCeiling > 0 && vol > prior.AnnualVolatilityCeiling {
			vol = prior.AnnualVolatilityCeiling
		}
		vol *= scenario.VolatilityMultiplier
	}
	res.AnnualVolatilityUsed = vol
	res.MonthlyVolatility = vol / math.Sqrt(12)
}
