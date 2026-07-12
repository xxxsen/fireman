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
// from the documented inputs WITHOUT calling the production helper, proving the
// published prior is reproducible from the artifact via the correct geometric
// convention: (1+real)*(1+pi)*(1-fee)-1, then round to 4 decimals.
func recomputeReturn(realRet, infl, fee float64) float64 {
	return math.Round(((1+realRet)*(1+infl)*(1-fee)-1)*1e4) / 1e4
}

// recomputeFX independently re-derives the FX drift via the relative-PPP ratio
// (NOT the additive difference): (1+base)/(1+quote)-1, then round to 4 decimals.
func recomputeFX(baseInfl, quoteInfl float64) float64 {
	return math.Round(((1+baseInfl)/(1+quoteInfl)-1)*1e4) / 1e4
}

// TestExactGeometricReturnFormula pins the exact (unrounded) compounded return for
// the documented worked example, using independent literal expectations rather
// than the production helper.
func TestExactGeometricReturnFormula(t *testing.T) {
	noFee := returnPriorEvidence{RealGeometricReturn: 0.04, ExpectedInflation: 0.02, AnnualFeeRate: 0}
	if got := noFee.ExactNominalAfterFee(); math.Abs(got-0.0608) > evidenceTol {
		t.Errorf("no-fee: got %.10f want 0.0608", got)
	}
	if got := round4(noFee.ExactNominalAfterFee()); math.Abs(got-0.0608) > evidenceTol {
		t.Errorf("no-fee canonical: got %.10f want 0.0608", got)
	}

	withFee := returnPriorEvidence{RealGeometricReturn: 0.04, ExpectedInflation: 0.02, AnnualFeeRate: 0.002}
	if got := withFee.ExactNominalAfterFee(); math.Abs(got-0.0586784) > evidenceTol {
		t.Errorf("with-fee exact: got %.10f want 0.0586784", got)
	}
	if got := round4(withFee.ExactNominalAfterFee()); math.Abs(got-0.0587) > evidenceTol {
		t.Errorf("with-fee canonical: got %.10f want 0.0587", got)
	}

	// The additive shortcut (the bug) would give 0.06 / 0.058; assert we are
	// NOT producing those.
	if math.Abs(noFee.ExactNominalAfterFee()-0.06) < evidenceTol {
		t.Error("no-fee return must not equal the additive shortcut 0.06")
	}
}

// TestNominalAfterFeePassthrough verifies a source already published as nominal,
// after-fee is used verbatim with no second conversion.
func TestNominalAfterFeePassthrough(t *testing.T) {
	e := returnPriorEvidence{
		NominalAfterFee: true, NominalAfterFeeReturn: 0.0731,
		RealGeometricReturn: 0.99, ExpectedInflation: 0.99, AnnualFeeRate: 0.99,
	}
	if got := e.ExactNominalAfterFee(); math.Abs(got-0.0731) > evidenceTol {
		t.Errorf("nominal_after_fee passthrough: got %.10f want 0.0731", got)
	}
}

// TestExactFXDriftFormula pins the relative-PPP ratio formula with independent
// expectations and covers a negative inflation differential.
func TestExactFXDriftFormula(t *testing.T) {
	pos := fxPriorEvidence{BaseInflation: 0.03, QuoteInflation: 0.01}
	want := 1.03/1.01 - 1
	if got := pos.ExactDrift(); math.Abs(got-want) > evidenceTol {
		t.Errorf("fx drift: got %.10f want %.10f", got, want)
	}
	if pos.ExactDrift() <= 0 {
		t.Error("higher base inflation must yield a positive (from-currency-appreciating) drift")
	}
	// The additive shortcut would give 0.02; the ratio is ~0.0198.
	if math.Abs(pos.ExactDrift()-0.02) < 1e-4 {
		t.Error("fx drift must not equal the additive shortcut 0.02")
	}

	neg := fxPriorEvidence{BaseInflation: 0.005, QuoteInflation: 0.025}
	wantNeg := 1.005/1.025 - 1
	if got := neg.ExactDrift(); math.Abs(got-wantNeg) > evidenceTol {
		t.Errorf("fx drift (negative): got %.10f want %.10f", got, wantNeg)
	}
	if neg.ExactDrift() >= 0 {
		t.Error("lower base inflation must yield a negative drift")
	}
}

