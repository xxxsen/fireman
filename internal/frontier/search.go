package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"sync"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

var (
	ErrMonotonicityViolated = errors.New("frontier monotonicity violated")
	ErrResultInconsistent   = errors.New("frontier result inconsistent")
)

type Evaluator func(context.Context, *simulation.InputSnapshot, int) (simulation.OutcomeEvaluation, error)

type SearchOptions struct {
	Parallelism int
	Progress    func(done, total int, phase string)
	Evaluator   Evaluator
}

type cachedEvaluation struct {
	evaluation Evaluation
	outcomes   []bool
	candidate  Candidate
	isSearch   bool
}

type evaluationCall struct {
	done  chan struct{}
	value cachedEvaluation
	err   error
}

type searcher struct {
	ctx         context.Context
	base        simulation.InputSnapshot
	baseConfig  domain.ConfigHashInput
	config      Config
	evaluator   Evaluator
	progress    func(done, total int, phase string)
	parallelism int

	mu        sync.Mutex
	cache     map[string]cachedEvaluation
	inflight  map[string]*evaluationCall
	evaluated int
	baseline  cachedEvaluation
}

// Search evaluates the baseline and discrete frontiers. It never returns a
// partial Result: cancellation, invalid candidates, or monotonicity violations
// all return an error and an empty result.
func Search(ctx context.Context, frozen FrozenInput, opt SearchOptions) (Result, error) {
	s, err := prepareSearcher(ctx, frozen, opt)
	if err != nil {
		return Result{}, err
	}
	if s.progress != nil {
		s.progress(0, s.config.EvaluationBudget, "validating")
		s.progress(0, s.config.EvaluationBudget, "evaluating_baseline")
	}
	baseline, err := s.evaluateBaseline()
	if err != nil {
		return Result{}, err
	}
	s.baseline = baseline
	if s.progress != nil {
		s.progress(s.evaluated, s.config.EvaluationBudget, "searching")
	}
	points, err := s.searchPoints()
	if err != nil {
		return Result{}, err
	}
	return s.buildResult(baseline, points)
}

func prepareSearcher(ctx context.Context, frozen FrozenInput, opt SearchOptions) (*searcher, error) {
	if err := validateFrozen(frozen); err != nil {
		return nil, err
	}
	baseConfig, err := DecodeConfigHashInput(frozen.ConfigHashInputJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: decode config hash input: %w", ErrResultInconsistent, err)
	}
	configHash, err := domain.ComputeConfigHash(cloneConfigHashInput(baseConfig))
	if err != nil {
		return nil, fmt.Errorf("%w: compute frozen config hash: %w", ErrResultInconsistent, err)
	}
	if configHash != frozen.SourceSnapshot.ConfigHash {
		return nil, fmt.Errorf("%w: frozen config hash mismatch (%s != %s)",
			ErrResultInconsistent, configHash, frozen.SourceSnapshot.ConfigHash)
	}
	evaluator := opt.Evaluator
	if evaluator == nil {
		evaluator = func(ctx context.Context, snapshot *simulation.InputSnapshot, runs int) (
			simulation.OutcomeEvaluation, error,
		) {
			return simulation.EvaluateOutcomes(snapshot, simulation.RunOptions{
				Runs: runs, CancelCheck: func() bool { return ctx.Err() != nil },
			})
		}
	}
	parallelism := min(16, max(1, opt.Parallelism))
	return &searcher{
		ctx: ctx, base: frozen.SourceSnapshot, baseConfig: baseConfig, config: frozen.Config,
		evaluator: evaluator, progress: opt.Progress, parallelism: parallelism,
		cache: map[string]cachedEvaluation{}, inflight: map[string]*evaluationCall{},
	}, nil
}

