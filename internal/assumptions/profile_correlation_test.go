package assumptions

import (
	"errors"
	"testing"
)

func TestSystemProfileValidatesWithFullCorrelationUniverse(t *testing.T) {
	p := SystemDefaultProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("system profile must be valid: %v", err)
	}
	// 4 non-cash asset factors + 2 FX factors (USD, HKD) = 6 factors => 15 pairs.
	if got := len(p.FactorUniverse()); got != 6 {
		t.Fatalf("factor universe size = %d, want 6 (%v)", got, p.FactorUniverse())
	}
	if got := len(p.CorrelationPriors); got != 15 {
		t.Fatalf("correlation priors = %d, want 15", got)
	}
}

func TestProfileRejectsMissingCorrelationPair(t *testing.T) {
	p := SystemDefaultProfile()
	p.CorrelationPriors = p.CorrelationPriors[:len(p.CorrelationPriors)-1]
	if err := p.Validate(); !errors.Is(err, errCorrelationIncomplete) {
		t.Fatalf("expected incomplete-correlation error, got %v", err)
	}
}

func TestProfileRejectsDuplicateCorrelationPair(t *testing.T) {
	p := SystemDefaultProfile()
	// Append a reverse-ordered duplicate of an existing pair.
	first := p.CorrelationPriors[0]
	p.CorrelationPriors = append(p.CorrelationPriors, CorrelationPrior{
		FactorA: first.FactorB, FactorB: first.FactorA, Rho: 0.1,
	})
	if err := p.Validate(); !errors.Is(err, errCorrelationDuplicate) {
		t.Fatalf("expected duplicate-correlation error, got %v", err)
	}
}

func TestProfileRejectsCorrelationForNonUniverseFactor(t *testing.T) {
	p := SystemDefaultProfile()
	cash := AssetFactorKey(cashAssetClass, "domestic")
	eqD := AssetFactorKey("equity", "domestic")
	p.CorrelationPriors = append(p.CorrelationPriors, CorrelationPrior{
		FactorA: cash, FactorB: eqD, Rho: 0,
	})
	if err := p.Validate(); !errors.Is(err, errCorrelationUnknownFactor) {
		t.Fatalf("expected unknown-factor error, got %v", err)
	}
}

// TestNativeCurrencyPriorsShareAssetFactor verifies that a native-currency
// foreign equity prior collapses into the same equity:foreign factor as the CNY
// one, so the correlation universe (and its completeness requirement) is unchanged
// (td/063 R2/R4). A GBP prior (not already in the system profile, and with no FX
// prior) must not introduce a new asset factor.
func TestNativeCurrencyPriorsShareAssetFactor(t *testing.T) {
	p := SystemDefaultProfile()
	before := len(p.FactorUniverse())
	p.ReturnPriors = append(p.ReturnPriors, ReturnPrior{
		AssetClass: "equity", Region: "foreign", ValuationCurrency: "GBP",
		AnnualGeometricReturn: 0.065, AnnualVolatilityFloor: 0.12, AnnualVolatilityCeiling: 0.40,
		SourceURL: "https://example.com", PublishedAt: "2026-06-20", ReviewedAt: "2026-06-20",
	})
	if got := len(p.FactorUniverse()); got != before {
		t.Fatalf("native-currency prior must not add a factor: before=%d after=%d", before, got)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("profile with native-currency prior must stay valid: %v", err)
	}
}
