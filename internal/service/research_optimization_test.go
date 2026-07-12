package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

func seedSucceededOptimization(
	t *testing.T,
	db *sql.DB,
	detail ResearchCollectionDetail,
	weights []OptimizationWeightEntry,
) string {
	t.Helper()
	ctx := context.Background()
	resultJSON, err := json.Marshal(OptimizationResult{
		CandidateCount: 1, EvaluatedCount: 1,
		BestByCAGR: []OptimizationResultItem{{
			Rank: 1, Objective: ObjectiveMaxCAGR, Score: 0.1, Weights: weights,
		}},
		BestByCVaR: []OptimizationResultItem{{
			Rank: 1, Objective: ObjectiveMinCVaR, Score: -0.1, Weights: weights,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := optimizationInputSnapshot{
		Assets: make([]researchSnapshotAsset, 0, len(detail.Items)),
		Config: OptimizationConfig{TailRisk: TailRiskSpec{Confidence: 0.99, HorizonDays: 1}},
	}
	for _, item := range detail.Items {
		snapshot.Assets = append(snapshot.Assets, researchSnapshotAsset{
			ItemID: item.ID, AssetKey: item.AssetKey,
		})
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	optimizationID := "ror_apply_test_" + detail.ID
	jobID := "job_apply_test_" + detail.ID
	if err := repository.NewJobRepo(db).Create(ctx, tx, repository.Job{
		ID: jobID, Type: repository.JobTypeResearchOptimization,
		Status: repository.JobStatusSucceeded, InputHash: "apply-test", CreatedAt: detail.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewResearchRepo(db).CreateOptimizationRunTx(ctx, tx, repository.ResearchOptimizationRun{
		ID: optimizationID, CollectionID: detail.ID, JobID: jobID,
		Status: repository.ResearchRunStatusSucceeded, InputHash: "apply-test",
		SourceHash: "source", EngineVersion: OptimizationEngineVersion,
		BaseCurrency: detail.BaseCurrency, RebalancePolicy: detail.RebalancePolicy,
		WindowStart: "2021-01-01", WindowEnd: "2023-12-31",
		ConfigJSON: "{}", InputSnapshotJSON: string(snapshotJSON), CandidateCount: 1, EvaluatedCount: 1,
		ResultJSON: string(resultJSON), CreatedAt: detail.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return optimizationID
}

func createOptimizationApplyFixture(t *testing.T) (*ResearchService, *sql.DB, ResearchCollectionDetail) {
	t.Helper()
	svc, db := newResearchTestService(t)
	for i, key := range []string{"OPT_A", "OPT_B", "OPT_C"} {
		insertResearchFixtureAsset(t, db, key, key, "CNY", "2020-01-01", 1643, growthValue(100+float64(i)))
	}
	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name: "调优应用测试",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "OPT_A", Weight: fptr(0.4)},
			{AssetKey: "OPT_B", Weight: fptr(0.4)},
			{AssetKey: "OPT_C", Weight: fptr(0.2)},
		},
	})
	return svc, db, detail
}

func TestOptimizationWeightEntryJSONUsesAPIFieldNames(t *testing.T) {
	data, err := json.Marshal(OptimizationWeightEntry{
		ItemID: "item_1", AssetKey: "CN|x", Name: "资产A", Weight: 0.5, Locked: true,
	})
	if err != nil {
		t.Fatalf("marshal weight entry: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal weight entry: %v", err)
	}
	for _, key := range []string{"item_id", "asset_key", "name", "weight", "locked"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected key %q in %s", key, string(data))
		}
	}
	for _, key := range []string{"ItemID", "AssetKey", "Name", "Weight", "Locked"} {
		if _, ok := got[key]; ok {
			t.Fatalf("unexpected legacy key %q in %s", key, string(data))
		}
	}
}

// --- input hash determinism ---

func TestComputeOptimizationInputHash_Deterministic(t *testing.T) {
	snapshot := optimizationInputSnapshot{
		EngineVersion: "v1",
		SourceHash:    "abc",
		CommonStart:   "2020-01-01",
		CommonEnd:     "2025-01-01",
		WindowStart:   "2020-01-01",
		WindowEnd:     "2025-01-01",
		Collection: researchSnapshotParams{
			BaseCurrency:    "CNY",
			RebalancePolicy: "monthly",
		},
		LockedWeights: map[string]float64{
			"item_c": 0.3,
			"item_a": 0.1,
			"item_b": 0.2,
		},
		TunableItemIDs: []string{"item_d", "item_e"},
		Config: OptimizationConfig{
			WeightStep:        0.05,
			MaxCandidateCount: 20000,
			TopK:              20,
		},
	}

	hash1 := computeOptimizationInputHash(snapshot)
	hash2 := computeOptimizationInputHash(snapshot)
	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %s != %s", hash1, hash2)
	}

	for i := 0; i < 50; i++ {
		h := computeOptimizationInputHash(snapshot)
		if h != hash1 {
			t.Fatalf("hash changed on iteration %d: %s != %s", i, h, hash1)
		}
	}
	snapshot.Config.TailRisk = TailRiskSpec{Confidence: 0.95, HorizonDays: 20}
	withTailRisk := computeOptimizationInputHash(snapshot)
	if withTailRisk == hash1 {
		t.Fatal("CVaR spec did not participate in optimization input hash")
	}
	minimum := 0.03
	snapshot.Config.MinimumCAGR = &minimum
	if got := computeOptimizationInputHash(snapshot); got == withTailRisk {
		t.Fatal("minimum CAGR did not participate in optimization input hash")
	}
}

// --- candidate generation ---

func TestValidateOptimizationInput_NoAssets(t *testing.T) {
	err := ValidateOptimizationInput(nil)
	if err == nil {
		t.Fatal("expected error for empty assets")
	}
}

func TestValidateOptimizationInput_TooMany(t *testing.T) {
	assets := make([]OptimizationAsset, 11)
	for i := range assets {
		assets[i] = OptimizationAsset{ItemID: "x"}
	}
	err := ValidateOptimizationInput(assets)
	if err == nil {
		t.Fatal("expected error for >10 assets")
	}
}

func TestValidateOptimizationInput_LockedExceeds100(t *testing.T) {
	err := ValidateOptimizationInput([]OptimizationAsset{
		{ItemID: "a", Weight: 0.6, Locked: true},
		{ItemID: "b", Weight: 0.5, Locked: true},
	})
	if err == nil {
		t.Fatal("expected error for locked sum > 100%")
	}
}

func TestValidateOptimizationInput_OK(t *testing.T) {
	err := ValidateOptimizationInput([]OptimizationAsset{
		{ItemID: "a", Weight: 0.2, Locked: true},
		{ItemID: "b", Weight: 0, Locked: false},
		{ItemID: "c", Weight: 0.1, Locked: false},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateCandidates_TwoTunableAssetsLocked20(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Name: "Asset A", Weight: 0.2, Locked: true},
		{ItemID: "b", AssetKey: "B", Name: "Asset B", Weight: 0, Locked: false},
		{ItemID: "c", AssetKey: "C", Name: "Asset C", Weight: 0, Locked: false},
	}
	candidates := GenerateCandidates(assets, 0.05)
	count := CountCandidates(assets, 0.05)

	if len(candidates) != count {
		t.Fatalf("CountCandidates=%d but GenerateCandidates=%d", count, len(candidates))
	}
	if count == 0 {
		t.Fatal("expected candidates but got 0")
	}

	for i, c := range candidates {
		sum := 0.0
		for _, w := range c.Weights {
			sum += w.Weight
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("candidate %d: weight sum %.6f != 1", i, sum)
		}
		for _, w := range c.Weights {
			if w.Locked && w.ItemID == "a" && w.Weight != 0.2 {
				t.Errorf("candidate %d: locked asset weight changed to %.4f", i, w.Weight)
			}
		}
	}
}

func TestGenerateCandidates_UnlockedPositiveWeightIsTuned(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Name: "A", Weight: 0.5, Locked: false},
		{ItemID: "b", AssetKey: "B", Name: "B", Weight: 0.5, Locked: false},
	}
	candidates := GenerateCandidates(assets, 0.05)
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
	foundDifferent := false
	for _, c := range candidates {
		for _, w := range c.Weights {
			if w.ItemID == "a" && w.Weight != 0.5 {
				foundDifferent = true
				break
			}
		}
		if foundDifferent {
			break
		}
	}
	if !foundDifferent {
		t.Error("unlocked positive-weight asset A was never adjusted")
	}
}

func TestGenerateCandidates_AllLocked100Percent(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Name: "A", Weight: 0.5, Locked: true},
		{ItemID: "b", AssetKey: "B", Name: "B", Weight: 0.5, Locked: true},
	}
	candidates := GenerateCandidates(assets, 0.05)
	count := CountCandidates(assets, 0.05)
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate for all-locked-100%%, got %d", len(candidates))
	}
	if count != 1 {
		t.Fatalf("count expected 1, got %d", count)
	}
}

