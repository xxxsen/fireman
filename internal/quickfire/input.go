// Package quickfire implements the stateless deterministic FIRE calculator.
package quickfire

import (
	"errors"
	"fmt"
	"math"
)

// EngineVersion identifies the calculator's frozen numerical contract.
const EngineVersion = "quick_fire_v1"

const (
	baseCurrencyCNY = "CNY"
	maxAssetsMinor  = int64(999_999_999_999_00)
	maxFlowMinor    = int64(99_999_999_999_00)
)

// Input is the complete request for one deterministic FIRE projection.
// Monetary amounts are CNY minor units and rates are decimal fractions.
type Input struct {
	BaseCurrency                     string  `json:"base_currency"`
	CurrentAge                       int     `json:"current_age"`
	PlannedFireAge                   int     `json:"planned_fire_age"`
	EndAge                           int     `json:"end_age"`
	CurrentAssetsMinor               int64   `json:"current_assets_minor"`
	AnnualSavingsMinor               int64   `json:"annual_savings_minor"`
	AnnualSavingsGrowthRate          float64 `json:"annual_savings_growth_rate"`
	AnnualSpendingMinor              int64   `json:"annual_spending_minor"`
	AnnualRetirementIncomeMinor      int64   `json:"annual_retirement_income_minor"`
	AnnualRetirementIncomeGrowthRate float64 `json:"annual_retirement_income_growth_rate"`
	AnnualReturnRate                 float64 `json:"annual_return_rate"`
	InflationRate                    float64 `json:"inflation_rate"`
	TerminalWealthFloorMinor         int64   `json:"terminal_wealth_floor_minor"`
}

// DefaultInput is the stable initial/reset form value.
func DefaultInput() Input {
	return Input{
		BaseCurrency:                     baseCurrencyCNY,
		CurrentAge:                       35,
		PlannedFireAge:                   45,
		EndAge:                           90,
		CurrentAssetsMinor:               300_0000_00,
		AnnualSavingsMinor:               12_0000_00,
		AnnualSavingsGrowthRate:          0,
		AnnualSpendingMinor:              12_0000_00,
		AnnualRetirementIncomeMinor:      3_0000_00,
		AnnualRetirementIncomeGrowthRate: 0,
		AnnualReturnRate:                 0.04,
		InflationRate:                    0.02,
		TerminalWealthFloorMinor:         0,
	}
}

// ValidationError identifies input fields that violate the public contract.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "quick fire parameters invalid" }

// ErrResultOutOfRange means a valid request grew beyond representable minor units.
var ErrResultOutOfRange = errors.New("quick fire result out of range")

// Validate checks all public input bounds before numerical work starts.
func (in Input) Validate() error {
	fields := map[string]string{}
	if in.BaseCurrency != baseCurrencyCNY {
		fields["base_currency"] = "必须为 CNY"
	}
	if in.CurrentAge < 18 || in.CurrentAge > 120 {
		fields["current_age"] = "必须在 18 到 120 之间"
	}
	if in.PlannedFireAge < in.CurrentAge || in.PlannedFireAge >= in.EndAge {
		fields["planned_fire_age"] = "必须大于等于当前年龄且小于目标年龄"
	}
	if in.EndAge <= in.PlannedFireAge || in.EndAge > 120 {
		fields["end_age"] = "必须大于计划 FIRE 年龄且不超过 120"
	}
	validateAmount(fields, "current_assets_minor", in.CurrentAssetsMinor, maxAssetsMinor, false)
	validateAmount(fields, "annual_savings_minor", in.AnnualSavingsMinor, maxFlowMinor, false)
	validateAmount(fields, "annual_spending_minor", in.AnnualSpendingMinor, maxFlowMinor, true)
	validateAmount(fields, "annual_retirement_income_minor", in.AnnualRetirementIncomeMinor, maxFlowMinor, false)
	validateAmount(fields, "terminal_wealth_floor_minor", in.TerminalWealthFloorMinor, maxAssetsMinor, false)
	validateRate(fields, "annual_savings_growth_rate", in.AnnualSavingsGrowthRate, -0.5, 0.5)
	validateRate(fields, "annual_retirement_income_growth_rate", in.AnnualRetirementIncomeGrowthRate, -0.5, 0.5)
	validateRate(fields, "annual_return_rate", in.AnnualReturnRate, -0.99, 1)
	validateRate(fields, "inflation_rate", in.InflationRate, -0.02, 0.2)
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateAmount(fields map[string]string, field string, value, maximum int64, strictlyPositive bool) {
	if value < 0 || value > maximum || (strictlyPositive && value == 0) {
		condition := fmt.Sprintf("必须在 0 到 %d 之间", maximum)
		if strictlyPositive {
			condition = fmt.Sprintf("必须大于 0 且不超过 %d", maximum)
		}
		fields[field] = condition
	}
}

func validateRate(fields map[string]string, field string, value, minimum, maximum float64) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < minimum || value > maximum {
		fields[field] = fmt.Sprintf("必须在 %.2f 到 %.2f 之间", minimum, maximum)
	}
}
