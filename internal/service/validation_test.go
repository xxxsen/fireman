package service

import (
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestValidateParametersEnumWhitelist(t *testing.T) {
	base := repository.PlanParameters{
		CurrentAge: 30, RetirementAge: 55, EndAge: 90,
		TotalAssetsMinor: 1_000_000_00, AnnualSpendingMinor: 400_000_00,
		SimulationRuns: 10000, StudentTDf: 7,
		InflationMode: "fixed_real", WithdrawalType: "fixed_real",
	}
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
