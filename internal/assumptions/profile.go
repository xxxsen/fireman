// Package assumptions holds the version-controlled, auditable capital-market
// assumption profiles that drive forward-looking FIRE simulation returns,
// volatility, correlation and fat-tail parameters. Everything here is
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
	"strings"
	"time"
)

// Scenario names available in every profile. They only shift forward returns and
// reviewed volatility ranges; they never change holdings, inflation, spending or
// the random seed.
const (
	ScenarioFollowGlobal = "follow_global"
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
	ID                        string `json:"id"`
	Version                   int    `json:"version"`
	OwnerScope                string `json:"owner_scope"`
	Name                      string `json:"name"`
	Status                    string `json:"status"`
	PriorStrengthYears        int    `json:"prior_strength_years"`
	CorrelationStrengthMonths int    `json:"correlation_strength_months"`
	StudentTDf                int    `json:"student_t_df"`
	// ReturnFloor/ReturnCeil are the per-month simple-return truncation bounds the
	// fat-tail sampler clamps to. They are part of the global profile (not a plan
	// setting) so the tail behavior is versioned and auditable, and frozen into
	// each run's InputSnapshot. ReturnFloor must be in (-1, 0) and
	// ReturnCeil > 0 with ReturnFloor < ReturnCeil.
	ReturnFloor       float64             `json:"return_floor"`
	ReturnCeil        float64             `json:"return_ceil"`
	Scenarios         map[string]Scenario `json:"scenarios"`
	ReturnPriors      []ReturnPrior       `json:"return_priors"`
	FXPriors          []FXPrior           `json:"fx_priors,omitempty"`
	CorrelationPriors []CorrelationPrior  `json:"correlation_priors,omitempty"`
}

var (
	errProfileID                = errors.New("profile id is required")
	errProfileVersion           = errors.New("profile version must be >= 1")
	errProfileOwnerScope        = errors.New("owner_scope must be system or user")
	errProfileStatus            = errors.New("status must be draft, active or superseded")
	errPriorStrength            = errors.New("prior_strength_years must be > 0")
	errCorrelationStrength      = errors.New("correlation_strength_months must be >= 0")
	errStudentTDf               = errors.New("student_t_df must be an integer > 2")
	errReturnTruncation         = errors.New("return truncation requires -1 < return_floor < 0 < return_ceil")
	errScenarioMissing          = errors.New("scenarios must define conservative, baseline and optimistic")
	errScenarioVolMult          = errors.New("scenario volatility_multiplier must be > 0")
	errScenarioNotFinite        = errors.New("scenario shifts and volatility_multiplier must be finite")
	errReturnPriorKey           = errors.New("return prior requires asset_class, region and valuation_currency")
	errReturnPriorReturn        = errors.New("return prior annual_geometric_return must be finite and > -100%")
	errReturnPriorVol           = errors.New("return prior volatility bounds must be finite with 0 <= floor <= ceiling")
	errReturnPriorAudit         = errors.New("return prior needs https source_url and ISO (YYYY-MM-DD) dates")
	errReturnPriorDuplicate     = errors.New("duplicate return prior for the same asset_class/region/currency")
	errFXPriorKey               = errors.New("fx prior requires from_currency and base_currency")
	errFXPriorAudit             = errors.New("fx prior needs https source_url and ISO (YYYY-MM-DD) dates")
	errFXPriorDuplicate         = errors.New("duplicate fx prior for the same from/base currency pair")
	errCorrelationRange         = errors.New("correlation rho must be finite and in [-1, 1]")
	errCorrelationFactor        = errors.New("correlation prior requires known non-empty factors")
	errCorrelationDuplicate     = errors.New("duplicate correlation prior for the same factor pair")
	errCorrelationUnknownFactor = errors.New("correlation prior references a factor with no asset/fx prior")
	errCorrelationIncomplete    = errors.New("missing correlation prior for a required factor pair")
	errCorrelationSelfMissing   = errors.New("missing same-type correlation prior")
	errCoverageMissingAsset     = errors.New("missing required base-currency return prior")
	errCoverageMissingFX        = errors.New("native-currency asset prior has no matching fx prior")
)

// cashAssetClass is the deterministic, non-random asset class that is excluded
// from the random factor universe.
const cashAssetClass = "cash"

// BaseCoverageCurrency is the home/base currency every active or global profile
// must fully cover so a supported plan never silently fails to calibrate at run
// time.
const BaseCoverageCurrency = "CNY"

// requiredAssetCell is one (asset_class, region) cell that must have a base
// currency return prior in every profile.
type requiredAssetCell struct{ AssetClass, Region string }

