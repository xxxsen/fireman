package service

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

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

// --- objective ranking ---

func TestTopKTracker_CAGR(t *testing.T) {
	tracker := NewTopKTracker(ObjectiveMaxCAGR, 3)
	for i := 0; i < 10; i++ {
		tracker.Push(OptimizationResultItem{
			Score: float64(i) * 0.01,
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
	tracker.Push(OptimizationResultItem{Score: -0.20})
	tracker.Push(OptimizationResultItem{Score: -0.05})
	tracker.Push(OptimizationResultItem{Score: -0.15})

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
	tracker.Push(OptimizationResultItem{Score: 0.5})
	tracker.Push(OptimizationResultItem{Score: 1.2})
	tracker.Push(OptimizationResultItem{Score: 0.8})

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
				Asset:  repository.MarketAsset{Name: "A", Currency: "USD"},
				Points: []repository.MarketAssetPoint{{TradeDate: "2020-01-01", Value: 1}},
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

func TestOptimizationReadiness_MaxCandidateCountBlocks(t *testing.T) {
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
	if r.Ready {
		t.Error("expected not ready when candidate count exceeds max_candidate_count")
	}
	found := false
	for _, b := range r.BlockingReasons {
		if b.Reason == "candidate_count_exceeds_limit" {
			found = true
		}
	}
	if !found {
		t.Error("expected candidate_count_exceeds_limit blocking reason")
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
