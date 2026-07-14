// Package investmentpath implements deterministic, single-asset historical
// investment-path experiments. It is deliberately independent from storage,
// clocks and market-data clients.
package investmentpath

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	EngineVersion       = "single_asset_investment_path_v2"
	QuantileVersion     = "linear_interpolation_v1"
	ModeIncomeDCA       = "income_dca"
	ModeExistingCapital = "existing_capital"

	StrategyIncomeDCA          = "income_dca"
	StrategyIncomeCashBaseline = "income_cash_baseline"
	StrategyLumpSum            = "lump_sum"

	DateLayout = "2006-01-02"
)

var (
	ErrInvalidInput        = errors.New("invalid investment path input")
	ErrNoCompleteWindow    = errors.New("no complete investment path window")
	ErrTradeBudgetTooSmall = errors.New("trade budget too small after fee")
)

type PricePoint struct {
	Date     string  `json:"date"`
	Value    float64 `json:"value"`
	Tradable bool    `json:"tradable"`
}

type IncomeDCAConfig struct {
	InitialInvestmentMinor   int64 `json:"initial_investment_minor"`
	MonthlyContributionMinor int64 `json:"monthly_contribution_minor"`
}

type ThresholdComparison struct {
	Enabled            bool    `json:"enabled"`
	TargetAssetWeight  float64 `json:"target_asset_weight"`
	RebalanceThreshold float64 `json:"rebalance_threshold"`
}

type ExistingCapitalConfig struct {
	InitialCapitalMinor int64                `json:"initial_capital_minor"`
	PhaseInMonths       []int                `json:"phase_in_months,omitempty"`
	Threshold           *ThresholdComparison `json:"threshold_comparison,omitempty"`
}

type Input struct {
	Mode                string                 `json:"mode"`
	EvaluationStart     string                 `json:"evaluation_start"`
	EvaluationEnd       string                 `json:"evaluation_end"`
	HorizonMonths       int                    `json:"horizon_months"`
	PrimaryStart        string                 `json:"primary_start,omitempty"`
	MonthlyDay          int                    `json:"monthly_day"`
	TransactionCostRate float64                `json:"transaction_cost_rate"`
	IncomeDCA           *IncomeDCAConfig       `json:"income_dca,omitempty"`
	ExistingCapital     *ExistingCapitalConfig `json:"existing_capital,omitempty"`
	Prices              []PricePoint           `json:"-"`
	MaxTailGapDays      int                    `json:"max_tail_gap_days"`
}

type Resolved struct {
	SourceStart       string   `json:"source_start"`
	SourceEnd         string   `json:"source_end"`
	PrimaryStart      string   `json:"primary_start"`
	PrimaryFirstTrade string   `json:"primary_first_execution_date"`
	PrimaryEnd        string   `json:"primary_end"`
	WindowStarts      []string `json:"window_starts"`
	StrategyKeys      []string `json:"strategy_keys"`
	PathDayBudget     int64    `json:"path_day_budget"`
}

type Point struct {
	StrategyKey                 string  `json:"strategy_key"`
	ValuationDate               string  `json:"valuation_date"`
	AccountValueMinor           int64   `json:"account_value_minor"`
	AssetValueMinor             int64   `json:"asset_value_minor"`
	CashValueMinor              int64   `json:"cash_value_minor"`
	CumulativeContributionMinor int64   `json:"cumulative_external_contribution_minor"`
	UnitNAV                     float64 `json:"unit_nav"`
	Drawdown                    float64 `json:"drawdown"`
}

type Trade struct {
	StrategyKey          string `json:"strategy_key"`
	SequenceNo           int    `json:"sequence_no"`
	TradeDate            string `json:"trade_date"`
	Side                 string `json:"side"`
	Reason               string `json:"reason"`
	GrossTradeMinor      int64  `json:"gross_trade_minor"`
	FeeMinor             int64  `json:"fee_minor"`
	AssetValueDeltaMinor int64  `json:"asset_value_delta_minor"`
	CashDeltaMinor       int64  `json:"cash_delta_minor"`
}

