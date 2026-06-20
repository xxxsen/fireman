package service

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/simulation"
)

func TestBuildFrozenFactorModelDomesticMix(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	assets := []simulation.SnapshotAsset{
		{
			HoldingID: "h1", AssetClass: "equity", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.06, AnnualVolatility: 0.18,
		},
		{
			HoldingID: "h2", AssetClass: "bond", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.03, AnnualVolatility: 0.05,
		},
		{HoldingID: "hc", AssetClass: "cash", Region: "domestic", Currency: "CNY", IsCash: true},
	}
	fm, refs, err := buildFrozenFactorModel(assets, "CNY", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected a factor model for risk assets")
	}
	if len(refs) != len(assets) {
		t.Fatalf("refs length %d != assets %d", len(refs), len(assets))
	}
	if len(fm.Factors) != 2 {
		t.Fatalf("expected 2 risk factors, got %d (%v)", len(fm.Factors), fm.Factors)
	}
	if refs[0].AssetFactorIndex != 0 || refs[1].AssetFactorIndex != 1 {
		t.Fatalf("unexpected asset factor refs: %+v", refs)
	}
	if refs[2].AssetFactorIndex != -1 {
		t.Fatalf("cash must have no asset factor, got %+v", refs[2])
	}
	if refs[0].FXFactorIndex != -1 {
		t.Fatalf("CNY asset must have no FX factor, got %+v", refs[0])
	}
	// equity:domestic vs bond:domestic prior is 0.15 in the system profile.
	if got := fm.Audit.RRaw[0][1]; math.Abs(got-0.15) > 1e-9 {
		t.Fatalf("raw correlation = %v, want 0.15", got)
	}
}

func TestBuildFrozenFactorModelAddsFXFactor(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	assets := []simulation.SnapshotAsset{
		{
			HoldingID: "h1", AssetClass: "equity", Region: "foreign", Currency: "USD",
			ModeledAnnualReturn: 0.065, AnnualVolatility: 0.16,
			FXSnapshotID: "fxhash", FXModeledReturn: 0.0, FXAnnualVolatility: 0.06,
		},
	}
	fm, refs, err := buildFrozenFactorModel(assets, "CNY", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected a factor model")
	}
	if len(fm.Factors) != 2 {
		t.Fatalf("expected asset + fx factor, got %d (%v)", len(fm.Factors), fm.Factors)
	}
	if refs[0].AssetFactorIndex != 0 || refs[0].FXFactorIndex != 1 {
		t.Fatalf("foreign asset must reference both asset and fx factors: %+v", refs[0])
	}
	// equity:foreign vs fx:USD:CNY prior is 0.15.
	if got := fm.Audit.RRaw[0][1]; math.Abs(got-0.15) > 1e-9 {
		t.Fatalf("asset/fx correlation = %v, want 0.15", got)
	}
}

func TestBuildFrozenFactorModelAllCashReturnsNil(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	assets := []simulation.SnapshotAsset{
		{HoldingID: "hc", AssetClass: "cash", Region: "domestic", Currency: "CNY", IsCash: true},
	}
	fm, refs, err := buildFrozenFactorModel(assets, "CNY", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm != nil || refs != nil {
		t.Fatalf("all-cash plan must keep the independent path, got fm=%v refs=%v", fm, refs)
	}
}

func TestBuildFrozenFactorModelUsesFrozenHistoryForCrossType(t *testing.T) {
	profile := assumptions.SystemDefaultProfile() // strength 36 months, eqD/bondD prior 0.15
	// 24 identical varying months for both factors => historical ρ = 1.
	months := map[string]float64{}
	for i := 0; i < 24; i++ {
		months[monthKey(2001+i/12, i%12+1)] = 0.01 * float64((i%5)-2)
	}
	assets := []simulation.SnapshotAsset{
		{
			HoldingID: "h1", AssetClass: "equity", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.06, AnnualVolatility: 0.18, Months: months,
		},
		{
			HoldingID: "h2", AssetClass: "bond", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.03, AnnualVolatility: 0.05, Months: months,
		},
	}
	fm, _, err := buildFrozenFactorModel(assets, "CNY", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected a factor model")
	}
	pk := simulation.PairKey(fm.Factors[0], fm.Factors[1])
	// λ = 36/(24+36) = 0.6; ρ = 0.4*1 + 0.6*0.15 = 0.49 — strictly between
	// historical (1) and prior (0.15), proving the frozen history is used.
	if got := fm.Audit.Lambda[pk]; math.Abs(got-0.6) > 1e-9 {
		t.Fatalf("lambda = %v, want 0.6", got)
	}
	if got := fm.Audit.RRaw[0][1]; math.Abs(got-0.49) > 1e-9 {
		t.Fatalf("shrunk correlation = %v, want 0.49", got)
	}
	if fm.Audit.PairMonths[pk] != 24 {
		t.Fatalf("pair months = %d, want 24", fm.Audit.PairMonths[pk])
	}
	for _, w := range fm.Audit.Warnings {
		if w == "correlation_prior_only" {
			t.Fatal("with 24 common months the pair must not be prior-only")
		}
	}
}

func TestBuildFrozenFactorModelSameTypeIsPerfectlyCorrelated(t *testing.T) {
	profile := assumptions.SystemDefaultProfile()
	assets := []simulation.SnapshotAsset{
		{
			HoldingID: "h1", AssetClass: "equity", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.06, AnnualVolatility: 0.18,
		},
		{
			HoldingID: "h2", AssetClass: "equity", Region: "domestic", Currency: "CNY",
			ModeledAnnualReturn: 0.06, AnnualVolatility: 0.18,
		},
	}
	fm, _, err := buildFrozenFactorModel(assets, "CNY", profile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm == nil {
		t.Fatal("expected a factor model")
	}
	if got := fm.Audit.RRaw[0][1]; math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("same-type holdings must be ρ=1, got %v", got)
	}
}
