package assumptions

import (
	"errors"
	"strings"
	"testing"
)

// A profile may not be saved or activated unless it covers every
// required base-currency asset cell, and every native-currency (non-base) asset
// prior has a matching FX prior. The errors must name the missing canonical key.

func TestProfileRejectsEmptyCoverage(t *testing.T) {
	p := SystemDefaultProfile()
	p.ReturnPriors = nil
	p.FXPriors = nil
	err := p.Validate()
	if !errors.Is(err, errCoverageMissingAsset) {
		t.Fatalf("empty profile must fail base coverage, got %v", err)
	}
}

func TestProfileRejectsMissingBaseCell(t *testing.T) {
	for _, cell := range RequiredGlobalCoverage {
		t.Run(cell.AssetClass+"/"+cell.Region, func(t *testing.T) {
			p := SystemDefaultProfile()
			kept := p.ReturnPriors[:0]
			for _, rp := range p.ReturnPriors {
				if rp.AssetClass == cell.AssetClass && rp.Region == cell.Region &&
					rp.ValuationCurrency == BaseCoverageCurrency {
					continue
				}
				kept = append(kept, rp)
			}
			p.ReturnPriors = kept
			err := p.Validate()
			if !errors.Is(err, errCoverageMissingAsset) {
				t.Fatalf("missing %s/%s base cell must fail coverage, got %v", cell.AssetClass, cell.Region, err)
			}
			wantKey := cell.AssetClass + "/" + cell.Region + "/" + BaseCoverageCurrency
			if !strings.Contains(err.Error(), wantKey) {
				t.Fatalf("coverage error must name the missing key %q, got %q", wantKey, err.Error())
			}
		})
	}
}

func TestProfileRejectsNativeCurrencyWithoutFX(t *testing.T) {
	p := SystemDefaultProfile()
	// Drop the USD FX prior, leaving the USD-priced equity/bond foreign priors
	// without a currency mapping.
	kept := p.FXPriors[:0]
	for _, fx := range p.FXPriors {
		if fx.FromCurrency == "USD" {
			continue
		}
		kept = append(kept, fx)
	}
	p.FXPriors = kept
	err := p.Validate()
	if !errors.Is(err, errCoverageMissingFX) {
		t.Fatalf("native USD prior without FX must fail coverage, got %v", err)
	}
	if !strings.Contains(err.Error(), FXFactorKey("USD", BaseCoverageCurrency)) {
		t.Fatalf("coverage error must name the missing FX key, got %q", err.Error())
	}
}

func TestSystemProfilePassesCoverage(t *testing.T) {
	p := SystemDefaultProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("system default profile must satisfy coverage: %v", err)
	}
}