// RequiredGlobalCoverage is the single source of truth for the minimum
// asset-class coverage the product supports today. A profile that
// does not define a base currency prior for every cell here — or that adds a
// native-currency (non-base) asset prior without the matching FX prior — cannot
// be saved or activated.
var RequiredGlobalCoverage = []requiredAssetCell{
	{AssetClass: "equity", Region: "domestic"},
	{AssetClass: "equity", Region: "foreign"},
	{AssetClass: "bond", Region: "domestic"},
	{AssetClass: "bond", Region: "foreign"},
	{AssetClass: cashAssetClass, Region: "domestic"},
}

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
	if err := p.validateCoverage(); err != nil {
		return err
	}
	return p.validateCorrelationPriors()
}

// validateCoverage enforces the minimum global coverage gate: every
// RequiredGlobalCoverage cell must have a base-currency (CNY) return prior, and
// every non-cash asset prior priced in a non-base currency must have the matching
// (currency, base) FX prior. Errors carry the missing canonical key so the editor
// can locate the gap. It runs before the correlation completeness check so an
// empty or under-covered profile fails with a coverage error rather than a
// confusing missing-pair error.
func (p *Profile) validateCoverage() error {
	for _, cell := range RequiredGlobalCoverage {
		if _, ok := p.LookupReturnPrior(cell.AssetClass, cell.Region, BaseCoverageCurrency); !ok {
			return fmt.Errorf("%w: %s/%s/%s",
				errCoverageMissingAsset, cell.AssetClass, cell.Region, BaseCoverageCurrency)
		}
	}
	for _, rp := range p.ReturnPriors {
		if rp.AssetClass == cashAssetClass || rp.ValuationCurrency == BaseCoverageCurrency {
			continue
		}
		if _, ok := p.LookupFXPrior(rp.ValuationCurrency, BaseCoverageCurrency); !ok {
			return fmt.Errorf("%w: %s", errCoverageMissingFX,
				FXFactorKey(rp.ValuationCurrency, BaseCoverageCurrency))
		}
	}
	return nil
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
	if !validReturnTruncation(p.ReturnFloor, p.ReturnCeil) {
		return errReturnTruncation
	}
	return nil
}

// validReturnTruncation enforces a usable, finite truncation band: a loss floor
// strictly between -100% and 0, a positive ceiling, and floor < ceil.
func validReturnTruncation(floor, ceil float64) bool {
	if math.IsNaN(floor) || math.IsInf(floor, 0) || math.IsNaN(ceil) || math.IsInf(ceil, 0) {
		return false
	}
	return floor > -1 && floor < 0 && ceil > 0 && floor < ceil
}

