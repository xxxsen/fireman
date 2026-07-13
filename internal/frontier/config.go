package frontier

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"

	"github.com/fireman/fireman/internal/simulation"
)

var (
	ErrConfigInvalid         = errors.New("frontier config invalid")
	ErrBudgetExceeded        = errors.New("frontier evaluation budget exceeded")
	ErrComputeBudgetExceeded = errors.New("frontier compute budget exceeded")
)

const (
	MaxMoneyMinor         int64 = 9_000_000_000_000_000
	MaxMoneyLevels              = 1024
	MaxAgePoints                = 21
	MaxEvaluationBudget         = 256
	MaxPathMonthBudget    int64 = 300_000_000
	DefaultEvaluationRuns       = 10_000
)

// Normalize validates the request and derives every budget value used by both
// readiness and task creation. An evaluationRuns value of zero selects the
// documented default.
func Normalize(frontierType string, target float64, evaluationRuns int, ageRange *AgeRange,
	search MoneySearch, currentAge, endAge, sourceRuns, horizonMonths int,
) (Config, error) {
	if !validType(frontierType) {
		return Config{}, fmt.Errorf("%w: unsupported frontier_type", ErrConfigInvalid)
	}
	if err := validateTargetProbability(target); err != nil {
		return Config{}, err
	}
	normalizedRuns, err := normalizeEvaluationRuns(evaluationRuns, sourceRuns)
	if err != nil {
		return Config{}, err
	}
	levels, err := validateMoneySearch(frontierType, search)
	if err != nil {
		return Config{}, err
	}
	normalizedAge, agePoints, err := normalizeAgeRange(frontierType, ageRange, currentAge, endAge)
	if err != nil {
		return Config{}, err
	}
	if horizonMonths <= 0 {
		return Config{}, fmt.Errorf("%w: source horizon must be positive", ErrConfigInvalid)
	}
	perPoint, evaluationBudget := EvaluationBudget(agePoints, levels)
	pathMonths, err := validateComputeBudget(evaluationBudget, normalizedRuns, horizonMonths)
	if err != nil {
		return Config{}, err
	}
	return Config{
		FrontierType: frontierType, TargetSuccessProbability: target, EvaluationRuns: normalizedRuns,
		RetirementAgeRange: normalizedAge, Search: search, MoneyLevels: levels, AgePoints: agePoints,
		PerPointBudget: perPoint, EvaluationBudget: evaluationBudget, PathMonthBudget: pathMonths,
	}, nil
}

func validateTargetProbability(target float64) error {
	if math.IsNaN(target) || math.IsInf(target, 0) || target < 0.50 || target > 0.99 ||
		math.Abs(target*10_000-math.Round(target*10_000)) > 1e-8 {
		return fmt.Errorf(
			"%w: target_success_probability must be in [0.50, 0.99] with at most 4 decimals",
			ErrConfigInvalid,
		)
	}
	return nil
}

func normalizeEvaluationRuns(evaluationRuns, sourceRuns int) (int, error) {
	if sourceRuns < 1000 {
		return 0, fmt.Errorf("%w: source simulation must contain at least 1000 runs", ErrConfigInvalid)
	}
	if evaluationRuns == 0 {
		evaluationRuns = min(sourceRuns, DefaultEvaluationRuns)
	}
	if evaluationRuns < 1000 || evaluationRuns > min(sourceRuns, 20000) {
		return 0, fmt.Errorf("%w: evaluation_runs outside source/run limits", ErrConfigInvalid)
	}
	return evaluationRuns, nil
}

func validateMoneySearch(frontierType string, search MoneySearch) (int, error) {
	if search.MinMinor < 0 || search.MaxMinor < search.MinMinor || search.MaxMinor > MaxMoneyMinor ||
		search.StepMinor <= 0 || (search.MaxMinor-search.MinMinor)%search.StepMinor != 0 {
		return 0, fmt.Errorf("%w: invalid discrete money search", ErrConfigInvalid)
	}
	if frontierType == TypeRetirementAgeMaxSpending && search.MinMinor < 1 {
		return 0, fmt.Errorf("%w: annual spending minimum must be positive", ErrConfigInvalid)
	}
	if (frontierType == TypeRequiredCurrentAssets || frontierType == TypeCoastRequiredAssets) &&
		search.MinMinor < 1 {
		return 0, fmt.Errorf("%w: asset minimum must be positive", ErrConfigInvalid)
	}
	levels64 := (search.MaxMinor-search.MinMinor)/search.StepMinor + 1
	if levels64 < 1 || levels64 > MaxMoneyLevels {
		return 0, fmt.Errorf("%w: money search may contain at most %d levels", ErrConfigInvalid, MaxMoneyLevels)
	}
	return int(levels64), nil
}

