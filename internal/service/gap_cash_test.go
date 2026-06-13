package service

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestUpdateParameters_ApplyUnallocatedGapToCash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plans := repository.NewPlanRepo(db)
	params := repository.NewParametersRepo(db)
	alloc := repository.NewAllocationRepo(db)
	scenario := repository.NewScenarioRepo(db)
	holdings := repository.NewHoldingsRepo(db)
	hash := NewConfigHashService(plans, params, alloc, holdings)
	snapSvc := marketdata.NewSnapshotService(
		repository.NewSnapshotRepo(db),
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
	)
	svc := NewPlanService(db, plans, params, alloc, scenario, holdings, repository.NewInstrumentRepo(db), hash, snapSvc,
		repository.NewMarketDataRepo(db))

	scn := "scn_builtin_near_fire"
	plan, err := svc.Create(context.Background(), CreatePlanRequest{
		Name: "gap-test", BaseCurrency: "CNY", ValuationDate: "2026-06-09",
		SelectedScenarioID: &scn,
	})
	if err != nil {
		t.Fatal(err)
	}

	p, flows, err := svc.GetParameters(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.TotalAssetsMinor = 2_000_000_00

	_, _, err = svc.UpdateParameters(context.Background(), plan.ID, ParametersUpdateRequest{
		ConfigVersion:          plan.ConfigVersion,
		Parameters:             p,
		CashFlows:              flows,
		ApplyUnallocatedToCash: true,
	})
	if err != nil {
		t.Fatalf("update with gap to cash: %v", err)
	}

	holds, err := holdings.ListByPlan(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var cashMinor int64
	for _, h := range holds {
		if h.InstrumentID == repository.SystemCashInstrumentID && h.Enabled {
			cashMinor = h.CurrentAmountMinor
		}
	}
	if cashMinor < 1_000_000_00-100 {
		t.Fatalf("expected system cash ~1M, got %d", cashMinor)
	}
}
