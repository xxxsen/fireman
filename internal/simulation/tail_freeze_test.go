package simulation

import "testing"

// TestEffectiveDfPrefersFrozenProfileValue verifies the sampler reads the
// profile-frozen df for forward runs and falls back to the legacy plan parameter
// only when no frozen value is present (td/063 R3).
func TestEffectiveDfPrefersFrozenProfileValue(t *testing.T) {
	in := &InputSnapshot{Parameters: SnapshotParameters{StudentTDf: 7}}
	if got := in.EffectiveDf(); got != 7 {
		t.Fatalf("legacy snapshot should use plan df 7, got %d", got)
	}
	in.TailStudentTDf = 12
	if got := in.EffectiveDf(); got != 12 {
		t.Fatalf("forward snapshot should use frozen df 12, got %d", got)
	}
}

// TestTailTruncationBoundsPreferFrozenValues verifies forward runs clamp to the
// frozen profile band and legacy snapshots fall back to the engine constants.
func TestTailTruncationBoundsPreferFrozenValues(t *testing.T) {
	in := &InputSnapshot{}
	if b := in.TailTruncationBounds(); b.Floor != ReturnFloor || b.Ceil != ReturnCeil {
		t.Fatalf("legacy snapshot must use constants, got %+v", b)
	}
	floor, ceil := -0.5, 1.0
	in.TailReturnFloor = &floor
	in.TailReturnCeil = &ceil
	if b := in.TailTruncationBounds(); b.Floor != floor || b.Ceil != ceil {
		t.Fatalf("forward snapshot must use frozen band, got %+v", b)
	}
}

// TestFrozenTruncationActuallyClampsSampling verifies a tighter frozen band
// changes the truncation a run produces, proving the sampler reads frozen values
// rather than the package constants (td/063 R3 acceptance #3).
func TestFrozenTruncationActuallyClampsSampling(t *testing.T) {
	p := ParamsFromAnnual(0.08, 0.60) // high vol so tails are hit
	tight := TailTruncation{Floor: -0.05, Ceil: 0.05}
	loose := LegacyTailTruncation()

	tightTrunc := 0
	looseTrunc := 0
	for i := 0; i < 2000; i++ {
		rngA := NewRNG(int64(i + 1))
		rngB := NewRNG(int64(i + 1))
		if _, tr := SampleStudentT(rngA, p, 7, tight); tr {
			tightTrunc++
		}
		if _, tr := SampleStudentT(rngB, p, 7, loose); tr {
			looseTrunc++
		}
	}
	if tightTrunc <= looseTrunc {
		t.Fatalf("tighter frozen band must truncate more often: tight=%d loose=%d",
			tightTrunc, looseTrunc)
	}
}
