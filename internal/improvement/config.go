package improvement

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrConfigInvalid  = errors.New("improvement config invalid")
	ErrNoEnabledLever = errors.New("no improvement lever enabled")
)

//nolint:gocyclo // Each lever has an explicit independent validation contract.
func (c Config) Validate(baseEndAge, baseRetirementAge int) error {
	if math.IsNaN(c.TargetSuccessProbability) || math.IsInf(c.TargetSuccessProbability, 0) ||
		c.TargetSuccessProbability < 0.50 || c.TargetSuccessProbability > 0.99 {
		return fmt.Errorf("%w: target_success_probability must be within [0.50, 0.99]", ErrConfigInvalid)
	}
	enabled := false
	if c.RetirementDelay != nil {
		maxDelay := c.RetirementDelay.MaxDelayYears
		if maxDelay < 0 || maxDelay > 10 || baseRetirementAge+maxDelay >= baseEndAge {
			return fmt.Errorf("%w: retirement delay must be within plan age bounds", ErrConfigInvalid)
		}
		enabled = enabled || maxDelay > 0
	}
	if c.SavingsIncrease != nil {
		if err := validateMoney(c.SavingsIncrease.MaxIncreaseMinor, c.SavingsIncrease.StepMinor); err != nil {
			return err
		}
		enabled = enabled || c.SavingsIncrease.MaxIncreaseMinor > 0
	}
	if c.SpendingReduction != nil {
		if err := validateMoney(c.SpendingReduction.MaxReductionMinor, c.SpendingReduction.StepMinor); err != nil {
			return err
		}
		enabled = enabled || c.SpendingReduction.MaxReductionMinor > 0
	}
	if c.RetirementIncomeIncrease != nil {
		if err := validateMoney(c.RetirementIncomeIncrease.MaxIncreaseMinor,
			c.RetirementIncomeIncrease.StepMinor); err != nil {
			return err
		}
		enabled = enabled || c.RetirementIncomeIncrease.MaxIncreaseMinor > 0
	}
	if !enabled {
		return ErrNoEnabledLever
	}
	return nil
}

func validateMoney(maximum, step int64) error {
	if maximum <= 0 || step <= 0 || step > maximum {
		return fmt.Errorf("%w: money maximum and step must be positive and step <= maximum", ErrConfigInvalid)
	}
	levels := maximum / step
	if maximum%step != 0 {
		levels++
	}
	if levels > 100 {
		return fmt.Errorf("%w: money lever may contain at most 100 levels", ErrConfigInvalid)
	}
	return nil
}

func moneyLevels(maximum, step int64) []int64 {
	levels := make([]int64, 0, 100)
	for value := step; value < maximum; value += step {
		levels = append(levels, value)
	}
	return append(levels, maximum)
}
