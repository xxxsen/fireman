package simulation

import "testing"

func cashFailureInput(balance, annualSpending, terminalFloor int64) *InputSnapshot {
	return &InputSnapshot{
		EngineVersion:          EngineVersion,
		BaseCurrency:           "CNY",
		AggregateCashLiquidity: true,
		Parameters: SnapshotParameters{
			CurrentAge: 55, RetirementAge: 55, EndAge: 56,
			TotalAssetsMinor: balance, AnnualSpendingMinor: annualSpending,
			TerminalWealthFloorMinor: terminalFloor,
			InflationMode:            "fixed_real", WithdrawalType: "fixed_real",
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			StudentTDf: 7, Seed: "1",
		},
		Assets: []SnapshotAsset{{
			HoldingID: "cash", AssetKey: "SYS|cash||CNY", Currency: "CNY",
			AssetClass: "cash", IsCash: true, InitialMinor: balance, TargetWeight: 1,
		}},
	}
}

func TestFailureStatusInsufficientFunds(t *testing.T) {
	summary, _ := RunPath(cashFailureInput(100, 2400, 0), 0, PathRunOpts{})
	if summary.Succeeded || summary.FailureReason != FailureInsufficientFunds || summary.FailureMonth == nil || *summary.FailureMonth != 0 {
		t.Fatalf("unexpected failure status: %+v", summary)
	}
}

func TestFailureStatusWealthDepleted(t *testing.T) {
	summary, _ := RunPath(cashFailureInput(100, 1200, 0), 0, PathRunOpts{})
	if summary.Succeeded || summary.FailureReason != FailureWealthDepleted || summary.FailureMonth == nil || *summary.FailureMonth != 0 {
		t.Fatalf("unexpected failure status: %+v", summary)
	}
}

func TestFailureStatusTerminalFloorNotMet(t *testing.T) {
	summary, _ := RunPath(cashFailureInput(1000, 0, 2000), 0, PathRunOpts{})
	if summary.Succeeded || summary.FailureReason != FailureTerminalFloor || summary.FailureMonth != nil {
		t.Fatalf("unexpected terminal-floor status: %+v", summary)
	}
}
