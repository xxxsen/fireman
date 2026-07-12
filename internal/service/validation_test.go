package service

import (
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func validParametersFixture() repository.PlanParameters {
	return repository.PlanParameters{
		CurrentAge: 30, RetirementAge: 55, EndAge: 90,
		TotalAssetsMinor: 1_000_000_00, AnnualSpendingMinor: 400_000_00,
		SimulationRuns: 10000, StudentTDf: 7,
		InflationMode: "fixed_real", FixedInflationRate: 0.03,
		InflationMu: 0.03, InflationPhi: 0.5, InflationSigma: 0.01,
		WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
		WithdrawalFloorRatio: 0.70, WithdrawalCeilingRatio: 1.30,
		WithdrawalTaxRate: 0, TaxableWithdrawalRatio: 0,
		RebalanceFrequency: "annual",
	}
}

func TestValidateTransactionCostAndRebalanceFrequency(t *testing.T) {
	for _, rate := range []float64{-0.01, 1, 1.2} {
		p := validParametersFixture()
		p.TransactionCostRate = rate
		if err := validateParameters(p); !errors.Is(err, errTransactionCostRateRange) {
			t.Fatalf("rate %v expected transaction cost range error, got %v", rate, err)
		}
	}
	for _, rate := range []float64{0, 0.999999} {
		p := validParametersFixture()
		p.TransactionCostRate = rate
		if err := validateParameters(p); err != nil {
			t.Fatalf("rate %v should be valid: %v", rate, err)
		}
	}
	p := validParametersFixture()
	p.RebalanceFrequency = "weekly"
	if err := validateParameters(p); !errors.Is(err, errRebalanceFrequencyInvalid) {
		t.Fatalf("expected rebalance frequency error, got %v", err)
	}
}

func TestValidateParametersAdvancedRanges(t *testing.T) {
	mutate := func(f func(p *repository.PlanParameters)) repository.PlanParameters {
		p := validParametersFixture()
		f(&p)
		return p
	}
	// Doc-defined boundaries must be rejected.
	invalid := map[string]repository.PlanParameters{
		"fixed_inflation high": mutate(func(p *repository.PlanParameters) { p.FixedInflationRate = 0.25 }),
		"fixed_inflation low":  mutate(func(p *repository.PlanParameters) { p.FixedInflationRate = -0.05 }),
		"inflation_mu high": mutate(func(p *repository.PlanParameters) {
			p.InflationMode = "random_ar1"
			p.InflationMu = 0.30
		}),
		"inflation_sigma neg": mutate(func(p *repository.PlanParameters) {
			p.InflationMode = "random_ar1"
			p.InflationSigma = -0.01
		}),
		"inflation_sigma high": mutate(func(p *repository.PlanParameters) {
			p.InflationMode = "random_ar1"
			p.InflationSigma = 0.30
		}),
		"inflation_phi high": mutate(func(p *repository.PlanParameters) {
			p.InflationMode = "random_ar1"
			p.InflationPhi = 1.2
		}),
		"withdrawal_rate high": mutate(func(p *repository.PlanParameters) {
			p.WithdrawalType = "fixed_portfolio"
			p.WithdrawalRate = 1.5
		}),
		"floor zero": mutate(func(p *repository.PlanParameters) {
			p.WithdrawalType = "guardrail"
			p.WithdrawalFloorRatio = 0
		}),
		"floor above one": mutate(func(p *repository.PlanParameters) {
			p.WithdrawalType = "guardrail"
			p.WithdrawalFloorRatio = 1.1
		}),
		"ceiling below one": mutate(func(p *repository.PlanParameters) {
			p.WithdrawalType = "guardrail"
			p.WithdrawalCeilingRatio = 0.9
		}),
		"ceiling above two": mutate(func(p *repository.PlanParameters) {
			p.WithdrawalType = "guardrail"
			p.WithdrawalCeilingRatio = 2.5
		}),
		"tax rate high":          mutate(func(p *repository.PlanParameters) { p.WithdrawalTaxRate = 1.2 }),
		"taxable ratio high":     mutate(func(p *repository.PlanParameters) { p.TaxableWithdrawalRatio = 1.2 }),
		"tax product equals one": mutate(func(p *repository.PlanParameters) { p.WithdrawalTaxRate = 1; p.TaxableWithdrawalRatio = 1 }),
	}
	for name, p := range invalid {
		if err := validateParameters(p); err == nil {
			t.Fatalf("expected invalid for %s", name)
		}
	}
	// 0.2 * 0.8 = 0.16 < 1 must pass.
	ok := mutate(func(p *repository.PlanParameters) { p.WithdrawalTaxRate = 0.2; p.TaxableWithdrawalRatio = 0.8 })
	if err := validateParameters(ok); err != nil {
		t.Fatalf("expected valid tax product: %v", err)
	}
}

func TestValidateParametersIgnoresInactiveModeFields(t *testing.T) {
	p := validParametersFixture()
	p.InflationMu = 99
	p.InflationPhi = 99
	p.InflationSigma = -99
	p.WithdrawalRate = 99
	p.WithdrawalFloorRatio = -99
	p.WithdrawalCeilingRatio = -99
	if err := validateParameters(p); err != nil {
		t.Fatalf("inactive fixed-real fields must not block save: %v", err)
	}

	p.InflationMode = "random_ar1"
	p.FixedInflationRate = 99
	p.InflationMu = 0.03
	p.InflationPhi = 0.5
	p.InflationSigma = 0.01
	p.WithdrawalType = "guardrail"
	p.WithdrawalRate = 99
	p.WithdrawalFloorRatio = 0.7
	p.WithdrawalCeilingRatio = 1.3
	if err := validateParameters(p); err != nil {
		t.Fatalf("inactive random/guardrail fields must not block save: %v", err)
	}
}

func TestValidateParametersEnumWhitelist(t *testing.T) {
	base := validParametersFixture()
	valid := []struct {
		withdrawal, inflation string
	}{
		{"fixed_real", "fixed_real"},
		{"fixed_portfolio", "fixed_real"},
		{"guardrail", "fixed_real"},
		{"fixed_real", "random_ar1"},
		{"fixed_portfolio", "random_ar1"},
	}
	for _, tc := range valid {
		p := base
		p.WithdrawalType = tc.withdrawal
		p.InflationMode = tc.inflation
		if err := validateParameters(p); err != nil {
			t.Fatalf("valid %s/%s: %v", tc.withdrawal, tc.inflation, err)
		}
	}
	invalid := []struct {
		withdrawal, inflation string
	}{
		{"percent_of_portfolio", "fixed_real"},
		{"fixed_real", "random"},
		{"unknown", "fixed_real"},
		{"fixed_real", "unknown"},
	}
	for _, tc := range invalid {
		p := base
		p.WithdrawalType = tc.withdrawal
		p.InflationMode = tc.inflation
		if err := validateParameters(p); err == nil {
			t.Fatalf("expected invalid for %s/%s", tc.withdrawal, tc.inflation)
		}
	}
}
