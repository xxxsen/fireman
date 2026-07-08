package service

import (
	"fmt"
	"math"
	"sort"
)

// research_optimization.go implements the pure candidate generation and
// objective ranking core for portfolio auto-optimization (td/103).
// No I/O: the service layer feeds inputs and the job runner orchestrates.

const (
	OptimizationEngineVersion       = "research_optimizer_v1"
	OptimizationDefaultWeightStep   = 0.05
	OptimizationDefaultTopK         = 20
	OptimizationDefaultMaxCandidate = 20000
	OptimizationMaxEnabledAssets    = 10
	OptimizationHardMaxCandidate    = 20000
)

// OptimizationObjective identifies which metric to rank by.
type OptimizationObjective string

const (
	ObjectiveMaxCAGR     OptimizationObjective = "max_cagr"
	ObjectiveMinDrawdown OptimizationObjective = "min_drawdown"
	ObjectiveMaxCalmar   OptimizationObjective = "max_calmar"
)

// OptimizationConfig is the user-controllable config for one optimization run.
type OptimizationConfig struct {
	WeightStep        float64 `json:"weight_step"`
	MaxCandidateCount int     `json:"max_candidate_count"`
	TopK              int     `json:"top_k"`
}

// NormalizeDefaults fills zero fields with defaults.
func (c *OptimizationConfig) NormalizeDefaults() {
	if c.WeightStep <= 0 {
		c.WeightStep = OptimizationDefaultWeightStep
	}
	if c.MaxCandidateCount <= 0 {
		c.MaxCandidateCount = OptimizationDefaultMaxCandidate
	}
	if c.MaxCandidateCount > OptimizationHardMaxCandidate {
		c.MaxCandidateCount = OptimizationHardMaxCandidate
	}
	if c.TopK <= 0 {
		c.TopK = OptimizationDefaultTopK
	}
}

// OptimizationAsset is one enabled asset in the optimization input.
type OptimizationAsset struct {
	ItemID   string
	AssetKey string
	Name     string
	Weight   float64
	Locked   bool
}

// OptimizationWeightVector is one candidate weight assignment.
type OptimizationWeightVector struct {
	Weights []OptimizationWeightEntry
}

// OptimizationWeightEntry is one asset's weight in a candidate.
type OptimizationWeightEntry struct {
	ItemID   string
	AssetKey string
	Name     string
	Weight   float64
	Locked   bool
}

// OptimizationResultItem is one ranked candidate result.
type OptimizationResultItem struct {
	Rank      int                        `json:"rank"`
	Objective OptimizationObjective      `json:"objective"`
	Score     float64                    `json:"score"`
	Weights   []OptimizationWeightEntry  `json:"weights"`
	Summary   BacktestSummary            `json:"summary"`
}

// OptimizationResult is the final output of one optimization run.
type OptimizationResult struct {
	CandidateCount int                      `json:"candidate_count"`
	EvaluatedCount int                      `json:"evaluated_count"`
	SkippedCount   int                      `json:"skipped_count"`
	BestByCAGR     []OptimizationResultItem `json:"best_by_cagr"`
	BestByDrawdown []OptimizationResultItem `json:"best_by_drawdown"`
	BestByCalmar   []OptimizationResultItem `json:"best_by_calmar"`
}

// ValidateOptimizationInput checks preconditions (td/103 §准入规则).
func ValidateOptimizationInput(assets []OptimizationAsset) error {
	if len(assets) == 0 {
		return fmt.Errorf("no enabled assets")
	}
	if len(assets) > OptimizationMaxEnabledAssets {
		return fmt.Errorf("enabled assets (%d) exceeds maximum (%d)",
			len(assets), OptimizationMaxEnabledAssets)
	}
	lockedSum := 0.0
	for _, a := range assets {
		if a.Locked {
			lockedSum += a.Weight
		}
	}
	if lockedSum > 1+ResearchWeightTolerance {
		return fmt.Errorf("locked weight sum %.4f exceeds 100%%", lockedSum)
	}
	return nil
}

