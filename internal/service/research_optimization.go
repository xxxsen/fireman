package service

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var (
	errOptimizationWeightStep        = errors.New("weight_step must be within (0, 1]")
	errOptimizationMaxCandidateCount = errors.New("max_candidate_count must be positive")
	errOptimizationTopK              = errors.New("top_k must be within [1, 100]")
	errOptimizationNoEnabledAssets   = errors.New("no enabled assets")
	errOptimizationTooManyAssets     = errors.New("too many enabled assets")
	errOptimizationLockedWeight      = errors.New("locked weight exceeds 100%")
	errOptimizationAllCandidates     = errors.New("all optimization candidates failed")
	errOptimizationSampleMismatch    = errors.New("optimization candidates used inconsistent effective samples")
	errOptimizationInvalidEntry      = errors.New("调优结果包含无效资产身份或权重，请重新运行调优")
	errOptimizationAssetMismatch     = errors.New("调优结果与当前组合资产不一致，请重新运行调优")
	errOptimizationDuplicateAsset    = errors.New("调优结果包含重复资产，请重新运行调优")
	errOptimizationWeightSum         = errors.New("调优结果权重合计不是 100%，请重新运行调优")
	errOptimizationMinimumCAGR       = errors.New("minimum_cagr must be finite and within [-0.95, 2.0]")
)

// research_optimization.go implements the pure candidate generation and
// objective ranking core for portfolio auto-optimization (td/103).
// No I/O: the service layer feeds inputs and the job runner orchestrates.

const (
	OptimizationEngineVersion       = "research_optimizer_v4"
	OptimizationTailRiskV3Version   = "research_optimizer_v3"
	OptimizationDefaultWeightStep   = 0.05
	OptimizationDefaultTopK         = 20
	OptimizationDefaultMaxCandidate = 20000
	OptimizationMaxEnabledAssets    = 10
	OptimizationHardMaxCandidate    = 20000
)

func optimizationEngineHasTailRiskSnapshot(version string) bool {
	return version == OptimizationEngineVersion || version == OptimizationTailRiskV3Version
}

// OptimizationObjective identifies which metric to rank by.
type OptimizationObjective string

const (
	ObjectiveMaxCAGR     OptimizationObjective = "max_cagr"
	ObjectiveMinDrawdown OptimizationObjective = "min_drawdown"
	ObjectiveMaxCalmar   OptimizationObjective = "max_calmar"
	ObjectiveMinCVaR     OptimizationObjective = "min_cvar"
)

// OptimizationConfig is the user-controllable config for one optimization run.
type OptimizationConfig struct {
	WeightStep        float64      `json:"weight_step"`
	MaxCandidateCount int          `json:"max_candidate_count"`
	TopK              int          `json:"top_k"`
	TailRisk          TailRiskSpec `json:"tail_risk"`
	MinimumCAGR       *float64     `json:"minimum_cagr,omitempty"`
}

// NormalizeDefaults fills zero fields with defaults.
func (c *OptimizationConfig) NormalizeDefaults() {
	if c.WeightStep == 0 {
		c.WeightStep = OptimizationDefaultWeightStep
	}
	if c.MaxCandidateCount == 0 {
		c.MaxCandidateCount = OptimizationDefaultMaxCandidate
	}
	if c.MaxCandidateCount > OptimizationHardMaxCandidate {
		c.MaxCandidateCount = OptimizationHardMaxCandidate
	}
	if c.TopK == 0 {
		c.TopK = OptimizationDefaultTopK
	}
}

func (c OptimizationConfig) Validate() error {
	if !optimizationFinite(c.WeightStep) || c.WeightStep <= 0 || c.WeightStep > 1 {
		return errOptimizationWeightStep
	}
	if c.MaxCandidateCount <= 0 {
		return errOptimizationMaxCandidateCount
	}
	if c.TopK <= 0 || c.TopK > 100 {
		return errOptimizationTopK
	}
	if c.TailRisk.Confidence != 0 || c.TailRisk.HorizonDays != 0 {
		if _, err := CanonicalTailRiskSpec(c.TailRisk); err != nil {
			return err
		}
	}
	if c.MinimumCAGR != nil && (!optimizationFinite(*c.MinimumCAGR) || *c.MinimumCAGR < -0.95 || *c.MinimumCAGR > 2) {
		return errOptimizationMinimumCAGR
	}
	return nil
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
	ItemID   string  `json:"item_id"`
	AssetKey string  `json:"asset_key"`
	Name     string  `json:"name"`
	Weight   float64 `json:"weight"`
	Locked   bool    `json:"locked"`
}

