package simulation

import "testing"

func TestPathMonthIncomeUsesRetirementIncomeOnlyAfterRetirement(t *testing.T) {
	p := SnapshotParameters{
		AnnualSavingsMinor:               120,
		AnnualSavingsGrowthRate:          0,
		AnnualRetirementIncomeMinor:      60,
		AnnualRetirementIncomeGrowthRate: 0.10,
	}
	in := &InputSnapshot{AggregateCashLiquidity: true}
	slots := []assetSlot{{isCash: true}}
	if got := pathMonthIncome(in, p, 11, 12, slots, 0); got != 10 {
		t.Fatalf("pre-retirement income = %d, want savings 10", got)
	}
	if got := pathMonthIncome(in, p, 12, 12, slots, 0); got != 5 {
		t.Fatalf("first retirement month income = %d, want 5", got)
	}
	if got := pathMonthIncome(in, p, 24, 12, slots, 0); got != 6 {
		t.Fatalf("second retirement year income = %d, want 6", got)
	}
}
