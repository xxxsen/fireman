package service

import (
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

func TestBuildRegionBars(t *testing.T) {
	targets := TargetView{
		Holdings: []domain.HoldingTargetLine{
			{
				Enabled: true, Region: domain.RegionDomestic,
				PortfolioTargetWeight: 0.35, StructuralCurrentWeight: 0.30,
			},
			{
				Enabled: true, Region: domain.RegionForeign,
				PortfolioTargetWeight: 0.25, StructuralCurrentWeight: 0.40,
			},
			{
				Enabled: false, Region: domain.RegionDomestic,
				PortfolioTargetWeight: 0.40, StructuralCurrentWeight: 0.30,
			},
		},
	}

	got := buildRegionBars(targets)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Region != domain.RegionDomestic || got[0].TargetWeight != 0.35 || got[0].CurrentWeight != 0.30 {
		t.Fatalf("domestic bar = %+v", got[0])
	}
	if got[1].Region != domain.RegionForeign || got[1].TargetWeight != 0.25 || got[1].CurrentWeight != 0.40 {
		t.Fatalf("foreign bar = %+v", got[1])
	}
}

func TestBuildAllocationBarsOrdersByBusinessClassAndAggregatesHoldings(t *testing.T) {
	targets := TargetView{
		Holdings: []domain.HoldingTargetLine{
			{
				Enabled: true, AssetClass: domain.AssetClassCash,
				InstrumentName: "现金", InstrumentCode: "CASH",
				PortfolioTargetWeight: 0.10, StructuralCurrentWeight: 0.05,
				TargetAmountMinor: 1_000, CurrentAmountMinor: 500,
			},
			{
				Enabled: true, AssetClass: domain.AssetClassEquity,
				InstrumentName: "小权益", InstrumentCode: "EQ-S",
				PortfolioTargetWeight: 0.10, StructuralCurrentWeight: 0.10,
				TargetAmountMinor: 1_000, CurrentAmountMinor: 1_000,
			},
			{
				Enabled: true, AssetClass: domain.AssetClassEquity,
				InstrumentName: "大权益", InstrumentCode: "EQ-L",
				PortfolioTargetWeight: 0.50, StructuralCurrentWeight: 0.45,
				TargetAmountMinor: 5_000, CurrentAmountMinor: 4_500,
			},
			{
				Enabled: true, AssetClass: domain.AssetClassBond,
				InstrumentName: "债券", InstrumentCode: "BD",
				PortfolioTargetWeight: 0.30, StructuralCurrentWeight: 0.40,
				TargetAmountMinor: 3_000, CurrentAmountMinor: 4_000,
			},
			{
				Enabled: false, AssetClass: domain.AssetClassEquity,
				InstrumentName: "停用", InstrumentCode: "OFF",
				PortfolioTargetWeight: 0.99, TargetAmountMinor: 9_999,
			},
		},
	}

	got := buildAllocationBars(targets)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].AssetClass != domain.AssetClassEquity ||
		got[1].AssetClass != domain.AssetClassBond ||
		got[2].AssetClass != domain.AssetClassCash {
		t.Fatalf("order = %s,%s,%s want equity,bond,cash", got[0].AssetClass, got[1].AssetClass, got[2].AssetClass)
	}
	equity := got[0]
	if equity.TargetWeight != 0.60 || equity.CurrentWeight != 0.55 {
		t.Fatalf("equity weights = %+v", equity)
	}
	if equity.TargetAmountMinor != 6_000 || equity.CurrentAmountMinor != 5_500 {
		t.Fatalf("equity amounts = %+v", equity)
	}
	if len(equity.Holdings) != 2 {
		t.Fatalf("equity holdings = %d, want 2 (disabled excluded)", len(equity.Holdings))
	}
	if equity.Holdings[0].InstrumentCode != "EQ-L" {
		t.Fatalf("first equity holding = %s, want EQ-L (largest first)", equity.Holdings[0].InstrumentCode)
	}
}

