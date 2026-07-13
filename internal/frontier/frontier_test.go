package frontier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

func TestNormalizeGridAndBudgets(t *testing.T) {
	config, err := Normalize(TypeRetirementAgeMaxSpending, 0.9, 3000,
		&AgeRange{Min: 50, Max: 60},
		MoneySearch{MinMinor: 24_000_000, MaxMinor: 72_000_000, StepMinor: 600_000},
		40, 90, 5000, 600)
	if err != nil {
		t.Fatal(err)
	}
	if config.MoneyLevels != 81 || config.AgePoints != 11 || config.PerPointBudget != 9 ||
		config.EvaluationBudget != 100 {
		t.Fatalf("unexpected normalized config: %+v", config)
	}
	if values := Values(MoneySearch{MinMinor: 1, MaxMinor: 7, StepMinor: 2}); !reflect.DeepEqual(values, []int64{1, 3, 5, 7}) {
		t.Fatalf("values=%v", values)
	}
	if _, total := EvaluationBudget(15, 1<<14); total != 256 {
		t.Fatalf("budget=%d want 256", total)
	}
	if _, total := EvaluationBudget(16, 1<<13); total != 257 {
		t.Fatalf("budget=%d want 257", total)
	}
	if got, ok := PathMonthBudget(10, 1000, 30_000); !ok || got != 300_000_000 {
		t.Fatalf("path budget=(%d,%v)", got, ok)
	}
	if got, ok := PathMonthBudget(7, 7, 6_122_449); !ok || got != 300_000_001 || got <= MaxPathMonthBudget {
		t.Fatalf("path budget exact +1=(%d,%v)", got, ok)
	}
}

func TestNormalizeAcceptsReportedProductionInputs(t *testing.T) {
	config, err := Normalize(TypeRetirementAgeMaxSpending, 0.95, 3000,
		&AgeRange{Min: 35, Max: 45},
		MoneySearch{MinMinor: 400_000, MaxMinor: 8_000_000, StepMinor: 10_000},
		35, 65, 10_000, 360)
	if err != nil {
		t.Fatal(err)
	}
	if config.MoneyLevels != 761 || config.AgePoints != 11 || config.PerPointBudget != 12 ||
		config.EvaluationBudget != 133 || config.PathMonthBudget != 143_640_000 {
		t.Fatalf("reported input normalization changed: %+v", config)
	}
}

func TestNormalizeDefaultsToTenThousandEvaluationRuns(t *testing.T) {
	config, err := Normalize(TypeRequiredCurrentAssets, 0.95, 0, nil,
		MoneySearch{MinMinor: 1, MaxMinor: 1, StepMinor: 1},
		40, 90, 20_000, 600)
	if err != nil {
		t.Fatal(err)
	}
	if config.EvaluationRuns != DefaultEvaluationRuns {
		t.Fatalf("default evaluation runs=%d want %d", config.EvaluationRuns, DefaultEvaluationRuns)
	}

	config, err = Normalize(TypeRequiredCurrentAssets, 0.95, 0, nil,
		MoneySearch{MinMinor: 1, MaxMinor: 1, StepMinor: 1},
		40, 90, 5_000, 600)
	if err != nil {
		t.Fatal(err)
	}
	if config.EvaluationRuns != 5_000 {
		t.Fatalf("source-limited default evaluation runs=%d want 5000", config.EvaluationRuns)
	}
}

func TestCanonicalIdentityAndOutcomeHashGoldens(t *testing.T) {
	identity := InputIdentity{
		AlgorithmVersion: AlgorithmVersion, SourceRunID: "sim_fixed", SourceEngine: "3.5.0",
		SourceConfigHash: "config", SourceMarketHash: "market", FrozenSnapshotHash: "sha256:snapshot",
		FrontierType: TypeRetirementAgeMaxSpending, TargetProbability: 0.9, EvaluationRuns: 3000,
		AgeRange:         &AgeRange{Min: 50, Max: 60},
		Search:           MoneySearch{MinMinor: 24_000_000, MaxMinor: 72_000_000, StepMinor: 600_000},
		EvaluationBudget: 100, PathMonthBudget: 180_000_000,
	}
	inputHash, err := HashIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	const wantInputHash = "sha256:2910c25763f1621ce98152321edbb306ff0fc1ea4bb33bd72f5fde287444a8da"
	const wantOutcomeHash = "sha256:411ac2e9c7660b4dea374958e82287816c29ba233e2485202fbf65e8202137b7"
	if inputHash != wantInputHash || OutcomeHash([]bool{true, false, true, true, false, false, false, true, true}) != wantOutcomeHash {
		t.Fatalf("canonical hash drifted: input=%s outcome=%s", inputHash,
			OutcomeHash([]bool{true, false, true, true, false, false, false, true, true}))
	}
}