type WindowResult struct {
	StrategyKey                     string   `json:"strategy_key"`
	WindowStart                     string   `json:"window_start"`
	WindowEnd                       string   `json:"window_end"`
	TotalContributionMinor          int64    `json:"total_contribution_minor"`
	TerminalValueMinor              int64    `json:"terminal_value_minor"`
	ProfitMinor                     int64    `json:"profit_minor"`
	XIRR                            *float64 `json:"xirr,omitempty"`
	XIRRReason                      string   `json:"xirr_reason,omitempty"`
	TWRTotal                        float64  `json:"twr_total"`
	TWRAnnualized                   float64  `json:"twr_annualized"`
	MaxDrawdown                     float64  `json:"max_drawdown"`
	MaxDrawdownStart                string   `json:"max_drawdown_start"`
	MaxDrawdownEnd                  string   `json:"max_drawdown_end"`
	LongestUnderwaterDays           int      `json:"longest_underwater_days"`
	MaxPrincipalDeficitMinor        int64    `json:"max_principal_deficit_minor"`
	MaxPrincipalDeficitRatio        float64  `json:"max_principal_deficit_ratio"`
	LongestBelowPrincipalDays       int      `json:"longest_below_principal_days"`
	FirstRecoveryAbovePrincipalDate string   `json:"first_recovery_above_principal_date,omitempty"`
	AverageCashWeight               float64  `json:"average_cash_weight"`
	TotalTransactionCostMinor       int64    `json:"total_transaction_cost_minor"`
	TradeCount                      int      `json:"trade_count"`
	Turnover                        float64  `json:"turnover"`
	DeploymentCompleteDate          string   `json:"deployment_complete_date,omitempty"`
	TerminalDeltaVsLumpSumMinor     *int64   `json:"terminal_delta_vs_lump_sum_minor,omitempty"`
	DrawdownDeltaVsLumpSum          *float64 `json:"drawdown_delta_vs_lump_sum,omitempty"`
}

type Quantiles struct {
	P10 float64 `json:"p10"`
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
}

type StrategyAggregate struct {
	StrategyKey         string    `json:"strategy_key"`
	WindowCount         int       `json:"window_count"`
	TerminalValue       Quantiles `json:"terminal_value_minor"`
	Profit              Quantiles `json:"profit_minor"`
	XIRR                Quantiles `json:"xirr"`
	XIRRCount           int       `json:"xirr_count"`
	TWRAnnualized       Quantiles `json:"twr_annualized"`
	MaxDrawdown         Quantiles `json:"max_drawdown"`
	UnderwaterDays      Quantiles `json:"longest_underwater_days"`
	BestStart           string    `json:"best_start"`
	WorstStart          string    `json:"worst_start"`
	BaselineKey         string    `json:"baseline_key,omitempty"`
	HigherTerminalCount int       `json:"higher_terminal_count,omitempty"`
	PairedWindowCount   int       `json:"paired_window_count,omitempty"`
	HigherTerminalRatio float64   `json:"higher_terminal_ratio,omitempty"`
	TradeoffCount       int       `json:"lower_drawdown_lower_terminal_count,omitempty"`
}

type Result struct {
	Resolved   Resolved            `json:"resolved"`
	Primary    []WindowResult      `json:"primary"`
	Points     []Point             `json:"points"`
	Trades     []Trade             `json:"trades"`
	Windows    []WindowResult      `json:"windows"`
	Aggregates []StrategyAggregate `json:"aggregates"`
	Warnings   []string            `json:"warnings,omitempty"`
}

type ProgressFunc func(completed, total int) error