func TestGenerateCandidates_AllWeightsSumTo100(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Name: "A", Weight: 0.1, Locked: true},
		{ItemID: "b", AssetKey: "B", Name: "B", Weight: 0, Locked: false},
		{ItemID: "c", AssetKey: "C", Name: "C", Weight: 0, Locked: false},
		{ItemID: "d", AssetKey: "D", Name: "D", Weight: 0.3, Locked: false},
	}
	candidates := GenerateCandidates(assets, 0.1)
	for i, c := range candidates {
		sum := 0.0
		for _, w := range c.Weights {
			sum += w.Weight
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("candidate %d: weight sum %.6f != 1", i, sum)
		}
		for _, w := range c.Weights {
			if !w.Locked && w.Weight > 0 {
				found := false
				for _, ww := range c.Weights {
					if ww.ItemID == w.ItemID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("candidate %d: weight entry for %s not found", i, w.ItemID)
				}
			}
		}
	}
}

func TestCountCandidates_MatchesGenerate(t *testing.T) {
	tests := []struct {
		name       string
		assets     []OptimizationAsset
		weightStep float64
	}{
		{
			name: "3 tunable step 10%",
			assets: []OptimizationAsset{
				{ItemID: "a", Weight: 0, Locked: false},
				{ItemID: "b", Weight: 0, Locked: false},
				{ItemID: "c", Weight: 0, Locked: false},
			},
			weightStep: 0.1,
		},
		{
			name: "locked 30% + 2 tunable step 5%",
			assets: []OptimizationAsset{
				{ItemID: "a", Weight: 0.3, Locked: true},
				{ItemID: "b", Weight: 0, Locked: false},
				{ItemID: "c", Weight: 0, Locked: false},
			},
			weightStep: 0.05,
		},
		{
			name: "single tunable step 25%",
			assets: []OptimizationAsset{
				{ItemID: "a", Weight: 0, Locked: false},
			},
			weightStep: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := CountCandidates(tt.assets, tt.weightStep)
			candidates := GenerateCandidates(tt.assets, tt.weightStep)
			if count != len(candidates) {
				t.Errorf("CountCandidates=%d != len(GenerateCandidates)=%d", count, len(candidates))
			}
		})
	}
}

func TestCountCandidatesReturnsExactCountAboveRecommendation(t *testing.T) {
	assets := make([]OptimizationAsset, 4)
	for i := range assets {
		assets[i] = OptimizationAsset{ItemID: fmt.Sprintf("i%d", i), AssetKey: fmt.Sprintf("a%d", i)}
	}
	const want = 167668501 // C(1000+4-1, 4-1)
	if got := CountCandidates(assets, 0.001); got != want {
		t.Fatalf("over-recommendation candidate count = %d, want %d", got, want)
	}
}

func TestVisitOptimizationCandidatesStopsWithoutMaterializingGrid(t *testing.T) {
	assets := make([]OptimizationAsset, 4)
	for i := range assets {
		assets[i] = OptimizationAsset{ItemID: fmt.Sprintf("i%d", i), AssetKey: fmt.Sprintf("a%d", i)}
	}
	visited := VisitOptimizationCandidates(assets, 0.001, func(OptimizationWeightVector) bool {
		return false
	})
	if visited != 1 {
		t.Fatalf("visited=%d, want early stop after 1 candidate", visited)
	}
}

func TestEvaluateOptimizationCandidatesUsesFourWorkersAndIsDeterministic(t *testing.T) {
	svc, _ := newResearchTestService(t)
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item:  repository.ResearchCollectionItem{ID: "i1", AssetKey: "A", Enabled: true},
				Asset: repository.MarketAsset{AssetKey: "A", Name: "A", Currency: "CNY"},
			},
			{
				Item:  repository.ResearchCollectionItem{ID: "i2", AssetKey: "B", Enabled: true},
				Asset: repository.MarketAsset{AssetKey: "B", Name: "B", Currency: "CNY"},
			},
		},
		FX: map[string]*researchFXData{},
	}
	assets := []OptimizationAsset{
		{ItemID: "i1", AssetKey: "A", Name: "A"},
		{ItemID: "i2", AssetKey: "B", Name: "B"},
	}
	snapshot := optimizationInputSnapshot{
		Collection: researchSnapshotParams{BaseCurrency: "CNY", RebalancePolicy: ResearchRebalanceMonthly},
		Config: OptimizationConfig{
			WeightStep: 0.1, TopK: 5,
			TailRisk: TailRiskSpec{Confidence: 0.95, HorizonDays: 20},
		},
	}
	candidateCount := CountCandidates(assets, snapshot.Config.WeightStep)
	backtest := func(input BacktestInput) (*BacktestResult, error) {
		weight := input.Assets[0].Weight
		calmar := weight
		return &BacktestResult{Summary: BacktestSummary{
			CAGR: weight, MaxDrawdown: -(1 - weight), Calmar: &calmar,
			EffectiveReturnDays: 252,
			TailRisk: &BacktestTailRisk{
				Confidence: 0.95, HorizonDays: 20, ScenarioCount: 233,
				VaRLoss: 1 - weight/2, CVaRLoss: 1 - weight,
			},
		}}, nil
	}

	svc.SetOptimizationConcurrency(1)
	svc.optimizationBacktest = backtest
	sequential, err := svc.evaluateOptimizationCandidates(
		context.Background(), "", snapshot, ds, assets, candidateCount, nil, nil,
	)
	if err != nil {
		t.Fatalf("sequential evaluation: %v", err)
	}

	started := make(chan struct{}, candidateCount)
	release := make(chan struct{})
	svc.SetOptimizationConcurrency(4)
	svc.optimizationBacktest = func(input BacktestInput) (*BacktestResult, error) {
		started <- struct{}{}
		<-release
		return backtest(input)
	}
	type evaluationOutcome struct {
		result OptimizationResult
		err    error
	}
	done := make(chan evaluationOutcome, 1)
	go func() {
		result, evalErr := svc.evaluateOptimizationCandidates(
			context.Background(), "", snapshot, ds, assets, candidateCount, nil, nil,
		)
		done <- evaluationOutcome{result: result, err: evalErr}
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("four optimization evaluators did not start concurrently")
		}
	}
	close(release)
	parallel := <-done
	if parallel.err != nil {
		t.Fatalf("parallel evaluation: %v", parallel.err)
	}
	sequentialJSON, err := json.Marshal(sequential)
	if err != nil {
		t.Fatal(err)
	}
	parallelJSON, err := json.Marshal(parallel.result)
	if err != nil {
		t.Fatal(err)
	}
	if string(parallelJSON) != string(sequentialJSON) {
		t.Fatalf("parallel result differs from sequential result\nparallel=%s\nsequential=%s",
			parallelJSON, sequentialJSON)
	}
}

