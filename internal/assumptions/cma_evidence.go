package assumptions

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

// cmaEvidenceRaw is the immutable, committed CMA evidence artifact backing the
// current system default profile (system_cma_v4@1). Each row records the internal
// policy input and review metadata so every published number is auditable rather
// than an unexplained constant. The profile is BUILT from this artifact and
// its sha256 is pinned in the system profile registry; any change to a source,
// input or conversion changes the hash and MUST be published as a NEW system
// profile identity/version — editing it in place fails CI.
//
//go:embed cma_evidence_v4.json
var cmaEvidenceRaw []byte

// returnPriorEvidence documents the derivation of one asset return prior.
type returnPriorEvidence struct {
	AssetClass        string `json:"asset_class"`
	Region            string `json:"region"`
	ValuationCurrency string `json:"valuation_currency"`
	Market            string `json:"market"`
	SourceURL         string `json:"source_url"`
	SourcePublishedAt string `json:"source_published_at"`
	// NominalAfterFee marks a source that already publishes a nominal, after-fee
	// figure; then NominalAfterFeeReturn is used verbatim with no further
	// conversion or fee deduction.
	NominalAfterFee       bool    `json:"nominal_after_fee"`
	NominalAfterFeeReturn float64 `json:"nominal_after_fee_return"`
	// Otherwise the nominal after-fee return is compounded from these inputs.
	RealGeometricReturn float64 `json:"real_geometric_return"`
	ExpectedInflation   float64 `json:"expected_inflation"`
	AnnualFeeRate       float64 `json:"annual_fee_rate"`
	VolatilityFloor     float64 `json:"annual_volatility_floor"`
	VolatilityCeiling   float64 `json:"annual_volatility_ceiling"`
	ReviewedBy          string  `json:"reviewed_by"`
	ReviewedAt          string  `json:"reviewed_at"`
	Note                string  `json:"note"`
}

// ExactNominalAfterFee returns the EXACT (unrounded) nominal, after-fee geometric
// return. Returns must compound, not add: a 4% real return with 2% inflation is
// (1.04)(1.02)-1 = 6.08%, not 6.00%. round4 is applied only when the
// value is written into the canonical profile.
func (e returnPriorEvidence) ExactNominalAfterFee() float64 {
	if e.NominalAfterFee {
		return e.NominalAfterFeeReturn
	}
	return (1+e.RealGeometricReturn)*(1+e.ExpectedInflation)*(1-e.AnnualFeeRate) - 1
}

// fxPriorEvidence documents the derivation of one FX (from->base) drift prior.
type fxPriorEvidence struct {
	FromCurrency      string  `json:"from_currency"`
	BaseCurrency      string  `json:"base_currency"`
	Method            string  `json:"method"`
	SourceURL         string  `json:"source_url"`
	SourcePublishedAt string  `json:"source_published_at"`
	BaseInflation     float64 `json:"base_inflation"`
	QuoteInflation    float64 `json:"quote_inflation"`
	VolatilityFloor   float64 `json:"annual_volatility_floor"`
	VolatilityCeiling float64 `json:"annual_volatility_ceiling"`
	ReviewedBy        string  `json:"reviewed_by"`
	ReviewedAt        string  `json:"reviewed_at"`
	Note              string  `json:"note"`
}

// ExactDrift returns the EXACT (unrounded) long-run FX drift from relative PPP:
// (1+base_inflation)/(1+quote_inflation)-1. For unequal inflations the ratio (not
// the difference) is required. HKD tracks USD via its peg.
func (e fxPriorEvidence) ExactDrift() float64 {
	return (1+e.BaseInflation)/(1+e.QuoteInflation) - 1
}

type cmaEvidence struct {
	Version               string                `json:"version"`
	EvidenceKind          string                `json:"evidence_kind"`
	CalculationConvention string                `json:"calculation_convention"`
	ReturnPriors          []returnPriorEvidence `json:"return_priors"`
	FXPriors              []fxPriorEvidence     `json:"fx_priors"`
}

// cmaEvidenceV4 is the parsed artifact. A parse failure is a build-time
// (committed-data) error, so it panics rather than silently shipping a bad
// default risk model.
var cmaEvidenceV4 = mustParseCMAEvidence(cmaEvidenceRaw)

// cmaEvidenceV3 is kept as a package-local compatibility alias for older tests
// and helpers; it now points at the current immutable evidence artifact.
var cmaEvidenceV3 = cmaEvidenceV4

// CMAEvidenceVersion identifies the evidence artifact version.
var CMAEvidenceVersion = cmaEvidenceV4.Version

// CMAEvidenceContentHash is the sha256 of the committed evidence artifact bytes.
// It pins the exact policy input set behind system_cma_v4@1 and is
// asserted against the registry in tests.
var CMAEvidenceContentHash = func() string {
	sum := sha256.Sum256(cmaEvidenceRaw)
	return hex.EncodeToString(sum[:])
}()

func mustParseCMAEvidence(raw []byte) cmaEvidence {
	var ev cmaEvidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		panic(fmt.Sprintf("assumptions: parse embedded CMA evidence artifact: %v", err))
	}
	if ev.Version == "" || len(ev.ReturnPriors) == 0 || len(ev.FXPriors) == 0 {
		panic("assumptions: embedded CMA evidence artifact is empty or missing version")
	}
	return ev
}

// round4 rounds to 4 decimal places, applied only when an exact derived value is
// written into the canonical profile.
func round4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}

// buildSystemReturnPriors materializes the current return priors from the evidence
// artifact: the value is the exact compounded return rounded to 4 decimals, and
// each carries its specific dated source.
func buildSystemReturnPriors() []ReturnPrior {
	out := make([]ReturnPrior, 0, len(cmaEvidenceV4.ReturnPriors))
	for _, e := range cmaEvidenceV4.ReturnPriors {
		out = append(out, ReturnPrior{
			AssetClass:              e.AssetClass,
			Region:                  e.Region,
			ValuationCurrency:       e.ValuationCurrency,
			AnnualGeometricReturn:   round4(e.ExactNominalAfterFee()),
			AnnualVolatilityFloor:   e.VolatilityFloor,
			AnnualVolatilityCeiling: e.VolatilityCeiling,
			SourceURL:               e.SourceURL,
			PublishedAt:             e.SourcePublishedAt,
			ReviewedAt:              e.ReviewedAt,
		})
	}
	return out
}

// buildSystemFXPriors materializes the current FX priors from the evidence artifact.
func buildSystemFXPriors() []FXPrior {
	out := make([]FXPrior, 0, len(cmaEvidenceV4.FXPriors))
	for _, e := range cmaEvidenceV4.FXPriors {
		out = append(out, FXPrior{
			FromCurrency:            e.FromCurrency,
			BaseCurrency:            e.BaseCurrency,
			AnnualGeometricReturn:   round4(e.ExactDrift()),
			AnnualVolatilityFloor:   e.VolatilityFloor,
			AnnualVolatilityCeiling: e.VolatilityCeiling,
			SourceURL:               e.SourceURL,
			PublishedAt:             e.SourcePublishedAt,
			ReviewedAt:              e.ReviewedAt,
		})
	}
	return out
}