func (s *searcher) searchPoints() ([]Point, error) {
	ages := s.ages()
	points := make([]Point, len(ages))
	errs := make([]error, len(ages))
	semaphore := make(chan struct{}, s.parallelism)
	var group sync.WaitGroup
	for i, age := range ages {
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-s.ctx.Done():
				errs[i] = fmt.Errorf("wait to search frontier point: %w", s.ctx.Err())
				return
			}
			points[i], errs[i] = s.searchPoint(age)
		}()
	}
	group.Wait()
	for _, searchErr := range errs {
		if searchErr != nil {
			return nil, searchErr
		}
	}
	if err := s.ctx.Err(); err != nil {
		return nil, fmt.Errorf("search frontier points: %w", err)
	}
	return points, nil
}

func (s *searcher) buildResult(baseline cachedEvaluation, points []Point) (Result, error) {
	if s.progress != nil {
		s.progress(s.evaluated, s.config.EvaluationBudget, "validating_result")
	}
	if err := s.ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("validate frontier result: %w", err)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].RetirementAge < points[j].RetirementAge })
	evaluations := s.sortedEvaluations()
	result := Result{
		AlgorithmVersion: AlgorithmVersion, FrontierType: s.config.FrontierType,
		TargetProbability: s.config.TargetSuccessProbability, EvaluationRuns: s.config.EvaluationRuns,
		Baseline: baseline.evaluation, Points: points, Evaluations: evaluations,
		DistinctEvaluations: s.evaluated,
		ActualPathMonths:    int64(s.evaluated) * int64(s.config.EvaluationRuns) * int64(s.base.HorizonMonths()),
		EvaluationBudget:    s.config.EvaluationBudget, PathMonthBudget: s.config.PathMonthBudget,
		DiscreteConnectionNote: "连线仅为视觉连接，不代表中间年龄或金额已计算。",
	}
	if err := validateResult(result, s.config, s.base.HorizonMonths()); err != nil {
		return Result{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("complete frontier search: %w", err)
	}
	if s.progress != nil {
		s.progress(s.evaluated, s.config.EvaluationBudget, "complete")
	}
	return result, nil
}

func validateFrozen(frozen FrozenInput) error {
	config := frozen.Config
	if config.FrontierType == "" || config.EvaluationRuns < 1000 || config.MoneyLevels < 1 ||
		config.EvaluationBudget < 1 || config.EvaluationBudget > MaxEvaluationBudget ||
		config.PathMonthBudget > MaxPathMonthBudget || frozen.SourceSnapshot.HorizonMonths() <= 0 {
		return fmt.Errorf("%w: normalized config is invalid", ErrResultInconsistent)
	}
	if config.EvaluationRuns > frozen.SourceSnapshot.Parameters.SimulationRuns ||
		frozen.SourceSnapshot.EngineVersion != simulation.EngineVersion {
		return fmt.Errorf("%w: source snapshot is not eligible", ErrResultInconsistent)
	}
	normalized, err := Normalize(config.FrontierType, config.TargetSuccessProbability, config.EvaluationRuns,
		config.RetirementAgeRange, config.Search, frozen.SourceSnapshot.Parameters.CurrentAge,
		frozen.SourceSnapshot.Parameters.EndAge, frozen.SourceSnapshot.Parameters.SimulationRuns,
		frozen.SourceSnapshot.HorizonMonths())
	if err != nil {
		return fmt.Errorf("%w: normalized config cannot be reproduced: %w", ErrResultInconsistent, err)
	}
	if !reflect.DeepEqual(normalized, config) {
		return fmt.Errorf("%w: normalized config differs from frozen config", ErrResultInconsistent)
	}
	return nil
}

func (s *searcher) evaluateBaseline() (cachedEvaluation, error) {
	candidate := Candidate{RetirementAge: s.base.Parameters.RetirementAge}
	switch s.config.FrontierType {
	case TypeRetirementAgeMaxSpending:
		candidate.ValueMinor = s.base.Parameters.AnnualSpendingMinor
	case TypeRetirementAgeMinSavings:
		candidate.ValueMinor = s.base.Parameters.AnnualSavingsMinor
	default:
		candidate.ValueMinor = s.base.Parameters.TotalAssetsMinor
	}
	return s.evaluateSnapshot(s.base, candidate, false)
}

