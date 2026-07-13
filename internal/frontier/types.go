// Package frontier implements deterministic FIRE confidence-frontier searches
// over an explicitly requested discrete grid.
package frontier

import (
	"encoding/json"

	"github.com/fireman/fireman/internal/simulation"
)

const AlgorithmVersion = "fire_frontier_v1"

const (
	TypeRetirementAgeMaxSpending = "retirement_age_max_spending"
	TypeRetirementAgeMinSavings  = "retirement_age_min_savings"
	TypeRequiredCurrentAssets    = "required_current_assets"
	TypeCoastRequiredAssets      = "coast_required_assets"
)

const (
	StatusBoundaryFound        = "boundary_found"
	StatusEntireDomainFeasible = "entire_domain_feasible"
	StatusNoFeasibleValue      = "no_feasible_value"
)

type AgeRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type MoneySearch struct {
	MinMinor  int64 `json:"min_minor"`
	MaxMinor  int64 `json:"max_minor"`
	StepMinor int64 `json:"step_minor"`
}

// Config is the normalized, persisted search contract. AgeRange is nil for
// the two single-point asset frontiers.
type Config struct {
	FrontierType             string      `json:"frontier_type"`
	TargetSuccessProbability float64     `json:"target_success_probability"`
	EvaluationRuns           int         `json:"evaluation_runs"`
	RetirementAgeRange       *AgeRange   `json:"retirement_age_range,omitempty"`
	Search                   MoneySearch `json:"search"`
	MoneyLevels              int         `json:"money_levels"`
	AgePoints                int         `json:"age_points"`
	PerPointBudget           int         `json:"per_point_budget"`
	EvaluationBudget         int         `json:"evaluation_budget"`
	PathMonthBudget          int64       `json:"path_month_budget"`
}

type Evaluation struct {
	RetirementAge       int     `json:"retirement_age"`
	ValueMinor          int64   `json:"value_minor"`
	Runs                int     `json:"runs"`
	SuccessCount        int     `json:"success_count"`
	SuccessProbability  float64 `json:"success_probability"`
	SuccessWilsonLow    float64 `json:"success_wilson_low"`
	SuccessWilsonHigh   float64 `json:"success_wilson_high"`
	TerminalP50Minor    int64   `json:"terminal_wealth_p50_minor"`
	MaxDrawdownP95      float64 `json:"max_drawdown_p95"`
	ImprovedPathCount   int     `json:"improved_path_count"`
	RegressedPathCount  int     `json:"regressed_path_count"`
	MeetsTarget         bool    `json:"meets_target"`
	OutcomeHash         string  `json:"outcome_hash"`
	SnapshotHash        string  `json:"snapshot_hash"`
	CandidateConfigHash string  `json:"candidate_config_hash"`
}

type Point struct {
	ID                       string      `json:"id"`
	RetirementAge            int         `json:"retirement_age"`
	ValueMinor               int64       `json:"value_minor"`
	Status                   string      `json:"status"`
	Applicable               bool        `json:"applicable"`
	Evaluation               Evaluation  `json:"evaluation"`
	WorseNeighbor            *Evaluation `json:"worse_neighbor,omitempty"`
	SourceCurrentAssetsMinor *int64      `json:"source_current_assets_minor,omitempty"`
	GapMinor                 *int64      `json:"gap_minor,omitempty"`
	Achieved                 *bool       `json:"achieved,omitempty"`
	CoastAchieved            *bool       `json:"coast_achieved,omitempty"`
}

type Result struct {
	AlgorithmVersion       string       `json:"algorithm_version"`
	FrontierType           string       `json:"frontier_type"`
	TargetProbability      float64      `json:"target_probability"`
	EvaluationRuns         int          `json:"evaluation_runs"`
	Baseline               Evaluation   `json:"baseline"`
	Points                 []Point      `json:"points"`
	Evaluations            []Evaluation `json:"evaluations"`
	DistinctEvaluations    int          `json:"distinct_evaluations"`
	ActualPathMonths       int64        `json:"actual_path_months"`
	EvaluationBudget       int          `json:"evaluation_budget"`
	PathMonthBudget        int64        `json:"path_month_budget"`
	DiscreteConnectionNote string       `json:"discrete_connection_note"`
}

// FrozenInput is the complete worker-owned copy. ConfigHashInputJSON is kept
// as raw canonical input because apply must be able to reproduce candidate
// configuration hashes even after the source simulation has been pruned.
type FrozenInput struct {
	SourceSnapshot      simulation.InputSnapshot `json:"source_snapshot"`
	Config              Config                   `json:"config"`
	ConfigHashInputJSON json.RawMessage          `json:"config_hash_input"`
}

type InputIdentity struct {
	AlgorithmVersion   string      `json:"algorithm_version"`
	SourceRunID        string      `json:"source_run_id"`
	SourceEngine       string      `json:"source_engine_version"`
	SourceConfigHash   string      `json:"source_config_hash"`
	SourceMarketHash   string      `json:"source_market_hash"`
	FrozenSnapshotHash string      `json:"frozen_snapshot_hash"`
	FrontierType       string      `json:"frontier_type"`
	TargetProbability  float64     `json:"target_success_probability"`
	EvaluationRuns     int         `json:"evaluation_runs"`
	AgeRange           *AgeRange   `json:"retirement_age_range,omitempty"`
	Search             MoneySearch `json:"search"`
	EvaluationBudget   int         `json:"evaluation_budget"`
	PathMonthBudget    int64       `json:"path_month_budget"`
}
