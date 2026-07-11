package quickfire

import (
	"fmt"
	"math"
)

// OutcomeStatus is a fact-based deterministic projection outcome.
type OutcomeStatus string

const (
	OutcomeSustainable         OutcomeStatus = "sustainable"
	OutcomeInsufficientFunds   OutcomeStatus = "insufficient_funds"
	OutcomeWealthDepleted      OutcomeStatus = "wealth_depleted"
	OutcomeTerminalFloorNotMet OutcomeStatus = "terminal_floor_not_met"
)

// Year is one annual ledger row. Amounts are CNY minor units.
type Year struct {
	Age                 int    `json:"age"`
	MonthsInPeriod      int    `json:"months_in_period"`
	Phase               string `json:"phase"`
	StartWealthMinor    int64  `json:"start_wealth_minor"`
	IncomeMinor         int64  `json:"income_minor"`
	SpendingMinor       int64  `json:"spending_minor"`
	InvestmentGainMinor int64  `json:"investment_gain_minor"`
	EndWealthMinor      int64  `json:"end_wealth_minor"`
	RealEndWealthMinor  int64  `json:"real_end_wealth_minor"`
	RequiredWealthMinor int64  `json:"required_wealth_minor"`
}

// Result is a full deterministic projection response.
type Result struct {
	EngineVersion              string        `json:"engine_version"`
	BaseCurrency               string        `json:"base_currency"`
	OutcomeStatus              OutcomeStatus `json:"outcome_status"`
	SustainableThroughEndAge   bool          `json:"sustainable_through_end_age"`
	ProjectedAssetsAtFireMinor int64         `json:"projected_assets_at_fire_minor"`
	RequiredAssetsAtFireMinor  int64         `json:"required_assets_at_fire_minor"`
	FireFundingGapMinor        int64         `json:"fire_funding_gap_minor"`
	SupportMonthsAfterFire     int           `json:"support_months_after_fire"`
	DepletionMonthOffset       *int          `json:"depletion_month_offset,omitempty"`
	DepletionAgeYears          *int          `json:"depletion_age_years,omitempty"`
	DepletionAgeMonths         *int          `json:"depletion_age_months,omitempty"`
	UnfundedSpendingMinor      int64         `json:"unfunded_spending_minor"`
	TerminalWealthMinor        int64         `json:"terminal_wealth_minor"`
	TerminalWealthFloorMinor   int64         `json:"terminal_wealth_floor_minor"`
	RealTerminalWealthMinor    int64         `json:"real_terminal_wealth_minor"`
	RealAnnualReturnRate       float64       `json:"real_annual_return_rate"`
	EarliestFireMonthOffset    *int          `json:"earliest_fire_month_offset,omitempty"`
	EarliestFireAgeYears       *int          `json:"earliest_fire_age_years,omitempty"`
	EarliestFireAgeMonths      *int          `json:"earliest_fire_age_months,omitempty"`
	Years                      []Year        `json:"years"`
}

type monthlyCashFlow struct {
	income   int64
	spending int64
}

type projectionState struct {
	terminal      float64
	status        OutcomeStatus
	supportMonths int
	depletion     *int
	unfunded      int64
	years         []Year
}

// Calculate runs the versioned deterministic monthly projection.
//
//nolint:gocyclo,funlen // result assembly is deliberately kept beside its public contract.
func Calculate(in Input) (Result, error) {
	if err := in.Validate(); err != nil {
		return Result{}, err
	}
	monthlyReturn := annualToMonthly(in.AnnualReturnRate)
	monthlyInflation := annualToMonthly(in.InflationRate)
	if !finite(monthlyReturn) || !finite(monthlyInflation) {
		return Result{}, ErrResultOutOfRange
	}
	horizon := (in.EndAge - in.CurrentAge) * 12
	plannedRetire := (in.PlannedFireAge - in.CurrentAge) * 12

	accumulated, err := accumulationStarts(in, horizon, monthlyReturn)
	if err != nil {
		return Result{}, err
	}
	requiredAtFire, err := requiredAssets(in, plannedRetire, horizon, monthlyReturn, monthlyInflation)
	if err != nil {
		return Result{}, err
	}
	projectedAtFire := accumulated[plannedRetire]
	state, err := project(
		in, plannedRetire, horizon, float64(in.CurrentAssetsMinor), monthlyReturn, monthlyInflation, true,
	)
	if err != nil {
		return Result{}, err
	}

	projectedMinor, err := roundMinor(projectedAtFire)
	if err != nil {
		return Result{}, err
	}
	requiredMinor, err := roundMinor(requiredAtFire)
	if err != nil {
		return Result{}, err
	}
	gap, err := safeSub(projectedMinor, requiredMinor)
	if err != nil {
		return Result{}, err
	}
	terminal, err := roundMinor(state.terminal)
	if err != nil {
		return Result{}, err
	}
	realTerminal, err := roundMinor(state.terminal / math.Pow(1+monthlyInflation, float64(horizon)))
	if err != nil {
		return Result{}, err
	}
	earliestMonth, err := earliestFireMonth(in, horizon, accumulated, monthlyReturn, monthlyInflation)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		EngineVersion:              EngineVersion,
		BaseCurrency:               in.BaseCurrency,
		OutcomeStatus:              state.status,
		SustainableThroughEndAge:   state.status == OutcomeSustainable,
		ProjectedAssetsAtFireMinor: projectedMinor,
		RequiredAssetsAtFireMinor:  requiredMinor,
		FireFundingGapMinor:        gap,
		SupportMonthsAfterFire:     state.supportMonths,
		UnfundedSpendingMinor:      state.unfunded,
		TerminalWealthMinor:        terminal,
		TerminalWealthFloorMinor:   in.TerminalWealthFloorMinor,
		RealTerminalWealthMinor:    realTerminal,
		RealAnnualReturnRate:       (1+in.AnnualReturnRate)/(1+in.InflationRate) - 1,
		Years:                      state.years,
	}
	if !finite(result.RealAnnualReturnRate) {
		return Result{}, ErrResultOutOfRange
	}
	if state.depletion != nil {
		result.DepletionMonthOffset = state.depletion
		years, months := ageAtMonthEnd(in.CurrentAge, *state.depletion)
		result.DepletionAgeYears = &years
		result.DepletionAgeMonths = &months
	}
	if earliestMonth != nil {
		result.EarliestFireMonthOffset = earliestMonth
		years, months := ageAtMonthStart(in.CurrentAge, *earliestMonth)
		result.EarliestFireAgeYears = &years
		result.EarliestFireAgeMonths = &months
	}
	return result, nil
}