func (s *searcher) evaluate(candidate Candidate) (cachedEvaluation, error) {
	if err := s.ctx.Err(); err != nil {
		return cachedEvaluation{}, fmt.Errorf("evaluate frontier candidate: %w", err)
	}
	snapshot, err := BuildCandidate(s.base, s.config.FrontierType, candidate)
	if err != nil {
		return cachedEvaluation{}, err
	}
	configInput, err := ApplyConfigCandidate(s.baseConfig, s.config.FrontierType, candidate, assetAmountMap(snapshot))
	if err != nil {
		return cachedEvaluation{}, err
	}
	configHash, err := domain.ComputeConfigHash(configInput)
	if err != nil {
		return cachedEvaluation{}, fmt.Errorf("hash frontier candidate config: %w", err)
	}
	snapshot.ConfigHash = configHash
	return s.evaluateSnapshot(snapshot, candidate, true)
}

func (s *searcher) evaluateSnapshot(snapshot simulation.InputSnapshot, candidate Candidate,
	isSearch bool,
) (cachedEvaluation, error) {
	snapshotHash, err := simulation.HashInput(&snapshot)
	if err != nil {
		return cachedEvaluation{}, fmt.Errorf("hash frontier candidate snapshot: %w", err)
	}
	s.mu.Lock()
	if cached, ok := s.cache[snapshotHash]; ok {
		if isSearch && !cached.isSearch {
			cached.isSearch = true
			cached.candidate = candidate
			cached.evaluation.RetirementAge = candidate.RetirementAge
			cached.evaluation.ValueMinor = candidate.ValueMinor
			if err := s.checkMonotonicityLocked(cached); err != nil {
				s.mu.Unlock()
				return cachedEvaluation{}, err
			}
			s.cache[snapshotHash] = cached
		}
		s.mu.Unlock()
		return cached, nil
	}
	if call, ok := s.inflight[snapshotHash]; ok {
		s.mu.Unlock()
		select {
		case <-s.ctx.Done():
			return cachedEvaluation{}, fmt.Errorf("wait for frontier evaluation: %w", s.ctx.Err())
		case <-call.done:
			return call.value, call.err
		}
	}
	call := &evaluationCall{done: make(chan struct{})}
	s.inflight[snapshotHash] = call
	s.mu.Unlock()

	value, evalErr := s.runEvaluation(snapshot, snapshotHash, candidate, isSearch)
	s.mu.Lock()
	if evalErr == nil && isSearch {
		evalErr = s.checkMonotonicityLocked(value)
	}
	if evalErr == nil {
		s.cache[snapshotHash] = value
		s.evaluated++
		if s.evaluated > s.config.EvaluationBudget {
			evalErr = fmt.Errorf("%w: actual evaluations exceed conservative budget", ErrResultInconsistent)
		} else if s.progress != nil {
			s.progress(s.evaluated, s.config.EvaluationBudget, "searching")
		}
	}
	call.value, call.err = value, evalErr
	delete(s.inflight, snapshotHash)
	close(call.done)
	s.mu.Unlock()
	return value, evalErr
}

