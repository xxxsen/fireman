package domain

import "testing"

func TestBuildFrozenDraftLines_OnlyEnabled(t *testing.T) {
	result := RebalanceResult{
		Lines: []RebalanceLine{
			{
				HoldingTargetLine: HoldingTargetLine{
					HoldingID: "h1", AssetKey: "i1", Enabled: true,
					CurrentAmountMinor: 120_000_00, StructuralTargetAmountMinor: 100_000_00,
					StructuralGapAmountMinor: -20_000_00, StructuralGapWeight: -0.067,
				},
				Action: "decrease", SuggestedTradeMinor: -20_000_00,
			},
			{
				HoldingTargetLine: HoldingTargetLine{
					HoldingID: "h2", AssetKey: "i2", Enabled: false,
					CurrentAmountMinor: 50_000_00,
				},
			},
		},
	}
	lines := BuildFrozenDraftLines(result)
	if len(lines) != 1 {
		t.Fatalf("len=%d want 1", len(lines))
	}
	if lines[0].BaselineCurrentMinor != 120_000_00 {
		t.Fatalf("baseline=%d", lines[0].BaselineCurrentMinor)
	}
	if lines[0].FrozenTargetMinor != 100_000_00 {
		t.Fatalf("target=%d", lines[0].FrozenTargetMinor)
	}
}

func TestComputeDraftFundPool(t *testing.T) {
	pool := ComputeDraftFundPool([]FrozenDraftLine{
		{BaselineCurrentMinor: 120_000_00, PlannedCurrentMinor: 100_000_00},
		{BaselineCurrentMinor: 90_000_00, PlannedCurrentMinor: 110_000_00},
	})
	if pool.ReleasedMinor != 20_000_00 {
		t.Fatalf("released=%d", pool.ReleasedMinor)
	}
	if pool.UsedMinor != 20_000_00 {
		t.Fatalf("used=%d", pool.UsedMinor)
	}
	if pool.NetMinor != 0 {
		t.Fatalf("net=%d", pool.NetMinor)
	}
}
