package simulation

import (
	"testing"
)

func TestDerivePathSeedNonNegativeForRootOnePathZero(t *testing.T) {
	seed := DerivePathSeed(1, 0)
	if seed < 0 {
		t.Fatalf("expected non-negative seed, got %d", seed)
	}
	if seed == 0 {
		t.Fatal("expected non-zero derived seed for root=1 path=0")
	}
}

func TestDerivePathSeedRangeAcrossPaths(t *testing.T) {
	seen := make(map[int64]struct{}, 10000)
	for pathNo := 0; pathNo < 10000; pathNo++ {
		seed := DerivePathSeed(1, pathNo)
		if seed < 0 {
			t.Fatalf("path %d seed out of range: %d", pathNo, seed)
		}
		if seed == 0 {
			t.Fatalf("path %d collapsed to zero", pathNo)
		}
		seen[seed] = struct{}{}
	}
	if len(seen) != 10000 {
		t.Fatalf("expected unique seeds, got %d duplicates", 10000-len(seen))
	}
}

func TestDerivePathSeedDeterministic(t *testing.T) {
	a := DerivePathSeed(42, 7)
	b := DerivePathSeed(42, 7)
	if a != b {
		t.Fatalf("expected deterministic seed, got %d vs %d", a, b)
	}
}