func (s *searcher) runEvaluation(snapshot simulation.InputSnapshot, snapshotHash string,
	candidate Candidate, isSearch bool,
) (cachedEvaluation, error) {
	if err := s.ctx.Err(); err != nil {
		return cachedEvaluation{}, fmt.Errorf("run frontier evaluation: %w", err)
	}
	outcome, err := s.evaluator(s.ctx, &snapshot, s.config.EvaluationRuns)
	if err != nil {
		return cachedEvaluation{}, err
	}
	if err := s.ctx.Err(); err != nil {
		return cachedEvaluation{}, fmt.Errorf("finish frontier evaluation: %w", err)
	}
	if outcome.Runs != s.config.EvaluationRuns || len(outcome.Outcomes) != s.config.EvaluationRuns ||
		countSuccess(outcome.Outcomes) != outcome.SuccessCount {
		return cachedEvaluation{}, fmt.Errorf("%w: evaluator aggregates do not match outcomes", ErrResultInconsistent)
	}
	probability := float64(outcome.SuccessCount) / float64(outcome.Runs)
	wilsonLow, wilsonHigh := simulation.WilsonInterval(outcome.SuccessCount, outcome.Runs, 1.96)
	evaluation := Evaluation{
		RetirementAge: candidate.RetirementAge, ValueMinor: candidate.ValueMinor,
		Runs: outcome.Runs, SuccessCount: outcome.SuccessCount,
		SuccessProbability: round10(probability),
		SuccessWilsonLow:   round10(wilsonLow), SuccessWilsonHigh: round10(wilsonHigh),
		TerminalP50Minor: outcome.TerminalP50Minor, MaxDrawdownP95: round10(outcome.MaxDrawdownP95),
		MeetsTarget: wilsonLow >= s.config.TargetSuccessProbability,
		OutcomeHash: OutcomeHash(outcome.Outcomes), SnapshotHash: snapshotHash,
		CandidateConfigHash: snapshot.ConfigHash,
	}
	if isSearch {
		baseline := s.baseline.outcomes
		if len(baseline) == len(outcome.Outcomes) {
			for i := range baseline {
				switch {
				case !baseline[i] && outcome.Outcomes[i]:
					evaluation.ImprovedPathCount++
				case baseline[i] && !outcome.Outcomes[i]:
					evaluation.RegressedPathCount++
				}
			}
		}
	}
	return cachedEvaluation{
		evaluation: evaluation, outcomes: append([]bool(nil), outcome.Outcomes...),
		candidate: candidate, isSearch: isSearch,
	}, nil
}

func (s *searcher) checkMonotonicityLocked(candidate cachedEvaluation) error {
	for _, other := range s.cache {
		if err := s.checkMonotonicPair(other, candidate); err != nil {
			return err
		}
	}
	return nil
}

func (s *searcher) checkMonotonicPair(other, candidate cachedEvaluation) error {
	if !other.isSearch || other.candidate.RetirementAge != candidate.candidate.RetirementAge ||
		other.candidate.ValueMinor == candidate.candidate.ValueMinor {
		return nil
	}
	lower, higher := other, candidate
	if lower.candidate.ValueMinor > higher.candidate.ValueMinor {
		lower, higher = higher, lower
	}
	better, worse := higher, lower
	if s.config.FrontierType == TypeRetirementAgeMaxSpending {
		better, worse = lower, higher
	}
	if worse.evaluation.SuccessCount > better.evaluation.SuccessCount {
		return ErrMonotonicityViolated
	}
	for i := range better.outcomes {
		if worse.outcomes[i] && !better.outcomes[i] {
			return ErrMonotonicityViolated
		}
	}
	return nil
}

func (s *searcher) searchPoint(age int) (Point, error) {
	values := Values(s.config.Search)
	if s.config.FrontierType == TypeRetirementAgeMaxSpending {
		return s.searchMaximum(age, values)
	}
	return s.searchMinimum(age, values)
}

func (s *searcher) searchMaximum(age int, values []int64) (Point, error) {
	minimum, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[0]})
	if err != nil {
		return Point{}, err
	}
	if !minimum.evaluation.MeetsTarget {
		return s.point(age, StatusNoFeasibleValue, minimum, nil), nil
	}
	maximum, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[len(values)-1]})
	if err != nil {
		return Point{}, err
	}
	if maximum.evaluation.MeetsTarget {
		return s.point(age, StatusEntireDomainFeasible, maximum, nil), nil
	}
	lo, hi := 0, len(values)-1
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		value, evalErr := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[mid]})
		if evalErr != nil {
			return Point{}, evalErr
		}
		if value.evaluation.MeetsTarget {
			lo = mid
		} else {
			hi = mid
		}
	}
	boundary, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[lo]})
	if err != nil {
		return Point{}, err
	}
	worse, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[lo+1]})
	if err != nil {
		return Point{}, err
	}
	return s.point(age, StatusBoundaryFound, boundary, &worse), nil
}

