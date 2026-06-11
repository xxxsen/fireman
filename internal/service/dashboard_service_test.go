package service

import (
	"testing"

	"github.com/fireman/fireman/internal/domain"
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