func TestEvaluateOptimizationCandidatesCancelsParallelPoolBeforeEvaluation(t *testing.T) {
	svc, _ := newResearchTestService(t)
	called := false
	svc.optimizationBacktest = func(BacktestInput) (*BacktestResult, error) {
		called = true
		return nil, errors.New("must not run")
	}
	assets := []OptimizationAsset{{ItemID: "i1", AssetKey: "A"}}
	_, err := svc.evaluateOptimizationCandidates(
		context.Background(), "", optimizationInputSnapshot{
			Config: OptimizationConfig{WeightStep: 0.1, TopK: 5},
		},
		&researchDataset{FX: map[string]*researchFXData{}},
		assets, CountCandidates(assets, 0.1), func() bool { return true }, nil,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v, want context.Canceled", err)
	}
	if called {
		t.Fatal("candidate evaluator ran after cancellation")
	}
}

// --- objective ranking ---

func TestTopKTracker_CAGR(t *testing.T) {
	tracker := NewTopKTracker(ObjectiveMaxCAGR, 3)
	for i := 0; i < 10; i++ {
		tracker.Push(OptimizationResultItem{
			Score:   float64(i) * 0.01,
			Weights: []OptimizationWeightEntry{{ItemID: string(rune('a' + i)), AssetKey: string(rune('A' + i)), Weight: 1}},
			Summary: BacktestSummary{
				CAGR: float64(i) * 0.01,
			},
		})
	}
	results := tracker.Results()
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Score != 0.09 {
		t.Errorf("top CAGR should be 0.09, got %f", results[0].Score)
	}
	if results[0].Rank != 1 {
		t.Errorf("top result rank should be 1, got %d", results[0].Rank)
	}
}

func TestTopKTracker_MinDrawdown(t *testing.T) {
	tracker := NewTopKTracker(ObjectiveMinDrawdown, 2)
	tracker.Push(OptimizationResultItem{Score: -0.20, Weights: []OptimizationWeightEntry{{ItemID: "a", Weight: 1}}})
	tracker.Push(OptimizationResultItem{Score: -0.05, Weights: []OptimizationWeightEntry{{ItemID: "b", Weight: 1}}})
	tracker.Push(OptimizationResultItem{Score: -0.15, Weights: []OptimizationWeightEntry{{ItemID: "c", Weight: 1}}})

	results := tracker.Results()
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// -0.05 is "highest" (closest to 0), so it should be rank 1
	if results[0].Score != -0.05 {
		t.Errorf("best drawdown should be -0.05, got %f", results[0].Score)
	}
}

func TestTopKTracker_Calmar(t *testing.T) {
	tracker := NewTopKTracker(ObjectiveMaxCalmar, 2)
	tracker.Push(OptimizationResultItem{Score: 0.5, Weights: []OptimizationWeightEntry{{ItemID: "a", Weight: 1}}})
	tracker.Push(OptimizationResultItem{Score: 1.2, Weights: []OptimizationWeightEntry{{ItemID: "b", Weight: 1}}})
	tracker.Push(OptimizationResultItem{Score: 0.8, Weights: []OptimizationWeightEntry{{ItemID: "c", Weight: 1}}})

	results := tracker.Results()
	if results[0].Score != 1.2 {
		t.Errorf("best calmar should be 1.2, got %f", results[0].Score)
	}
}

func TestScoreForObjective(t *testing.T) {
	calmar := 0.75
	summary := BacktestSummary{
		CAGR:        0.12,
		MaxDrawdown: -0.16,
		Calmar:      &calmar,
	}

	score, ok := ScoreForObjective(ObjectiveMaxCAGR, summary)
	if !ok || score != 0.12 {
		t.Errorf("CAGR score: got %f, ok=%v", score, ok)
	}

	score, ok = ScoreForObjective(ObjectiveMinDrawdown, summary)
	if !ok || score != -0.16 {
		t.Errorf("Drawdown score: got %f, ok=%v", score, ok)
	}

	score, ok = ScoreForObjective(ObjectiveMaxCalmar, summary)
	if !ok || score != 0.75 {
		t.Errorf("Calmar score: got %f, ok=%v", score, ok)
	}
}

func TestGenerateCandidates_NonDivisibleRemaining(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Name: "A", Weight: 0.33, Locked: true},
		{ItemID: "b", AssetKey: "B", Name: "B", Weight: 0, Locked: false},
		{ItemID: "c", AssetKey: "C", Name: "C", Weight: 0, Locked: false},
	}
	candidates := GenerateCandidates(assets, 0.05)
	if len(candidates) == 0 {
		t.Fatal("expected candidates for non-divisible remaining")
	}
	for i, c := range candidates {
		sum := 0.0
		for _, w := range c.Weights {
			sum += w.Weight
		}
		if math.Abs(sum-1) > ResearchWeightTolerance {
			t.Errorf("candidate %d: weight sum %.10f != 1 (drift=%.10f)", i, sum, sum-1)
		}
	}
}