func TestBuildAssetClassRegionGroupsSplitsWithinClass(t *testing.T) {
	// Plan: equity 70% / bond 30%; equity 70/30 domestic/foreign, bond 80/20.
	targets := TargetView{
		AssetClass: []repository.AssetClassTarget{
			{AssetClass: domain.AssetClassEquity, Weight: 0.70},
			{AssetClass: domain.AssetClassBond, Weight: 0.30},
			{AssetClass: domain.AssetClassCash, Weight: 0.0},
		},
		RegionTargets: []repository.RegionTarget{
			{AssetClass: domain.AssetClassEquity, Region: domain.RegionForeign, WeightWithinClass: 0.30},
			{AssetClass: domain.AssetClassEquity, Region: domain.RegionDomestic, WeightWithinClass: 0.70},
			{AssetClass: domain.AssetClassBond, Region: domain.RegionDomestic, WeightWithinClass: 0.80},
			{AssetClass: domain.AssetClassBond, Region: domain.RegionForeign, WeightWithinClass: 0.20},
			{AssetClass: domain.AssetClassCash, Region: domain.RegionDomestic, WeightWithinClass: 1.0},
			{AssetClass: domain.AssetClassCash, Region: domain.RegionForeign, WeightWithinClass: 0.0},
		},
		Holdings: []domain.HoldingTargetLine{
			{
				Enabled: true, AssetClass: domain.AssetClassEquity, Region: domain.RegionDomestic,
				InstrumentName: "A股", InstrumentCode: "EQD",
				PortfolioTargetWeight: 0.49, CurrentAmountMinor: 7_000, TargetAmountMinor: 7_000,
			},
			{
				Enabled: true, AssetClass: domain.AssetClassEquity, Region: domain.RegionForeign,
				InstrumentName: "美股", InstrumentCode: "EQF",
				PortfolioTargetWeight: 0.21, CurrentAmountMinor: 3_000, TargetAmountMinor: 3_000,
			},
			{
				Enabled: true, AssetClass: domain.AssetClassBond, Region: domain.RegionDomestic,
				InstrumentName: "国债", InstrumentCode: "BDD",
				PortfolioTargetWeight: 0.24, CurrentAmountMinor: 2_400, TargetAmountMinor: 2_400,
			},
			{
				Enabled: true, AssetClass: domain.AssetClassBond, Region: domain.RegionForeign,
				InstrumentName: "海外债", InstrumentCode: "BDF",
				PortfolioTargetWeight: 0.06, CurrentAmountMinor: 600, TargetAmountMinor: 600,
			},
		},
	}

	groups := buildAssetClassRegionGroups(targets)
	if len(groups) != 2 {
		t.Fatalf("groups len = %d, want 2 (equity, bond; cash excluded)", len(groups))
	}
	if groups[0].AssetClass != domain.AssetClassEquity || groups[1].AssetClass != domain.AssetClassBond {
		t.Fatalf("group order = %s,%s want equity,bond", groups[0].AssetClass, groups[1].AssetClass)
	}
	equity := groups[0]
	if len(equity.Regions) != 2 || equity.Regions[0].Region != domain.RegionDomestic {
		t.Fatalf("equity regions = %+v want domestic first", equity.Regions)
	}
	if equity.Regions[0].TargetWeight != 0.70 || equity.Regions[1].TargetWeight != 0.30 {
		t.Fatalf("equity target split = %v/%v want 0.7/0.3",
			equity.Regions[0].TargetWeight, equity.Regions[1].TargetWeight)
	}
	if equity.Regions[0].CurrentWeight != 0.70 {
		t.Fatalf("equity domestic current within class = %v want 0.70", equity.Regions[0].CurrentWeight)
	}
	bond := groups[1]
	if bond.Regions[0].TargetWeight != 0.80 || bond.Regions[1].TargetWeight != 0.20 {
		t.Fatalf("bond target split = %v/%v want 0.8/0.2",
			bond.Regions[0].TargetWeight, bond.Regions[1].TargetWeight)
	}
	if bond.Regions[0].CurrentWeight != 0.80 {
		t.Fatalf("bond domestic current within class = %v want 0.80", bond.Regions[0].CurrentWeight)
	}

	// Full-portfolio region exposure should be ~73% / 27%.
	region := buildRegionBars(targets)
	if len(region) != 2 || region[0].Region != domain.RegionDomestic {
		t.Fatalf("region bars = %+v", region)
	}
	if d := region[0].TargetWeight; d < 0.729 || d > 0.731 {
		t.Fatalf("full-portfolio domestic target = %v want ~0.73", d)
	}
}

func TestTopDeviationsSortsByAbsoluteAmount(t *testing.T) {
	lines := []domain.HoldingTargetLine{
		{
			HoldingID: "large-percent", Enabled: true,
			StructuralGapAmountMinor: 5_000, StructuralGapWeight: 0.20,
		},
		{
			HoldingID: "large-amount", Enabled: true,
			StructuralGapAmountMinor: -10_000, StructuralGapWeight: -0.01,
		},
	}

	got := topDeviations(lines, nil, 2)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].DeviationMinor != -10_000 {
		t.Fatalf("first deviation = %d, want -10000", got[0].DeviationMinor)
	}
}

func TestTopDeviationsExcludesNearZeroGap(t *testing.T) {
	lines := []domain.HoldingTargetLine{
		{HoldingID: "a", Enabled: true, StructuralGapAmountMinor: 0},
		{HoldingID: "b", Enabled: true, StructuralGapAmountMinor: 50},
		{HoldingID: "c", Enabled: true, StructuralGapAmountMinor: -100},
		{HoldingID: "d", Enabled: true, StructuralGapAmountMinor: 101},
	}

	got := topDeviations(lines, nil, 5)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only |gap| > 1元)", len(got))
	}
	if got[0].DeviationMinor != 101 {
		t.Fatalf("deviation = %d, want 101", got[0].DeviationMinor)
	}
}

func TestTopDeviationsB1AllStructuralHoldReturnsEmpty(t *testing.T) {
	lines := []domain.HoldingTargetLine{
		{HoldingID: "equity-a", Enabled: true, StructuralGapAmountMinor: 0, StructuralGapWeight: 0},
		{HoldingID: "bond-b", Enabled: true, StructuralGapAmountMinor: 0, StructuralGapWeight: 0},
		{HoldingID: "cash-c", Enabled: true, StructuralGapAmountMinor: 0, StructuralGapWeight: 0},
	}

	got := topDeviations(lines, nil, 5)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 for B1-style zero structural gap", len(got))
	}
}

func TestComputeInvestedRatioUsesPlanTotalAssets(t *testing.T) {
	got := computeInvestedRatio(320_000_00, 500_000_00)
	if got != 0.64 {
		t.Fatalf("ratio = %v, want 0.64", got)
	}
	if computeInvestedRatio(320_000_00, 0) != 0 {
		t.Fatal("zero total assets should return 0")
	}
	if computeInvestedRatio(320_000_00, -1) != 0 {
		t.Fatal("negative total assets should return 0")
	}
}
