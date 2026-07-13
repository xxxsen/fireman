package improvement

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

func TestConfigValidationAndNonDivisibleMoneyLevels(t *testing.T) {
	config := Config{
		TargetSuccessProbability: 0.9,
		SavingsIncrease:          &MoneyIncreaseLever{MaxIncreaseMinor: 250, StepMinor: 100},
	}
	if err := config.Validate(90, 50); err != nil {
		t.Fatal(err)
	}
	if got, want := moneyLevels(250, 100), []int64{100, 200, 250}; !reflect.DeepEqual(got, want) {
		t.Fatalf("levels=%v want %v", got, want)
	}
	config.SavingsIncrease.StepMinor = 1
	config.SavingsIncrease.MaxIncreaseMinor = 101
	if !errors.Is(config.Validate(90, 50), ErrConfigInvalid) {
		t.Fatal("more than 100 money levels must be rejected")
	}
}

func TestApplyAdjustmentsDoesNotMutateSource(t *testing.T) {
	base := testSnapshot()
	base.Assets[0].Months = map[string]float64{"2024-01": 0.01}
	out, err := ApplyAdjustments(base, Adjustments{
		DelayYears: 2, SavingsIncreaseMinor: 100, SpendingReductionMinor: 200,
		RetirementIncomeIncreaseMinor: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	if base.Parameters.RetirementAge != 50 || base.Parameters.AnnualSavingsMinor != 1_000 ||
		base.Parameters.AnnualSpendingMinor != 2_000 || base.Parameters.AnnualRetirementIncomeMinor != 100 {
		t.Fatal("source snapshot was mutated")
	}
	if out.Parameters.RetirementAge != 52 || out.Parameters.AnnualSavingsMinor != 1_100 ||
		out.Parameters.AnnualSpendingMinor != 1_800 || out.Parameters.AnnualRetirementIncomeMinor != 400 {
		t.Fatalf("unexpected adjusted parameters: %#v", out.Parameters)
	}
	out.Assets[0].Months["2024-01"] = 0.02
	if base.Assets[0].Months["2024-01"] != 0.01 {
		t.Fatal("nested snapshot maps were aliased")
	}
}

func TestOutcomeBitsRejectTampering(t *testing.T) {
	want := []bool{true, false, true, true, false, false, false, true, true}
	encoded, hash := EncodeOutcomeBits(want)
	got, err := DecodeOutcomeBits(encoded, hash, len(want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded=%v err=%v", got, err)
	}
	if _, err := DecodeOutcomeBits(encoded, hash+"x", len(want)); !errors.Is(err, ErrOutcomeBitsInvalid) {
		t.Fatal("tampered hash must be rejected")
	}
	if _, err := DecodeOutcomeBits("gA==", "irrelevant", 1); !errors.Is(err, ErrOutcomeBitsInvalid) {
		t.Fatal("non-zero bitset padding must be rejected")
	}
}

func TestBalancedLevelsIncludeEveryLeverBoundary(t *testing.T) {
	levels := balancedLevels(Config{
		TargetSuccessProbability: 0.9,
		SavingsIncrease:          &MoneyIncreaseLever{MaxIncreaseMinor: 300, StepMinor: 100},
		SpendingReduction:        &MoneyReductionLever{MaxReductionMinor: 200, StepMinor: 100},
	})
	wantRatios := []float64{0, 1.0 / 3, 0.5, 2.0 / 3, 1}
	if len(levels) != len(wantRatios) {
		t.Fatalf("balanced level count=%d want=%d: %#v", len(levels), len(wantRatios), levels)
	}
	for i := range levels {
		if levels[i].ratio != wantRatios[i] {
			t.Fatalf("ratio[%d]=%v want=%v", i, levels[i].ratio, wantRatios[i])
		}
	}
	last := levels[len(levels)-1].adjustments
	if last.SavingsIncreaseMinor != 300 || last.SpendingReductionMinor != 200 {
		t.Fatalf("balanced maximum=%#v", last)
	}
}

func TestParetoKeepsIncomparableAndRemovesDominatedCandidates(t *testing.T) {
	a := recipeCandidate{recipe: RecipePureSavings, eval: Evaluation{
		Adjustments: Adjustments{SavingsIncreaseMinor: 100}, SuccessWilsonLow: 0.8,
	}}
	b := recipeCandidate{recipe: RecipePureSavings, eval: Evaluation{
		Adjustments: Adjustments{SavingsIncreaseMinor: 200}, SuccessWilsonLow: 0.7,
	}}
	c := recipeCandidate{recipe: RecipePureSpending, eval: Evaluation{
		Adjustments: Adjustments{SpendingReductionMinor: 100}, SuccessWilsonLow: 0.75,
	}}
	got := pareto([]recipeCandidate{a, b, c})
	if len(got) != 2 || got[0].eval.Adjustments != a.eval.Adjustments ||
		got[1].eval.Adjustments != c.eval.Adjustments {
		t.Fatalf("pareto=%#v", got)
	}
}

func TestSearchRejectsPathwiseMonotonicityViolation(t *testing.T) {
	frozen := testFrozen(t, Config{
		TargetSuccessProbability: 0.5,
		SavingsIncrease:          &MoneyIncreaseLever{MaxIncreaseMinor: 200, StepMinor: 100},
	})
	evaluator := func(_ context.Context, in *simulation.InputSnapshot) (simulation.OutcomeEvaluation, error) {
		delta := in.Parameters.AnnualSavingsMinor - frozen.SourceSnapshot.Parameters.AnnualSavingsMinor
		success := 4
		switch delta {
		case 100:
			success = 10
		case 200:
			success = 9
		}
		return outcomeWithSuccess(success), nil
	}
	_, err := Search(context.Background(), "fpir_monotonicity", frozen,
		SearchOptions{Parallelism: 4, Evaluator: evaluator})
	if !errors.Is(err, ErrMonotonicityViolation) {
		t.Fatalf("monotonicity error=%v", err)
	}
}

func TestPointEstimateAboveTargetDoesNotProduceFeasibleProposal(t *testing.T) {
	frozen := testFrozen(t, Config{
		TargetSuccessProbability: 0.55,
		SavingsIncrease:          &MoneyIncreaseLever{MaxIncreaseMinor: 300, StepMinor: 100},
	})
	result, err := Search(context.Background(), "fpir_near", frozen,
		SearchOptions{Evaluator: fakeEvaluator(frozen.SourceSnapshot)})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetReached || len(result.Proposals) != 0 || result.BestAttainable == nil {
		t.Fatalf("near-target result=%#v", result)
	}
	last := result.Evaluations[len(result.Evaluations)-1]
	if last.SuccessProbability < result.TargetProbability || last.SuccessWilsonLow >= result.TargetProbability {
		t.Fatalf("expected point-estimate-only near target: %#v", last)
	}
}

func TestSearchFindsMinimumDiscreteLeverAndIsDeterministic(t *testing.T) {
	frozen := testFrozen(t, Config{
		TargetSuccessProbability: 0.55,
		SavingsIncrease:          &MoneyIncreaseLever{MaxIncreaseMinor: 600, StepMinor: 100},
		SpendingReduction:        &MoneyReductionLever{MaxReductionMinor: 500, StepMinor: 100},
	})
	evaluator := fakeEvaluator(frozen.SourceSnapshot)
	first, err := Search(context.Background(), "fpir_1", frozen, SearchOptions{Parallelism: 1, Evaluator: evaluator})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Search(context.Background(), "fpir_1", frozen, SearchOptions{Parallelism: 16, Evaluator: evaluator})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("result JSON changed with configured parallelism")
	}
	var savings *Proposal
	for i := range first.Proposals {
		if first.Proposals[i].Recipe == RecipePureSavings {
			savings = &first.Proposals[i]
		}
	}
	if savings == nil || savings.SavingsIncreaseMinor != 500 {
		t.Fatalf("minimum savings proposal=%#v", savings)
	}
	if first.EvaluatedCount > SearchUpperBound(frozen.Config) {
		t.Fatalf("evaluated %d exceeds upper bound %d", first.EvaluatedCount, SearchUpperBound(frozen.Config))
	}
}

func TestSearchReturnsBestAttainableWithoutFailing(t *testing.T) {
	frozen := testFrozen(t, Config{
		TargetSuccessProbability: 0.9,
		SavingsIncrease:          &MoneyIncreaseLever{MaxIncreaseMinor: 100, StepMinor: 100},
	})
	result, err := Search(context.Background(), "fpir_2", frozen,
		SearchOptions{Evaluator: fakeEvaluator(frozen.SourceSnapshot)})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetReached || result.BestAttainable == nil || result.BestAttainable.SavingsIncreaseMinor != 100 {
		t.Fatalf("unexpected unattainable result: %#v", result)
	}
}

func testSnapshot() simulation.InputSnapshot {
	return simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion, PlanID: "plan_1", BaseCurrency: "CNY",
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 40, RetirementAge: 50, EndAge: 90, TotalAssetsMinor: 10_000,
			AnnualSavingsMinor: 1_000, AnnualSpendingMinor: 2_000,
			AnnualRetirementIncomeMinor: 100, SimulationRuns: 10, Seed: "42",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "h1", AssetKey: "a1", InitialMinor: 10_000,
			TargetWeight: 1, SourceHash: "market",
		}},
		MarketSnapshotHash: "market",
	}
}

