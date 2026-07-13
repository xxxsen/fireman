package improvement

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

var ErrAdjustmentInvalid = errors.New("improvement adjustment invalid")

func ApplyAdjustments(base simulation.InputSnapshot, a Adjustments) (simulation.InputSnapshot, error) {
	if a.DelayYears < 0 || a.SavingsIncreaseMinor < 0 || a.SpendingReductionMinor < 0 ||
		a.RetirementIncomeIncreaseMinor < 0 {
		return simulation.InputSnapshot{}, ErrAdjustmentInvalid
	}
	raw, err := json.Marshal(base)
	if err != nil {
		return simulation.InputSnapshot{}, fmt.Errorf("clone snapshot: %w", err)
	}
	var out simulation.InputSnapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return simulation.InputSnapshot{}, fmt.Errorf("clone snapshot: %w", err)
	}
	p := &out.Parameters
	if a.DelayYears > math.MaxInt-p.RetirementAge || p.RetirementAge+a.DelayYears >= p.EndAge {
		return simulation.InputSnapshot{}, fmt.Errorf("%w: retirement age outside horizon", ErrAdjustmentInvalid)
	}
	if addOverflows(p.AnnualSavingsMinor, a.SavingsIncreaseMinor) ||
		addOverflows(p.AnnualRetirementIncomeMinor, a.RetirementIncomeIncreaseMinor) {
		return simulation.InputSnapshot{}, fmt.Errorf("%w: amount overflow", ErrAdjustmentInvalid)
	}
	if a.SpendingReductionMinor >= p.AnnualSpendingMinor {
		return simulation.InputSnapshot{}, fmt.Errorf("%w: annual spending must stay positive", ErrAdjustmentInvalid)
	}
	p.RetirementAge += a.DelayYears
	p.AnnualSavingsMinor += a.SavingsIncreaseMinor
	p.AnnualSpendingMinor -= a.SpendingReductionMinor
	p.AnnualRetirementIncomeMinor += a.RetirementIncomeIncreaseMinor
	return out, nil
}

func ApplyConfigAdjustments(base domain.ConfigHashInput, a Adjustments) (domain.ConfigHashInput, error) {
	out := cloneConfigHashInput(base)
	retirementAge, ok := integerValue(out.Parameters["retirement_age"])
	if !ok {
		return domain.ConfigHashInput{}, fmt.Errorf("%w: retirement_age missing", ErrAdjustmentInvalid)
	}
	savings, ok := int64Value(out.Parameters["annual_savings_minor"])
	if !ok {
		return domain.ConfigHashInput{}, fmt.Errorf("%w: annual_savings_minor missing", ErrAdjustmentInvalid)
	}
	spending, ok := int64Value(out.Parameters["annual_spending_minor"])
	if !ok {
		return domain.ConfigHashInput{}, fmt.Errorf("%w: annual_spending_minor missing", ErrAdjustmentInvalid)
	}
	income, ok := int64Value(out.Parameters["annual_retirement_income_minor"])
	if !ok {
		return domain.ConfigHashInput{}, fmt.Errorf("%w: annual_retirement_income_minor missing", ErrAdjustmentInvalid)
	}
	if addOverflows(savings, a.SavingsIncreaseMinor) || addOverflows(income, a.RetirementIncomeIncreaseMinor) ||
		a.SpendingReductionMinor >= spending {
		return domain.ConfigHashInput{}, ErrAdjustmentInvalid
	}
	out.Parameters["retirement_age"] = retirementAge + a.DelayYears
	out.Parameters["annual_savings_minor"] = savings + a.SavingsIncreaseMinor
	out.Parameters["annual_spending_minor"] = spending - a.SpendingReductionMinor
	out.Parameters["annual_retirement_income_minor"] = income + a.RetirementIncomeIncreaseMinor
	return out, nil
}

func cloneConfigHashInput(in domain.ConfigHashInput) domain.ConfigHashInput {
	out := in
	out.Parameters = cloneAnyMap(in.Parameters)
	out.AssetClass = cloneMapSlice(in.AssetClass)
	out.RegionTargets = cloneMapSlice(in.RegionTargets)
	out.Holdings = cloneMapSlice(in.Holdings)
	return out
}

func cloneMapSlice(in []map[string]any) []map[string]any {
	out := make([]map[string]any, len(in))
	for i := range in {
		out[i] = cloneAnyMap(in[i])
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneAnyMap(typed)
		case []map[string]any:
			out[key] = cloneMapSlice(typed)
		case []any:
			out[key] = append([]any(nil), typed...)
		default:
			out[key] = value
		}
	}
	return out
}

func addOverflows(left, right int64) bool { return right > 0 && left > math.MaxInt64-right }

func integerValue(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), int64(int(n)) == n
	case float64:
		return int(n), n == float64(int(n))
	case json.Number:
		value, err := n.Int64()
		return int(value), err == nil && int64(int(value)) == value
	default:
		return 0, false
	}
}

func int64Value(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), n == float64(int64(n))
	case json.Number:
		value, err := n.Int64()
		return value, err == nil
	default:
		return 0, false
	}
}
