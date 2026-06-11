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
				PortfolioTargetWeight: 0.35, CurrentWeight: 0.30,
			},
			{
				Enabled: true, Region: domain.RegionForeign,
				PortfolioTargetWeight: 0.25, CurrentWeight: 0.40,
			},
			{
				Enabled: false, Region: domain.RegionDomestic,
				PortfolioTargetWeight: 0.40, CurrentWeight: 0.30,
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
			DeviationAmountMinor: 100, DeviationWeight: 0.20,
		},
		{
			HoldingID: "large-amount", Enabled: true,
			DeviationAmountMinor: -10_000, DeviationWeight: -0.01,
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