func TestNormalizeRejectsContractEdges(t *testing.T) {
	base := func(target float64, runs int, age *AgeRange, search MoneySearch) error {
		_, err := Normalize(TypeRetirementAgeMaxSpending, target, runs, age, search, 40, 90, 3000, 600)
		return err
	}
	validAge := &AgeRange{Min: 50, Max: 50}
	validSearch := MoneySearch{MinMinor: 1, MaxMinor: 9, StepMinor: 2}
	for name, err := range map[string]error{
		"target decimals": base(0.90001, 1000, validAge, validSearch),
		"runs":            base(0.9, 999, validAge, validSearch),
		"age":             base(0.9, 1000, &AgeRange{Min: 39, Max: 50}, validSearch),
		"step":            base(0.9, 1000, validAge, MoneySearch{MinMinor: 1, MaxMinor: 8, StepMinor: 2}),
		"levels":          base(0.9, 1000, validAge, MoneySearch{MinMinor: 1, MaxMinor: 1025, StepMinor: 1}),
	} {
		if err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	_, err := Normalize(TypeRetirementAgeMaxSpending, 0.9, 1000,
		&AgeRange{Min: 40, Max: 60}, MoneySearch{MinMinor: 1, MaxMinor: 1024, StepMinor: 1},
		40, 90, 3000, 600)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("budget error=%v", err)
	}
}