func normalizeAgeRange(frontierType string, ageRange *AgeRange, currentAge, endAge int) (*AgeRange, int, error) {
	if isAgeFrontier(frontierType) {
		if ageRange == nil || ageRange.Min < currentAge || ageRange.Max < ageRange.Min ||
			ageRange.Max >= endAge || ageRange.Max-ageRange.Min+1 > MaxAgePoints {
			return nil, 0, fmt.Errorf("%w: invalid retirement_age_range", ErrConfigInvalid)
		}
		copyRange := *ageRange
		return &copyRange, ageRange.Max - ageRange.Min + 1, nil
	}
	if ageRange != nil {
		return nil, 0, fmt.Errorf("%w: retirement_age_range is only valid for age frontiers", ErrConfigInvalid)
	}
	return nil, 1, nil
}

func validateComputeBudget(evaluationBudget, evaluationRuns, horizonMonths int) (int64, error) {
	if evaluationBudget > MaxEvaluationBudget {
		return 0, fmt.Errorf("%w: %d > %d", ErrBudgetExceeded, evaluationBudget, MaxEvaluationBudget)
	}
	pathMonths, ok := PathMonthBudget(evaluationBudget, evaluationRuns, horizonMonths)
	if !ok {
		return 0, fmt.Errorf("%w: path-month budget overflow", ErrComputeBudgetExceeded)
	}
	if pathMonths > MaxPathMonthBudget {
		return 0, fmt.Errorf("%w: %d > %d", ErrComputeBudgetExceeded, pathMonths, MaxPathMonthBudget)
	}
	return pathMonths, nil
}

// EvaluationBudget returns the documented conservative binary-search bound.
func EvaluationBudget(agePoints, moneyLevels int) (int, int) {
	if agePoints < 0 || moneyLevels < 1 {
		return 0, 0
	}
	perPoint := bits.Len(uint(moneyLevels)) + 2 // ceil(log2(levels+1)) + 2
	return perPoint, 1 + agePoints*perPoint
}

func PathMonthBudget(evaluations, runs, horizonMonths int) (int64, bool) {
	if evaluations < 0 || runs < 0 || horizonMonths < 0 {
		return 0, false
	}
	left := int64(evaluations) * int64(runs)
	if evaluations != 0 && left/int64(evaluations) != int64(runs) {
		return 0, false
	}
	if horizonMonths != 0 && left > math.MaxInt64/int64(horizonMonths) {
		return 0, false
	}
	return left * int64(horizonMonths), true
}

func HashIdentity(identity InputIdentity) (string, error) {
	raw, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("marshal frontier identity: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// HashFrozenIdentity derives the complete canonical admission identity from a
// worker-owned frozen input. Both admission and execution use this function so
// persisted metadata cannot silently diverge from the snapshot being run.
func HashFrozenIdentity(sourceRunID string, frozen FrozenInput) (string, error) {
	snapshotHash, err := simulation.HashInput(&frozen.SourceSnapshot)
	if err != nil {
		return "", fmt.Errorf("hash frozen frontier snapshot: %w", err)
	}
	config := frozen.Config
	snapshot := frozen.SourceSnapshot
	return HashIdentity(InputIdentity{
		AlgorithmVersion: AlgorithmVersion, SourceRunID: sourceRunID,
		SourceEngine: snapshot.EngineVersion, SourceConfigHash: snapshot.ConfigHash,
		SourceMarketHash: snapshot.MarketSnapshotHash, FrozenSnapshotHash: "sha256:" + snapshotHash,
		FrontierType: config.FrontierType, TargetProbability: config.TargetSuccessProbability,
		EvaluationRuns: config.EvaluationRuns, AgeRange: config.RetirementAgeRange, Search: config.Search,
		EvaluationBudget: config.EvaluationBudget, PathMonthBudget: config.PathMonthBudget,
	})
}

func validType(value string) bool {
	switch value {
	case TypeRetirementAgeMaxSpending, TypeRetirementAgeMinSavings,
		TypeRequiredCurrentAssets, TypeCoastRequiredAssets:
		return true
	default:
		return false
	}
}

func isAgeFrontier(value string) bool {
	return value == TypeRetirementAgeMaxSpending || value == TypeRetirementAgeMinSavings
}

func Values(search MoneySearch) []int64 {
	count := int((search.MaxMinor-search.MinMinor)/search.StepMinor) + 1
	out := make([]int64, count)
	for i := range out {
		out[i] = search.MinMinor + int64(i)*search.StepMinor
	}
	return out
}
