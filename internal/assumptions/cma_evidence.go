package assumptions

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

// cmaEvidenceRaw is the immutable, committed CMA evidence artifact backing
// system_cma_v2@1 (td/065 R10). Each row records the specific source, dated
// publication, raw real-return / inflation / fee inputs and the conversion to a
// CNY-nominal after-fee geometric prior, so every published number is auditable
// and independently reproducible rather than an unexplained constant. The profile
// is BUILT from this artifact, and its sha256 is referenced by the profile's
// source_note; any change to a source, input or conversion changes the hash and
// must be published as a NEW system profile identity/version.
//
//go:embed cma_evidence_v2.json
var cmaEvidenceRaw []byte

// returnPriorEvidence documents the derivation of one asset return prior.
type returnPriorEvidence struct {
	AssetClass          string  `json:"asset_class"`
	Region              string  `json:"region"`
	ValuationCurrency   string  `json:"valuation_currency"`
	Market              string  `json:"market"`
	SourceURL           string  `json:"source_url"`
	SourcePublishedAt   string  `json:"source_published_at"`
	RealGeometricReturn float64 `json:"real_geometric_return"`
	ExpectedInflation   float64 `json:"expected_inflation"`
	FeeDrag             float64 `json:"fee_drag"`
	VolatilityFloor     float64 `json:"annual_volatility_floor"`
	VolatilityCeiling   float64 `json:"annual_volatility_ceiling"`
	ReviewedBy          string  `json:"reviewed_by"`
	ReviewedAt          string  `json:"reviewed_at"`
	Note                string  `json:"note"`
}

// FinalGeometricNominal recomputes the after-fee nominal geometric return from the
// documented inputs using the log-additive CMA shortcut. An independent
// re-derivation in the tests must reproduce the canonical prior value.
func (e returnPriorEvidence) FinalGeometricNominal() float64 {
	return round4(e.RealGeometricReturn + e.ExpectedInflation - e.FeeDrag)
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

// FinalGeometricNominal recomputes the long-run FX drift from relative PPP
// (base inflation - quote inflation). HKD tracks USD via its peg.
func (e fxPriorEvidence) FinalGeometricNominal() float64 {
	return round4(e.BaseInflation - e.QuoteInflation)
}

type cmaEvidence struct {
	Version      string                `json:"version"`
	Convention   string                `json:"convention"`
	ReturnPriors []returnPriorEvidence `json:"return_priors"`
	FXPriors     []fxPriorEvidence     `json:"fx_priors"`
}

// cmaEvidenceV2 is the parsed artifact. A parse failure is a build-time
// (committed-data) error, so it panics rather than silently shipping a bad
// default risk model.
var cmaEvidenceV2 = mustParseCMAEvidence(cmaEvidenceRaw)

// CMAEvidenceVersion identifies the evidence artifact version.
var CMAEvidenceVersion = cmaEvidenceV2.Version

// CMAEvidenceContentHash is the sha256 of the committed evidence artifact bytes.
// It pins the exact source/input/conversion set behind system_cma_v2@1.
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

// round4 rounds to 4 decimal places, yielding the canonical float64 for the
// published two-decimal-percent priors so the derived value marshals identically
// to a hand-set literal (stable content hash).
func round4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}

// buildSystemReturnPriors materializes the v2 return priors from the evidence
// artifact: the value is recomputed from the documented inputs and the provenance
// fields point at the specific dated source.
func buildSystemReturnPriors() []ReturnPrior {
	out := make([]ReturnPrior, 0, len(cmaEvidenceV2.ReturnPriors))
	for _, e := range cmaEvidenceV2.ReturnPriors {
		out = append(out, ReturnPrior{
			AssetClass:              e.AssetClass,
			Region:                  e.Region,
			ValuationCurrency:       e.ValuationCurrency,
			AnnualGeometricReturn:   e.FinalGeometricNominal(),
			AnnualVolatilityFloor:   e.VolatilityFloor,
			AnnualVolatilityCeiling: e.VolatilityCeiling,
			SourceURL:               e.SourceURL,
			PublishedAt:             e.SourcePublishedAt,
			ReviewedAt:              e.ReviewedAt,
		})
	}
	return out
}

// buildSystemFXPriors materializes the v2 FX priors from the evidence artifact.
func buildSystemFXPriors() []FXPrior {
	out := make([]FXPrior, 0, len(cmaEvidenceV2.FXPriors))
	for _, e := range cmaEvidenceV2.FXPriors {
		out = append(out, FXPrior{
			FromCurrency:            e.FromCurrency,
			BaseCurrency:            e.BaseCurrency,
			AnnualGeometricReturn:   e.FinalGeometricNominal(),
			AnnualVolatilityFloor:   e.VolatilityFloor,
			AnnualVolatilityCeiling: e.VolatilityCeiling,
			SourceURL:               e.SourceURL,
			PublishedAt:             e.SourcePublishedAt,
			ReviewedAt:              e.ReviewedAt,
		})
	}
	return out
}