func TestBuildCandidateWhitelistsAndCoastDefinition(t *testing.T) {
	frozen, _ := testFrozen(t, TypeRetirementAgeMaxSpending, &AgeRange{Min: 50, Max: 50},
		MoneySearch{MinMinor: 100, MaxMinor: 900, StepMinor: 100})
	tests := []struct {
		typeName string
		age      int
		value    int64
		check    func(*testing.T, simulation.InputSnapshot)
	}{
		{TypeRetirementAgeMaxSpending, 55, 500, func(t *testing.T, got simulation.InputSnapshot) {
			if got.Parameters.RetirementAge != 55 || got.Parameters.AnnualSpendingMinor != 500 {
				t.Fatalf("max spending candidate=%+v", got.Parameters)
			}
		}},
		{TypeRetirementAgeMinSavings, 56, 400, func(t *testing.T, got simulation.InputSnapshot) {
			if got.Parameters.RetirementAge != 56 || got.Parameters.AnnualSavingsMinor != 400 {
				t.Fatalf("min savings candidate=%+v", got.Parameters)
			}
		}},
		{TypeRequiredCurrentAssets, 0, 7, func(t *testing.T, got simulation.InputSnapshot) {
			if got.Parameters.TotalAssetsMinor != 7 || got.Parameters.AnnualSavingsMinor != 300 {
				t.Fatalf("assets candidate=%+v", got.Parameters)
			}
		}},
		{TypeCoastRequiredAssets, 0, 7, func(t *testing.T, got simulation.InputSnapshot) {
			if got.Parameters.TotalAssetsMinor != 7 || got.Parameters.AnnualSavingsMinor != 0 ||
				got.Parameters.RetirementAge != frozen.SourceSnapshot.Parameters.RetirementAge {
				t.Fatalf("coast candidate=%+v", got.Parameters)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.typeName, func(t *testing.T) {
			before, _ := json.Marshal(frozen.SourceSnapshot)
			got, err := BuildCandidate(frozen.SourceSnapshot, test.typeName,
				Candidate{RetirementAge: test.age, ValueMinor: test.value})
			if err != nil {
				t.Fatal(err)
			}
			test.check(t, got)
			after, _ := json.Marshal(frozen.SourceSnapshot)
			if string(before) != string(after) {
				t.Fatal("source snapshot mutated")
			}
			got.BaseCurrency = "USD"
			if err := ValidateCandidateDiff(frozen.SourceSnapshot, got, test.typeName); !errors.Is(err, ErrCandidateInvalid) {
				t.Fatalf("off-whitelist diff error=%v", err)
			}
		})
	}
}

func TestLargestRemainderPositiveAndZeroTotalFallback(t *testing.T) {
	frozen, configInput := testFrozen(t, TypeRequiredCurrentAssets, nil,
		MoneySearch{MinMinor: 1, MaxMinor: 10, StepMinor: 1})
	configInput.Holdings = append(configInput.Holdings,
		map[string]any{"asset_key": "disabled", "enabled": false, "current_amount_minor": int64(77)})
	if err := ValidateConfigAssets(frozen.SourceSnapshot, configInput); err != nil {
		t.Fatalf("disabled config holding should not become a simulation asset: %v", err)
	}
	configCandidate, err := ApplyConfigCandidate(configInput, TypeRequiredCurrentAssets,
		Candidate{ValueMinor: 7}, map[string]int64{"a": 3, "b": 4})
	if err != nil || configCandidate.Holdings[2]["current_amount_minor"] != int64(77) {
		t.Fatalf("disabled holding was changed: config=%+v err=%v", configCandidate.Holdings, err)
	}
	frozen.SourceSnapshot.Assets = []simulation.SnapshotAsset{
		{AssetKey: "z", InitialMinor: 1, TargetWeight: 0.5},
		{AssetKey: "zero", InitialMinor: 0, TargetWeight: 0},
		{AssetKey: "a", InitialMinor: 1, TargetWeight: 0.5},
	}
	frozen.SourceSnapshot.Parameters.TotalAssetsMinor = 2
	got, err := BuildCandidate(frozen.SourceSnapshot, TypeRequiredCurrentAssets, Candidate{ValueMinor: 1})
	if err != nil {
		t.Fatal(err)
	}
	amounts := assetAmountMap(got)
	if amounts["a"] != 1 || amounts["z"] != 0 || amounts["zero"] != 0 {
		t.Fatalf("positive-total tie break=%v", amounts)
	}
	frozen.SourceSnapshot.Parameters.TotalAssetsMinor = 0
	for i := range frozen.SourceSnapshot.Assets {
		frozen.SourceSnapshot.Assets[i].InitialMinor = 0
	}
	got, err = BuildCandidate(frozen.SourceSnapshot, TypeRequiredCurrentAssets, Candidate{ValueMinor: 1})
	if err != nil {
		t.Fatal(err)
	}
	amounts = assetAmountMap(got)
	if amounts["a"] != 1 || amounts["z"] != 0 || amounts["zero"] != 0 {
		t.Fatalf("zero-total tie break=%v", amounts)
	}
	got, err = BuildCandidate(frozen.SourceSnapshot, TypeRequiredCurrentAssets,
		Candidate{ValueMinor: MaxMoneyMinor})
	if err != nil {
		t.Fatal(err)
	}
	var sum int64
	for _, asset := range got.Assets {
		sum += asset.InitialMinor
	}
	if sum != MaxMoneyMinor {
		t.Fatalf("large allocation sum=%d", sum)
	}
}

func TestSearchFourFrontiersAndBoundaryEvidence(t *testing.T) {
	tests := []struct {
		typeName string
		age      *AgeRange
		field    func(simulation.InputSnapshot) int64
	}{
		{TypeRetirementAgeMaxSpending, &AgeRange{Min: 50, Max: 50}, func(in simulation.InputSnapshot) int64 {
			return 1000 - in.Parameters.AnnualSpendingMinor
		}},
		{TypeRetirementAgeMinSavings, &AgeRange{Min: 50, Max: 50}, func(in simulation.InputSnapshot) int64 {
			return in.Parameters.AnnualSavingsMinor
		}},
		{TypeRequiredCurrentAssets, nil, func(in simulation.InputSnapshot) int64 {
			return in.Parameters.TotalAssetsMinor
		}},
		{TypeCoastRequiredAssets, nil, func(in simulation.InputSnapshot) int64 {
			if in.Parameters.AnnualSavingsMinor != 0 {
				return 1000
			}
			return in.Parameters.TotalAssetsMinor
		}},
	}
	for _, test := range tests {
		t.Run(test.typeName, func(t *testing.T) {
			frozen, _ := testFrozen(t, test.typeName, test.age,
				MoneySearch{MinMinor: 100, MaxMinor: 900, StepMinor: 100})
			result, err := Search(context.Background(), frozen, SearchOptions{
				Parallelism: 8, Evaluator: thresholdEvaluator(test.field, 500),
			})
			if err != nil {
				t.Fatal(err)
			}
			point := result.Points[0]
			wantWorse := int64(400)
			if test.typeName == TypeRetirementAgeMaxSpending {
				wantWorse = 600
			}
			if len(result.Points) != 1 || point.Status != StatusBoundaryFound ||
				point.ValueMinor != 500 || point.WorseNeighbor == nil || point.WorseNeighbor.ValueMinor != wantWorse {
				t.Fatalf("unexpected point: %+v", point)
			}
			if isAgeFrontier(test.typeName) != point.Applicable {
				t.Fatalf("applicable=%v type=%s", point.Applicable, test.typeName)
			}
			if !isAgeFrontier(test.typeName) && point.GapMinor == nil {
				t.Fatalf("asset point has no gap: %+v", point)
			}
		})
	}
}

func TestSearchDomainStatusesAndWilsonRule(t *testing.T) {
	frozen, _ := testFrozen(t, TypeRetirementAgeMinSavings, &AgeRange{Min: 50, Max: 50},
		MoneySearch{MinMinor: 100, MaxMinor: 900, StepMinor: 100})
	all := func(value bool) Evaluator {
		return func(_ context.Context, _ *simulation.InputSnapshot, runs int) (simulation.OutcomeEvaluation, error) {
			out := make([]bool, runs)
			for i := range out {
				out[i] = value
			}
			return simulation.OutcomeEvaluation{Runs: runs, SuccessCount: countSuccess(out), Outcomes: out}, nil
		}
	}
	for _, test := range []struct {
		status   string
		value    bool
		endpoint int64
	}{{StatusEntireDomainFeasible, true, 100}, {StatusNoFeasibleValue, false, 900}} {
		result, err := Search(context.Background(), frozen, SearchOptions{Evaluator: all(test.value)})
		if err != nil {
			t.Fatal(err)
		}
		point := result.Points[0]
		if point.Status != test.status || point.ValueMinor != test.endpoint || point.WorseNeighbor != nil {
			t.Fatalf("point=%+v", point)
		}
	}

	frozen.Config.Search = MoneySearch{MinMinor: 100, MaxMinor: 100, StepMinor: 1}
	frozen.Config.MoneyLevels = 1
	frozen.Config.PerPointBudget, frozen.Config.EvaluationBudget = EvaluationBudget(1, 1)
	frozen.Config.PathMonthBudget, _ = PathMonthBudget(frozen.Config.EvaluationBudget, 1000, 600)
	frozen.Config.TargetSuccessProbability = 0.99
	evaluator := func(_ context.Context, _ *simulation.InputSnapshot, runs int) (simulation.OutcomeEvaluation, error) {
		out := make([]bool, runs)
		for i := 0; i < 995; i++ {
			out[i] = true
		}
		// Deliberately lie in aggregate floating fields. Search must derive the
		// probability and Wilson interval from k/N and branch on Wilson low only.
		return simulation.OutcomeEvaluation{Runs: runs, SuccessCount: 995, SuccessProbability: 1,
			SuccessWilsonLow: 1, SuccessWilsonHigh: 1, Outcomes: out}, nil
	}
	result, err := Search(context.Background(), frozen, SearchOptions{Evaluator: evaluator})
	if err != nil {
		t.Fatal(err)
	}
	if result.Points[0].Status != StatusNoFeasibleValue ||
		result.Points[0].Evaluation.SuccessProbability != 0.995 {
		t.Fatalf("point=%+v", result.Points[0])
	}
}

func TestSingleLevelEndpointAndSnapshotCacheDedup(t *testing.T) {
	frozen, _ := testFrozen(t, TypeRetirementAgeMaxSpending, &AgeRange{Min: 50, Max: 50},
		MoneySearch{MinMinor: 300, MaxMinor: 300, StepMinor: 1})
	calls := 0
	evaluator := func(_ context.Context, _ *simulation.InputSnapshot, runs int) (simulation.OutcomeEvaluation, error) {
		calls++
		outcomes := make([]bool, runs)
		for i := range outcomes {
			outcomes[i] = true
		}
		return simulation.OutcomeEvaluation{Runs: runs, SuccessCount: runs, Outcomes: outcomes}, nil
	}
	result, err := Search(context.Background(), frozen, SearchOptions{Evaluator: evaluator})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.DistinctEvaluations != 1 || len(result.Evaluations) != 1 ||
		result.Points[0].Status != StatusEntireDomainFeasible || result.Points[0].ValueMinor != 300 {
		t.Fatalf("single-level cache/result mismatch: calls=%d result=%+v", calls, result)
	}
}

func TestSearchRejectsCountAndPathMonotonicityViolations(t *testing.T) {
	frozen, _ := testFrozen(t, TypeRetirementAgeMaxSpending, &AgeRange{Min: 50, Max: 50},
		MoneySearch{MinMinor: 100, MaxMinor: 900, StepMinor: 100})
	for _, pathViolation := range []bool{false, true} {
		evaluator := func(_ context.Context, in *simulation.InputSnapshot, runs int) (simulation.OutcomeEvaluation, error) {
			out := make([]bool, runs)
			count := 950
			if in.Parameters.AnnualSpendingMinor >= 900 && !pathViolation {
				count = 951
			}
			for i := 0; i < count; i++ {
				out[i] = true
			}
			if pathViolation && in.Parameters.AnnualSpendingMinor >= 900 {
				out[0], out[950] = false, true
			}
			return simulation.OutcomeEvaluation{Runs: runs, SuccessCount: countSuccess(out), Outcomes: out}, nil
		}
		result, err := Search(context.Background(), frozen, SearchOptions{Evaluator: evaluator})
		if !errors.Is(err, ErrMonotonicityViolated) || !reflect.DeepEqual(result, Result{}) {
			t.Fatalf("path=%v result=%+v err=%v", pathViolation, result, err)
		}
	}
}

func TestSearchConcurrencyIsByteStable(t *testing.T) {
	frozen, _ := testFrozen(t, TypeRetirementAgeMaxSpending, &AgeRange{Min: 50, Max: 52},
		MoneySearch{MinMinor: 100, MaxMinor: 900, StepMinor: 100})
	evaluator := thresholdEvaluator(func(in simulation.InputSnapshot) int64 {
		return int64(in.Parameters.RetirementAge-50)*100 + 1000 - in.Parameters.AnnualSpendingMinor
	}, 500)
	var golden []byte
	for _, parallelism := range []int{1, 8, 16} {
		result, err := Search(context.Background(), frozen, SearchOptions{
			Parallelism: parallelism, Evaluator: evaluator,
		})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := MarshalResult(result)
		if err != nil {
			t.Fatal(err)
		}
		if golden == nil {
			golden = raw
		} else if string(raw) != string(golden) {
			t.Fatalf("parallelism %d produced different result", parallelism)
		}
	}
}

func TestFixedSeedActualEngineConcurrencyIsByteStable(t *testing.T) {
	frozen := goldenFrozen(t, TypeRetirementAgeMaxSpending, &AgeRange{Min: 54, Max: 56},
		MoneySearch{MinMinor: 10_000_000, MaxMinor: 100_000_000, StepMinor: 10_000_000})
	var golden []byte
	for _, parallelism := range []int{1, 8, 16} {
		result, err := Search(context.Background(), frozen, SearchOptions{Parallelism: parallelism})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := MarshalResult(result)
		if err != nil {
			t.Fatal(err)
		}
		if golden == nil {
			golden = raw
		} else if string(raw) != string(golden) {
			t.Fatalf("actual fixed-seed result changed at parallelism %d", parallelism)
		}
	}
}

func TestFixedSeedGoldenAllFrontierTypes(t *testing.T) {
	tests := []struct {
		typeName   string
		age        *AgeRange
		search     MoneySearch
		status     string
		valueMinor int64
		successes  int
		wilsonLow  float64
		wilsonHigh float64
		pointID    string
		resultHash string
	}{
		{TypeRetirementAgeMaxSpending, &AgeRange{Min: 55, Max: 55},
			MoneySearch{MinMinor: 10_000_000, MaxMinor: 100_000_000, StepMinor: 10_000_000},
			StatusBoundaryFound, 70_000_000, 838, 0.8138767565, 0.8595362600,
			"fpt_b84d8b752e34f5dd9664", "sha256:e56784b1027233cb59866a1091911afd3ae16ae6385c8fb543ee4edd5f3bc276"},
		{TypeRetirementAgeMinSavings, &AgeRange{Min: 55, Max: 55},
			MoneySearch{MinMinor: 0, MaxMinor: 100_000_000, StepMinor: 10_000_000},
			StatusBoundaryFound, 10_000_000, 973, 0.9610010106, 0.9813787434,
			"fpt_0f45eb002c22bf54eb25", "sha256:3e2635707ff4bbd53009668f908d86b968d3f091a960f1e1cb810ec4c5341f11"},
		{TypeRequiredCurrentAssets, nil,
			MoneySearch{MinMinor: 10_000_000, MaxMinor: 300_000_000, StepMinor: 10_000_000},
			StatusEntireDomainFeasible, 10_000_000, 881, 0.8594587715, 0.8996251318,
			"fpt_0fbb43843c29e3894e07", "sha256:fd5023589501531c94504824042578133120228c0dc254923bd8e90c1ff0bc56"},
		{TypeCoastRequiredAssets, nil,
			MoneySearch{MinMinor: 10_000_000, MaxMinor: 300_000_000, StepMinor: 10_000_000},
			StatusBoundaryFound, 150_000_000, 839, 0.8149295246, 0.8604758382,
			"fpt_211a286e24ca103d9def", "sha256:bc9921c7f74c2b0a71223371ac17bd862376c3f6d3e729a3d55c89d73ebbd984"},
	}
	const baselineOutcomeHash = "sha256:ceb29e5e0030c55a97cb375e70fed98d43be8b20fc129a91a67e8eaf56acf038"
	for _, test := range tests {
		t.Run(test.typeName, func(t *testing.T) {
			frozen := goldenFrozen(t, test.typeName, test.age, test.search)
			result, err := Search(context.Background(), frozen, SearchOptions{Parallelism: 8})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := MarshalResult(result)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			point := result.Points[0]
			resultHash := "sha256:" + hex.EncodeToString(sum[:])
			if result.Baseline.OutcomeHash != baselineOutcomeHash || point.Status != test.status ||
				point.ValueMinor != test.valueMinor || point.Evaluation.SuccessCount != test.successes ||
				point.Evaluation.SuccessWilsonLow != test.wilsonLow ||
				point.Evaluation.SuccessWilsonHigh != test.wilsonHigh || point.ID != test.pointID ||
				resultHash != test.resultHash {
				t.Fatalf("fixed-seed golden drifted: baseline=%s point=%+v result=%s",
					result.Baseline.OutcomeHash, point, resultHash)
			}
		})
	}
}

func TestDeterministicCashSnapshotHasHandCheckableFourTypeBoundaries(t *testing.T) {
	tests := []struct {
		typeName string
		age      *AgeRange
		search   MoneySearch
		boundary int64
		worse    int64
	}{
		{TypeRetirementAgeMaxSpending, &AgeRange{Min: 31, Max: 31},
			MoneySearch{MinMinor: 1_200, MaxMinor: 3_600, StepMinor: 1_200}, 1_200, 2_400},
		{TypeRetirementAgeMinSavings, &AgeRange{Min: 31, Max: 31},
			MoneySearch{MinMinor: 0, MaxMinor: 3_600, StepMinor: 1_200}, 2_400, 1_200},
		{TypeRequiredCurrentAssets, nil,
			MoneySearch{MinMinor: 1, MaxMinor: 3_598, StepMinor: 1_199}, 2_399, 1_200},
		{TypeCoastRequiredAssets, nil,
			MoneySearch{MinMinor: 1_200, MaxMinor: 4_800, StepMinor: 1_200}, 3_600, 2_400},
	}
	for _, test := range tests {
		t.Run(test.typeName, func(t *testing.T) {
			result, err := Search(context.Background(), deterministicFrozen(t, test.typeName, test.age, test.search),
				SearchOptions{Parallelism: 4})
			if err != nil {
				t.Fatal(err)
			}
			point := result.Points[0]
			if point.Status != StatusBoundaryFound || point.ValueMinor != test.boundary ||
				point.Evaluation.SuccessCount != 1000 || point.WorseNeighbor == nil ||
				point.WorseNeighbor.ValueMinor != test.worse || point.WorseNeighbor.SuccessCount != 0 {
				t.Fatalf("deterministic boundary differs from cash-flow arithmetic: %+v", point)
			}
		})
	}
}

func TestSearchCancellationNeverReturnsPartialResult(t *testing.T) {
	for _, stage := range []string{"baseline", "binary", "validating_result"} {
		t.Run(stage, func(t *testing.T) {
			frozen, _ := testFrozen(t, TypeRetirementAgeMaxSpending, &AgeRange{Min: 50, Max: 50},
				MoneySearch{MinMinor: 100, MaxMinor: 900, StepMinor: 100})
			ctx, cancel := context.WithCancel(context.Background())
			calls := 0
			evaluator := func(_ context.Context, in *simulation.InputSnapshot, runs int) (simulation.OutcomeEvaluation, error) {
				calls++
				if stage == "baseline" && calls == 1 || stage == "binary" && calls == 3 {
					cancel()
					return simulation.OutcomeEvaluation{}, context.Canceled
				}
				return thresholdEvaluator(func(candidate simulation.InputSnapshot) int64 {
					return 1000 - candidate.Parameters.AnnualSpendingMinor
				}, 500)(ctx, in, runs)
			}
			progress := func(_, _ int, phase string) {
				if stage == "validating_result" && phase == "validating_result" {
					cancel()
				}
			}
			result, err := Search(ctx, frozen, SearchOptions{Evaluator: evaluator, Progress: progress})
			if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("stage=%s result=%+v err=%v", stage, result, err)
			}
		})
	}
}

func thresholdEvaluator(field func(simulation.InputSnapshot) int64, threshold int64) Evaluator {
	return func(_ context.Context, in *simulation.InputSnapshot, runs int) (simulation.OutcomeEvaluation, error) {
		out := make([]bool, runs)
		if field(*in) >= threshold {
			for i := range out {
				out[i] = true
			}
		}
		return simulation.OutcomeEvaluation{Runs: runs, SuccessCount: countSuccess(out),
			TerminalP50Minor: field(*in), Outcomes: out}, nil
	}
}

func testFrozen(t *testing.T, frontierType string, age *AgeRange, search MoneySearch) (FrozenInput, domain.ConfigHashInput) {
	t.Helper()
	configInput := domain.ConfigHashInput{
		PlanID: "plan", Name: "plan", BaseCurrency: "CNY", ValuationDate: "2026-07-13",
		Parameters: map[string]any{
			"current_age": 40, "retirement_age": 50, "end_age": 90,
			"total_assets_minor": int64(1000), "annual_savings_minor": int64(300),
			"annual_spending_minor": int64(300),
		},
		Holdings: []map[string]any{
			{"asset_key": "b", "current_amount_minor": int64(500)},
			{"asset_key": "a", "current_amount_minor": int64(500)},
		},
	}
	hash, err := domain.ComputeConfigHash(configInput)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion, PlanID: "plan", BaseCurrency: "CNY",
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 40, RetirementAge: 50, EndAge: 90, TotalAssetsMinor: 1000,
			AnnualSavingsMinor: 300, AnnualSpendingMinor: 300, SimulationRuns: 3000, Seed: "42",
		},
		Assets: []simulation.SnapshotAsset{
			{HoldingID: "h_b", AssetKey: "b", InitialMinor: 500, TargetWeight: 0.5},
			{HoldingID: "h_a", AssetKey: "a", InitialMinor: 500, TargetWeight: 0.5},
		},
		ConfigHash: hash, MarketSnapshotHash: "market",
	}
	config, err := Normalize(frontierType, 0.9, 1000, age, search, 40, 90, 3000, 600)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(configInput)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeConfigHashInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	decodedHash, err := domain.ComputeConfigHash(decoded)
	if err != nil || decodedHash != hash {
		originalJSON, _ := json.Marshal(configInput)
		decodedJSON, _ := json.Marshal(decoded)
		t.Fatalf("fixture hash changed after JSON round trip: %s != %s\n%s\n%s",
			decodedHash, hash, originalJSON, decodedJSON)
	}
	return FrozenInput{SourceSnapshot: snapshot, Config: config, ConfigHashInputJSON: raw}, configInput
}