func TestGenerateCandidates_RemainingBelowStep(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Weight: 0.98, Locked: true},
		{ItemID: "b", AssetKey: "B"},
		{ItemID: "c", AssetKey: "C"},
	}
	candidates := GenerateCandidates(assets, 0.05)
	if len(candidates) != 2 || CountCandidates(assets, 0.05) != len(candidates) {
		t.Fatalf("remaining below one step should yield one residual candidate per tunable asset, got %d", len(candidates))
	}
	for _, candidate := range candidates {
		positiveTunable := 0
		for _, entry := range candidate.Weights {
			if entry.ItemID == "a" && math.Abs(entry.Weight-0.98) > 1e-12 {
				t.Fatalf("locked weight changed: %+v", entry)
			}
			if !entry.Locked && entry.Weight > 0 {
				positiveTunable++
				if math.Abs(entry.Weight-0.02) > 1e-12 {
					t.Fatalf("residual weight expected 2%%, got %+v", entry)
				}
			}
		}
		if positiveTunable != 1 {
			t.Fatalf("expected exactly one residual receiver: %+v", candidate.Weights)
		}
	}
}

func TestGenerateCandidates_Locked100WithTunableAssets(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Weight: 1, Locked: true},
		{ItemID: "b", AssetKey: "B"},
		{ItemID: "c", AssetKey: "C"},
	}
	candidates := GenerateCandidates(assets, 0.05)
	if len(candidates) != 1 || CountCandidates(assets, 0.05) != 1 {
		t.Fatalf("expected one fixed candidate, got %d", len(candidates))
	}
	for _, entry := range candidates[0].Weights {
		if !entry.Locked && entry.Weight != 0 {
			t.Fatalf("tunable entry must be zero: %+v", entry)
		}
	}
}

func TestGenerateCandidates_OneTunableReceivesRemaining(t *testing.T) {
	assets := []OptimizationAsset{
		{ItemID: "a", AssetKey: "A", Weight: 0.3, Locked: true},
		{ItemID: "b", AssetKey: "B"},
	}
	candidates := GenerateCandidates(assets, 0.05)
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(candidates))
	}
	if got := candidates[0].Weights[1].Weight; math.Abs(got-0.7) > 1e-12 {
		t.Fatalf("single tunable asset expected 70%%, got %v", got)
	}
}

func TestTopKTrackerStableTiesAndDeduplicates(t *testing.T) {
	inputs := []OptimizationResultItem{
		{Score: 0.1, Summary: BacktestSummary{CAGR: 0.1, MaxDrawdown: -0.2}, Weights: []OptimizationWeightEntry{{ItemID: "b", AssetKey: "B", Weight: 1}}},
		{Score: 0.1, Summary: BacktestSummary{CAGR: 0.1, MaxDrawdown: -0.2}, Weights: []OptimizationWeightEntry{{ItemID: "a", AssetKey: "A", Weight: 1}}},
		{Score: 0.1, Summary: BacktestSummary{CAGR: 0.1, MaxDrawdown: -0.2}, Weights: []OptimizationWeightEntry{{ItemID: "c", AssetKey: "C", Weight: 1}}},
	}
	var expected string
	for run := 0; run < 50; run++ {
		tracker := NewTopKTracker(ObjectiveMaxCAGR, 3)
		for i := len(inputs) - 1; i >= 0; i-- {
			tracker.Push(inputs[(i+run)%len(inputs)])
		}
		tracker.Push(inputs[0])
		got, err := json.Marshal(tracker.Results())
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			expected = string(got)
		} else if string(got) != expected {
			t.Fatalf("tie order changed on run %d:\n%s\n%s", run, expected, got)
		}
	}
}

func TestTopKTrackerCVaROrderUsesVaRBeforeReturn(t *testing.T) {
	tail := func(cvar, valueAtRisk float64) *BacktestTailRisk {
		return &BacktestTailRisk{CVaRLoss: cvar, VaRLoss: valueAtRisk}
	}
	tracker := NewTopKTracker(ObjectiveMinCVaR, 3)
	tracker.Push(OptimizationResultItem{
		Score: -0.08, Summary: BacktestSummary{CAGR: 0.20, TailRisk: tail(0.08, 0.07)},
		Weights: []OptimizationWeightEntry{{ItemID: "b", AssetKey: "B", Weight: 1}},
	})
	tracker.Push(OptimizationResultItem{
		Score: -0.08, Summary: BacktestSummary{CAGR: 0.10, TailRisk: tail(0.08, 0.06)},
		Weights: []OptimizationWeightEntry{{ItemID: "a", AssetKey: "A", Weight: 1}},
	})
	tracker.Push(OptimizationResultItem{
		Score: -0.07, Summary: BacktestSummary{CAGR: 0.01, TailRisk: tail(0.07, 0.07)},
		Weights: []OptimizationWeightEntry{{ItemID: "c", AssetKey: "C", Weight: 1}},
	})
	got := tracker.Results()
	if len(got) != 3 || got[0].Weights[0].AssetKey != "C" || got[1].Weights[0].AssetKey != "A" {
		t.Fatalf("unexpected CVaR order: %+v", got)
	}
}

func TestTrackOptimizationCandidateMinimumCAGROnlyFiltersCVaR(t *testing.T) {
	trackers := newOptimizationCandidateTrackers(5)
	minimum := 0.05
	eligible := trackOptimizationCandidate(trackers, OptimizationWeightVector{
		Weights: []OptimizationWeightEntry{{ItemID: "a", AssetKey: "A", Weight: 1}},
	}, BacktestSummary{
		CAGR: 0.04, MaxDrawdown: -0.1,
		TailRisk: &BacktestTailRisk{CVaRLoss: 0.08, VaRLoss: 0.06},
	}, &minimum)
	if eligible || len(trackers.cvar.Results()) != 0 {
		t.Fatal("candidate below minimum CAGR entered CVaR ranking")
	}
	if len(trackers.cagr.Results()) != 1 || len(trackers.drawdown.Results()) != 1 || len(trackers.calmar.Results()) != 1 {
		t.Fatal("minimum CAGR incorrectly filtered a non-CVaR ranking")
	}
}

func optimizationTestPoints(start string, count int) []repository.MarketAssetPoint {
	day, _ := time.Parse("2006-01-02", start)
	out := make([]repository.MarketAssetPoint, count)
	for i := range out {
		out[i] = repository.MarketAssetPoint{
			TradeDate: day.AddDate(0, 0, i).Format("2006-01-02"), Value: 100 + float64(i),
		}
	}
	return out
}

func TestBuildBacktestInputForCandidateRetainsZeroWeightAssetsAndFreezesCalendar(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{Item: repository.ResearchCollectionItem{AssetKey: "A"}, Asset: repository.MarketAsset{Name: "A", Currency: "CNY"}},
			{Item: repository.ResearchCollectionItem{AssetKey: "B"}, Asset: repository.MarketAsset{Name: "B", Currency: "CNY"}},
		},
		FX: map[string]*researchFXData{},
	}
	snapshot := optimizationInputSnapshot{
		Collection: researchSnapshotParams{BaseCurrency: "CNY"},
		Config:     OptimizationConfig{TailRisk: TailRiskSpec{Confidence: 0.95, HorizonDays: 20}},
	}
	input := buildBacktestInputForCandidate(snapshot, ds, OptimizationWeightVector{
		Weights: []OptimizationWeightEntry{{AssetKey: "A", Weight: 1}, {AssetKey: "B", Weight: 0}},
	})
	if !input.FreezeEffectiveCalendar || len(input.Assets) != 2 || input.Assets[1].Weight != 0 {
		t.Fatalf("optimization input did not preserve its frozen asset universe: %+v", input)
	}
}

