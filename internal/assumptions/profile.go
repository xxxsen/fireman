// Package assumptions holds the version-controlled, auditable capital-market
// assumption profiles that drive forward-looking FIRE simulation returns,
// volatility, correlation and fat-tail parameters (td/061). Everything here is
// pure: it never reads the database, RNG or runtime config, so the same profile
// always calibrates to the same frozen run inputs.
package assumptions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

// Scenario names available in every profile. They only shift forward returns and
// reviewed volatility ranges; they never change holdings, inflation, spending or
// the random seed.
const (
	ScenarioConservative = "conservative"
	ScenarioBaseline     = "baseline"
	ScenarioOptimistic   = "optimistic"
)

// Owner scopes for a profile.
const (
	OwnerSystem = "system"
	OwnerUser   = "user"
)

// Profile lifecycle states.
const (
	StatusDraft      = "draft"
	StatusActive     = "active"
	StatusSuperseded = "superseded"
)

// Scenario shifts forward returns in log space and scales reviewed volatility.
type Scenario struct {
	// ReturnShiftLog is added to the asset forward log return (per 12-month
	// compounding center), so exp(shift) multiplies the annual geometric center.
	ReturnShiftLog float64 `json:"return_shift_log"`
	// ReturnShiftLogFX defaults to 0; non-zero requires its own sourced rationale.
	ReturnShiftLogFX float64 `json:"return_shift_log_fx"`
	// VolatilityMultiplier scales the reviewed (clipped) volatility.
	VolatilityMultiplier float64 `json:"volatility_multiplier"`
}

// ReturnPrior is the long-run, after-fee, base-currency nominal geometric return
// prior for one (asset_class, region, valuation_currency) cell, plus reviewed
// volatility bounds and audit provenance.
type ReturnPrior struct {
	AssetClass              string  `json:"asset_class"`
	Region                  string  `json:"region"`
	ValuationCurrency       string  `json:"valuation_currency"`
	AnnualGeometricReturn   float64 `json:"annual_geometric_return"`
	AnnualVolatilityFloor   float64 `json:"annual_volatility_floor"`
	AnnualVolatilityCeiling float64 `json:"annual_volatility_ceiling"`
	SourceURL               string  `json:"source_url"`
	PublishedAt             string  `json:"published_at"`
	ReviewedAt              string  `json:"reviewed_at"`
}

// FXPrior is the long-run prior for an FX factor keyed by (from, base) currency.
type FXPrior struct {
	FromCurrency            string  `json:"from_currency"`
	BaseCurrency            string  `json:"base_currency"`
	AnnualGeometricReturn   float64 `json:"annual_geometric_return"`
	AnnualVolatilityFloor   float64 `json:"annual_volatility_floor"`
	AnnualVolatilityCeiling float64 `json:"annual_volatility_ceiling"`
	SourceURL               string  `json:"source_url"`
	PublishedAt             string  `json:"published_at"`
	ReviewedAt              string  `json:"reviewed_at"`
}

// CorrelationPrior is the prior correlation between two named factors. Factors
// are canonicalised so FactorA <= FactorB lexicographically.
type CorrelationPrior struct {
	FactorA string  `json:"factor_a"`
	FactorB string  `json:"factor_b"`
	Rho     float64 `json:"rho"`
}

// Profile is a complete, version-locked assumption set.
type Profile struct {
	ID                        string              `json:"id"`
	Version                   int                 `json:"version"`
	OwnerScope                string              `json:"owner_scope"`
	Name                      string              `json:"name"`
	Status                    string              `json:"status"`
	PriorStrengthYears        int                 `json:"prior_strength_years"`
	CorrelationStrengthMonths int                 `json:"correlation_strength_months"`
	StudentTDf                int                 `json:"student_t_df"`
	Scenarios                 map[string]Scenario `json:"scenarios"`
	ReturnPriors              []ReturnPrior       `json:"return_priors"`
	FXPriors                  []FXPrior           `json:"fx_priors,omitempty"`
	CorrelationPriors         []CorrelationPrior  `json:"correlation_priors,omitempty"`
}