func annualToMonthly(rate float64) float64 {
	return math.Pow(1+rate, 1.0/12) - 1
}

func accumulationStarts(in Input, horizon int, monthlyReturn float64) ([]float64, error) {
	starts := make([]float64, horizon+1)
	wealth := float64(in.CurrentAssetsMinor)
	starts[0] = wealth
	for month := 0; month < horizon; month++ {
		flow, err := cashFlow(in, month, horizon+1, 0)
		if err != nil {
			return nil, err
		}
		wealth = (wealth + float64(flow.income)) * (1 + monthlyReturn)
		if !finite(wealth) || math.Abs(wealth) > float64(math.MaxInt64) {
			return nil, ErrResultOutOfRange
		}
		starts[month+1] = wealth
	}
	return starts, nil
}

func requiredAssets(in Input, retireMonth, horizon int, monthlyReturn, monthlyInflation float64) (float64, error) {
	required := float64(maxInt64(in.TerminalWealthFloorMinor, 1))
	for month := horizon - 1; month >= retireMonth; month-- {
		flow, err := retirementCashFlow(in, month, retireMonth, monthlyInflation)
		if err != nil {
			return 0, err
		}
		required = required/(1+monthlyReturn) + float64(flow.spending-flow.income)
		if !finite(required) || math.Abs(required) > float64(math.MaxInt64) {
			return 0, ErrResultOutOfRange
		}
		if required < 0 {
			required = 0
		}
	}
	return required, nil
}

func earliestFireMonth(
	in Input, horizon int, accumulated []float64, monthlyReturn, monthlyInflation float64,
) (*int, error) {
	for month := 0; month < horizon; month++ {
		required, err := requiredAssets(in, month, horizon, monthlyReturn, monthlyInflation)
		if err != nil {
			return nil, err
		}
		if accumulated[month]+0.5 >= required {
			candidate := month
			return &candidate, nil
		}
	}
	return nil, nil //nolint:nilnil // nil means no candidate can FIRE before the target age.
}