func testFrozen(t *testing.T, config Config) FrozenInput {
	t.Helper()
	snapshot := testSnapshot()
	configInput := domain.ConfigHashInput{
		PlanID: "plan_1", Name: "Plan", BaseCurrency: "CNY", ValuationDate: "2026-01-01",
		Parameters: map[string]any{
			"retirement_age": 50, "annual_savings_minor": int64(1_000),
			"annual_spending_minor": int64(2_000), "annual_retirement_income_minor": int64(100),
		},
		AssetClass: []map[string]any{{"asset_class": "equity", "weight": 1.0}},
		Holdings:   []map[string]any{{"asset_key": "a1"}},
	}
	configHash, err := domain.ComputeConfigHash(cloneConfigHashInput(configInput))
	if err != nil {
		t.Fatal(err)
	}
	snapshot.ConfigHash = configHash
	raw, err := json.Marshal(configInput)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := []bool{true, true, true, true, false, false, false, false, false, false}
	low, high := simulation.WilsonInterval(4, 10, 1.96)
	encoded, hash := EncodeOutcomeBits(outcomes)
	return FrozenInput{
		SourceSnapshot: snapshot, Config: config, ConfigHashInputJSON: raw,
		BaselineBits: encoded, BaselineHash: hash,
		Baseline: simulation.OutcomeEvaluation{
			Runs: 10, SuccessCount: 4,
			SuccessProbability: 0.4, SuccessWilsonLow: low, SuccessWilsonHigh: high,
			TerminalP50Minor: 4_000, MaxDrawdownP95: 0.46,
		},
	}
}

