package improvement

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

var (
	ErrMonotonicityViolation = errors.New("improvement monotonicity violation")
	ErrResultInconsistent    = errors.New("improvement result inconsistent")
)

type Evaluator func(context.Context, *simulation.InputSnapshot) (simulation.OutcomeEvaluation, error)

type SearchOptions struct {
	Parallelism int
	Progress    func(done, total int, phase string)
	Evaluator   Evaluator
}

type searcher struct {
	ctx         context.Context
	runID       string
	base        simulation.InputSnapshot
	baseConfig  domain.ConfigHashInput
	config      Config
	baseline    simulation.OutcomeEvaluation
	evaluator   Evaluator
	parallelism int
	progress    func(done, total int, phase string)
	mu          sync.Mutex
	cache       map[string]Evaluation
	outcomes    map[string][]bool
	inflight    map[string]*evaluationCall
	evaluated   int
	upperBound  int
}

type evaluationCall struct {
	done chan struct{}
	eval Evaluation
	err  error
}

type recipeCandidate struct {
	recipe string
	eval   Evaluation
}

//nolint:funlen,gocognit,gocyclo // Validation, replay, recipe execution and result assembly form one audit boundary.
func Search(ctx context.Context, runID string, frozen FrozenInput, opt SearchOptions) (Result, error) {
	if err := frozen.Config.Validate(frozen.SourceSnapshot.Parameters.EndAge,
		frozen.SourceSnapshot.Parameters.RetirementAge); err != nil {
		return Result{}, err
	}
	baseConfig, err := decodeConfigHashInput(frozen.ConfigHashInputJSON)
	if err != nil {
		return Result{}, fmt.Errorf("%w: decode config hash input: %w", ErrResultInconsistent, err)
	}
	baseConfigHash, err := domain.ComputeConfigHash(cloneConfigHashInput(baseConfig))
	if err != nil || baseConfigHash != frozen.SourceSnapshot.ConfigHash {
		return Result{}, fmt.Errorf("%w: source config hash mismatch", ErrResultInconsistent)
	}
	bits, err := DecodeOutcomeBits(
		frozen.BaselineBits, frozen.BaselineHash, frozen.SourceSnapshot.Parameters.SimulationRuns,
	)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrResultInconsistent, err)
	}
	if len(frozen.Baseline.Outcomes) > 0 && !equalOutcomes(frozen.Baseline.Outcomes, bits) {
		return Result{}, fmt.Errorf("%w: baseline outcome copies differ", ErrResultInconsistent)
	}
	frozen.Baseline.Outcomes = bits
	if frozen.Baseline.Runs != len(bits) || countSuccess(bits) != frozen.Baseline.SuccessCount {
		return Result{}, fmt.Errorf("%w: baseline aggregates differ from outcomes", ErrResultInconsistent)
	}
	evaluator := opt.Evaluator
	if evaluator == nil {
		evaluator = func(ctx context.Context, in *simulation.InputSnapshot) (simulation.OutcomeEvaluation, error) {
			return simulation.EvaluateOutcomes(in, simulation.RunOptions{
				Runs:        in.Parameters.SimulationRuns,
				CancelCheck: func() bool { return ctx.Err() != nil },
			})
		}
	}
	s := &searcher{
		ctx: ctx, runID: runID, base: frozen.SourceSnapshot, baseConfig: baseConfig,
		config: frozen.Config, baseline: frozen.Baseline, evaluator: evaluator,
		progress: opt.Progress, cache: map[string]Evaluation{}, outcomes: map[string][]bool{},
		inflight: map[string]*evaluationCall{}, parallelism: opt.Parallelism,
	}
	if s.parallelism < 1 {
		s.parallelism = 1
	}
	if s.parallelism > 16 {
		s.parallelism = 16
	}
	s.upperBound = SearchUpperBound(frozen.Config)
	replayed, err := s.evaluator(ctx, &s.base)
	if err != nil {
		return Result{}, err
	}
	if !sameOutcomeEvaluation(replayed, s.baseline) {
		return Result{}, fmt.Errorf("%w: source path replay differs from frozen baseline", ErrResultInconsistent)
	}
	baseline, err := s.baselineEvaluation()
	if err != nil {
		return Result{}, err
	}
	candidates, recipes, err := s.runRecipes()
	if err != nil {
		return Result{}, err
	}
	proposals := s.proposals(pareto(candidates))
	proposalByRecipe := make(map[string]string, len(proposals))
	for _, proposal := range proposals {
		if _, exists := proposalByRecipe[proposal.Recipe]; !exists {
			proposalByRecipe[proposal.Recipe] = proposal.ID
		}
	}
	for i := range recipes {
		if proposalID, ok := proposalByRecipe[recipes[i].Recipe]; ok {
			recipes[i].Status = "feasible"
			recipes[i].ProposalID = proposalID
		} else if recipes[i].Status == "feasible" {
			recipes[i].Status = "duplicate"
			recipes[i].ProposalID = ""
		}
	}
	evaluations := s.sortedEvaluations()
	result := Result{
		AlgorithmVersion: AlgorithmVersion, TargetProbability: frozen.Config.TargetSuccessProbability,
		Baseline: baseline, TargetReached: len(proposals) > 0, Proposals: proposals,
		Recipes: recipes, Evaluations: evaluations, EvaluatedCount: len(evaluations) - 1,
	}
	if !result.TargetReached {
		if best, ok := s.bestAttainable(evaluations); ok {
			proposal := s.proposal(RecipeBalanced, best)
			result.BestAttainable = &proposal
		}
	}
	return result, nil
}

