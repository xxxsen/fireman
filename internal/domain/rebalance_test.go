package domain

import "testing"

func testHoldings() ([]HoldingWeightInput, []struct {
	ID, InstrumentID, SimulationSnapshotID string
	SortOrder                              int
}) {
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1.0, CurrentAmountMinor: 300_000_00},
		{AssetClass: AssetClassEquity, Region: RegionForeign, Enabled: true,
			WeightWithinGroup: 1.0, CurrentAmountMinor: 200_000_00},
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 0.5, CurrentAmountMinor: 150_000_00},
		{AssetClass: AssetClassBond, Region: RegionForeign, Enabled: true,
			WeightWithinGroup: 0.5, CurrentAmountMinor: 150_000_00},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{
		{"h1", "ins1", "snap1", 1},
		{"h2", "ins2", "snap2", 2},
		{"h3", "ins3", "snap3", 3},
		{"h4", "ins4", "snap4", 4},
	}
	return holdings, meta
}

func TestComputeFullRebalance(t *testing.T) {
	alloc := testAlloc()
	holdings, meta := testHoldings()
	total := int64(800_000_00)
	res := ComputeFullRebalance(alloc, holdings, meta, total, 0.03, 0.001)
	if res.Mode != RebalanceModeFull {
		t.Fatalf("mode=%q", res.Mode)
	}
	if len(res.Lines) != 4 {
		t.Fatalf("lines=%d", len(res.Lines))
	}
	var tradeSum int64
	for _, l := range res.Lines {
		if l.Action == RebalanceActionIncrease || l.Action == RebalanceActionDecrease {
			tradeSum += abs64(l.SuggestedTradeMinor)
		}
	}
	if tradeSum != res.Summary.EstimatedTradeMinor {
		t.Fatalf("trade sum mismatch %d vs %d", tradeSum, res.Summary.EstimatedTradeMinor)
	}
}

func TestComputeNewCashRebalanceRoundingRemainderToLargestGap(t *testing.T) {
	alloc := AllocationWeights{
		AssetClass: map[string]float64{
			AssetClassEquity: 1.0,
			AssetClassBond:   0.0,
			AssetClassCash:   0.0,
		},
		Region: map[string]map[string]float64{
			AssetClassEquity: {RegionDomestic: 1.0, RegionForeign: 0.0},
		},
	}
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1.0 / 3, CurrentAmountMinor: 123},
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1.0 / 3, CurrentAmountMinor: 123},
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1.0 / 3, CurrentAmountMinor: 123},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{
		{"h1", "ins1", "snap1", 1},
		{"h2", "ins2", "snap2", 2},
		{"h3", "ins3", "snap3", 3},
	}
	newCash := int64(7)
	res := ComputeNewCashRebalance(alloc, holdings, meta, 900, newCash, 0, 0)

	var buySum int64
	var firstMaxGapBuy int64
	for _, l := range res.Lines {
		buySum += l.SuggestedTradeMinor
		if l.HoldingID == "h1" {
			firstMaxGapBuy = l.SuggestedTradeMinor
		}
	}
	if buySum != newCash {
		t.Fatalf("allocated %d != new cash %d", buySum, newCash)
	}
	// Equal gaps tie on max; remainder goes to the first max-gap holding.
	if firstMaxGapBuy != 3 {
		t.Fatalf("first max-gap holding should receive rounding remainder, got buy=%d", firstMaxGapBuy)
	}
}

func TestComputeNewCashRebalance(t *testing.T) {
	alloc := testAlloc()
	holdings, meta := testHoldings()
	total := int64(800_000_00)
	newCash := int64(100_000_00)
	res := ComputeNewCashRebalance(alloc, holdings, meta, total, newCash, 0.03, 0)
	if res.Mode != RebalanceModeNewCash {
		t.Fatalf("mode=%q", res.Mode)
	}
	var buySum int64
	for _, l := range res.Lines {
		if l.SuggestedTradeMinor < 0 {
			t.Fatalf("new cash mode must not suggest sells")
		}
		buySum += l.SuggestedTradeMinor
	}
	if buySum > newCash {
		t.Fatalf("allocated %d > new cash %d", buySum, newCash)
	}
	if buySum == 0 {
		t.Fatal("expected some buy suggestions")
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