var (
	errProfileID            = errors.New("profile id is required")
	errProfileVersion       = errors.New("profile version must be >= 1")
	errProfileOwnerScope    = errors.New("owner_scope must be system or user")
	errProfileStatus        = errors.New("status must be draft, active or superseded")
	errPriorStrength        = errors.New("prior_strength_years must be > 0")
	errCorrelationStrength  = errors.New("correlation_strength_months must be >= 0")
	errStudentTDf           = errors.New("student_t_df must be an integer > 2")
	errScenarioMissing      = errors.New("scenarios must define conservative, baseline and optimistic")
	errScenarioVolMult      = errors.New("scenario volatility_multiplier must be > 0")
	errReturnPriorKey       = errors.New("return prior requires asset_class, region and valuation_currency")
	errReturnPriorReturn    = errors.New("return prior annual_geometric_return must be finite and > -100%")
	errReturnPriorVol       = errors.New("return prior volatility bounds must satisfy 0 <= floor <= ceiling")
	errReturnPriorAudit     = errors.New("return prior requires source_url, published_at and reviewed_at")
	errReturnPriorDuplicate = errors.New("duplicate return prior for the same asset_class/region/currency")
	errFXPriorKey           = errors.New("fx prior requires from_currency and base_currency")
	errFXPriorAudit         = errors.New("fx prior requires source_url, published_at and reviewed_at")
	errCorrelationRange     = errors.New("correlation rho must be in [-1, 1]")
	errCorrelationFactor    = errors.New("correlation prior requires two distinct factors")
)

// Validate checks structural validity required before a profile may be persisted
// or used to build a run. It does not perform the PSD/Cholesky repair (that is a
// run-time concern in the factor model) but does ensure every cell is auditable.
func (p *Profile) Validate() error {
	if err := p.validateHeader(); err != nil {
		return err
	}
	if err := p.validateScenarios(); err != nil {
		return err
	}
	if err := p.validateReturnPriors(); err != nil {
		return err
	}
	if err := p.validateFXPriors(); err != nil {
		return err
	}
	return p.validateCorrelationPriors()
}

func (p *Profile) validateHeader() error {
	if p.ID == "" {
		return errProfileID
	}
	if p.Version < 1 {
		return errProfileVersion
	}
	if p.OwnerScope != OwnerSystem && p.OwnerScope != OwnerUser {
		return errProfileOwnerScope
	}
	switch p.Status {
	case StatusDraft, StatusActive, StatusSuperseded:
	default:
		return errProfileStatus
	}
	if p.PriorStrengthYears <= 0 {
		return errPriorStrength
	}
	if p.CorrelationStrengthMonths < 0 {
		return errCorrelationStrength
	}
	if p.StudentTDf <= 2 {
		return errStudentTDf
	}
	return nil
}

func (p *Profile) validateScenarios() error {
	for _, name := range []string{ScenarioConservative, ScenarioBaseline, ScenarioOptimistic} {
		s, ok := p.Scenarios[name]
		if !ok {
			return errScenarioMissing
		}
		if s.VolatilityMultiplier <= 0 {
			return errScenarioVolMult
		}
	}
	return nil
}

