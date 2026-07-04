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

// TestNativeCurrencyPriorsShareAssetFactor verifies that native-currency foreign
// equity priors collapse into the same equity:foreign factor as the CNY one, so
// the correlation universe (and its completeness requirement) is unchanged.
// The system profile already prices equity:foreign in CNY, USD
// and HKD, so the factor must appear exactly once.
func TestNativeCurrencyPriorsShareAssetFactor(t *testing.T) {
	p := SystemDefaultProfile()
	eqF := AssetFactorKey("equity", "foreign")
	count := 0
	for _, k := range p.FactorUniverse() {
		if k == eqF {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("equity:foreign must collapse to a single factor, got %d (%v)", count, p.FactorUniverse())
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("profile with native-currency priors must stay valid: %v", err)
	}
}
