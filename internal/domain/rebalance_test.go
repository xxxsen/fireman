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
	if res.Summary.StructuralActionableCount != 0 {
		t.Fatalf("new_cash structural_actionable=%d want 0", res.Summary.StructuralActionableCount)
	}
	if res.Summary.PlanScaleActionableCount != res.Summary.ActionableCount {
		t.Fatalf("plan_scale actionable mismatch: %d vs %d",
			res.Summary.PlanScaleActionableCount, res.Summary.ActionableCount)
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func bondOnlyAlloc() AllocationWeights {
	return AllocationWeights{
		AssetClass: map[string]float64{
			AssetClassEquity: 0,
			AssetClassBond:   1,
			AssetClassCash:   0,
		},
		Region: map[string]map[string]float64{
			AssetClassBond: {RegionDomestic: 1, RegionForeign: 0},
		},
	}
}

func equityBondAlloc() AllocationWeights {
	return AllocationWeights{
		AssetClass: map[string]float64{
			AssetClassEquity: 0.6,
			AssetClassBond:   0.4,
			AssetClassCash:   0,
		},
		Region: map[string]map[string]float64{
			AssetClassEquity: {RegionDomestic: 1, RegionForeign: 0},
			AssetClassBond:   {RegionDomestic: 1, RegionForeign: 0},
		},
	}
}

// A1: 450w plan, 500w proportional bond holding → structural hold, scale +50w.
func TestComputeFullRebalance_A1_ScaleOverProportional(t *testing.T) {
	alloc := bondOnlyAlloc()
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 500_000_00},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{{"h1", "ins1", "snap1", 1}}

	res := ComputeFullRebalance(alloc, holdings, meta, 450_000_00, 0.03, 0)
	if res.Summary.ScaleGapMinor != 50_000_00 {
		t.Fatalf("scale_gap=%d want %d", res.Summary.ScaleGapMinor, 50_000_00)
	}
	if res.Summary.ActionableCount != 0 {
		t.Fatalf("actionable=%d want 0", res.Summary.ActionableCount)
	}
	if len(res.Lines) != 1 || res.Lines[0].Action != RebalanceActionHold {
		t.Fatalf("expected structural hold, got %+v", res.Lines)
	}
	if res.Lines[0].PlanScaleAction != RebalanceActionDecrease {
		t.Fatalf("plan_scale_action=%q want decrease", res.Lines[0].PlanScaleAction)
	}
}

// B1: 450w plan, 400w proportional bond holding → structural hold, scale -50w.
func TestComputeFullRebalance_B1_ScaleUnderProportional(t *testing.T) {
	alloc := bondOnlyAlloc()
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 400_000_00},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{{"h1", "ins1", "snap1", 1}}

	res := ComputeFullRebalance(alloc, holdings, meta, 450_000_00, 0.03, 0)
	if res.Summary.ScaleGapMinor != -50_000_00 {
		t.Fatalf("scale_gap=%d want %d", res.Summary.ScaleGapMinor, -50_000_00)
	}
	if res.Summary.ActionableCount != 0 {
		t.Fatalf("actionable=%d want 0", res.Summary.ActionableCount)
	}
	if len(res.Lines) != 1 || res.Lines[0].Action != RebalanceActionHold {
		t.Fatalf("expected structural hold, got %+v", res.Lines)
	}
	if res.Lines[0].PlanScaleAction != RebalanceActionIncrease {
		t.Fatalf("plan_scale_action=%q want increase", res.Lines[0].PlanScaleAction)
	}
}

// A2: 450w 60/40, 500w holdings 70/30 → structural rotation, scale +50w.
func TestComputeFullRebalance_A2_StructuralRotationScaleOver(t *testing.T) {
	alloc := equityBondAlloc()
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 350_000_00},
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 150_000_00},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{
		{"h1", "ins1", "snap1", 1},
		{"h2", "ins2", "snap2", 2},
	}

	res := ComputeFullRebalance(alloc, holdings, meta, 450_000_00, 0.03, 0)
	if res.Summary.ScaleGapMinor != 50_000_00 {
		t.Fatalf("scale_gap=%d want %d", res.Summary.ScaleGapMinor, 50_000_00)
	}
	if res.Summary.ActionableCount != 2 {
		t.Fatalf("actionable=%d want 2", res.Summary.ActionableCount)
	}
	actions := map[string]string{}
	for _, line := range res.Lines {
		actions[line.AssetClass] = line.Action
	}
	if actions[AssetClassEquity] != RebalanceActionDecrease {
		t.Fatalf("equity action=%q want decrease", actions[AssetClassEquity])
	}
	if actions[AssetClassBond] != RebalanceActionIncrease {
		t.Fatalf("bond action=%q want increase", actions[AssetClassBond])
	}
}

// B2: 450w 60/40, 400w holdings 70/30 → structural rotation, scale -50w.
func TestComputeFullRebalance_B2_StructuralRotationScaleUnder(t *testing.T) {
	alloc := equityBondAlloc()
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 280_000_00},
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 120_000_00},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{
		{"h1", "ins1", "snap1", 1},
		{"h2", "ins2", "snap2", 2},
	}

	res := ComputeFullRebalance(alloc, holdings, meta, 450_000_00, 0.03, 0)
	if res.Summary.ScaleGapMinor != -50_000_00 {
		t.Fatalf("scale_gap=%d want %d", res.Summary.ScaleGapMinor, -50_000_00)
	}
	if res.Summary.ActionableCount != 2 {
		t.Fatalf("actionable=%d want 2", res.Summary.ActionableCount)
	}
	actions := map[string]string{}
	for _, line := range res.Lines {
		actions[line.AssetClass] = line.Action
	}
	if actions[AssetClassEquity] != RebalanceActionDecrease {
		t.Fatalf("equity action=%q want decrease", actions[AssetClassEquity])
	}
	if actions[AssetClassBond] != RebalanceActionIncrease {
		t.Fatalf("bond action=%q want increase", actions[AssetClassBond])
	}
}

// C1: 450w plan, 450w 60/40 holdings on target → structural hold, scale ~0.
func TestComputeFullRebalance_C1_ScaleAligned(t *testing.T) {
	alloc := equityBondAlloc()
	holdings := []HoldingWeightInput{
		{AssetClass: AssetClassEquity, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 270_000_00},
		{AssetClass: AssetClassBond, Region: RegionDomestic, Enabled: true,
			WeightWithinGroup: 1, CurrentAmountMinor: 180_000_00},
	}
	meta := []struct {
		ID, InstrumentID, SimulationSnapshotID string
		SortOrder                              int
	}{
		{"h1", "ins1", "snap1", 1},
		{"h2", "ins2", "snap2", 2},
	}

	res := ComputeFullRebalance(alloc, holdings, meta, 450_000_00, 0.03, 0)
	if res.Summary.ScaleGapMinor != 0 {
		t.Fatalf("scale_gap=%d want 0", res.Summary.ScaleGapMinor)
	}
	if res.Summary.ActionableCount != 0 {
		t.Fatalf("actionable=%d want 0", res.Summary.ActionableCount)
	}
	for _, line := range res.Lines {
		if line.Action != RebalanceActionHold {
			t.Fatalf("expected hold for %s, got %q", line.HoldingID, line.Action)
		}
	}
}