func (s *searcher) searchMinimum(age int, values []int64) (Point, error) {
	maximum, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[len(values)-1]})
	if err != nil {
		return Point{}, err
	}
	if !maximum.evaluation.MeetsTarget {
		return s.point(age, StatusNoFeasibleValue, maximum, nil), nil
	}
	minimum, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[0]})
	if err != nil {
		return Point{}, err
	}
	if minimum.evaluation.MeetsTarget {
		return s.point(age, StatusEntireDomainFeasible, minimum, nil), nil
	}
	lo, hi := 0, len(values)-1
	for lo+1 < hi {
		mid := lo + (hi-lo)/2
		value, evalErr := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[mid]})
		if evalErr != nil {
			return Point{}, evalErr
		}
		if value.evaluation.MeetsTarget {
			hi = mid
		} else {
			lo = mid
		}
	}
	boundary, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[hi]})
	if err != nil {
		return Point{}, err
	}
	worse, err := s.evaluate(Candidate{RetirementAge: age, ValueMinor: values[hi-1]})
	if err != nil {
		return Point{}, err
	}
	return s.point(age, StatusBoundaryFound, boundary, &worse), nil
}

func (s *searcher) point(age int, status string, value cachedEvaluation, worse *cachedEvaluation) Point {
	displayAge := age
	if !isAgeFrontier(s.config.FrontierType) {
		displayAge = 0
	}
	point := Point{
		ID:            PointID(s.config.FrontierType, displayAge, value.candidate.ValueMinor),
		RetirementAge: displayAge, ValueMinor: value.candidate.ValueMinor, Status: status,
		Applicable: isAgeFrontier(s.config.FrontierType) && status != StatusNoFeasibleValue &&
			value.evaluation.MeetsTarget,
		Evaluation: value.evaluation,
	}
	point.Evaluation.RetirementAge = displayAge
	if worse != nil {
		copyEval := worse.evaluation
		copyEval.RetirementAge = displayAge
		point.WorseNeighbor = &copyEval
	}
	if !isAgeFrontier(s.config.FrontierType) {
		current := s.base.Parameters.TotalAssetsMinor
		point.SourceCurrentAssetsMinor = &current
		if status == StatusBoundaryFound {
			gap := value.candidate.ValueMinor - current
			achieved := current >= value.candidate.ValueMinor
			point.GapMinor, point.Achieved = &gap, &achieved
			if s.config.FrontierType == TypeCoastRequiredAssets {
				point.CoastAchieved = &achieved
			}
		}
	}
	return point
}

func (s *searcher) ages() []int {
	if !isAgeFrontier(s.config.FrontierType) {
		return []int{0}
	}
	out := make([]int, s.config.AgePoints)
	for i := range out {
		out[i] = s.config.RetirementAgeRange.Min + i
	}
	return out
}

func (s *searcher) sortedEvaluations() []Evaluation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Evaluation, 0, len(s.cache))
	for _, value := range s.cache {
		out = append(out, value.evaluation)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RetirementAge != out[j].RetirementAge {
			return out[i].RetirementAge < out[j].RetirementAge
		}
		if out[i].ValueMinor != out[j].ValueMinor {
			return out[i].ValueMinor < out[j].ValueMinor
		}
		return out[i].SnapshotHash < out[j].SnapshotHash
	})
	return out
}

func validateResult(result Result, config Config, horizonMonths int) error {
	if !validResultIdentity(result, config) || !validResultBudget(result, config, horizonMonths) ||
		!validEvaluation(result.Baseline, config.TargetSuccessProbability, config.EvaluationRuns) ||
		!validSortedEvaluations(result.Evaluations, config) {
		return ErrResultInconsistent
	}
	for i, point := range result.Points {
		if !validFrontierPoint(point, config, expectedPointAge(config, i)) {
			return ErrResultInconsistent
		}
	}
	return nil
}

