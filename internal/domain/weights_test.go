package domain

import "testing"

func testAlloc() AllocationWeights {
	return AllocationWeights{
		AssetClass: map[string]float64{
			AssetClassEquity: 0.70,
			AssetClassBond:   0.30,
			AssetClassCash:   0.00,
		},
		Region: map[string]map[string]float64{
			AssetClassEquity: {RegionDomestic: 0.60, RegionForeign: 0.40},
			AssetClassBond:   {RegionDomestic: 0.50, RegionForeign: 0.50},
		},
	}
}

func TestPortfolioTargetWeight(t *testing.T) {
	alloc := testAlloc()
	got := PortfolioTargetWeight(alloc, HoldingWeightInput{
		AssetClass: AssetClassEquity, Region: RegionDomestic,
		Enabled: true, WeightWithinGroup: 0.50,
	})
	want := 0.70 * 0.60 * 0.50
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestTargetAmountAndDeviation(t *testing.T) {
	total := int64(1_000_000_00) // 1M CNY in fen
	w := 0.21
	tam := TargetAmountMinor(total, w)
	if tam != 210_000_00 {
		t.Fatalf("target amount got %d want %d", tam, 210_000_00)
	}
	cur := int64(170_000_00)
	dw := DeviationWeight(w, CurrentWeight(cur, total))
	if dw < 0.039999 || dw > 0.040001 {
		t.Fatalf("deviation weight got %v", dw)
	}
}

func TestSuggestActionThresholdBoundary(t *testing.T) {
	threshold := 0.03
	cases := []struct {
		name   string
		weight float64
		amount int64
		want   string
	}{
		{"below threshold", 0.029, 100, RebalanceActionHold},
		{"at threshold", 0.03, 100, RebalanceActionIncrease},
		{"above threshold", 0.031, 100, RebalanceActionIncrease},
		{"negative at threshold", -0.03, -100, RebalanceActionDecrease},
		{"disabled", 0.10, 100, RebalanceActionDisabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enabled := tc.name != "disabled"
			got := SuggestAction(enabled, tc.weight, tc.amount, threshold)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestValidateAllocationWeights(t *testing.T) {
	alloc := testAlloc()
	res := ValidateAllocationWeights(alloc)
	if !res.Passed {
		t.Fatalf("expected pass, checks=%+v", res.Checks)
	}
	alloc.AssetClass[AssetClassEquity] = 0.75
	res = ValidateAllocationWeights(alloc)
	if res.Passed {
		t.Fatal("expected fail when asset class sum != 1")
	}
}

func TestValidateHoldingGroupWeights(t *testing.T) {
	alloc := testAlloc()
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true, WeightWithinGroup: 0.60},
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true, WeightWithinGroup: 0.40},
		{AssetClass: AssetClassEquity, Region: RegionForeign, Enabled: true, WeightWithinGroup: 1.0},
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true, WeightWithinGroup: 1.0},
		{AssetClass: AssetClassBond, Region: RegionForeign, Enabled: true, WeightWithinGroup: 1.0},
	}
	res := ValidateHoldingGroupWeights(alloc, holdings)
	if !res.Passed {
		t.Fatalf("expected pass, checks=%+v", res.Checks)
	}
}