// CountCandidates computes the exact number of candidates that
// GenerateCandidates would produce, without allocating them.
func CountCandidates(assets []OptimizationAsset, weightStep float64) int {
	if weightStep <= 0 {
		weightStep = OptimizationDefaultWeightStep
	}
	var tunable []OptimizationAsset
	lockedSum := 0.0
	for _, a := range assets {
		if a.Locked {
			lockedSum += a.Weight
		} else {
			tunable = append(tunable, a)
		}
	}
	if len(tunable) == 0 {
		if math.Abs(lockedSum-1) <= ResearchWeightTolerance {
			return 1
		}
		return 0
	}
	remaining := 1 - lockedSum
	if remaining < -ResearchWeightTolerance {
		return 0
	}
	if remaining < 0 {
		remaining = 0
	}
	totalParts := int(math.Round(remaining / weightStep))
	if totalParts <= 0 {
		return 0
	}

	count := 0
	n := len(tunable)
	subsets := 1 << n
	for mask := 1; mask < subsets; mask++ {
		k := popcount(mask)
		if k > totalParts {
			continue
		}
		count += compositions(totalParts, k)
	}
	return count
}

// GenerateCandidates enumerates all candidate weight vectors (td/103 §权重搜索规则).
func GenerateCandidates(
	assets []OptimizationAsset, weightStep float64,
) []OptimizationWeightVector {
	if weightStep <= 0 {
		weightStep = OptimizationDefaultWeightStep
	}
	var locked []OptimizationAsset
	var tunable []OptimizationAsset
	lockedSum := 0.0
	for _, a := range assets {
		if a.Locked {
			locked = append(locked, a)
			lockedSum += a.Weight
		} else {
			tunable = append(tunable, a)
		}
	}

	if len(tunable) == 0 {
		if math.Abs(lockedSum-1) <= ResearchWeightTolerance {
			vec := OptimizationWeightVector{}
			for _, a := range locked {
				vec.Weights = append(vec.Weights, OptimizationWeightEntry{
					ItemID: a.ItemID, AssetKey: a.AssetKey, Name: a.Name,
					Weight: a.Weight, Locked: true,
				})
			}
			return []OptimizationWeightVector{vec}
		}
		return nil
	}

	remaining := 1 - lockedSum
	if remaining < -ResearchWeightTolerance {
		return nil
	}
	if remaining < 0 {
		remaining = 0
	}
	totalParts := int(math.Round(remaining / weightStep))
	if totalParts <= 0 {
		return nil
	}

	lockedEntries := make([]OptimizationWeightEntry, 0, len(locked))
	for _, a := range locked {
		lockedEntries = append(lockedEntries, OptimizationWeightEntry{
			ItemID: a.ItemID, AssetKey: a.AssetKey, Name: a.Name,
			Weight: a.Weight, Locked: true,
		})
	}

	var results []OptimizationWeightVector
	n := len(tunable)
	subsets := 1 << n
	for mask := 1; mask < subsets; mask++ {
		var subset []OptimizationAsset
		for i := 0; i < n; i++ {
			if mask&(1<<i) != 0 {
				subset = append(subset, tunable[i])
			}
		}
		k := len(subset)
		if k > totalParts {
			continue
		}

		partitions := integerCompositions(totalParts, k)
		for _, parts := range partitions {
			vec := OptimizationWeightVector{
				Weights: make([]OptimizationWeightEntry, 0, len(locked)+len(tunable)),
			}
			vec.Weights = append(vec.Weights, lockedEntries...)

			selected := map[string]bool{}
			tunableSum := 0.0
			for i, a := range subset {
				w := float64(parts[i]) * weightStep
				tunableSum += w
				vec.Weights = append(vec.Weights, OptimizationWeightEntry{
					ItemID: a.ItemID, AssetKey: a.AssetKey, Name: a.Name,
					Weight: w, Locked: false,
				})
				selected[a.ItemID] = true
			}
			// Absorb floating-point residual into the last selected asset
			// so that total always equals exactly 1.
			residual := remaining - tunableSum
			if math.Abs(residual) > 1e-15 && len(subset) > 0 {
				lastIdx := len(lockedEntries) + len(subset) - 1
				vec.Weights[lastIdx].Weight += residual
			}
			for _, a := range tunable {
				if !selected[a.ItemID] {
					vec.Weights = append(vec.Weights, OptimizationWeightEntry{
						ItemID: a.ItemID, AssetKey: a.AssetKey, Name: a.Name,
						Weight: 0, Locked: false,
					})
				}
			}
			results = append(results, vec)
		}
	}
	return results
}