func validResultIdentity(result Result, config Config) bool {
	return result.AlgorithmVersion == AlgorithmVersion &&
		result.FrontierType == config.FrontierType &&
		result.TargetProbability == config.TargetSuccessProbability &&
		result.EvaluationRuns == config.EvaluationRuns &&
		result.DiscreteConnectionNote == "连线仅为视觉连接，不代表中间年龄或金额已计算。"
}

func validResultBudget(result Result, config Config, horizonMonths int) bool {
	expectedPathMonths := int64(result.DistinctEvaluations) * int64(config.EvaluationRuns) * int64(horizonMonths)
	return result.EvaluationBudget == config.EvaluationBudget &&
		result.PathMonthBudget == config.PathMonthBudget &&
		result.DistinctEvaluations >= 1 &&
		result.DistinctEvaluations <= config.EvaluationBudget &&
		result.DistinctEvaluations == len(result.Evaluations) &&
		result.ActualPathMonths == expectedPathMonths &&
		result.ActualPathMonths <= config.PathMonthBudget &&
		len(result.Points) == config.AgePoints
}

func validSortedEvaluations(evaluations []Evaluation, config Config) bool {
	seenSnapshots := make(map[string]struct{}, len(evaluations))
	for i, evaluation := range evaluations {
		if !validEvaluation(evaluation, config.TargetSuccessProbability, config.EvaluationRuns) {
			return false
		}
		if i > 0 && evaluationLess(evaluation, evaluations[i-1]) {
			return false
		}
		if _, duplicate := seenSnapshots[evaluation.SnapshotHash]; duplicate {
			return false
		}
		seenSnapshots[evaluation.SnapshotHash] = struct{}{}
	}
	return true
}

func evaluationLess(left, right Evaluation) bool {
	if left.RetirementAge != right.RetirementAge {
		return left.RetirementAge < right.RetirementAge
	}
	if left.ValueMinor != right.ValueMinor {
		return left.ValueMinor < right.ValueMinor
	}
	return left.SnapshotHash < right.SnapshotHash
}

func expectedPointAge(config Config, index int) int {
	if !isAgeFrontier(config.FrontierType) {
		return 0
	}
	return config.RetirementAgeRange.Min + index
}

func validFrontierPoint(point Point, config Config, expectedAge int) bool {
	if !validFrontierPointCore(point, config, expectedAge) ||
		!validFrontierPointStatus(point, config, expectedAge) {
		return false
	}
	return validFrontierPointMetadata(point, config)
}

func validFrontierPointCore(point Point, config Config, expectedAge int) bool {
	expectedApplicable := isAgeFrontier(config.FrontierType) &&
		point.Status != StatusNoFeasibleValue && point.Evaluation.MeetsTarget
	return point.RetirementAge == expectedAge &&
		point.ID == PointID(config.FrontierType, expectedAge, point.ValueMinor) &&
		point.ValueMinor >= config.Search.MinMinor && point.ValueMinor <= config.Search.MaxMinor &&
		(point.ValueMinor-config.Search.MinMinor)%config.Search.StepMinor == 0 &&
		point.Evaluation.RetirementAge == expectedAge && point.Evaluation.ValueMinor == point.ValueMinor &&
		validEvaluation(point.Evaluation, config.TargetSuccessProbability, config.EvaluationRuns) &&
		point.Applicable == expectedApplicable
}