//nolint:funlen,gocyclo,lll // Resolution validates the complete public contract before exposing any window.
func ValidateAndResolve(in Input) (Input, Resolved, error) {
	start, err := time.Parse(DateLayout, in.EvaluationStart)
	if err != nil {
		return in, Resolved{}, fmt.Errorf("%w: invalid evaluation_start", ErrInvalidInput)
	}
	end, err := time.Parse(DateLayout, in.EvaluationEnd)
	if err != nil || !start.Before(end) {
		return in, Resolved{}, fmt.Errorf("%w: invalid evaluation_end", ErrInvalidInput)
	}
	if in.HorizonMonths < 12 || in.HorizonMonths > 360 || in.MonthlyDay < 1 || in.MonthlyDay > 28 {
		return in, Resolved{}, fmt.Errorf("%w: horizon_months or monthly_day out of range", ErrInvalidInput)
	}
	if math.IsNaN(in.TransactionCostRate) || math.IsInf(in.TransactionCostRate, 0) || in.TransactionCostRate < 0 || in.TransactionCostRate > .1 {
		return in, Resolved{}, fmt.Errorf("%w: transaction_cost_rate out of range", ErrInvalidInput)
	}
	if in.MaxTailGapDays <= 0 {
		in.MaxTailGapDays = 10
	}
	if err := validateMode(&in); err != nil {
		return in, Resolved{}, err
	}
	prices, _, tradableDays, err := normalizePrices(in.Prices)
	if err != nil {
		return in, Resolved{}, err
	}
	in.Prices = prices
	strategies := strategyKeys(in)
	windowStarts := make([]string, 0)
	firstTrades := make(map[string]string)
	for cursor := monthDate(start.Year(), start.Month(), in.MonthlyDay); cursor.Before(start); cursor = cursor.AddDate(0, 1, 0) {
		// Align to the first candidate month on or after evaluation_start.
		start = cursor.AddDate(0, 1, 0)
		break
	}
	for cursor := monthDate(start.Year(), start.Month(), in.MonthlyDay); !cursor.Before(start); cursor = cursor.AddDate(0, 1, 0) {
		if cursor.After(end) {
			break
		}
		windowEnd := cursor.AddDate(0, in.HorizonMonths, 0)
		lastDay := windowEnd.AddDate(0, 0, -1)
		first, last, ok := coverageForWindow(tradableDays, cursor, lastDay)
		if !ok || int(first.Sub(cursor).Hours()/24) > in.MaxTailGapDays ||
			int(lastDay.Sub(last).Hours()/24) > in.MaxTailGapDays ||
			windowHasExcessGap(tradableDays, cursor, lastDay, in.MaxTailGapDays) {
			continue
		}
		windowStarts = append(windowStarts, cursor.Format(DateLayout))
		firstTrades[cursor.Format(DateLayout)] = first.Format(DateLayout)
	}
	if len(windowStarts) == 0 {
		return in, Resolved{}, ErrNoCompleteWindow
	}
	if len(windowStarts) > 600 {
		return in, Resolved{}, fmt.Errorf("%w: rolling window count exceeds 600", ErrInvalidInput)
	}
	primary := in.PrimaryStart
	if primary == "" {
		primary = windowStarts[len(windowStarts)-1]
	}
	idx := sort.SearchStrings(windowStarts, primary)
	if idx == len(windowStarts) || windowStarts[idx] != primary {
		return in, Resolved{}, fmt.Errorf("%w: primary_start is not a complete rolling start", ErrInvalidInput)
	}
	in.PrimaryStart = primary
	primaryDate, _ := time.Parse(DateLayout, primary)
	calendarDays := int64(0)
	for _, ws := range windowStarts {
		d, _ := time.Parse(DateLayout, ws)
		calendarDays += int64(d.AddDate(0, in.HorizonMonths, 0).Sub(d).Hours() / 24)
	}
	budget := calendarDays * int64(len(strategies))
	if budget > 8_000_000 {
		return in, Resolved{}, fmt.Errorf("%w: path-day budget exceeds 8000000", ErrInvalidInput)
	}
	resolved := Resolved{
		SourceStart:       prices[0].Date,
		SourceEnd:         prices[len(prices)-1].Date,
		PrimaryStart:      primary,
		PrimaryFirstTrade: firstTrades[primary],
		PrimaryEnd:        primaryDate.AddDate(0, in.HorizonMonths, 0).AddDate(0, 0, -1).Format(DateLayout),
		WindowStarts:      windowStarts,
		StrategyKeys:      strategies,
		PathDayBudget:     budget,
	}
	return in, resolved, nil
}