func (p *Profile) validateScenarios() error {
	for _, name := range []string{ScenarioConservative, ScenarioBaseline, ScenarioOptimistic} {
		s, ok := p.Scenarios[name]
		if !ok {
			return errScenarioMissing
		}
		if !finiteFloat(s.ReturnShiftLog) || !finiteFloat(s.ReturnShiftLogFX) ||
			!finiteFloat(s.VolatilityMultiplier) {
			return errScenarioNotFinite
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
		if !finiteFloat(rp.AnnualGeometricReturn) || rp.AnnualGeometricReturn <= -1 {
			return errReturnPriorReturn
		}
		if !finiteFloat(rp.AnnualVolatilityFloor) || !finiteFloat(rp.AnnualVolatilityCeiling) ||
			rp.AnnualVolatilityFloor < 0 || rp.AnnualVolatilityCeiling < rp.AnnualVolatilityFloor {
			return errReturnPriorVol
		}
		if !validAuditMeta(rp.SourceURL, rp.PublishedAt, rp.ReviewedAt) {
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
	seen := make(map[string]struct{}, len(p.FXPriors))
	for _, fx := range p.FXPriors {
		if fx.FromCurrency == "" || fx.BaseCurrency == "" {
			return errFXPriorKey
		}
		if !finiteFloat(fx.AnnualGeometricReturn) || fx.AnnualGeometricReturn <= -1 {
			return errReturnPriorReturn
		}
		if !finiteFloat(fx.AnnualVolatilityFloor) || !finiteFloat(fx.AnnualVolatilityCeiling) ||
			fx.AnnualVolatilityFloor < 0 || fx.AnnualVolatilityCeiling < fx.AnnualVolatilityFloor {
			return errReturnPriorVol
		}
		if !validAuditMeta(fx.SourceURL, fx.PublishedAt, fx.ReviewedAt) {
			return errFXPriorAudit
		}
		key := FXFactorKey(fx.FromCurrency, fx.BaseCurrency)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("%w: %s", errFXPriorDuplicate, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// finiteFloat reports whether x is a usable real number (not NaN or ±Inf).
func finiteFloat(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}

// validAuditMeta enforces auditable provenance on every prior: a
// non-empty https source URL and ISO (YYYY-MM-DD) published/reviewed dates so a
// reviewer can open the source and the dates are machine-checkable.
func validAuditMeta(sourceURL, publishedAt, reviewedAt string) bool {
	if !strings.HasPrefix(sourceURL, "https://") || len(sourceURL) <= len("https://") {
		return false
	}
	return isISODate(publishedAt) && isISODate(reviewedAt)
}

// isISODate validates a strict YYYY-MM-DD calendar date.
func isISODate(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// FactorUniverse is the canonical, sorted set of random factor keys implied by a
// profile: one factor per distinct non-cash (asset_class, region) return prior
// cell and one per FX prior pair. Deterministic cash is intentionally excluded.
// Multiple valuation currencies for the same
// (asset_class, region) collapse to a single asset factor.
func (p *Profile) FactorUniverse() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(k string) {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	for _, rp := range p.ReturnPriors {
		if rp.AssetClass == cashAssetClass {
			continue
		}
		add(AssetFactorKey(rp.AssetClass, rp.Region))
	}
	for _, fx := range p.FXPriors {
		add(FXFactorKey(fx.FromCurrency, fx.BaseCurrency))
	}
	sort.Strings(out)
	return out
}

// validateCorrelationPriors enforces a complete, non-degenerate correlation
// structure: every prior must reference two distinct factors that
// exist in the factor universe, rho must be finite and in [-1,1], no factor pair
// may be specified twice (in either order), and every distinct pair of universe
// factors must have exactly one prior. A missing pair would otherwise be silently
// treated as rho=0 at run time, which the forward engine explicitly forbids.
func (p *Profile) validateCorrelationPriors() error {
	keys := p.FactorUniverse()
	universe := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		universe[k] = struct{}{}
	}
	seen, err := collectCorrelationPairs(p.CorrelationPriors, universe)
	if err != nil {
		return err
	}
	return validateCorrelationComplete(keys, seen)
}

// collectCorrelationPairs validates each prior individually and returns the set of
// canonical pair keys, rejecting unknown factors and duplicates.
func collectCorrelationPairs(
	priors []CorrelationPrior, universe map[string]struct{},
) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(priors))
	for _, c := range priors {
		if c.FactorA == "" || c.FactorB == "" {
			return nil, errCorrelationFactor
		}
		if math.IsNaN(c.Rho) || math.IsInf(c.Rho, 0) || c.Rho < -1 || c.Rho > 1 {
			return nil, errCorrelationRange
		}
		a, b := canonicalPair(c.FactorA, c.FactorB)
		if _, ok := universe[a]; !ok {
			return nil, fmt.Errorf("%w: %s", errCorrelationUnknownFactor, a)
		}
		if _, ok := universe[b]; !ok {
			return nil, fmt.Errorf("%w: %s", errCorrelationUnknownFactor, b)
		}
		key := a + "|" + b
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: %s", errCorrelationDuplicate, key)
		}
		seen[key] = struct{}{}
	}
	return seen, nil
}

// validateCorrelationComplete requires exactly one prior for every distinct pair
// of universe factors.
func validateCorrelationComplete(keys []string, seen map[string]struct{}) error {
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			key := keys[i] + "|" + keys[j]
			if _, ok := seen[key]; !ok {
				return fmt.Errorf("%w: %s", errCorrelationIncomplete, key)
			}
		}
	}
	return nil
}

// ValidateSelfCorrelationCoverage requires one versioned prior for two distinct
// assets of each non-cash asset factor type. Legacy profiles may omit these rows
// and retain their historical rho=1 behavior, but a newly published/global
// profile must call this gate.
func (p *Profile) ValidateSelfCorrelationCoverage() error {
	seen := make(map[string]struct{})
	for _, c := range p.CorrelationPriors {
		if c.FactorA == c.FactorB {
			seen[c.FactorA] = struct{}{}
		}
	}
	for _, key := range p.FactorUniverse() {
		if !strings.HasPrefix(key, "asset:") {
			continue
		}
		if _, ok := seen[key]; !ok {
			return fmt.Errorf("%w: %s", errCorrelationSelfMissing, key)
		}
	}
	return nil
}

// HasSelfCorrelationPriors distinguishes profiles using the v4 same-type
// contract from legacy profiles whose implicit same-type rho was 1.
func (p *Profile) HasSelfCorrelationPriors() bool {
	for _, c := range p.CorrelationPriors {
		if c.FactorA != "" && c.FactorA == c.FactorB {
			return true
		}
	}
	return false
}

// canonicalPair orders two factor keys so a <= b.
func canonicalPair(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
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