//nolint:gocognit,gocyclo,funlen // the loop mirrors the documented monthly settlement and failure states.
func project(
	in Input, retireMonth, horizon int, startingWealth, monthlyReturn, monthlyInflation float64,
	collectYears bool,
) (projectionState, error) {
	state := projectionState{terminal: startingWealth, status: OutcomeSustainable}
	wealth := startingWealth
	var current *yearAccumulator
	for month := 0; month < horizon; month++ {
		if collectYears && (current == nil || month%12 == 0) { //nolint:nestif // annual ledger initialization.
			required, err := requiredAssets(in, month, horizon, monthlyReturn, monthlyInflation)
			if err != nil {
				return projectionState{}, err
			}
			start, err := roundMinor(wealth)
			if err != nil {
				return projectionState{}, err
			}
			requiredMinor, err := roundMinor(required)
			if err != nil {
				return projectionState{}, err
			}
			phase := "accumulation"
			if month >= retireMonth {
				phase = "retirement"
			}
			current = &yearAccumulator{month: month, start: start, required: requiredMinor, phase: phase}
		}
		flow, err := cashFlow(in, month, retireMonth, monthlyInflation)
		if err != nil {
			return projectionState{}, err
		}
		preReturn := wealth + float64(flow.income) - float64(flow.spending)
		if month >= retireMonth && preReturn < 0 { //nolint:nestif // insufficient-funds is a terminal state.
			state.status = OutcomeInsufficientFunds
			state.unfunded, err = roundMinor(-preReturn)
			if err != nil {
				return projectionState{}, err
			}
			depletion := month
			state.depletion = &depletion
			state.terminal = 0
			if collectYears && current != nil {
				current.income += flow.income
				current.spending += flow.spending - state.unfunded
				year, finishErr := current.finish(in, monthlyInflation, month, 0)
				if finishErr != nil {
					return projectionState{}, finishErr
				}
				state.years = append(state.years, year)
			}
			return state, nil
		}
		end := preReturn * (1 + monthlyReturn)
		if !finite(end) || math.Abs(end) > float64(math.MaxInt64) {
			return projectionState{}, ErrResultOutOfRange
		}
		if collectYears && current != nil {
			current.income += flow.income
			current.spending += flow.spending
		}
		if month >= retireMonth {
			state.supportMonths++
		}
		wealth = end
		state.terminal = wealth
		if month >= retireMonth && math.Round(wealth) <= 0 {
			state.status = OutcomeWealthDepleted
			depletion := month
			state.depletion = &depletion
			if collectYears && current != nil {
				year, finishErr := current.finish(in, monthlyInflation, month, wealth)
				if finishErr != nil {
					return projectionState{}, finishErr
				}
				state.years = append(state.years, year)
			}
			return state, nil
		}
		if collectYears && current != nil && (month%12 == 11 || month == horizon-1) {
			year, finishErr := current.finish(in, monthlyInflation, month, wealth)
			if finishErr != nil {
				return projectionState{}, finishErr
			}
			state.years = append(state.years, year)
			current = nil
		}
	}
	if math.Round(wealth) < float64(in.TerminalWealthFloorMinor) {
		state.status = OutcomeTerminalFloorNotMet
	}
	return state, nil
}

func cashFlow(in Input, month, retirementMonth int, monthlyInflation float64) (monthlyCashFlow, error) {
	if month < retirementMonth {
		year := month / 12
		income, err := roundMinor(
			float64(in.AnnualSavingsMinor) * math.Pow(1+in.AnnualSavingsGrowthRate, float64(year)) / 12,
		)
		return monthlyCashFlow{income: income}, err
	}
	return retirementCashFlow(in, month, retirementMonth, monthlyInflation)
}

func retirementCashFlow(in Input, month, retirementMonth int, monthlyInflation float64) (monthlyCashFlow, error) {
	retiredYear := (month - retirementMonth) / 12
	income, err := roundMinor(
		float64(in.AnnualRetirementIncomeMinor) *
			math.Pow(1+in.AnnualRetirementIncomeGrowthRate, float64(retiredYear)) / 12,
	)
	if err != nil {
		return monthlyCashFlow{}, err
	}
	spending, err := roundMinor(float64(in.AnnualSpendingMinor) * math.Pow(1+monthlyInflation, float64(month)) / 12)
	if err != nil {
		return monthlyCashFlow{}, err
	}
	return monthlyCashFlow{income: income, spending: spending}, nil
}

type yearAccumulator struct {
	month    int
	start    int64
	required int64
	phase    string
	income   int64
	spending int64
}

func (a *yearAccumulator) finish(in Input, monthlyInflation float64, lastMonth int, endWealth float64) (Year, error) {
	end, err := roundMinor(endWealth)
	if err != nil {
		return Year{}, err
	}
	gain, err := ledgerGain(end, a.start, a.income, a.spending)
	if err != nil {
		return Year{}, err
	}
	realWealth, err := roundMinor(endWealth / math.Pow(1+monthlyInflation, float64(lastMonth+1)))
	if err != nil {
		return Year{}, err
	}
	return Year{
		Age:                 in.CurrentAge + a.month/12,
		MonthsInPeriod:      lastMonth - a.month + 1,
		Phase:               a.phase,
		StartWealthMinor:    a.start,
		IncomeMinor:         a.income,
		SpendingMinor:       a.spending,
		InvestmentGainMinor: gain,
		EndWealthMinor:      end,
		RealEndWealthMinor:  realWealth,
		RequiredWealthMinor: a.required,
	}, nil
}

func ledgerGain(end, start, income, spending int64) (int64, error) {
	value := float64(end) - float64(start) - float64(income) + float64(spending)
	return roundMinor(value)
}

func roundMinor(value float64) (int64, error) {
	if !finite(value) || value > float64(math.MaxInt64)-0.5 || value < float64(math.MinInt64)+0.5 {
		return 0, ErrResultOutOfRange
	}
	return int64(math.Round(value)), nil
}

func safeSub(left, right int64) (int64, error) {
	if (right > 0 && left < math.MinInt64+right) || (right < 0 && left > math.MaxInt64+right) {
		return 0, ErrResultOutOfRange
	}
	return left - right, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func ageAtMonthStart(currentAge, offset int) (int, int) {
	total := currentAge*12 + offset
	return total / 12, total % 12
}

func ageAtMonthEnd(currentAge, offset int) (int, int) {
	return ageAtMonthStart(currentAge, offset+1)
}

func (r Result) String() string {
	return fmt.Sprintf("%s:%s", r.EngineVersion, r.OutcomeStatus)
}
