package simulation

import "testing"

func TestMedianInt64_EvenLengthAveragesMiddlePairWithIntegerTruncation(t *testing.T) {
	if got := medianInt64([]int64{1, 2, 3, 4}); got != 2 {
		t.Fatalf("median([1,2,3,4]) = %d, want 2 ((2+3)/2 truncated)", got)
	}
	if got := medianInt64([]int64{1, 3}); got != 2 {
		t.Fatalf("median([1,3]) = %d, want 2", got)
	}
}

func TestMedianInt64_OddLengthReturnsMiddleElement(t *testing.T) {
	if got := medianInt64([]int64{3, 1, 2}); got != 2 {
		t.Fatalf("median([3,1,2]) = %d, want 2", got)
	}
}

func TestMedianInt64_EmptyReturnsZero(t *testing.T) {
	if got := medianInt64(nil); got != 0 {
		t.Fatalf("median(nil) = %d, want 0", got)
	}
}