func TestSystemReturnPriorsTraceToEvidence(t *testing.T) {
	p := SystemDefaultProfile()
	evByKey := map[string]returnPriorEvidence{}
	for _, e := range cmaEvidenceV3.ReturnPriors {
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
	for _, e := range cmaEvidenceV3.ReturnPriors {
		key := e.AssetClass + "|" + e.Region + "|" + e.ValuationCurrency
		rp, ok := byKey[key]
		if !ok {
			t.Fatalf("evidence %s has no profile prior", key)
		}
		want := recomputeReturn(e.RealGeometricReturn, e.ExpectedInflation, e.AnnualFeeRate)
		if math.Abs(rp.AnnualGeometricReturn-want) > evidenceTol {
			t.Errorf("%s: canonical %.6f != recomputed %.6f", key, rp.AnnualGeometricReturn, want)
		}
	}
	// Independent headline expectations: hardcoded geometric values,
	// NOT derived by calling the production builder.
	expect := map[string]float64{
		"equity|domestic|CNY": 0.0608,
		"equity|foreign|CNY":  0.0659,
		"bond|domestic|CNY":   0.0302,
		"bond|foreign|CNY":    0.0302,
		"cash|domestic|CNY":   0.0180,
		"equity|foreign|USD":  0.0659,
		"bond|foreign|USD":    0.0302,
		"equity|foreign|HKD":  0.0659,
		"bond|foreign|HKD":    0.0302,
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
	if len(p.FXPriors) != len(cmaEvidenceV3.FXPriors) {
		t.Fatalf("profile has %d fx priors, evidence has %d", len(p.FXPriors), len(cmaEvidenceV3.FXPriors))
	}
	for _, e := range cmaEvidenceV3.FXPriors {
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
	for i := range cmaEvidenceV3.ReturnPriors {
		if cmaEvidenceV3.ReturnPriors[i].AssetClass == "cash" {
			cash = &cmaEvidenceV3.ReturnPriors[i]
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

// TestSystemProfileRegistryGuardsV3 is the CI guard for the frozen registry: editing the
// v3 evidence artifact (or its built canonical content) without publishing a new
// identity and updating the pinned registry fails here.
func TestSystemProfileRegistryGuardsV3(t *testing.T) {
	cur := CurrentSystemIdentity()
	if cur.ID != SystemProfileID || cur.Version != SystemProfileVersion {
		t.Fatalf("current identity %s != %s@%d", cur.Ref(), SystemProfileID, SystemProfileVersion)
	}
	p := SystemDefaultProfile()
	builtHash, err := p.ContentHash()
	if err != nil {
		t.Fatalf("content hash: %v", err)
	}
	if builtHash != cur.CanonicalHash {
		t.Fatalf("built current canonical hash %q != registry %q; publish a new identity and update the registry",
			builtHash, cur.CanonicalHash)
	}
	if cur.EvidenceHash != CMAEvidenceContentHash {
		t.Fatalf("registry current evidence hash %q != artifact hash %q; edited artifact must bump identity",
			cur.EvidenceHash, CMAEvidenceContentHash)
	}
}

// TestHistoricalSystemProfileVariants pins the read-only variant registry that
// records every recognized published system CONTENT keyed by (id, version,
// content_hash), including the evidence-backed v2 variant. LookupSystemContent
// must accept exactly these contents and reject unknown ones, and only the current
// identity may be a global default.
func TestHistoricalSystemProfileVariants(t *testing.T) {
	variants := HistoricalSystemProfileVariants()
	// v1, initial v2, evidence-backed v2 variant, v3, v4.
	if len(variants) != 5 {
		t.Fatalf("expected 5 recognized system contents, got %d", len(variants))
	}
	// Both published v2 contents must be recognized under the same identity.
	v2Hashes := RecognizedSystemContentHashes(SystemProfileV2ID, SystemProfileV2Version)
	if len(v2Hashes) != 2 {
		t.Fatalf("expected 2 recognized v2 contents, got %v", v2Hashes)
	}

	// The current v4 content is recognized and carries the live evidence hash.
	cur := CurrentSystemIdentity()
	v4, ok := LookupSystemContent(SystemProfileID, SystemProfileVersion, cur.CanonicalHash)
	if !ok {
		t.Fatal("current v4 content must be recognized")
	}
	if v4.EvidenceHash != CMAEvidenceContentHash {
		t.Errorf("v4 variant evidence hash %q != artifact %q", v4.EvidenceHash, CMAEvidenceContentHash)
	}

	// The evidence-backed v2 variant is recognized and pins its own evidence hash,
	// distinct from the initial v2 content (which has no evidence artifact).
	variant, ok := LookupSystemContent(SystemProfileV2ID, SystemProfileV2Version, systemProfileV2EvidenceVariantCanonicalHash)
	if !ok {
		t.Fatal("evidence-backed v2 variant content must be recognized")
	}
	if variant.EvidenceHash != systemProfileV2EvidenceVariantEvidenceHash || variant.EvidenceHash == "" {
		t.Errorf("v2 variant evidence hash %q != pinned %q", variant.EvidenceHash, systemProfileV2EvidenceVariantEvidenceHash)
	}
	initial, ok := LookupSystemContent(SystemProfileV2ID, SystemProfileV2Version, systemProfileV2CanonicalHash)
	if !ok {
		t.Fatal("initial v2 content must be recognized")
	}
	if initial.EvidenceHash != "" {
		t.Errorf("initial v2 content must have no evidence artifact, got %q", initial.EvidenceHash)
	}

	// A fabricated system content is never recognized (cannot forge provenance).
	if _, ok := LookupSystemContent(SystemProfileV2ID, SystemProfileV2Version, strings.Repeat("0", 64)); ok {
		t.Error("an unknown system content must not be recognized")
	}
	// Every recognized content has a 64-char canonical hash.
	for _, v := range variants {
		if len(v.CanonicalHash) != 64 {
			t.Errorf("%s content hash must be a 64-char sha256, got %q", v.Ref(), v.CanonicalHash)
		}
	}
}

// TestReservedSystemNamespace verifies that the system_cma_ prefix is reserved.
func TestReservedSystemNamespace(t *testing.T) {
	for _, id := range []string{"system_cma_v3", "system_cma_v1", "system_cma_anything"} {
		if !HasReservedSystemID(id) {
			t.Errorf("%q must be reserved", id)
		}
	}
	for _, id := range []string{"user_abc", "user_cma_x", "custom", ""} {
		if HasReservedSystemID(id) {
			t.Errorf("%q must NOT be reserved", id)
		}
	}
}

// TestSystemProfileRegistryChain pins the immutable identity chain and the frozen
// v1/v2 canonical hashes.
func TestSystemProfileRegistryChain(t *testing.T) {
	reg := SystemProfileRegistry()
	if len(reg) != 4 {
		t.Fatalf("expected 4 system identities, got %d", len(reg))
	}
	wantRefs := []string{"system_cma_v1@1", "system_cma_v2@1", "system_cma_v3@1", "system_cma_v4@1"}
	wantPred := []string{"", "system_cma_v1@1", "system_cma_v2@1", "system_cma_v3@1"}
	for i, e := range reg {
		if e.Ref() != wantRefs[i] {
			t.Errorf("entry %d ref %q != %q", i, e.Ref(), wantRefs[i])
		}
		if e.Predecessor != wantPred[i] {
			t.Errorf("entry %d predecessor %q != %q", i, e.Predecessor, wantPred[i])
		}
		if len(e.CanonicalHash) != 64 {
			t.Errorf("entry %d canonical hash must be a 64-char sha256, got %q", i, e.CanonicalHash)
		}
	}
	if reg[3].Predecessor != CurrentSystemPredecessorRef() {
		t.Errorf("predecessor ref mismatch: %q vs %q", reg[3].Predecessor, CurrentSystemPredecessorRef())
	}
	// Only the current identity is default-able.
	if !IsCurrentSystemDefaultIdentity(SystemProfileID, SystemProfileVersion) {
		t.Error("v4 must be the current default-able identity")
	}
	if IsCurrentSystemDefaultIdentity(SystemProfileV2ID, SystemProfileV2Version) {
		t.Error("v2 must NOT be default-able")
	}
	if IsCurrentSystemDefaultIdentity(SystemLegacyProfileID, SystemLegacyProfileVersion) {
		t.Error("v1 must NOT be default-able")
	}
}