// OptimizationResultItem is one ranked candidate result.
type OptimizationResultItem struct {
	Rank      int                       `json:"rank"`
	Objective OptimizationObjective     `json:"objective"`
	Score     float64                   `json:"score"`
	Weights   []OptimizationWeightEntry `json:"weights"`
	Summary   BacktestSummary           `json:"summary"`
}

// OptimizationResult is the final output of one optimization run.
type OptimizationResult struct {
	CandidateCount    int                      `json:"candidate_count"`
	EvaluatedCount    int                      `json:"evaluated_count"`
	SkippedCount      int                      `json:"skipped_count"`
	BestByCAGR        []OptimizationResultItem `json:"best_by_cagr"`
	BestByDrawdown    []OptimizationResultItem `json:"best_by_drawdown"`
	BestByCalmar      []OptimizationResultItem `json:"best_by_calmar"`
	BestByCVaR        []OptimizationResultItem `json:"best_by_cvar"`
	CVaREligibleCount int                      `json:"cvar_eligible_count"`
	Warnings          []OptimizationWarning    `json:"warnings,omitempty"`
}

type OptimizationWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidateOptimizationInput checks preconditions (td/103 §准入规则).
func ValidateOptimizationInput(assets []OptimizationAsset) error {
	if len(assets) == 0 {
		return errOptimizationNoEnabledAssets
	}
	if len(assets) > OptimizationMaxEnabledAssets {
		return fmt.Errorf("%w: enabled=%d maximum=%d", errOptimizationTooManyAssets,
			len(assets), OptimizationMaxEnabledAssets)
	}
	lockedSum := 0.0
	for _, a := range assets {
		if a.Locked {
			lockedSum += a.Weight
		}
	}
	if lockedSum > 1+1e-12 {
		return fmt.Errorf("%w: sum=%.4f", errOptimizationLockedWeight, lockedSum)
	}
	return nil
}

// CountCandidates computes the exact number for every runnable grid. Once the
// hard execution limit is exceeded it returns limit+1 immediately; readiness
// blocks that grid, so enumerating the unreachable tail would only waste work.
func CountCandidates(assets []OptimizationAsset, weightStep float64) int {
	return enumerateOptimizationCandidates(assets, weightStep, nil, OptimizationHardMaxCandidate+1)
}

// GenerateCandidates enumerates all candidate weight vectors (td/103 §权重搜索规则).
func GenerateCandidates(
	assets []OptimizationAsset, weightStep float64,
) []OptimizationWeightVector {
	var results []OptimizationWeightVector
	enumerateOptimizationCandidates(assets, weightStep, func(vec OptimizationWeightVector) {
		results = append(results, vec)
	}, 0)
	return results
}

// enumerateOptimizationCandidates is the single source of truth for both
// counting and generating the residual-aware candidate grid.
func enumerateOptimizationCandidates(
	assets []OptimizationAsset,
	weightStep float64,
	emit func(OptimizationWeightVector),
	limit int,
) int {
	grid, ok := prepareOptimizationGrid(assets, weightStep)
	if !ok {
		return 0
	}
	if math.Abs(grid.remaining) <= 1e-12 {
		return emitOptimizationCandidate(grid.locked, grid.tunable, nil, emit)
	}
	if len(grid.tunable) == 0 {
		return 0
	}
	count := 0
	seen := map[string]struct{}{}
	n := len(grid.tunable)
	for mask := 1; mask < 1<<n; mask++ {
		indices := optimizationSubsetIndices(mask, n)
		count += enumerateOptimizationSubset(grid, indices, seen, emit, remainingCandidateLimit(limit, count))
		if limit > 0 && count >= limit {
			return limit
		}
	}
	return count
}

type optimizationGrid struct {
	locked, tunable []OptimizationAsset
	weightStep      float64
	remaining       float64
	fullParts       int
	residual        float64
}

