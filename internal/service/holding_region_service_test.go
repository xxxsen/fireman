package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func setupHoldingRegionTest(t *testing.T) (*HoldingsService, *ConfigHashService, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	plans := repository.NewPlanRepo(db)
	params := repository.NewParametersRepo(db)
	allocRepo := repository.NewAllocationRepo(db)
	holdingsRepo := repository.NewHoldingsRepo(db)
	assetRepo := repository.NewMarketAssetRepo(db)
	hash := NewConfigHashService(
		plans, params, allocRepo, holdingsRepo, repository.NewReturnOverrideRepo(db),
		repository.NewAssumptionProfileRepo(db),
	)
	snapSvc := marketdata.NewSnapshotService(repository.NewSnapshotRepo(db), assetRepo)
	planSvc := NewPlanService(
		db, plans, params, allocRepo, repository.NewScenarioRepo(db), holdingsRepo,
		assetRepo, hash, snapSvc,
	)
	scenarioID := "scn_builtin_near_fire"
	plan, err := planSvc.Create(ctx, CreatePlanRequest{
		Name: "region-test", BaseCurrency: "CNY", ValuationDate: "2026-07-12",
		SelectedScenarioID: &scenarioID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := allocRepo.Replace(ctx, nil, plan.ID, repository.PlanAllocation{
		AssetClassTargets: []repository.AssetClassTarget{
			{AssetClass: "equity", Weight: 1}, {AssetClass: "bond", Weight: 0}, {AssetClass: "cash", Weight: 0},
		},
		RegionTargets: []repository.RegionTarget{
			{AssetClass: "equity", Region: "domestic", WeightWithinClass: 0.6},
			{AssetClass: "equity", Region: "foreign", WeightWithinClass: 0.4},
		},
	}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"asset_a", "asset_b", "asset_c", "asset_d"} {
		if _, err := db.Exec(`INSERT INTO market_assets
			(asset_key,market,instrument_type,symbol,name,last_seen_at,source_name,refreshed_at,created_at,updated_at)
			VALUES (?,?,?,?,?,0,'test',0,0,0)`, key, "CN", "test_fund", key, key); err != nil {
			t.Fatal(err)
		}
	}
	holdings := []repository.PlanHolding{
		{ID: "hold_a", AssetKey: "asset_a", Enabled: true, AssetClass: "equity", Region: "domestic", WeightWithinGroup: 1.0 / 3, CurrentAmountMinor: 100, SortOrder: 1},
		{ID: "hold_b", AssetKey: "asset_b", Enabled: true, AssetClass: "equity", Region: "domestic", WeightWithinGroup: 2.0 / 3, CurrentAmountMinor: 200, SortOrder: 2},
		{ID: "hold_c", AssetKey: "asset_c", Enabled: true, AssetClass: "equity", Region: "foreign", WeightWithinGroup: 0.5, CurrentAmountMinor: 300, SortOrder: 3},
		{ID: "hold_d", AssetKey: "asset_d", Enabled: true, AssetClass: "equity", Region: "foreign", WeightWithinGroup: 0.5, CurrentAmountMinor: 400, SortOrder: 4},
	}
	if err := holdingsRepo.Replace(ctx, nil, plan.ID, holdings); err != nil {
		t.Fatal(err)
	}
	return NewHoldingsService(db, plans, holdingsRepo, snapSvc, assetRepo), hash, plan.ID
}

func TestConfigHashTracksEffectiveGlobalIdentityButNotPinnedExplicitScenario(t *testing.T) {
	svc, hash, planID := setupHoldingRegionTest(t)
	ctx := context.Background()
	profiles := repository.NewAssumptionProfileRepo(svc.sql)
	baselineHash, err := hash.Compute(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.SetPreferences(ctx, repository.AssumptionPreferences{
		DefaultProfileID:      assumptions.SystemProfileID,
		DefaultProfileVersion: assumptions.SystemProfileVersion,
		DefaultScenario:       assumptions.ScenarioConservative,
	}); err != nil {
		t.Fatal(err)
	}
	conservativeHash, err := hash.Compute(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if conservativeHash == baselineHash {
		t.Fatal("follow-global scenario change did not change config hash")
	}
	paramsRepo := repository.NewParametersRepo(svc.sql)
	params, err := paramsRepo.Get(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	params.AssumptionSelectionMode = SelectionPinnedProfile
	params.ReturnAssumptionSetID = assumptions.SystemProfileID
	params.ReturnAssumptionSetVersion = assumptions.SystemProfileVersion
	params.ReturnAssumptionScenario = assumptions.ScenarioBaseline
	if err := paramsRepo.Upsert(ctx, nil, params); err != nil {
		t.Fatal(err)
	}
	pinnedHash, err := hash.Compute(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.SetPreferences(ctx, repository.AssumptionPreferences{
		DefaultProfileID:      assumptions.SystemProfileID,
		DefaultProfileVersion: assumptions.SystemProfileVersion,
		DefaultScenario:       assumptions.ScenarioOptimistic,
	}); err != nil {
		t.Fatal(err)
	}
	pinnedAfterGlobalChange, err := hash.Compute(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if pinnedAfterGlobalChange != pinnedHash {
		t.Fatal("pinned profile with explicit scenario changed after global preference update")
	}
}

func TestHoldingRegionChangePreservesAbsoluteWeightsAndChangesConfig(t *testing.T) {
	svc, hash, planID := setupHoldingRegionTest(t)
	ctx := context.Background()
	beforeHash, err := hash.Compute(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewRegionChange(ctx, planID, HoldingRegionChangeRequest{
		HoldingID: "hold_b", TargetRegion: "foreign",
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.PreviewHash == "" || preview.FromRegion != "domestic" || preview.TargetRegion != "foreign" {
		t.Fatalf("preview=%+v", preview)
	}
	for i := range preview.BeforeWeights {
		if math.Abs(preview.BeforeWeights[i].PortfolioTargetWeight-
			preview.AfterWeights[i].PortfolioTargetWeight) > holdingRegionWeightTolerance {
			t.Fatalf("weight changed: before=%+v after=%+v", preview.BeforeWeights[i], preview.AfterWeights[i])
		}
	}
	result, err := svc.ApplyRegionChange(ctx, planID, HoldingRegionChangeRequest{
		HoldingID: "hold_b", TargetRegion: "foreign", PreviewHash: preview.PreviewHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.ConfigVersion != preview.PlanConfigVersion+1 {
		t.Fatalf("config version=%d", result.Plan.ConfigVersion)
	}
	byID := map[string]repository.PlanHolding{}
	for _, holding := range result.Holdings {
		byID[holding.ID] = holding
	}
	if byID["hold_b"].Region != "foreign" || byID["hold_b"].CurrentAmountMinor != 200 || byID["hold_b"].SortOrder != 2 {
		t.Fatalf("moved holding fields changed: %+v", byID["hold_b"])
	}
	afterHash, err := hash.Compute(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	if afterHash == beforeHash {
		t.Fatal("region change did not change plan config hash")
	}
}

func TestHoldingRegionChangeRejectsStalePreview(t *testing.T) {
	svc, _, planID := setupHoldingRegionTest(t)
	ctx := context.Background()
	preview, err := svc.PreviewRegionChange(ctx, planID, HoldingRegionChangeRequest{
		HoldingID: "hold_b", TargetRegion: "foreign",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.sql.Exec(`UPDATE plans SET config_version=config_version+1 WHERE id=?`, planID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.ApplyRegionChange(ctx, planID, HoldingRegionChangeRequest{
		HoldingID: "hold_b", TargetRegion: "foreign", PreviewHash: preview.PreviewHash,
	})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != "holding_region_change_preview_stale" {
		t.Fatalf("err=%v", err)
	}
}
