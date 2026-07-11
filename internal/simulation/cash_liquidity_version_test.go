package simulation

import (
	"math"
	"testing"
)

func TestAggregateCashLiquidityIsVersionedBySnapshotField(t *testing.T) {
	legacySlots := []assetSlot{
		{isCash: true, balance: 100},
		{isCash: true, balance: 200},
		{balance: 700},
	}
	legacy := &InputSnapshot{EngineVersion: "3.1.0", AggregateCashLiquidity: false}
	ok, legacyCost := withdrawPathAmount(legacy, legacySlots, 1, 250, 0.01)
	if !ok || legacyCost == 0 || legacySlots[0].balance >= 100 {
		t.Fatalf("legacy single-cash semantics changed: ok=%v cost=%d slots=%+v", ok, legacyCost, legacySlots)
	}

	currentSlots := []assetSlot{
		{isCash: true, balance: 100},
		{isCash: true, balance: 200},
		{balance: 700},
	}
	current := &InputSnapshot{EngineVersion: EngineVersion, AggregateCashLiquidity: true}
	ok, currentCost := withdrawPathAmount(current, currentSlots, 1, 250, 0.01)
	if !ok || currentCost != 0 || math.Abs(currentSlots[0].balance-100.0/6) > 1e-9 ||
		math.Abs(currentSlots[1].balance-100.0/3) > 1e-9 {
		t.Fatalf("current aggregate cash semantics wrong: ok=%v cost=%d slots=%+v", ok, currentCost, currentSlots)
	}
}

func TestFailureLabelsAreVersionedInSimulationPackage(t *testing.T) {
	legacy := &InputSnapshot{EngineVersion: "3.1.0"}
	if got := pathFailureReason(legacy, FailureInsufficientFunds, 1, 0, 360, 1); got != FailureEarlySequence {
		t.Fatalf("legacy failure label = %q", got)
	}
	current := &InputSnapshot{EngineVersion: EngineVersion}
	if got := pathFailureReason(current, FailureInsufficientFunds, 1, 0, 360, 1); got != FailureInsufficientFunds {
		t.Fatalf("current failure status = %q", got)
	}
}