func goldenFrozen(t *testing.T, frontierType string, age *AgeRange, search MoneySearch) FrozenInput {
	t.Helper()
	configInput := domain.ConfigHashInput{
		PlanID: "golden-plan", Name: "fixed-seed-golden", BaseCurrency: "CNY", ValuationDate: "2026-07-13",
		Parameters: map[string]any{
			"current_age": 30, "retirement_age": 55, "end_age": 60,
			"total_assets_minor": int64(100_000_000), "annual_savings_minor": int64(10_000_000),
			"annual_spending_minor": int64(40_000_000),
		},
		Holdings: []map[string]any{{"asset_key": "equity", "current_amount_minor": int64(100_000_000)}},
	}
	configHash, err := domain.ComputeConfigHash(configInput)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion, PlanID: "golden-plan", BaseCurrency: "CNY",
		RandomFactorModel: simulation.FactorModelMultivariate,
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 30, RetirementAge: 55, EndAge: 60,
			TotalAssetsMinor: 100_000_000, AnnualSavingsMinor: 10_000_000,
			AnnualSpendingMinor: 40_000_000, TerminalWealthFloorMinor: 0,
			InflationMode: "fixed", FixedInflationRate: 0.03,
			WithdrawalType: "fixed_real", WithdrawalRate: 0.04,
			WithdrawalFloorRatio: 0.7, WithdrawalCeilingRatio: 1.3,
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 1000, StudentTDf: 7, Seed: "424242",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "holding-equity", AssetKey: "equity", SnapshotID: "snapshot-equity",
			Currency: "CNY", AssetClass: "equity", InitialMinor: 100_000_000, TargetWeight: 1,
			ModeledAnnualReturn: 0.07, AnnualVolatility: 0.15, SourceHash: "fixed-seed-fixture",
		}},
		ConfigHash: configHash, MarketSnapshotHash: "sha256:fixed-seed-market",
	}
	params := simulation.ParamsFromAnnual(0.07, 0.15)
	model, ok := simulation.AssembleFactorModel(
		[]string{"asset:equity:domestic"}, []float64{params.MonthlyMu},
		[]float64{params.MonthlySigma}, [][]float64{{1}}, nil,
	)
	if !ok {
		t.Fatal("assemble fixed-seed factor model")
	}
	snapshot.FactorModel = &model
	snapshot.AssetFactorRefs = []simulation.FactorRef{{AssetFactorIndex: 0, FXFactorIndex: -1}}
	config, err := Normalize(frontierType, 0.8, 1000, age, search, 30, 60, 1000, snapshot.HorizonMonths())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(configInput)
	if err != nil {
		t.Fatal(err)
	}
	return FrozenInput{SourceSnapshot: snapshot, Config: config, ConfigHashInputJSON: raw}
}