func SearchUpperBound(config Config) int {
	bound := 1
	if config.RetirementDelay != nil {
		bound += config.RetirementDelay.MaxDelayYears
	}
	for _, levels := range [][]int64{
		increaseLevels(config.SavingsIncrease), reductionLevels(config.SpendingReduction),
		increaseLevels(config.RetirementIncomeIncrease),
	} {
		if len(levels) > 0 {
			bound += binaryEvaluationBound(len(levels))
		}
	}
	if hasMoneyLever(config) {
		delays := 1
		if config.RetirementDelay != nil {
			delays += config.RetirementDelay.MaxDelayYears
		}
		bound += delays * binaryEvaluationBound(len(balancedLevels(config))-1)
	}
	return bound
}

func binaryEvaluationBound(levels int) int {
	if levels <= 0 {
		return 0
	}
	bound := 3
	for n := levels; n > 1; n = (n + 1) / 2 {
		bound++
	}
	return min(levels, bound)
}

func (s *searcher) baselineEvaluation() (Evaluation, error) {
	hash, err := simulation.HashInput(&s.base)
	if err != nil {
		return Evaluation{}, fmt.Errorf("hash baseline snapshot: %w", err)
	}
	eval := evaluationFromOutcome(Adjustments{}, s.baseline, s.baseline.Outcomes,
		s.baseline.Outcomes, s.config.TargetSuccessProbability)
	eval.CandidateConfigHash = s.base.ConfigHash
	eval.CandidateSnapshotHash = hash
	s.cache[tupleKey(Adjustments{})] = eval
	s.outcomes[tupleKey(Adjustments{})] = append([]bool(nil), s.baseline.Outcomes...)
	return eval, nil
}