func TestOptimizationTailRiskSnapshotVersionCompatibility(t *testing.T) {
	for _, version := range []string{OptimizationTailRiskV3Version, OptimizationEngineVersion} {
		if !optimizationEngineHasTailRiskSnapshot(version) {
			t.Fatalf("tail-risk optimization version %q was not recognized", version)
		}
	}
	if optimizationEngineHasTailRiskSnapshot("research_optimizer_v2") {
		t.Fatal("v2 optimization must not be treated as carrying a tail-risk snapshot")
	}
}

func TestValidateOptimizationCandidateSamplesRejectsMismatch(t *testing.T) {
	expectedDays, expectedScenarios := -1, -1
	first := BacktestSummary{
		EffectiveReturnDays: 1006,
		TailRisk:            &BacktestTailRisk{ScenarioCount: 987},
	}
	if err := validateOptimizationCandidateSamples(first, &expectedDays, &expectedScenarios); err != nil {
		t.Fatal(err)
	}
	if expectedDays != 1006 || expectedScenarios != 987 {
		t.Fatalf("first candidate did not freeze samples: %d/%d", expectedDays, expectedScenarios)
	}
	second := first
	second.EffectiveReturnDays = 1003
	second.TailRisk = &BacktestTailRisk{ScenarioCount: 984}
	if err := validateOptimizationCandidateSamples(second, &expectedDays, &expectedScenarios); !errors.Is(err, errOptimizationSampleMismatch) {
		t.Fatalf("sample mismatch error = %v", err)
	}
}

func TestOptimizationTailRiskReadinessChecksEveryPotentialPositiveAsset(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item:  repository.ResearchCollectionItem{ID: "long", AssetKey: "LONG", Enabled: true},
				Asset: repository.MarketAsset{Name: "Long"}, Points: optimizationTestPoints("2020-01-01", 140),
			},
			{
				Item:  repository.ResearchCollectionItem{ID: "short", AssetKey: "SHORT", Enabled: true},
				Asset: repository.MarketAsset{Name: "Short"}, Points: optimizationTestPoints("2020-01-01", 100),
			},
			{
				Item:  repository.ResearchCollectionItem{ID: "ignored", AssetKey: "IGNORED", Enabled: true, WeightLocked: true},
				Asset: repository.MarketAsset{Name: "Ignored"}, Points: optimizationTestPoints("2020-01-01", 2),
			},
		},
		FX: map[string]*researchFXData{},
	}
	out := OptimizationReadiness{BlockingReasons: []ResearchReadinessIssue{}}
	appendOptimizationTailRiskIssues(ds, ResearchReadiness{
		WindowStart: "2020-01-01", WindowEnd: "2020-05-19",
	}, TailRiskSpec{Confidence: 0.95, HorizonDays: 20}, &out)
	if out.TailRisk == nil || out.TailRisk.ScenarioCount != 80 || out.TailRisk.MinimumScenarioCount != 100 {
		t.Fatalf("unexpected conservative tail-risk summary: %+v", out.TailRisk)
	}
	if len(out.BlockingReasons) != 1 || out.BlockingReasons[0].AssetKey != "SHORT" || out.BlockingReasons[0].Reason != ResearchReasonCVARSample {
		t.Fatalf("unexpected blockers: %+v", out.BlockingReasons)
	}
}

// --- readiness ---

func TestOptimizationReadiness_WeightSumDoesNotBlock(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item: repository.ResearchCollectionItem{
					ID: "i1", AssetKey: "A", Weight: 0.3, WeightLocked: true, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "A"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
			},
			{
				Item: repository.ResearchCollectionItem{
					ID: "i2", AssetKey: "B", Weight: 0, WeightLocked: false, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "B"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
			},
		},
		FX: map[string]*researchFXData{},
	}
	r := evaluateOptimizationReadiness(ds, OptimizationConfig{WeightStep: 0.05})
	if !r.Ready {
		t.Errorf("expected ready despite weight sum != 100%%, blocking: %v", r.BlockingReasons)
	}
	if r.CandidateCount == 0 {
		t.Error("expected positive candidate count")
	}
}

func TestOptimizationReadiness_AllZeroWeightsReady(t *testing.T) {
	ds := &researchDataset{
		FX: map[string]*researchFXData{},
	}
	for _, key := range []string{"A", "B", "C", "D"} {
		ds.Enabled = append(ds.Enabled, researchAssetData{
			Item: repository.ResearchCollectionItem{
				ID: "i" + key, AssetKey: key, Weight: 0, WeightLocked: false, Enabled: true,
			},
			Asset:  repository.MarketAsset{Name: key},
			Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
		})
	}

	r := evaluateOptimizationReadiness(ds, OptimizationConfig{WeightStep: 0.05})
	if !r.Ready {
		t.Fatalf("expected all-zero enabled assets to be ready for optimization, blocking: %v", r.BlockingReasons)
	}
	if r.EnabledCount != 4 || r.TunableCount != 4 {
		t.Fatalf("unexpected counts: enabled=%d tunable=%d", r.EnabledCount, r.TunableCount)
	}
	if r.CandidateCount == 0 {
		t.Fatal("expected positive candidate count")
	}
}

func TestOptimizationReadiness_HistoryMissingBlocks(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item: repository.ResearchCollectionItem{
					ID: "i1", AssetKey: "A", Weight: 0, WeightLocked: false, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "A"},
				Points: nil, // no history
			},
		},
		FX: map[string]*researchFXData{},
	}
	r := evaluateOptimizationReadiness(ds, OptimizationConfig{WeightStep: 0.05})
	if r.Ready {
		t.Error("expected not ready due to missing history")
	}
	foundHistoryMissing := false
	for _, b := range r.BlockingReasons {
		if b.Reason == ResearchReasonHistoryMissing {
			foundHistoryMissing = true
		}
	}
	if !foundHistoryMissing {
		t.Error("expected history_missing blocking reason")
	}
}

func TestOptimizationReadiness_FXMissingBlocks(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item: repository.ResearchCollectionItem{
					ID: "i1", AssetKey: "A", Weight: 0, WeightLocked: false, Enabled: true,
				},
				Asset:   repository.MarketAsset{Name: "A", Currency: "USD"},
				Points:  []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
				FXPairs: []string{"USDCNY"},
			},
		},
		FX: map[string]*researchFXData{},
	}
	r := evaluateOptimizationReadiness(ds, OptimizationConfig{WeightStep: 0.05})
	if r.Ready {
		t.Error("expected not ready due to missing FX")
	}
	foundFXMissing := false
	for _, b := range r.BlockingReasons {
		if b.Reason == ResearchReasonFXMissing {
			foundFXMissing = true
		}
	}
	if !foundFXMissing {
		t.Error("expected fx_missing blocking reason")
	}
}