func deterministicFrozen(t *testing.T, frontierType string, age *AgeRange, search MoneySearch) FrozenInput {
	t.Helper()
	configInput := domain.ConfigHashInput{
		PlanID: "deterministic-plan", Name: "deterministic-cash", BaseCurrency: "CNY", ValuationDate: "2026-07-13",
		Parameters: map[string]any{
			"current_age": 30, "retirement_age": 31, "end_age": 32,
			"total_assets_minor": int64(1_200), "annual_savings_minor": int64(1_200),
			"annual_spending_minor": int64(2_400),
		},
		Holdings: []map[string]any{{"asset_key": "cash", "current_amount_minor": int64(1_200)}},
	}
	configHash, err := domain.ComputeConfigHash(configInput)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := simulation.InputSnapshot{
		EngineVersion: simulation.EngineVersion, PlanID: "deterministic-plan", BaseCurrency: "CNY",
		RandomFactorModel: simulation.FactorModelIndependent, AggregateCashLiquidity: true,
		Parameters: simulation.SnapshotParameters{
			CurrentAge: 30, RetirementAge: 31, EndAge: 32,
			TotalAssetsMinor: 1_200, AnnualSavingsMinor: 1_200, AnnualSpendingMinor: 2_400,
			InflationMode: "fixed_real", WithdrawalType: "fixed_real",
			RebalanceFrequency: "annual", RebalanceThreshold: 0.03,
			SimulationRuns: 1000, StudentTDf: 7, Seed: "123",
		},
		Assets: []simulation.SnapshotAsset{{
			HoldingID: "holding-cash", AssetKey: "cash", SnapshotID: "snapshot-cash",
			Currency: "CNY", AssetClass: "cash", IsCash: true,
			InitialMinor: 1_200, TargetWeight: 1, SourceHash: "deterministic-cash",
		}},
		ConfigHash: configHash, MarketSnapshotHash: "sha256:deterministic-market",
	}
	config, err := Normalize(frontierType, 0.5, 1000, age, search, 30, 32, 1000, snapshot.HorizonMonths())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(configInput)
	if err != nil {
		t.Fatal(err)
	}
	return FrozenInput{SourceSnapshot: snapshot, Config: config, ConfigHashInputJSON: raw}
}
