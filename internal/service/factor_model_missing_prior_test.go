package service

import (
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/simulation"
)

// TestBuildFrozenFactorModelMissingPriorBlocks verifies that a cross-type
// factor pair with no correlation prior must fail the build instead of silently
// becoming ρ=0.
func TestBuildFrozenFactorModelMissingPriorBlocks(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	// Drop the equity:domestic / bond:domestic correlation prior so the pair has
	// no prior at run time.
	eqD := assumptions.AssetFactorKey("equity", "domestic")
	bdD := assumptions.AssetFactorKey("bond", "domestic")
	kept := profile.CorrelationPriors[:0]
	for _, c := range profile.CorrelationPriors {
		if (c.FactorA == eqD && c.FactorB == bdD) || (c.FactorA == bdD && c.FactorB == eqD) {
			continue
		}
		kept = append(kept, c)
	}
	profile.CorrelationPriors = kept

	assets := []simulation.SnapshotAsset{
		{
			HoldingID: "h1", AssetClass: "equity", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.06, AnnualVolatility: 0.18,
		},
		{
			HoldingID: "h2", AssetClass: "bond", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.03, AnnualVolatility: 0.05,
		},
	}
	fm, refs, err := buildFrozenFactorModel(assets, "CNY", profile)
	if !errors.Is(err, errFactorCorrelationMissing) {
		t.Fatalf("expected missing-correlation error, got fm=%v refs=%v err=%v", fm, refs, err)
	}
}
