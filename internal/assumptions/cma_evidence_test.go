package assumptions

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

const evidenceTol = 1e-9

// recomputeReturn independently re-derives the after-fee nominal geometric return
// from the documented inputs (not via the production helper), proving the
// published prior is reproducible from the artifact (td/065 R10 acceptance #2).
func recomputeReturn(realRet, infl, fee float64) float64 {
	return math.Round((realRet+infl-fee)*1e4) / 1e4
}

func recomputeFX(baseInfl, quoteInfl float64) float64 {
	return math.Round((baseInfl-quoteInfl)*1e4) / 1e4
}

func TestSystemReturnPriorsTraceToEvidence(t *testing.T) {
	p := SystemDefaultProfile()
	evByKey := map[string]returnPriorEvidence{}
	for _, e := range cmaEvidenceV2.ReturnPriors {
		key := e.AssetClass + "|" + e.Region + "|" + e.ValuationCurrency
		if _, dup := evByKey[key]; dup {
			t.Fatalf("duplicate evidence row for %s", key)
		}
		evByKey[key] = e
	}
	if len(p.ReturnPriors) != len(evByKey) {
		t.Fatalf("profile has %d return priors, evidence has %d", len(p.ReturnPriors), len(evByKey))
	}
	for _, rp := range p.ReturnPriors {
		key := rp.AssetClass + "|" + rp.Region + "|" + rp.ValuationCurrency
		e, ok := evByKey[key]
		if !ok {
			t.Fatalf("return prior %s has no evidence row", key)
		}
		if rp.SourceURL != e.SourceURL || !strings.HasPrefix(rp.SourceURL, "https://") {
			t.Errorf("%s: source url not traced to evidence (%q vs %q)", key, rp.SourceURL, e.SourceURL)
		}
		if rp.PublishedAt != e.SourcePublishedAt {
			t.Errorf("%s: published_at %q != evidence source date %q", key, rp.PublishedAt, e.SourcePublishedAt)
		}
		// The published date must be the source's date, not the code review date.
		if rp.PublishedAt == SystemProfileReviewedAt {
			t.Errorf("%s: published_at must be the source date, not the review date", key)
		}
	}
}

func TestSystemReturnPriorsRecomputeFromInputs(t *testing.T) {
	p := SystemDefaultProfile()
	byKey := map[string]ReturnPrior{}
	for _, rp := range p.ReturnPriors {
		byKey[rp.AssetClass+"|"+rp.Region+"|"+rp.ValuationCurrency] = rp
	}
	for _, e := range cmaEvidenceV2.ReturnPriors {
		key := e.AssetClass + "|" + e.Region + "|" + e.ValuationCurrency
		rp, ok := byKey[key]
		if !ok {
			t.Fatalf("evidence %s has no profile prior", key)
		}
		want := recomputeReturn(e.RealGeometricReturn, e.ExpectedInflation, e.FeeDrag)
		if math.Abs(rp.AnnualGeometricReturn-want) > evidenceTol {
			t.Errorf("%s: canonical %.6f != recomputed %.6f", key, rp.AnnualGeometricReturn, want)
		}
	}
	// Lock the published headline numbers so a silent input change is caught.
	expect := map[string]float64{
		"equity|domestic|CNY": 0.060,
		"equity|foreign|CNY":  0.065,
		"bond|domestic|CNY":   0.030,
		"bond|foreign|CNY":    0.030,
		"cash|domestic|CNY":   0.018,
		"equity|foreign|USD":  0.065,
		"bond|foreign|USD":    0.030,
		"equity|foreign|HKD":  0.065,
		"bond|foreign|HKD":    0.030,
	}
	for key, want := range expect {
		rp, ok := byKey[key]
		if !ok {
			t.Fatalf("missing prior %s", key)
		}
		if math.Abs(rp.AnnualGeometricReturn-want) > evidenceTol {
			t.Errorf("%s: got %.6f want %.6f", key, rp.AnnualGeometricReturn, want)
		}
	}
}

func TestSystemFXPriorsRecomputeFromInputs(t *testing.T) {
	p := SystemDefaultProfile()
	byFrom := map[string]FXPrior{}
	for _, fx := range p.FXPriors {
		byFrom[fx.FromCurrency] = fx
	}
	if len(p.FXPriors) != len(cmaEvidenceV2.FXPriors) {
		t.Fatalf("profile has %d fx priors, evidence has %d", len(p.FXPriors), len(cmaEvidenceV2.FXPriors))
	}
	for _, e := range cmaEvidenceV2.FXPriors {
		fx, ok := byFrom[e.FromCurrency]
		if !ok {
			t.Fatalf("evidence fx %s->%s has no profile prior", e.FromCurrency, e.BaseCurrency)
		}
		if e.BaseCurrency != "CNY" || fx.BaseCurrency != "CNY" {
			t.Errorf("%s: base currency must be CNY", e.FromCurrency)
		}
		if e.Method == "" {
			t.Errorf("%s: fx evidence must state a bilateral derivation method", e.FromCurrency)
		}
		want := recomputeFX(e.BaseInflation, e.QuoteInflation)
		if math.Abs(fx.AnnualGeometricReturn-want) > evidenceTol {
			t.Errorf("%s/CNY: canonical %.6f != recomputed %.6f", e.FromCurrency, fx.AnnualGeometricReturn, want)
		}
		if fx.PublishedAt != e.SourcePublishedAt {
			t.Errorf("%s/CNY: published_at %q != evidence date %q", e.FromCurrency, fx.PublishedAt, e.SourcePublishedAt)
		}
	}
	if _, ok := byFrom["USD"]; !ok {
		t.Error("missing USD/CNY bilateral fx prior")
	}
	if _, ok := byFrom["HKD"]; !ok {
		t.Error("missing HKD/CNY bilateral fx prior")
	}
}

func TestCashHasDedicatedEvidenceSource(t *testing.T) {
	var cash *returnPriorEvidence
	for i := range cmaEvidenceV2.ReturnPriors {
		if cmaEvidenceV2.ReturnPriors[i].AssetClass == "cash" {
			cash = &cmaEvidenceV2.ReturnPriors[i]
		}
	}
	if cash == nil {
		t.Fatal("no cash evidence row")
	}
	if cash.SourceURL == "" || cash.Note == "" {
		t.Error("cash prior must have its own source URL and derivation note")
	}
}

func TestCMAEvidenceArtifactHashStable(t *testing.T) {
	if CMAEvidenceVersion == "" {
		t.Fatal("evidence version must be set")
	}
	sum := sha256.Sum256(cmaEvidenceRaw)
	want := hex.EncodeToString(sum[:])
	if CMAEvidenceContentHash != want {
		t.Errorf("content hash %q != sha256(artifact) %q", CMAEvidenceContentHash, want)
	}
	// The profile source_note must pin both the artifact version and hash so a
	// changed artifact is detectable from the persisted profile metadata.
	if !strings.Contains(SystemProfileSourceNote, CMAEvidenceVersion) {
		t.Error("source_note must reference the evidence artifact version")
	}
	if !strings.Contains(SystemProfileSourceNote, CMAEvidenceContentHash[:12]) {
		t.Error("source_note must reference the evidence artifact hash")
	}
}

func TestSystemProfileWithEvidenceValidates(t *testing.T) {
	p := SystemDefaultProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("system profile built from evidence must validate: %v", err)
	}
}