func fakeEvaluator(base simulation.InputSnapshot) Evaluator {
	return func(_ context.Context, in *simulation.InputSnapshot) (simulation.OutcomeEvaluation, error) {
		delta := (in.Parameters.AnnualSavingsMinor-base.Parameters.AnnualSavingsMinor)/100 +
			(base.Parameters.AnnualSpendingMinor-in.Parameters.AnnualSpendingMinor)/100 +
			(in.Parameters.AnnualRetirementIncomeMinor-base.Parameters.AnnualRetirementIncomeMinor)/100 +
			int64(in.Parameters.RetirementAge-base.Parameters.RetirementAge)
		success := min(10, 4+int(delta))
		outcomes := make([]bool, 10)
		for i := 0; i < success; i++ {
			outcomes[i] = true
		}
		low, high := simulation.WilsonInterval(success, 10, 1.96)
		return simulation.OutcomeEvaluation{
			Runs: 10, SuccessCount: success, SuccessProbability: float64(success) / 10,
			SuccessWilsonLow: low, SuccessWilsonHigh: high, TerminalP50Minor: int64(success) * 1_000,
			MaxDrawdownP95: 0.5 - float64(success)/100, Outcomes: outcomes,
		}, nil
	}
}

func outcomeWithSuccess(success int) simulation.OutcomeEvaluation {
	outcomes := make([]bool, 10)
	for i := 0; i < success; i++ {
		outcomes[i] = true
	}
	low, high := simulation.WilsonInterval(success, 10, 1.96)
	return simulation.OutcomeEvaluation{
		Runs: 10, SuccessCount: success,
		SuccessProbability: float64(success) / 10, SuccessWilsonLow: low, SuccessWilsonHigh: high,
		TerminalP50Minor: int64(success) * 1_000, MaxDrawdownP95: 0.5 - float64(success)/100,
		Outcomes: outcomes,
	}
}
