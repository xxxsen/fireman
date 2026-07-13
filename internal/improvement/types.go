// Package improvement implements deterministic, cash-flow-only FIRE plan
// improvement searches on top of frozen simulation inputs.
package improvement

import (
	"encoding/json"

	"github.com/fireman/fireman/internal/simulation"
)

const AlgorithmVersion = "fire_plan_improver_v1"

const (
	RecipePureDelay            = "pure_retirement_delay"
	RecipePureSavings          = "pure_savings_increase"
	RecipePureSpending         = "pure_spending_reduction"
	RecipePureRetirementIncome = "pure_retirement_income_increase"
	RecipeBalanced             = "balanced"
)

type RetirementDelayLever struct {
	MaxDelayYears int `json:"max_delay_years"`
}

type MoneyIncreaseLever struct {
	MaxIncreaseMinor int64 `json:"max_increase_minor"`
	StepMinor        int64 `json:"step_minor"`
}

type MoneyReductionLever struct {
	MaxReductionMinor int64 `json:"max_reduction_minor"`
	StepMinor         int64 `json:"step_minor"`
}

type Config struct {
	TargetSuccessProbability float64               `json:"target_success_probability"`
	RetirementDelay          *RetirementDelayLever `json:"retirement_delay,omitempty"`
	SavingsIncrease          *MoneyIncreaseLever   `json:"savings_increase,omitempty"`
	SpendingReduction        *MoneyReductionLever  `json:"spending_reduction,omitempty"`
	RetirementIncomeIncrease *MoneyIncreaseLever   `json:"retirement_income_increase,omitempty"`
}

type Adjustments struct {
	DelayYears                    int   `json:"delay_years"`
	SavingsIncreaseMinor          int64 `json:"savings_increase_minor"`
	SpendingReductionMinor        int64 `json:"spending_reduction_minor"`
	RetirementIncomeIncreaseMinor int64 `json:"retirement_income_increase_minor"`
}

type Evaluation struct {
	Adjustments           Adjustments `json:"adjustments"`
	Runs                  int         `json:"runs"`
	SuccessCount          int         `json:"success_count"`
	SuccessProbability    float64     `json:"success_probability"`
	SuccessWilsonLow      float64     `json:"success_wilson_low"`
	SuccessWilsonHigh     float64     `json:"success_wilson_high"`
	TerminalP50Minor      int64       `json:"terminal_p50_minor"`
	MaxDrawdownP95        float64     `json:"max_drawdown_p95"`
	ImprovedPathCount     int         `json:"improved_path_count"`
	RegressedPathCount    int         `json:"regressed_path_count"`
	UnchangedSuccessCount int         `json:"unchanged_success_count"`
	UnchangedFailureCount int         `json:"unchanged_failure_count"`
	MeetsTarget           bool        `json:"meets_target"`
	CandidateConfigHash   string      `json:"candidate_config_hash"`
	CandidateSnapshotHash string      `json:"candidate_snapshot_hash"`
}

type Proposal struct {
	ID                            string  `json:"id"`
	Recipe                        string  `json:"recipe"`
	DelayYears                    int     `json:"delay_years"`
	SavingsIncreaseMinor          int64   `json:"savings_increase_minor"`
	SpendingReductionMinor        int64   `json:"spending_reduction_minor"`
	RetirementIncomeIncreaseMinor int64   `json:"retirement_income_increase_minor"`
	ResultRetirementAge           int     `json:"result_retirement_age"`
	ResultAnnualSavingsMinor      int64   `json:"result_annual_savings_minor"`
	ResultAnnualSpendingMinor     int64   `json:"result_annual_spending_minor"`
	ResultRetirementIncomeMinor   int64   `json:"result_annual_retirement_income_minor"`
	SuccessProbability            float64 `json:"success_probability"`
	SuccessWilsonLow              float64 `json:"success_wilson_low"`
	SuccessWilsonHigh             float64 `json:"success_wilson_high"`
	TerminalP50Minor              int64   `json:"terminal_p50_minor"`
	MaxDrawdownP95                float64 `json:"max_drawdown_p95"`
	ImprovedPathCount             int     `json:"improved_path_count"`
	RegressedPathCount            int     `json:"regressed_path_count"`
	CandidateConfigHash           string  `json:"candidate_config_hash"`
	CandidateSnapshotHash         string  `json:"candidate_snapshot_hash"`
}

type RecipeResult struct {
	Recipe     string `json:"recipe"`
	Status     string `json:"status"`
	ProposalID string `json:"proposal_id,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	AlgorithmVersion  string         `json:"algorithm_version"`
	TargetProbability float64        `json:"target_probability"`
	Baseline          Evaluation     `json:"baseline"`
	TargetReached     bool           `json:"target_reached"`
	Proposals         []Proposal     `json:"proposals"`
	BestAttainable    *Proposal      `json:"best_attainable,omitempty"`
	Recipes           []RecipeResult `json:"recipes"`
	Evaluations       []Evaluation   `json:"evaluations"`
	EvaluatedCount    int            `json:"evaluated_count"`
	Warnings          []Warning      `json:"warnings,omitempty"`
}

type FrozenInput struct {
	SourceSnapshot      simulation.InputSnapshot     `json:"source_snapshot"`
	SourceSummary       simulation.Summary           `json:"source_summary"`
	Baseline            simulation.OutcomeEvaluation `json:"baseline"`
	BaselineBits        string                       `json:"baseline_outcome_bits"`
	BaselineHash        string                       `json:"baseline_outcome_hash"`
	Config              Config                       `json:"config"`
	ConfigHashInputJSON json.RawMessage              `json:"config_hash_input"`
}