//nolint:gocyclo,lll // The tagged-union validation is intentionally exhaustive and centralized.
func validateMode(in *Input) error {
	switch in.Mode {
	case ModeIncomeDCA:
		if in.IncomeDCA == nil || in.ExistingCapital != nil || in.IncomeDCA.InitialInvestmentMinor < 0 ||
			in.IncomeDCA.InitialInvestmentMinor > 1_000_000_000_000_000 || in.IncomeDCA.MonthlyContributionMinor < 1 ||
			in.IncomeDCA.MonthlyContributionMinor > 1_000_000_000_000_000 {
			return fmt.Errorf("%w: invalid income_dca union", ErrInvalidInput)
		}
	case ModeExistingCapital:
		if in.ExistingCapital == nil || in.IncomeDCA != nil || in.ExistingCapital.InitialCapitalMinor < 1 ||
			in.ExistingCapital.InitialCapitalMinor > 1_000_000_000_000_000 || len(in.ExistingCapital.PhaseInMonths) > 3 {
			return fmt.Errorf("%w: invalid existing_capital union", ErrInvalidInput)
		}
		seen := map[int]bool{}
		for _, months := range in.ExistingCapital.PhaseInMonths {
			if months < 2 || months > 60 || seen[months] {
				return fmt.Errorf("%w: invalid phase_in_months", ErrInvalidInput)
			}
			seen[months] = true
		}
		sort.Ints(in.ExistingCapital.PhaseInMonths)
		if t := in.ExistingCapital.Threshold; t != nil && t.Enabled {
			if !finite(t.TargetAssetWeight) || !finite(t.RebalanceThreshold) || t.TargetAssetWeight < .05 || t.TargetAssetWeight > .95 ||
				t.RebalanceThreshold <= 0 || t.RebalanceThreshold > .5 || t.RebalanceThreshold > math.Max(t.TargetAssetWeight, 1-t.TargetAssetWeight) {
				return fmt.Errorf("%w: invalid threshold comparison", ErrInvalidInput)
			}
		}
	default:
		return fmt.Errorf("%w: unsupported mode", ErrInvalidInput)
	}
	if len(strategyKeys(*in)) > 6 {
		return fmt.Errorf("%w: strategy count exceeds 6", ErrInvalidInput)
	}
	return nil
}

func normalizePrices(points []PricePoint) ([]PricePoint, []time.Time, []time.Time, error) {
	if len(points) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: price history is empty", ErrInvalidInput)
	}
	out := append([]PricePoint(nil), points...)
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	anyTradable := false
	for _, point := range out {
		anyTradable = anyTradable || point.Tradable
	}
	parsed := make([]time.Time, len(out))
	tradable := make([]time.Time, 0, len(out))
	for i, p := range out {
		d, err := time.Parse(DateLayout, p.Date)
		if err != nil || !finite(p.Value) || p.Value <= 0 || (i > 0 && p.Date == out[i-1].Date) {
			return nil, nil, nil, fmt.Errorf("%w: invalid or duplicate price point", ErrInvalidInput)
		}
		if !anyTradable {
			out[i].Tradable = true
		}
		if out[i].Tradable {
			tradable = append(tradable, d)
		}
		parsed[i] = d
	}
	if len(tradable) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: history has no tradable points", ErrInvalidInput)
	}
	return out, parsed, tradable, nil
}

//nolint:lll // Canonical strategy keys are kept next to their conditions.
func strategyKeys(in Input) []string {
	if in.Mode == ModeIncomeDCA {
		return []string{StrategyIncomeDCA, StrategyIncomeCashBaseline}
	}
	keys := []string{StrategyLumpSum}
	for _, months := range in.ExistingCapital.PhaseInMonths {
		keys = append(keys, fmt.Sprintf("phase_in_%dm", months))
	}
	if t := in.ExistingCapital.Threshold; t != nil && t.Enabled {
		keys = append(keys, weightKey("static", t.TargetAssetWeight, 0), weightKey("threshold", t.TargetAssetWeight, t.RebalanceThreshold))
	}
	return keys
}

func weightKey(prefix string, weight, threshold float64) string {
	if prefix == "static" {
		return fmt.Sprintf("static_%g", weight)
	}
	return fmt.Sprintf("threshold_%g_%g", weight, threshold)
}

func monthDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func coverageForWindow(points []time.Time, start, end time.Time) (time.Time, time.Time, bool) {
	firstIdx := sort.Search(len(points), func(i int) bool { return !points[i].Before(start) })
	lastIdx := sort.Search(len(points), func(i int) bool { return points[i].After(end) }) - 1
	if firstIdx >= len(points) || lastIdx < firstIdx {
		return time.Time{}, time.Time{}, false
	}
	return points[firstIdx], points[lastIdx], true
}

func windowHasExcessGap(points []time.Time, start, end time.Time, tolerance int) bool {
	first := sort.Search(len(points), func(i int) bool { return !points[i].Before(start) })
	for i := first + 1; i < len(points) && !points[i].After(end); i++ {
		missingDays := int(points[i].Sub(points[i-1]).Hours()/24) - 1
		if missingDays > tolerance {
			return true
		}
	}
	return false
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("investment path context ended: %w", ctx.Err())
	default:
		return nil
	}
}