//nolint:funlen,gocognit,gocyclo,nestif // Recipe order and shared-cache calls are intentionally explicit.
func (s *searcher) runRecipes() ([]recipeCandidate, []RecipeResult, error) {
	if err := s.prefetchBoundaries(); err != nil {
		return nil, nil, err
	}
	candidates := make([]recipeCandidate, 0, 8)
	recipes := make([]RecipeResult, 0, 5)
	if lever := s.config.RetirementDelay; lever != nil && lever.MaxDelayYears > 0 {
		var found *Evaluation
		for delay := 1; delay <= lever.MaxDelayYears; delay++ {
			eval, err := s.evaluate(Adjustments{DelayYears: delay})
			if err != nil {
				return nil, nil, err
			}
			if eval.MeetsTarget && found == nil {
				matched := eval
				found = &matched
			}
		}
		candidates, recipes = appendRecipe(candidates, recipes, RecipePureDelay, found, s.runID)
	}
	var err error
	candidates, recipes, err = s.moneyRecipe(candidates, recipes, RecipePureSavings,
		increaseLevels(s.config.SavingsIncrease), func(value int64) Adjustments {
			return Adjustments{SavingsIncreaseMinor: value}
		})
	if err != nil {
		return nil, nil, err
	}
	candidates, recipes, err = s.moneyRecipe(candidates, recipes, RecipePureSpending,
		reductionLevels(s.config.SpendingReduction), func(value int64) Adjustments {
			return Adjustments{SpendingReductionMinor: value}
		})
	if err != nil {
		return nil, nil, err
	}
	candidates, recipes, err = s.moneyRecipe(candidates, recipes, RecipePureRetirementIncome,
		increaseLevels(s.config.RetirementIncomeIncrease), func(value int64) Adjustments {
			return Adjustments{RetirementIncomeIncreaseMinor: value}
		})
	if err != nil {
		return nil, nil, err
	}
	if hasMoneyLever(s.config) {
		levels := balancedLevels(s.config)
		maxDelay := 0
		if s.config.RetirementDelay != nil {
			maxDelay = s.config.RetirementDelay.MaxDelayYears
		}
		var foundAny bool
		for delay := 0; delay <= maxDelay; delay++ {
			adjustments := make([]Adjustments, 0, len(levels)-1)
			for _, level := range levels[1:] {
				out := level.adjustments
				out.DelayYears = delay
				adjustments = append(adjustments, out)
			}
			found, searchErr := s.binaryFirst(adjustments)
			if searchErr != nil {
				return nil, nil, searchErr
			}
			if found != nil {
				foundAny = true
				candidates = append(candidates, recipeCandidate{recipe: RecipeBalanced, eval: *found})
			}
		}
		if foundAny {
			recipes = append(recipes, RecipeResult{Recipe: RecipeBalanced, Status: "feasible"})
		} else {
			recipes = append(recipes, RecipeResult{Recipe: RecipeBalanced, Status: "unattainable"})
		}
	}
	for i := range candidates {
		id := proposalID(s.runID, candidates[i].eval.Adjustments)
		for j := range recipes {
			if recipes[j].Recipe == candidates[i].recipe && recipes[j].ProposalID == "" {
				recipes[j].ProposalID = id
				break
			}
		}
	}
	return candidates, recipes, nil
}

