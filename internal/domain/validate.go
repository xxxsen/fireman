package domain

import (
	"fmt"
	"math"
)

// WeightCheck describes a single weight validation result.
type WeightCheck struct {
	Scope   string  `json:"scope"`
	Key     string  `json:"key,omitempty"`
	Actual  float64 `json:"actual"`
	Target  float64 `json:"target"`
	Passed  bool    `json:"passed"`
	Message string  `json:"message,omitempty"`
}

// WeightValidationResult aggregates all weight checks.
type WeightValidationResult struct {
	Passed bool          `json:"passed"`
	Checks []WeightCheck `json:"checks"`
}

func nearEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// ValidateAllocationWeights checks asset class and region weight sums.
func ValidateAllocationWeights(alloc AllocationWeights) WeightValidationResult {
	var checks []WeightCheck
	allPassed := true

	acSum := 0.0
	for _, ac := range AssetClasses {
		acSum += alloc.AssetClass[ac]
	}
	passed := nearEqual(acSum, 1.0, WeightTolerance)
	if !passed {
		allPassed = false
	}
	checks = append(checks, WeightCheck{
		Scope:   "asset_class",
		Actual:  acSum,
		Target:  1.0,
		Passed:  passed,
		Message: formatWeightGapMessage("大类目标权重", acSum, 1.0, passed),
	})

	for _, ac := range AssetClasses {
		acW := alloc.AssetClass[ac]
		if acW <= WeightTolerance {
			continue
		}
		regSum := 0.0
		if m, ok := alloc.Region[ac]; ok {
			for _, r := range Regions {
				regSum += m[r]
			}
		}
		rp := nearEqual(regSum, 1.0, WeightTolerance)
		if !rp {
			allPassed = false
		}
		checks = append(checks, WeightCheck{
			Scope:   "region",
			Key:     ac,
			Actual:  regSum,
			Target:  1.0,
			Passed:  rp,
			Message: formatWeightGapMessage(assetClassLabel(ac)+"地区权重", regSum, 1.0, rp),
		})
	}

	return WeightValidationResult{Passed: allPassed, Checks: checks}
}

// ValidateHoldingGroupWeights checks group weights and portfolio totals for enabled holdings.
func ValidateHoldingGroupWeights(alloc AllocationWeights, holdings []HoldingWeightInput) WeightValidationResult {
	var checks []WeightCheck
	allPassed := true

	type groupKey struct{ ac, region string }
	groups := make(map[groupKey][]HoldingWeightInput)
	for _, h := range holdings {
		if !h.Enabled {
			continue
		}
		k := groupKey{h.AssetClass, h.Region}
		groups[k] = append(groups[k], h)
	}

	for k, members := range groups {
		sum := 0.0
		for _, m := range members {
			sum += m.WeightWithinGroup
		}
		passed := nearEqual(sum, 1.0, WeightTolerance)
		if !passed {
			allPassed = false
		}
		checks = append(checks, WeightCheck{
			Scope:   "holding_group",
			Key:     k.ac + "/" + k.region,
			Actual:  sum,
			Target:  1.0,
			Passed:  passed,
			Message: formatWeightGapMessage(groupLabel(k.ac, k.region)+"组内占比", sum, 1.0, passed),
		})
	}

	enabledCount := 0
	portfolioSum := 0.0
	for _, h := range holdings {
		if !h.Enabled {
			continue
		}
		enabledCount++
		portfolioSum += PortfolioTargetWeight(alloc, h)
	}
	if enabledCount > 0 {
		pp := nearEqual(portfolioSum, 1.0, WeightTolerance)
		if !pp {
			allPassed = false
		}
		checks = append(checks, WeightCheck{
			Scope:   "portfolio",
			Actual:  portfolioSum,
			Target:  1.0,
			Passed:  pp,
			Message: formatWeightGapMessage("已启用标的全组合目标权重", portfolioSum, 1.0, pp),
		})
	}

	return WeightValidationResult{Passed: allPassed, Checks: checks}
}

// ValidateAllWeights runs allocation and holding validations.
func ValidateAllWeights(alloc AllocationWeights, holdings []HoldingWeightInput) WeightValidationResult {
	ac := ValidateAllocationWeights(alloc)
	hg := ValidateHoldingGroupWeights(alloc, holdings)
	checks := append(ac.Checks, hg.Checks...)
	return WeightValidationResult{
		Passed: ac.Passed && hg.Passed,
		Checks: checks,
	}
}

func formatWeightGapMessage(label string, actual, target float64, passed bool) string {
	if passed {
		return label + "合计 " + formatPercent(actual) + "，通过"
	}
	gap := target - actual
	return fmt.Sprintf("%s当前为 %s，还差 %s。请调整至 100%%。",
		label, formatPercent(actual), formatPercent(gap))
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.2f%%", v*100)
}

func assetClassLabel(ac string) string {
	switch ac {
	case AssetClassEquity:
		return "权益"
	case AssetClassBond:
		return "债券"
	case AssetClassCash:
		return "现金/其他"
	default:
		return ac
	}
}

func groupLabel(ac, region string) string {
	regionLabel := "国内"
	if region == RegionForeign {
		regionLabel = "国外"
	}
	return regionLabel + assetClassLabel(ac)
}
