package domain

import "testing"

func TestPortfolioCoverageByClass_postFireSingleEquity(t *testing.T) {
	alloc := AllocationWeights{
		AssetClass: map[string]float64{
			AssetClassEquity: 0.55,
			AssetClassBond:   0.35,
			AssetClassCash:   0.10,
		},
		Region: map[string]map[string]float64{
			AssetClassEquity: {RegionDomestic: 1.0, RegionForeign: 0.0},
			AssetClassBond:   {RegionDomestic: 1.0, RegionForeign: 0.0},
			AssetClassCash:   {RegionDomestic: 1.0, RegionForeign: 0.0},
		},
	}
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true, WeightWithinGroup: 1.0},
		{AssetClass: AssetClassCash, Region: RegionDomestic, Enabled: true, WeightWithinGroup: 1.0},
	}
	missing := PortfolioCoverageByClass(alloc, holdings)
	if len(missing) != 1 || missing[0].AssetClass != AssetClassBond {
		t.Fatalf("missing=%+v", missing)
	}
	sum := 0.0
	for _, h := range holdings {
		sum += PortfolioTargetWeight(alloc, h)
	}
	if sum < 0.649 || sum > 0.651 {
		t.Fatalf("portfolio sum=%v want ~0.65", sum)
	}
	res := ValidateHoldingGroupWeights(alloc, holdings)
	for _, c := range res.Checks {
		if c.Scope == "portfolio" && c.Passed {
			t.Fatalf("expected portfolio fail, got %+v", c)
		}
		if c.Scope == "portfolio" {
			if !containsAll(c.Message, "还缺少", "债券", "已配置", "权益", "现金/其他") {
				t.Fatalf("message=%q", c.Message)
			}
		}
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !containsSubstring(s, p) {
			return false
		}
	}
	return true
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