func validFrontierPointStatus(point Point, config Config, expectedAge int) bool {
	switch point.Status {
	case StatusBoundaryFound:
		return validBoundaryPoint(point, config, expectedAge)
	case StatusEntireDomainFeasible:
		expected := config.Search.MinMinor
		if config.FrontierType == TypeRetirementAgeMaxSpending {
			expected = config.Search.MaxMinor
		}
		return point.Evaluation.MeetsTarget && point.WorseNeighbor == nil && point.ValueMinor == expected
	case StatusNoFeasibleValue:
		expected := config.Search.MaxMinor
		if config.FrontierType == TypeRetirementAgeMaxSpending {
			expected = config.Search.MinMinor
		}
		return !point.Evaluation.MeetsTarget && point.WorseNeighbor == nil &&
			!point.Applicable && point.ValueMinor == expected
	default:
		return false
	}
}

func validBoundaryPoint(point Point, config Config, expectedAge int) bool {
	if point.WorseNeighbor == nil || !point.Evaluation.MeetsTarget || point.WorseNeighbor.MeetsTarget ||
		abs64(point.Evaluation.ValueMinor-point.WorseNeighbor.ValueMinor) != config.Search.StepMinor ||
		point.WorseNeighbor.RetirementAge != expectedAge ||
		!validEvaluation(*point.WorseNeighbor, config.TargetSuccessProbability, config.EvaluationRuns) {
		return false
	}
	if config.FrontierType == TypeRetirementAgeMaxSpending {
		return point.WorseNeighbor.ValueMinor == point.ValueMinor+config.Search.StepMinor
	}
	return point.WorseNeighbor.ValueMinor == point.ValueMinor-config.Search.StepMinor
}

func validFrontierPointMetadata(point Point, config Config) bool {
	if isAgeFrontier(config.FrontierType) {
		return validAgeFrontierPointMetadata(point)
	}
	return validAssetFrontierPointMetadata(point, config.FrontierType)
}

func validAgeFrontierPointMetadata(point Point) bool {
	return point.SourceCurrentAssetsMinor == nil && point.GapMinor == nil && point.Achieved == nil &&
		point.CoastAchieved == nil
}

func validAssetFrontierPointMetadata(point Point, frontierType string) bool {
	if point.SourceCurrentAssetsMinor == nil {
		return false
	}
	if point.Status != StatusBoundaryFound {
		return point.GapMinor == nil && point.Achieved == nil && point.CoastAchieved == nil
	}
	if point.GapMinor == nil || point.Achieved == nil ||
		*point.GapMinor != point.ValueMinor-*point.SourceCurrentAssetsMinor ||
		*point.Achieved != (*point.SourceCurrentAssetsMinor >= point.ValueMinor) {
		return false
	}
	if frontierType == TypeCoastRequiredAssets {
		return point.CoastAchieved != nil && *point.CoastAchieved == *point.Achieved
	}
	return frontierType == TypeRequiredCurrentAssets && point.CoastAchieved == nil
}

func validEvaluation(evaluation Evaluation, target float64, runs int) bool {
	if evaluation.Runs != runs || evaluation.SuccessCount < 0 || evaluation.SuccessCount > runs ||
		evaluation.OutcomeHash == "" || evaluation.SnapshotHash == "" || evaluation.CandidateConfigHash == "" ||
		evaluation.ImprovedPathCount < 0 || evaluation.RegressedPathCount < 0 ||
		evaluation.ImprovedPathCount > runs || evaluation.RegressedPathCount > runs ||
		evaluation.ImprovedPathCount+evaluation.RegressedPathCount > runs {
		return false
	}
	probability := float64(evaluation.SuccessCount) / float64(runs)
	low, high := simulation.WilsonInterval(evaluation.SuccessCount, runs, 1.96)
	return evaluation.SuccessProbability == round10(probability) &&
		evaluation.SuccessWilsonLow == round10(low) && evaluation.SuccessWilsonHigh == round10(high) &&
		evaluation.MeetsTarget == (low >= target)
}

func PointID(frontierType string, age int, value int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%d\n%d", frontierType, age, value)))
	return "fpt_" + hex.EncodeToString(sum[:])[:20]
}

func MarshalResult(result Result) ([]byte, error) {
	return json.Marshal(result)
}

func round10(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	return math.Round(value*1e10) / 1e10
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