func prepareOptimizationGrid(assets []OptimizationAsset, weightStep float64) (optimizationGrid, bool) {
	const eps = 1e-12
	if !optimizationFinite(weightStep) || weightStep <= 0 {
		weightStep = OptimizationDefaultWeightStep
	}
	grid := optimizationGrid{weightStep: weightStep}
	lockedSum := 0.0
	for _, asset := range assets {
		if !optimizationFinite(asset.Weight) || asset.Weight < 0 {
			return optimizationGrid{}, false
		}
		if asset.Locked {
			grid.locked = append(grid.locked, asset)
			lockedSum += asset.Weight
		} else {
			grid.tunable = append(grid.tunable, asset)
		}
	}
	grid.remaining = 1 - lockedSum
	if grid.remaining < -eps {
		return optimizationGrid{}, false
	}
	grid.fullParts = int(math.Floor((grid.remaining + eps) / weightStep))
	grid.residual = grid.remaining - float64(grid.fullParts)*weightStep
	if math.Abs(grid.residual) <= eps {
		grid.residual = 0
	} else if grid.residual < 0 {
		grid.fullParts--
		grid.residual = grid.remaining - float64(grid.fullParts)*weightStep
	}
	return grid, true
}

func optimizationSubsetIndices(mask, size int) []int {
	indices := make([]int, 0, popcount(mask))
	for i := 0; i < size; i++ {
		if mask&(1<<i) != 0 {
			indices = append(indices, i)
		}
	}
	return indices
}

func enumerateOptimizationSubset(
	grid optimizationGrid,
	indices []int,
	seen map[string]struct{},
	emit func(OptimizationWeightVector),
	limit int,
) int {
	if grid.residual == 0 {
		return enumerateExactOptimizationSubset(grid, indices, seen, emit, limit)
	}
	return enumerateResidualOptimizationSubset(grid, indices, seen, emit, limit)
}

func enumerateExactOptimizationSubset(
	grid optimizationGrid,
	indices []int,
	seen map[string]struct{},
	emit func(OptimizationWeightVector),
	limit int,
) int {
	count := 0
	for _, parts := range integerCompositions(grid.fullParts, len(indices)) {
		weights := make([]float64, len(grid.tunable))
		for i, index := range indices {
			weights[index] = float64(parts[i]) * grid.weightStep
		}
		count += emitUniqueOptimizationCandidate(grid.locked, grid.tunable, weights, seen, emit)
		if limit > 0 && count >= limit {
			return limit
		}
	}
	return count
}

func enumerateResidualOptimizationSubset(
	grid optimizationGrid,
	indices []int,
	seen map[string]struct{},
	emit func(OptimizationWeightVector),
	limit int,
) int {
	minimumParts := len(indices) - 1
	if grid.fullParts < minimumParts {
		return 0
	}
	count := 0
	for receiverPos, receiverIdx := range indices {
		for _, extras := range integerWeakCompositions(grid.fullParts-minimumParts, len(indices)) {
			weights := residualOptimizationWeights(grid, indices, extras, receiverPos)
			weights[receiverIdx] += grid.residual
			count += emitUniqueOptimizationCandidate(grid.locked, grid.tunable, weights, seen, emit)
			if limit > 0 && count >= limit {
				return limit
			}
		}
	}
	return count
}

func residualOptimizationWeights(
	grid optimizationGrid, indices, extras []int, receiverPos int,
) []float64 {
	weights := make([]float64, len(grid.tunable))
	for pos, index := range indices {
		parts := extras[pos]
		if pos != receiverPos {
			parts++
		}
		weights[index] = float64(parts) * grid.weightStep
	}
	return weights
}

func remainingCandidateLimit(limit, count int) int {
	if limit <= 0 {
		return 0
	}
	return limit - count
}

func optimizationFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func emitUniqueOptimizationCandidate(
	locked, tunable []OptimizationAsset,
	weights []float64,
	seen map[string]struct{},
	emit func(OptimizationWeightVector),
) int {
	vec := buildOptimizationCandidate(locked, tunable, weights)
	key := canonicalOptimizationWeights(vec.Weights)
	if _, ok := seen[key]; ok {
		return 0
	}
	seen[key] = struct{}{}
	if emit != nil {
		emit(vec)
	}
	return 1
}

func emitOptimizationCandidate(
	locked, tunable []OptimizationAsset,
	weights []float64,
	emit func(OptimizationWeightVector),
) int {
	if emit != nil {
		emit(buildOptimizationCandidate(locked, tunable, weights))
	}
	return 1
}

func buildOptimizationCandidate(
	locked, tunable []OptimizationAsset, tunableWeights []float64,
) OptimizationWeightVector {
	vec := OptimizationWeightVector{Weights: make([]OptimizationWeightEntry, 0, len(locked)+len(tunable))}
	for _, asset := range locked {
		vec.Weights = append(vec.Weights, optimizationWeightEntry(asset, asset.Weight, true))
	}
	for i, asset := range tunable {
		weight := 0.0
		if i < len(tunableWeights) {
			weight = tunableWeights[i]
		}
		vec.Weights = append(vec.Weights, optimizationWeightEntry(asset, weight, false))
	}
	return vec
}

