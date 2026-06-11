package domain

import "testing"

func TestComputeReferencePackageDeltas_PKG1_FourLineClosure(t *testing.T) {
	result := ComputeReferencePackageDeltas([]PackageDeltaInput{
		{HoldingID: "hA", StructuralGapMinor: -30_000_00},
		{HoldingID: "hB", StructuralGapMinor: 10_000_00},
		{HoldingID: "hC", StructuralGapMinor: 10_000_00},
		{HoldingID: "hD", StructuralGapMinor: 10_000_00},
	})
	d := result.ByHoldingID
	if d["hA"] != -30_000_00 || d["hB"] != 10_000_00 || d["hC"] != 10_000_00 || d["hD"] != 10_000_00 {
		t.Fatalf("unexpected deltas: %+v", d)
	}
	var sum int64
	for _, v := range d {
		sum += v
	}
	if sum != 0 {
		t.Fatalf("sum=%d want 0", sum)
	}
}

func TestComputeReferencePackageDeltas_PKG2_SingleLine(t *testing.T) {
	result := ComputeReferencePackageDeltas([]PackageDeltaInput{
		{HoldingID: "hA", StructuralGapMinor: -30_000_00},
		{HoldingID: "hB", StructuralGapMinor: 0},
		{HoldingID: "hC", StructuralGapMinor: 0},
	})
	d := result.ByHoldingID
	if d["hA"] != -30_000_00 {
		t.Fatalf("hA=%d", d["hA"])
	}
	if d["hB"] != 0 || d["hC"] != 0 {
		t.Fatalf("expected zero gaps unchanged: %+v", d)
	}
}

func TestComputeReferencePackageDeltas_RoundingClosure(t *testing.T) {
	result := ComputeReferencePackageDeltas([]PackageDeltaInput{
		{HoldingID: "h1", StructuralGapMinor: 33_333_33},
		{HoldingID: "h2", StructuralGapMinor: -33_333_33},
		{HoldingID: "h3", StructuralGapMinor: -33_333_33},
	})
	var sum int64
	for _, v := range result.ByHoldingID {
		sum += v
	}
	if sum < -packageDeltaToleranceMinor || sum > packageDeltaToleranceMinor {
		t.Fatalf("sum=%d outside tolerance", sum)
	}
}

func TestRecommendedPlannedMinor(t *testing.T) {
	got := RecommendedPlannedMinor(150_000_00, -30_000_00)
	if got != 120_000_00 {
		t.Fatalf("got %d want 12000000", got)
	}
}