func TestOptimizationReadiness_MaxCandidateCountWarnsWithoutBlocking(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item: repository.ResearchCollectionItem{
					ID: "i1", AssetKey: "A", Weight: 0, WeightLocked: false, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "A"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
			},
			{
				Item: repository.ResearchCollectionItem{
					ID: "i2", AssetKey: "B", Weight: 0, WeightLocked: false, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "B"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
			},
		},
		FX: map[string]*researchFXData{},
	}
	r := evaluateOptimizationReadiness(ds, OptimizationConfig{
		WeightStep:        0.05,
		MaxCandidateCount: 1,
	})
	if !r.Ready {
		t.Errorf("expected ready when candidate count exceeds recommendation, blocking: %v", r.BlockingReasons)
	}
	found := false
	for _, warning := range r.Warnings {
		if warning.Reason == "candidate_count_exceeds_recommendation" {
			found = true
			for _, want := range []string{"当前候选数量 21", "推荐控制在 1 以内", "模拟耗时和内存占用会急剧增加"} {
				if !strings.Contains(warning.Message, want) {
					t.Errorf("warning %q does not contain %q", warning.Message, want)
				}
			}
		}
	}
	if !found {
		t.Error("expected candidate_count_exceeds_recommendation warning")
	}
}

func TestOptimizationReadiness_MaxCandidateCountAllows(t *testing.T) {
	ds := &researchDataset{
		Enabled: []researchAssetData{
			{
				Item: repository.ResearchCollectionItem{
					ID: "i1", AssetKey: "A", Weight: 0.5, WeightLocked: true, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "A"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
			},
			{
				Item: repository.ResearchCollectionItem{
					ID: "i2", AssetKey: "B", Weight: 0, WeightLocked: false, Enabled: true,
				},
				Asset:  repository.MarketAsset{Name: "B"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
			},
		},
		FX: map[string]*researchFXData{},
	}
	r := evaluateOptimizationReadiness(ds, OptimizationConfig{
		WeightStep:        0.05,
		MaxCandidateCount: 20000,
	})
	if !r.Ready {
		t.Errorf("expected ready when candidate count within limit, blocking: %v", r.BlockingReasons)
	}
}

func TestScoreForObjective_CalmarFallback(t *testing.T) {
	summary := BacktestSummary{
		CAGR:        0.10,
		MaxDrawdown: -0.20,
		Calmar:      nil,
	}
	score, ok := ScoreForObjective(ObjectiveMaxCalmar, summary)
	if !ok {
		t.Fatal("expected fallback calmar to succeed")
	}
	expected := 0.10 / 0.20
	if math.Abs(score-expected) > 1e-9 {
		t.Errorf("fallback calmar: got %f, expected %f", score, expected)
	}
}

func TestApplyOptimizationAtomically(t *testing.T) {
	svc, db, detail := createOptimizationApplyFixture(t)
	weights := []OptimizationWeightEntry{
		{ItemID: detail.Items[0].ID, AssetKey: detail.Items[0].AssetKey, Weight: 0.7},
		{ItemID: detail.Items[1].ID, AssetKey: detail.Items[1].AssetKey, Weight: 0.3},
		{ItemID: detail.Items[2].ID, AssetKey: detail.Items[2].AssetKey, Weight: 0},
	}
	optimizationID := seedSucceededOptimization(t, db, detail, weights)
	updated, err := svc.ApplyOptimization(context.Background(), optimizationID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMinCVaR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("ApplyOptimization: %v", err)
	}
	if updated.StartPolicy != ResearchStartPolicyCustom || updated.WindowStart != "2021-01-01" || updated.WindowEnd != "2023-12-31" {
		t.Fatalf("optimization window not applied: %+v", updated.ResearchCollection)
	}
	if updated.TailRiskConfidence != 0.99 || updated.TailRiskHorizonDays != 1 {
		t.Fatalf("optimization tail-risk spec not applied: %+v", updated.ResearchCollection)
	}
	if updated.UpdatedAt <= detail.UpdatedAt {
		t.Fatalf("collection version did not advance: %d <= %d", updated.UpdatedAt, detail.UpdatedAt)
	}
	for i, item := range updated.Items {
		want := weights[i].Weight
		if math.Abs(item.Weight-want) > 1e-12 || item.Enabled != (want > 0) || item.WeightLocked != (want > 0) {
			t.Fatalf("item %d not atomically applied: %+v", i, item.ResearchCollectionItem)
		}
	}
}

func TestApplyLegacyOptimizationPreservesCurrentTailRiskSpec(t *testing.T) {
	svc, db, detail := createOptimizationApplyFixture(t)
	weights := []OptimizationWeightEntry{
		{ItemID: detail.Items[0].ID, AssetKey: detail.Items[0].AssetKey, Weight: 0.7},
		{ItemID: detail.Items[1].ID, AssetKey: detail.Items[1].AssetKey, Weight: 0.3},
		{ItemID: detail.Items[2].ID, AssetKey: detail.Items[2].AssetKey, Weight: 0},
	}
	optimizationID := seedSucceededOptimization(t, db, detail, weights)
	if _, err := db.Exec(`UPDATE research_optimization_runs SET engine_version='research_optimizer_v2' WHERE id=?`, optimizationID); err != nil {
		t.Fatal(err)
	}
	updated, err := svc.ApplyOptimization(context.Background(), optimizationID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMaxCAGR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TailRiskConfidence != detail.TailRiskConfidence || updated.TailRiskHorizonDays != detail.TailRiskHorizonDays {
		t.Fatalf("legacy apply overwrote current CVaR spec: before=%+v after=%+v", detail.ResearchCollection, updated.ResearchCollection)
	}
}

func TestApplyOptimizationRejectsResultMissingFrozenAsset(t *testing.T) {
	svc, db, detail := createOptimizationApplyFixture(t)
	optimizationID := seedSucceededOptimization(t, db, detail, []OptimizationWeightEntry{
		{ItemID: detail.Items[0].ID, AssetKey: detail.Items[0].AssetKey, Weight: 1},
	})
	_, err := svc.ApplyOptimization(context.Background(), optimizationID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMaxCAGR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != "research_optimization_result_stale" {
		t.Fatalf("expected stale result, got %v", err)
	}
	after, getErr := svc.GetCollection(context.Background(), detail.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	for i := range detail.Items {
		if after.Items[i].Weight != detail.Items[i].Weight ||
			after.Items[i].WeightLocked != detail.Items[i].WeightLocked {
			t.Fatalf("stale result changed item %d", i)
		}
	}
}

func TestApplyOptimizationCASConflictDoesNotWrite(t *testing.T) {
	svc, db, detail := createOptimizationApplyFixture(t)
	weights := []OptimizationWeightEntry{
		{ItemID: detail.Items[0].ID, AssetKey: detail.Items[0].AssetKey, Weight: 0.7},
		{ItemID: detail.Items[1].ID, AssetKey: detail.Items[1].AssetKey, Weight: 0.3},
		{ItemID: detail.Items[2].ID, AssetKey: detail.Items[2].AssetKey, Weight: 0},
	}
	optimizationID := seedSucceededOptimization(t, db, detail, weights)
	description := "concurrent edit"
	concurrent, err := svc.UpdateCollection(context.Background(), detail.ID, ResearchCollectionUpdate{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ApplyOptimization(context.Background(), optimizationID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMaxCAGR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != "research_collection_changed" {
		t.Fatalf("expected research_collection_changed, got %v", err)
	}
	after, err := svc.GetCollection(context.Background(), detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != concurrent.UpdatedAt || after.StartPolicy != concurrent.StartPolicy {
		t.Fatalf("CAS failure changed collection: before=%+v after=%+v", concurrent.ResearchCollection, after.ResearchCollection)
	}
	for i := range after.Items {
		if after.Items[i].Weight != concurrent.Items[i].Weight || after.Items[i].WeightLocked != concurrent.Items[i].WeightLocked {
			t.Fatalf("CAS failure changed item %d", i)
		}
	}
}

func TestApplyOptimizationRollsBackOnItemUpdateFailure(t *testing.T) {
	svc, db, detail := createOptimizationApplyFixture(t)
	weights := []OptimizationWeightEntry{
		{ItemID: detail.Items[0].ID, AssetKey: detail.Items[0].AssetKey, Weight: 0.7},
		{ItemID: detail.Items[1].ID, AssetKey: detail.Items[1].AssetKey, Weight: 0.3},
		{ItemID: detail.Items[2].ID, AssetKey: detail.Items[2].AssetKey, Weight: 0},
	}
	optimizationID := seedSucceededOptimization(t, db, detail, weights)
	if _, err := db.Exec(`CREATE TRIGGER fail_optimization_apply BEFORE UPDATE ON research_collection_items
		WHEN OLD.sort_order = 1 BEGIN SELECT RAISE(ABORT, 'injected apply failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyOptimization(context.Background(), optimizationID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMaxCAGR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	}); err == nil {
		t.Fatal("expected injected item update failure")
	}
	after, err := svc.GetCollection(context.Background(), detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != detail.UpdatedAt || after.WindowStart != detail.WindowStart || after.WindowEnd != detail.WindowEnd {
		t.Fatalf("failed transaction changed collection: before=%+v after=%+v", detail.ResearchCollection, after.ResearchCollection)
	}
	for i := range after.Items {
		if after.Items[i].Weight != detail.Items[i].Weight || after.Items[i].Enabled != detail.Items[i].Enabled || after.Items[i].WeightLocked != detail.Items[i].WeightLocked {
			t.Fatalf("failed transaction changed item %d: before=%+v after=%+v", i, detail.Items[i], after.Items[i])
		}
	}
}

func TestApplyOptimizationRollsBackWhenCollectionSpecWriteFails(t *testing.T) {
	svc, db, detail := createOptimizationApplyFixture(t)
	weights := []OptimizationWeightEntry{
		{ItemID: detail.Items[0].ID, AssetKey: detail.Items[0].AssetKey, Weight: 0.7},
		{ItemID: detail.Items[1].ID, AssetKey: detail.Items[1].AssetKey, Weight: 0.3},
		{ItemID: detail.Items[2].ID, AssetKey: detail.Items[2].AssetKey, Weight: 0},
	}
	optimizationID := seedSucceededOptimization(t, db, detail, weights)
	if _, err := db.Exec(`CREATE TRIGGER fail_optimization_collection_apply BEFORE UPDATE ON research_collections
		BEGIN SELECT RAISE(ABORT, 'injected collection failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyOptimization(context.Background(), optimizationID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMaxCAGR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	}); err == nil {
		t.Fatal("expected injected collection update failure")
	}
	after, err := svc.GetCollection(context.Background(), detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != detail.UpdatedAt || after.WindowStart != detail.WindowStart ||
		after.TailRiskConfidence != detail.TailRiskConfidence || after.TailRiskHorizonDays != detail.TailRiskHorizonDays {
		t.Fatalf("failed transaction changed collection: before=%+v after=%+v", detail.ResearchCollection, after.ResearchCollection)
	}
	for i := range after.Items {
		if after.Items[i].Weight != detail.Items[i].Weight || after.Items[i].Enabled != detail.Items[i].Enabled {
			t.Fatalf("failed transaction changed item %d", i)
		}
	}
}

func TestOptimizationSnapshotReloadPreservesItemIdentityAndLocks(t *testing.T) {
	svc, db := newResearchTestService(t)
	for i, key := range []string{"LOCK_A", "LOCK_B", "LOCK_C"} {
		insertResearchFixtureAsset(t, db, key, key, "CNY", "2020-01-01", 1643, growthValue(100+float64(i)))
	}
	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name: "锁定快照测试",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "LOCK_A", Weight: fptr(0.2), WeightLocked: true},
			{AssetKey: "LOCK_B", Weight: fptr(0.4)},
			{AssetKey: "LOCK_C", Weight: fptr(0.4)},
		},
	})
	collection, err := svc.research.GetCollection(context.Background(), detail.ID)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := svc.loadResearchDataset(context.Background(), collection)
	if err != nil {
		t.Fatal(err)
	}
	readiness := evaluateResearchReadiness(ds, researchTestNow)
	if !readiness.Ready {
		t.Fatalf("fixture not ready: %+v", readiness.BlockingReasons)
	}
	snapshot := buildOptimizationSnapshot(ds, readiness, OptimizationConfig{WeightStep: 0.05, TopK: 20, MaxCandidateCount: 20000})
	for _, asset := range snapshot.Assets {
		if asset.ItemID == "" {
			t.Fatalf("snapshot lost item id: %+v", asset)
		}
		if asset.AssetKey == "LOCK_A" && !asset.WeightLocked {
			t.Fatalf("snapshot lost lock flag: %+v", asset)
		}
	}
	reloaded, err := svc.loadDatasetFromSnapshot(context.Background(), researchInputSnapshot{
		EngineVersion: snapshot.EngineVersion, SourceHash: snapshot.SourceHash,
		CommonStart: snapshot.CommonStart, CommonEnd: snapshot.CommonEnd,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		Collection: snapshot.Collection, Assets: snapshot.Assets, FX: snapshot.FX,
		Benchmark: snapshot.Benchmark,
	})
	if err != nil {
		t.Fatal(err)
	}
	assets := buildOptimizationAssets(reloaded)
	candidates := GenerateCandidates(assets, 0.05)
	if len(candidates) == 0 {
		t.Fatal("reloaded snapshot produced no candidates")
	}
	for _, candidate := range candidates {
		for _, entry := range candidate.Weights {
			if entry.ItemID == "" || entry.AssetKey == "" {
				t.Fatalf("candidate lost identity: %+v", entry)
			}
			if entry.AssetKey == "LOCK_A" && (!entry.Locked || math.Abs(entry.Weight-0.2) > 1e-12) {
				t.Fatalf("locked weight did not survive reload: %+v", entry)
			}
		}
	}
}

func TestOptimizationWorkerAndAtomicApplyEndToEnd(t *testing.T) {
	svc, db := newResearchTestService(t)
	for i, key := range []string{"WORKER_A", "WORKER_B", "WORKER_C", "WORKER_BENCH"} {
		insertResearchFixtureAsset(t, db, key, key, "CNY", "2020-01-01", 1643,
			func(day int) float64 {
				return (100 + float64(i)*10) * math.Pow(1.0001+float64(i)*0.00005, float64(day))
			})
	}
	detail := mustCreateResearchCollection(t, svc, ResearchCollectionInput{
		Name:              "调优 worker 端到端测试",
		BenchmarkAssetKey: "WORKER_BENCH",
		Items: []ResearchCollectionItemInput{
			{AssetKey: "WORKER_A", Weight: fptr(0.2), WeightLocked: true},
			{AssetKey: "WORKER_B", Weight: fptr(0.4)},
			{AssetKey: "WORKER_C", Weight: fptr(0.4)},
		},
	})

	created, err := svc.CreateOptimization(context.Background(), detail.ID, ResearchOptimizationRequest{
		WeightStep: 0.05, MaxCandidateCount: 1, TopK: 20,
	})
	if err != nil {
		t.Fatalf("create optimization: %v", err)
	}
	if created.Optimization.CandidateCount <= 1 {
		t.Fatalf("expected creation above recommendation to succeed, candidate_count=%d",
			created.Optimization.CandidateCount)
	}
	if err := svc.ExecuteOptimizationJob(context.Background(), created.Optimization.JobID,
		func() bool { return false }, nil); err != nil {
		t.Fatalf("execute optimization worker: %v", err)
	}
	view, err := svc.GetOptimization(context.Background(), created.Optimization.ID)
	if err != nil {
		t.Fatalf("get optimization: %v", err)
	}
	if view.Status != repository.ResearchRunStatusSucceeded || view.EngineVersion != OptimizationEngineVersion {
		t.Fatalf("unexpected completed optimization: %+v", view)
	}
	var result OptimizationResult
	if err := json.Unmarshal(view.Result, &result); err != nil {
		t.Fatalf("decode optimization result: %v", err)
	}
	if result.CandidateCount == 0 || result.EvaluatedCount != result.CandidateCount ||
		view.CandidateCount != result.CandidateCount || view.EvaluatedCount != result.EvaluatedCount {
		t.Fatalf("candidate count mismatch: view=%+v result=%+v", view, result)
	}
	if len(result.BestByCAGR) == 0 || len(result.BestByCVaR) == 0 || result.CVaREligibleCount != result.EvaluatedCount {
		t.Fatalf("worker returned incomplete ranked results: %+v", result)
	}
	for _, ranked := range [][]OptimizationResultItem{
		result.BestByCAGR, result.BestByDrawdown, result.BestByCVaR, result.BestByCalmar,
	} {
		for _, item := range ranked {
			if item.Summary.Benchmark == nil {
				t.Fatalf("candidate backtest lost benchmark summary: %+v", item)
			}
			foundLocked := false
			for _, weight := range item.Weights {
				if weight.ItemID == "" || weight.AssetKey == "" {
					t.Fatalf("worker result lost item identity: %+v", weight)
				}
				if weight.AssetKey == "WORKER_A" {
					foundLocked = weight.Locked && math.Abs(weight.Weight-0.2) <= 1e-12
				}
			}
			if !foundLocked {
				t.Fatalf("worker result lost the frozen 20%% lock: %+v", item.Weights)
			}
		}
	}

	highMinimum := 2.0
	filtered, err := svc.CreateOptimization(context.Background(), detail.ID, ResearchOptimizationRequest{
		WeightStep: 0.05, MaxCandidateCount: 20000, TopK: 20,
		MinimumCAGR: &highMinimum,
	})
	if err != nil {
		t.Fatalf("create filtered optimization: %v", err)
	}
	if err := svc.ExecuteOptimizationJob(context.Background(), filtered.Optimization.JobID,
		func() bool { return false }, nil); err != nil {
		t.Fatalf("execute filtered optimization: %v", err)
	}
	filteredView, err := svc.GetOptimization(context.Background(), filtered.Optimization.ID)
	if err != nil {
		t.Fatal(err)
	}
	var filteredResult OptimizationResult
	if err := json.Unmarshal(filteredView.Result, &filteredResult); err != nil {
		t.Fatal(err)
	}
	if len(filteredResult.BestByCVaR) != 0 || filteredResult.CVaREligibleCount != 0 ||
		len(filteredResult.BestByCAGR) == 0 || len(filteredResult.Warnings) != 1 ||
		filteredResult.Warnings[0].Code != "cvar_minimum_cagr_unmet" {
		t.Fatalf("unexpected empty CVaR leaderboard result: %+v", filteredResult)
	}

	applied, err := svc.ApplyOptimization(context.Background(), view.ID, ResearchOptimizationApplyRequest{
		Objective: ObjectiveMaxCAGR, Rank: 1, ExpectedCollectionUpdatedAt: detail.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("apply ranked result: %v", err)
	}
	if applied.WindowStart != view.WindowStart || applied.WindowEnd != view.WindowEnd {
		t.Fatalf("apply did not restore frozen window: optimization=%s..%s collection=%s..%s",
			view.WindowStart, view.WindowEnd, applied.WindowStart, applied.WindowEnd)
	}
	if applied.TailRiskConfidence != DefaultTailRiskConfidence || applied.TailRiskHorizonDays != DefaultTailRiskHorizon {
		t.Fatalf("apply did not restore frozen CVaR spec: %+v", applied.ResearchCollection)
	}
	weightSum := 0.0
	for _, item := range applied.Items {
		if item.Enabled {
			weightSum += item.Weight
		}
	}
	if math.Abs(weightSum-1) > 1e-12 {
		t.Fatalf("applied weights must sum to 100%%, got %.16f", weightSum)
	}
	ordinary, err := svc.CreateBacktest(context.Background(), applied.ID)
	if err != nil {
		t.Fatalf("create reproducing backtest: %v", err)
	}
	if err := svc.ExecuteBacktestJob(context.Background(), ordinary.Run.JobID, nil, nil); err != nil {
		t.Fatalf("execute reproducing backtest: %v", err)
	}
	ordinaryDetail, err := svc.GetRun(context.Background(), ordinary.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var ordinarySummary BacktestSummary
	if err := json.Unmarshal(ordinaryDetail.Summary, &ordinarySummary); err != nil {
		t.Fatal(err)
	}
	selected := result.BestByCAGR[0]
	if ordinarySummary.TailRisk == nil || selected.Summary.TailRisk == nil ||
		math.Abs(ordinarySummary.TailRisk.CVaRLoss-selected.Summary.TailRisk.CVaRLoss) > 1e-12 ||
		math.Abs(ordinarySummary.TailRisk.VaRLoss-selected.Summary.TailRisk.VaRLoss) > 1e-12 {
		t.Fatalf("ordinary backtest did not reproduce applied optimization CVaR: ordinary=%+v selected=%+v",
			ordinarySummary.TailRisk, selected.Summary.TailRisk)
	}
}