// prefetchBoundaries evaluates independent recipe boundaries concurrently.
// Binary-search dependencies remain sequential, while pure and per-delay
// maximum checks share the single-flight cache below.
func (s *searcher) prefetchBoundaries() error {
	unique := make(map[string]Adjustments)
	add := func(a Adjustments) { unique[tupleKey(a)] = a }
	if lever := s.config.RetirementDelay; lever != nil {
		for delay := 1; delay <= lever.MaxDelayYears; delay++ {
			add(Adjustments{DelayYears: delay})
		}
	}
	if levels := increaseLevels(s.config.SavingsIncrease); len(levels) > 0 {
		add(Adjustments{SavingsIncreaseMinor: levels[len(levels)-1]})
	}
	if levels := reductionLevels(s.config.SpendingReduction); len(levels) > 0 {
		add(Adjustments{SpendingReductionMinor: levels[len(levels)-1]})
	}
	if levels := increaseLevels(s.config.RetirementIncomeIncrease); len(levels) > 0 {
		add(Adjustments{RetirementIncomeIncreaseMinor: levels[len(levels)-1]})
	}
	if hasMoneyLever(s.config) {
		levels := balancedLevels(s.config)
		maximum := levels[len(levels)-1].adjustments
		maxDelay := 0
		if s.config.RetirementDelay != nil {
			maxDelay = s.config.RetirementDelay.MaxDelayYears
		}
		for delay := 0; delay <= maxDelay; delay++ {
			candidate := maximum
			candidate.DelayYears = delay
			add(candidate)
		}
	}
	items := make([]Adjustments, 0, len(unique))
	for _, candidate := range unique {
		items = append(items, candidate)
	}
	sort.Slice(items, func(i, j int) bool { return adjustmentLess(items[i], items[j]) })
	semaphore := make(chan struct{}, s.parallelism)
	errorsByIndex := make([]error, len(items))
	var group sync.WaitGroup
	for i, candidate := range items {
		group.Add(1)
		go func() {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			_, errorsByIndex[i] = s.evaluate(candidate)
		}()
	}
	group.Wait()
	for _, err := range errorsByIndex {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *searcher) moneyRecipe(candidates []recipeCandidate, recipes []RecipeResult, recipe string,
	levels []int64, adjustment func(int64) Adjustments,
) ([]recipeCandidate, []RecipeResult, error) {
	if len(levels) == 0 {
		return candidates, recipes, nil
	}
	adjustments := make([]Adjustments, len(levels))
	for i, value := range levels {
		adjustments[i] = adjustment(value)
	}
	found, err := s.binaryFirst(adjustments)
	if err != nil {
		return nil, nil, err
	}
	updatedCandidates, updatedRecipes := appendRecipe(candidates, recipes, recipe, found, s.runID)
	return updatedCandidates, updatedRecipes, nil
}

func appendRecipe(candidates []recipeCandidate, recipes []RecipeResult, recipe string,
	found *Evaluation, runID string,
) ([]recipeCandidate, []RecipeResult) {
	if found == nil {
		return candidates, append(recipes, RecipeResult{Recipe: recipe, Status: "unattainable"})
	}
	id := proposalID(runID, found.Adjustments)
	return append(candidates, recipeCandidate{recipe: recipe, eval: *found}),
		append(recipes, RecipeResult{Recipe: recipe, Status: "feasible", ProposalID: id})
}

type balancedLevel struct {
	ratio       float64
	adjustments Adjustments
}

//nolint:nilnil // A nil candidate with nil error means the configured boundary is unattainable.
func (s *searcher) binaryFirst(levels []Adjustments) (*Evaluation, error) {
	if len(levels) == 0 {
		return nil, nil
	}
	maxEval, err := s.evaluate(levels[len(levels)-1])
	if err != nil {
		return nil, err
	}
	if !maxEval.MeetsTarget {
		return nil, nil
	}
	lo, hi := 0, len(levels)-1
	for lo < hi {
		mid := lo + (hi-lo)/2
		eval, err := s.evaluate(levels[mid])
		if err != nil {
			return nil, err
		}
		if eval.MeetsTarget {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	current, err := s.evaluate(levels[lo])
	if err != nil {
		return nil, err
	}
	if lo > 0 {
		previous, err := s.evaluate(levels[lo-1])
		if err != nil {
			return nil, err
		}
		if previous.MeetsTarget || !current.MeetsTarget {
			return nil, ErrMonotonicityViolation
		}
	}
	return &current, nil
}

func (s *searcher) evaluate(a Adjustments) (Evaluation, error) {
	key := tupleKey(a)
	s.mu.Lock()
	if cached, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return cached, nil
	}
	if call, ok := s.inflight[key]; ok {
		s.mu.Unlock()
		select {
		case <-s.ctx.Done():
			return Evaluation{}, fmt.Errorf("wait for candidate evaluation: %w", s.ctx.Err())
		case <-call.done:
			return call.eval, call.err
		}
	}
	call := &evaluationCall{done: make(chan struct{})}
	s.inflight[key] = call
	s.mu.Unlock()
	eval, outcomes, err := s.evaluateCandidate(a)
	s.mu.Lock()
	if err == nil {
		err = s.checkMonotonicityLocked(a, outcomes)
	}
	if err == nil {
		s.cache[key], s.outcomes[key] = eval, append([]bool(nil), outcomes...)
		s.evaluated++
		if s.evaluated > s.upperBound {
			err = ErrResultInconsistent
		} else if s.progress != nil {
			s.progress(s.evaluated, s.upperBound, "searching")
		}
	}
	call.eval, call.err = eval, err
	delete(s.inflight, key)
	close(call.done)
	s.mu.Unlock()
	return eval, err
}

func (s *searcher) evaluateCandidate(a Adjustments) (Evaluation, []bool, error) {
	if err := s.ctx.Err(); err != nil {
		return Evaluation{}, nil, fmt.Errorf("candidate evaluation canceled: %w", err)
	}
	snapshot, err := ApplyAdjustments(s.base, a)
	if err != nil {
		return Evaluation{}, nil, err
	}
	configInput, err := ApplyConfigAdjustments(s.baseConfig, a)
	if err != nil {
		return Evaluation{}, nil, err
	}
	configHash, err := domain.ComputeConfigHash(configInput)
	if err != nil {
		return Evaluation{}, nil, fmt.Errorf("hash candidate config: %w", err)
	}
	snapshot.ConfigHash = configHash
	snapshotHash, err := simulation.HashInput(&snapshot)
	if err != nil {
		return Evaluation{}, nil, fmt.Errorf("hash candidate snapshot: %w", err)
	}
	outcome, err := s.evaluator(s.ctx, &snapshot)
	if err != nil {
		return Evaluation{}, nil, err
	}
	if outcome.Runs != s.baseline.Runs || len(outcome.Outcomes) != s.baseline.Runs ||
		countSuccess(outcome.Outcomes) != outcome.SuccessCount {
		return Evaluation{}, nil, ErrResultInconsistent
	}
	eval := evaluationFromOutcome(a, outcome, s.baseline.Outcomes, outcome.Outcomes,
		s.config.TargetSuccessProbability)
	eval.CandidateConfigHash, eval.CandidateSnapshotHash = configHash, snapshotHash
	return eval, outcome.Outcomes, nil
}

func (s *searcher) checkMonotonicityLocked(a Adjustments, outcomes []bool) error {
	for key, other := range s.cache {
		b := other.Adjustments
		if a.DelayYears != b.DelayYears {
			continue
		}
		var lower, higher []bool
		if financialLE(a, b) {
			lower, higher = outcomes, s.outcomes[key]
		}
		if financialLE(b, a) {
			lower, higher = s.outcomes[key], outcomes
		}
		if lower == nil {
			continue
		}
		for i := range lower {
			if lower[i] && !higher[i] {
				return ErrMonotonicityViolation
			}
		}
	}
	return nil
}

func evaluationFromOutcome(a Adjustments, outcome simulation.OutcomeEvaluation,
	baseline, candidate []bool, target float64,
) Evaluation {
	e := Evaluation{
		Adjustments: a, Runs: outcome.Runs, SuccessCount: outcome.SuccessCount,
		SuccessProbability: outcome.SuccessProbability, SuccessWilsonLow: outcome.SuccessWilsonLow,
		SuccessWilsonHigh: outcome.SuccessWilsonHigh, TerminalP50Minor: outcome.TerminalP50Minor,
		MaxDrawdownP95: outcome.MaxDrawdownP95, MeetsTarget: outcome.SuccessWilsonLow >= target,
	}
	for i := range baseline {
		switch {
		case !baseline[i] && candidate[i]:
			e.ImprovedPathCount++
		case baseline[i] && !candidate[i]:
			e.RegressedPathCount++
		case baseline[i] && candidate[i]:
			e.UnchangedSuccessCount++
		default:
			e.UnchangedFailureCount++
		}
	}
	return e
}

func (s *searcher) proposals(candidates []recipeCandidate) []Proposal {
	out := make([]Proposal, len(candidates))
	for i, candidate := range candidates {
		out[i] = s.proposal(candidate.recipe, candidate.eval)
	}
	sort.Slice(out, func(i, j int) bool { return proposalLess(out[i], out[j]) })
	return out
}

func (s *searcher) proposal(recipe string, e Evaluation) Proposal {
	a := e.Adjustments
	p := s.base.Parameters
	return Proposal{
		ID: proposalID(s.runID, a), Recipe: recipe, DelayYears: a.DelayYears,
		SavingsIncreaseMinor: a.SavingsIncreaseMinor, SpendingReductionMinor: a.SpendingReductionMinor,
		RetirementIncomeIncreaseMinor: a.RetirementIncomeIncreaseMinor,
		ResultRetirementAge:           p.RetirementAge + a.DelayYears,
		ResultAnnualSavingsMinor:      p.AnnualSavingsMinor + a.SavingsIncreaseMinor,
		ResultAnnualSpendingMinor:     p.AnnualSpendingMinor - a.SpendingReductionMinor,
		ResultRetirementIncomeMinor:   p.AnnualRetirementIncomeMinor + a.RetirementIncomeIncreaseMinor,
		SuccessProbability:            e.SuccessProbability, SuccessWilsonLow: e.SuccessWilsonLow,
		SuccessWilsonHigh: e.SuccessWilsonHigh, TerminalP50Minor: e.TerminalP50Minor,
		MaxDrawdownP95: e.MaxDrawdownP95, ImprovedPathCount: e.ImprovedPathCount,
		RegressedPathCount: e.RegressedPathCount, CandidateConfigHash: e.CandidateConfigHash,
		CandidateSnapshotHash: e.CandidateSnapshotHash,
	}
}

func (s *searcher) sortedEvaluations() []Evaluation {
	out := make([]Evaluation, 0, len(s.cache))
	for _, eval := range s.cache {
		out = append(out, eval)
	}
	sort.Slice(out, func(i, j int) bool { return adjustmentLess(out[i].Adjustments, out[j].Adjustments) })
	return out
}

func (s *searcher) bestAttainable(evals []Evaluation) (Evaluation, bool) {
	var candidates []Evaluation
	for _, eval := range evals {
		if eval.Adjustments != (Adjustments{}) {
			candidates = append(candidates, eval)
		}
	}
	if len(candidates) == 0 {
		return Evaluation{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.SuccessWilsonLow != b.SuccessWilsonLow {
			return a.SuccessWilsonLow > b.SuccessWilsonLow
		}
		if a.SuccessProbability != b.SuccessProbability {
			return a.SuccessProbability > b.SuccessProbability
		}
		return adjustmentLess(a.Adjustments, b.Adjustments)
	})
	return candidates[0], true
}

func pareto(in []recipeCandidate) []recipeCandidate {
	out := make([]recipeCandidate, 0, len(in))
	seen := map[string]struct{}{}
	for i, candidate := range in {
		key := tupleKey(candidate.eval.Adjustments)
		if _, ok := seen[key]; ok {
			continue
		}
		dominated := false
		for j, other := range in {
			if i == j {
				continue
			}
			if dominates(other.eval, candidate.eval) {
				dominated = true
				break
			}
		}
		if !dominated {
			seen[key] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func dominates(a, b Evaluation) bool {
	aa, bb := a.Adjustments, b.Adjustments
	le := aa.DelayYears <= bb.DelayYears && aa.SavingsIncreaseMinor <= bb.SavingsIncreaseMinor &&
		aa.SpendingReductionMinor <= bb.SpendingReductionMinor &&
		aa.RetirementIncomeIncreaseMinor <= bb.RetirementIncomeIncreaseMinor
	strict := aa != bb
	return le && strict && a.SuccessWilsonLow >= b.SuccessWilsonLow
}

func balancedLevels(config Config) []balancedLevel {
	ratios := map[float64]struct{}{0: {}, 1: {}}
	for _, count := range []int{
		len(increaseLevels(config.SavingsIncrease)),
		len(reductionLevels(config.SpendingReduction)), len(increaseLevels(config.RetirementIncomeIncrease)),
	} {
		for i := 1; i <= count; i++ {
			ratios[float64(i)/float64(count)] = struct{}{}
		}
	}
	ordered := make([]float64, 0, len(ratios))
	for ratio := range ratios {
		ordered = append(ordered, ratio)
	}
	sort.Float64s(ordered)
	out := make([]balancedLevel, 0, len(ordered))
	for _, ratio := range ordered {
		out = append(out, balancedLevel{ratio: ratio, adjustments: Adjustments{
			SavingsIncreaseMinor:          levelAtRatio(increaseLevels(config.SavingsIncrease), ratio),
			SpendingReductionMinor:        levelAtRatio(reductionLevels(config.SpendingReduction), ratio),
			RetirementIncomeIncreaseMinor: levelAtRatio(increaseLevels(config.RetirementIncomeIncrease), ratio),
		}})
	}
	return out
}

func levelAtRatio(levels []int64, ratio float64) int64 {
	if len(levels) == 0 || ratio <= 0 {
		return 0
	}
	idx := int(ratio*float64(len(levels))) - 1
	if ratio >= 1 {
		idx = len(levels) - 1
	}
	if idx < 0 {
		return 0
	}
	return levels[min(idx, len(levels)-1)]
}

func decodeConfigHashInput(raw json.RawMessage) (domain.ConfigHashInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out domain.ConfigHashInput
	err := decoder.Decode(&out)
	return out, err
}

func increaseLevels(lever *MoneyIncreaseLever) []int64 {
	if lever == nil {
		return nil
	}
	return moneyLevels(lever.MaxIncreaseMinor, lever.StepMinor)
}

func reductionLevels(lever *MoneyReductionLever) []int64 {
	if lever == nil {
		return nil
	}
	return moneyLevels(lever.MaxReductionMinor, lever.StepMinor)
}

func hasMoneyLever(config Config) bool {
	return config.SavingsIncrease != nil || config.SpendingReduction != nil ||
		config.RetirementIncomeIncrease != nil
}

func financialLE(a, b Adjustments) bool {
	return a.SavingsIncreaseMinor <= b.SavingsIncreaseMinor &&
		a.SpendingReductionMinor <= b.SpendingReductionMinor &&
		a.RetirementIncomeIncreaseMinor <= b.RetirementIncomeIncreaseMinor
}

func proposalID(runID string, a Adjustments) string {
	sum := sha256.Sum256([]byte(runID + "\n" + tupleKey(a)))
	return "fpip_" + hex.EncodeToString(sum[:])[:24]
}

func tupleKey(a Adjustments) string {
	return fmt.Sprintf("%d|%d|%d|%d", a.DelayYears, a.SavingsIncreaseMinor,
		a.SpendingReductionMinor, a.RetirementIncomeIncreaseMinor)
}

func adjustmentLess(a, b Adjustments) bool {
	if a.DelayYears != b.DelayYears {
		return a.DelayYears < b.DelayYears
	}
	if a.SavingsIncreaseMinor != b.SavingsIncreaseMinor {
		return a.SavingsIncreaseMinor < b.SavingsIncreaseMinor
	}
	if a.SpendingReductionMinor != b.SpendingReductionMinor {
		return a.SpendingReductionMinor < b.SpendingReductionMinor
	}
	return a.RetirementIncomeIncreaseMinor < b.RetirementIncomeIncreaseMinor
}

func proposalLess(a, b Proposal) bool {
	order := map[string]int{
		RecipePureDelay: 0, RecipePureSavings: 1, RecipePureSpending: 2,
		RecipePureRetirementIncome: 3, RecipeBalanced: 4,
	}
	if order[a.Recipe] != order[b.Recipe] {
		return order[a.Recipe] < order[b.Recipe]
	}
	return adjustmentLess(Adjustments{
		a.DelayYears, a.SavingsIncreaseMinor, a.SpendingReductionMinor,
		a.RetirementIncomeIncreaseMinor,
	}, Adjustments{
		b.DelayYears, b.SavingsIncreaseMinor,
		b.SpendingReductionMinor, b.RetirementIncomeIncreaseMinor,
	})
}

func countSuccess(outcomes []bool) int {
	n := 0
	for _, value := range outcomes {
		if value {
			n++
		}
	}
	return n
}

func equalOutcomes(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameOutcomeEvaluation(a, b simulation.OutcomeEvaluation) bool {
	return a.Runs == b.Runs && a.SuccessCount == b.SuccessCount &&
		a.SuccessProbability == b.SuccessProbability &&
		a.SuccessWilsonLow == b.SuccessWilsonLow && a.SuccessWilsonHigh == b.SuccessWilsonHigh &&
		a.TerminalP50Minor == b.TerminalP50Minor && a.MaxDrawdownP95 == b.MaxDrawdownP95 &&
		equalOutcomes(a.Outcomes, b.Outcomes)
}