func (p *Profile) validateReturnPriors() error {
	seen := make(map[string]struct{}, len(p.ReturnPriors))
	for _, rp := range p.ReturnPriors {
		if rp.AssetClass == "" || rp.Region == "" || rp.ValuationCurrency == "" {
			return errReturnPriorKey
		}
		if math.IsNaN(rp.AnnualGeometricReturn) || math.IsInf(rp.AnnualGeometricReturn, 0) ||
			rp.AnnualGeometricReturn <= -1 {
			return errReturnPriorReturn
		}
		if rp.AnnualVolatilityFloor < 0 || rp.AnnualVolatilityCeiling < rp.AnnualVolatilityFloor {
			return errReturnPriorVol
		}
		if rp.SourceURL == "" || rp.PublishedAt == "" || rp.ReviewedAt == "" {
			return errReturnPriorAudit
		}
		key := returnPriorKey(rp.AssetClass, rp.Region, rp.ValuationCurrency)
		if _, dup := seen[key]; dup {
			return errReturnPriorDuplicate
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (p *Profile) validateFXPriors() error {
	for _, fx := range p.FXPriors {
		if fx.FromCurrency == "" || fx.BaseCurrency == "" {
			return errFXPriorKey
		}
		if math.IsNaN(fx.AnnualGeometricReturn) || math.IsInf(fx.AnnualGeometricReturn, 0) ||
			fx.AnnualGeometricReturn <= -1 {
			return errReturnPriorReturn
		}
		if fx.AnnualVolatilityFloor < 0 || fx.AnnualVolatilityCeiling < fx.AnnualVolatilityFloor {
			return errReturnPriorVol
		}
		if fx.SourceURL == "" || fx.PublishedAt == "" || fx.ReviewedAt == "" {
			return errFXPriorAudit
		}
	}
	return nil
}

func (p *Profile) validateCorrelationPriors() error {
	for _, c := range p.CorrelationPriors {
		if c.FactorA == "" || c.FactorB == "" || c.FactorA == c.FactorB {
			return errCorrelationFactor
		}
		if c.Rho < -1 || c.Rho > 1 {
			return errCorrelationRange
		}
	}
	return nil
}

func returnPriorKey(assetClass, region, currency string) string {
	return assetClass + "|" + region + "|" + currency
}

// Canonical returns a deep copy with deterministically ordered slices and
// canonicalised correlation factor pairs, so CanonicalJSON/ContentHash are stable
// regardless of input ordering or map iteration.
func (p *Profile) Canonical() Profile {
	out := *p
	out.ReturnPriors = append([]ReturnPrior(nil), p.ReturnPriors...)
	sort.Slice(out.ReturnPriors, func(i, j int) bool {
		a := out.ReturnPriors[i]
		b := out.ReturnPriors[j]
		return returnPriorKey(a.AssetClass, a.Region, a.ValuationCurrency) <
			returnPriorKey(b.AssetClass, b.Region, b.ValuationCurrency)
	})
	out.FXPriors = append([]FXPrior(nil), p.FXPriors...)
	sort.Slice(out.FXPriors, func(i, j int) bool {
		a := out.FXPriors[i]
		b := out.FXPriors[j]
		return a.FromCurrency+a.BaseCurrency < b.FromCurrency+b.BaseCurrency
	})
	out.CorrelationPriors = append([]CorrelationPrior(nil), p.CorrelationPriors...)
	for i := range out.CorrelationPriors {
		c := &out.CorrelationPriors[i]
		if c.FactorA > c.FactorB {
			c.FactorA, c.FactorB = c.FactorB, c.FactorA
		}
	}
	sort.Slice(out.CorrelationPriors, func(i, j int) bool {
		a := out.CorrelationPriors[i].FactorA + "|" + out.CorrelationPriors[i].FactorB
		b := out.CorrelationPriors[j].FactorA + "|" + out.CorrelationPriors[j].FactorB
		return a < b
	})
	return out
}

// CanonicalJSON marshals the canonical profile. Map keys are sorted by
// encoding/json, slices are pre-sorted, so the bytes are deterministic.
func (p *Profile) CanonicalJSON() ([]byte, error) {
	c := p.Canonical()
	return json.Marshal(&c)
}

// ContentHash is the SHA-256 of CanonicalJSON, used to detect changes and to
// pin a profile version into a run.
func (p *Profile) ContentHash() (string, error) {
	b, err := p.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Ref is the immutable reference (id@version) for a profile.
func (p *Profile) Ref() string {
	return fmt.Sprintf("%s@%d", p.ID, p.Version)
}

// AssetFactorKey is the canonical correlation/factor key for an asset cell.
func AssetFactorKey(assetClass, region string) string {
	return "asset:" + assetClass + ":" + region
}

// FXFactorKey is the canonical correlation/factor key for an FX pair.
func FXFactorKey(from, base string) string {
	return "fx:" + from + ":" + base
}