// integerCompositions returns all ways to split total into k positive parts.
func integerCompositions(total, k int) [][]int {
	if k <= 0 || total < k {
		return nil
	}
	if k == 1 {
		return [][]int{{total}}
	}
	var results [][]int
	var helper func(remaining, slots int, current []int)
	helper = func(remaining, slots int, current []int) {
		if slots == 1 {
			results = append(results, append(append([]int{}, current...), remaining))
			return
		}
		for v := 1; v <= remaining-slots+1; v++ {
			helper(remaining-v, slots-1, append(current, v))
		}
	}
	helper(total, k, nil)
	return results
}

// compositions returns C(total-1, k-1): the number of ways to split total
// into k positive integer parts.
func compositions(total, k int) int {
	if k <= 0 || total < k {
		return 0
	}
	return binomial(total-1, k-1)
}

func binomial(n, k int) int {
	if k < 0 || k > n {
		return 0
	}
	if k == 0 || k == n {
		return 1
	}
	if k > n-k {
		k = n - k
	}
	result := 1
	for i := 0; i < k; i++ {
		result = result * (n - i) / (i + 1)
	}
	return result
}

func popcount(x int) int {
	count := 0
	for x != 0 {
		count += x & 1
		x >>= 1
	}
	return count
}

// TopKTracker maintains the top-K items for one objective.
type TopKTracker struct {
	objective OptimizationObjective
	k         int
	items     []OptimizationResultItem
	less      func(a, b OptimizationResultItem) bool
}

// NewTopKTracker creates a tracker for the given objective.
func NewTopKTracker(objective OptimizationObjective, k int) *TopKTracker {
	t := &TopKTracker{objective: objective, k: k}
	switch objective {
	case ObjectiveMaxCAGR:
		t.less = func(a, b OptimizationResultItem) bool { return a.Score < b.Score }
	case ObjectiveMinDrawdown:
		// Higher max_drawdown (closer to 0) is better.
		t.less = func(a, b OptimizationResultItem) bool { return a.Score < b.Score }
	case ObjectiveMaxCalmar:
		t.less = func(a, b OptimizationResultItem) bool { return a.Score < b.Score }
	}
	return t
}

// Push considers one candidate for the top-K.
func (t *TopKTracker) Push(item OptimizationResultItem) {
	item.Objective = t.objective
	if len(t.items) < t.k {
		t.items = append(t.items, item)
		return
	}
	minIdx := 0
	for i := 1; i < len(t.items); i++ {
		if t.less(t.items[i], t.items[minIdx]) {
			minIdx = i
		}
	}
	if t.less(t.items[minIdx], item) {
		t.items[minIdx] = item
	}
}

// Results returns the sorted top-K with ranks assigned.
func (t *TopKTracker) Results() []OptimizationResultItem {
	out := make([]OptimizationResultItem, len(t.items))
	copy(out, t.items)
	sort.Slice(out, func(i, j int) bool {
		return !t.less(out[i], out[j])
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

// ScoreForObjective extracts the score from a backtest summary.
func ScoreForObjective(objective OptimizationObjective, summary BacktestSummary) (float64, bool) {
	switch objective {
	case ObjectiveMaxCAGR:
		return summary.CAGR, true
	case ObjectiveMinDrawdown:
		return summary.MaxDrawdown, true
	case ObjectiveMaxCalmar:
		if summary.Calmar != nil {
			return *summary.Calmar, true
		}
		if summary.MaxDrawdown != 0 {
			return summary.CAGR / math.Abs(summary.MaxDrawdown), true
		}
		return 0, false
	default:
		return 0, false
	}
}