func optimizationWeightEntry(asset OptimizationAsset, weight float64, locked bool) OptimizationWeightEntry {
	return OptimizationWeightEntry{
		ItemID: asset.ItemID, AssetKey: asset.AssetKey, Name: asset.Name,
		Weight: weight, Locked: locked,
	}
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

// integerWeakCompositions returns all ordered splits into non-negative parts.
func integerWeakCompositions(total, k int) [][]int {
	if total < 0 || k <= 0 {
		return nil
	}
	if k == 1 {
		return [][]int{{total}}
	}
	var results [][]int
	for first := 0; first <= total; first++ {
		for _, rest := range integerWeakCompositions(total-first, k-1) {
			results = append(results, append([]int{first}, rest...))
		}
	}
	return results
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
	seen      map[string]struct{}
}

// NewTopKTracker creates a tracker for the given objective.
func NewTopKTracker(objective OptimizationObjective, k int) *TopKTracker {
	return &TopKTracker{objective: objective, k: k, seen: map[string]struct{}{}}
}

// Push considers one candidate for the top-K.
func (t *TopKTracker) Push(item OptimizationResultItem) {
	if t.k <= 0 {
		return
	}
	item.Objective = t.objective
	key := canonicalOptimizationWeights(item.Weights)
	if _, ok := t.seen[key]; ok {
		return
	}
	t.seen[key] = struct{}{}
	if len(t.items) < t.k {
		t.items = append(t.items, item)
		return
	}
	worstIdx := 0
	for i := 1; i < len(t.items); i++ {
		if optimizationResultBetter(t.items[worstIdx], t.items[i]) {
			worstIdx = i
		}
	}
	if optimizationResultBetter(item, t.items[worstIdx]) {
		t.items[worstIdx] = item
	}
}

// Results returns the sorted top-K with ranks assigned.
func (t *TopKTracker) Results() []OptimizationResultItem {
	out := make([]OptimizationResultItem, len(t.items))
	copy(out, t.items)
	sort.Slice(out, func(i, j int) bool {
		return optimizationResultBetter(out[i], out[j])
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}

func optimizationResultBetter(a, b OptimizationResultItem) bool {
	if a.Objective == ObjectiveMinCVaR && b.Objective == ObjectiveMinCVaR &&
		a.Summary.TailRisk != nil && b.Summary.TailRisk != nil {
		if a.Summary.TailRisk.CVaRLoss != b.Summary.TailRisk.CVaRLoss {
			return a.Summary.TailRisk.CVaRLoss < b.Summary.TailRisk.CVaRLoss
		}
		if a.Summary.TailRisk.VaRLoss != b.Summary.TailRisk.VaRLoss {
			return a.Summary.TailRisk.VaRLoss < b.Summary.TailRisk.VaRLoss
		}
	}
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Summary.CAGR != b.Summary.CAGR {
		return a.Summary.CAGR > b.Summary.CAGR
	}
	aDrawdown, bDrawdown := math.Abs(a.Summary.MaxDrawdown), math.Abs(b.Summary.MaxDrawdown)
	if aDrawdown != bDrawdown {
		return aDrawdown < bDrawdown
	}
	return canonicalOptimizationWeights(a.Weights) < canonicalOptimizationWeights(b.Weights)
}

func canonicalOptimizationWeights(weights []OptimizationWeightEntry) string {
	entries := append([]OptimizationWeightEntry(nil), weights...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ItemID != entries[j].ItemID {
			return entries[i].ItemID < entries[j].ItemID
		}
		return entries[i].AssetKey < entries[j].AssetKey
	})
	var out strings.Builder
	for _, entry := range entries {
		out.WriteString(entry.ItemID)
		out.WriteByte('|')
		out.WriteString(entry.AssetKey)
		out.WriteByte('=')
		out.WriteString(strconv.FormatFloat(entry.Weight, 'f', 12, 64))
		out.WriteByte(';')
	}
	return out.String()
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
	case ObjectiveMinCVaR:
		if summary.TailRisk == nil {
			return 0, false
		}
		return -summary.TailRisk.CVaRLoss, true
	default:
		return 0, false
	}
}
