package api

import (
	"context"
	"database/sql"
	"testing"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
)

func seedEquityInstrument(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	snap := repository.NewSnapshotRepo(db)
	inst := repository.Instrument{
		ID: id, Code: "TEST001", Name: "测试权益基金",
		Market: "CN", AssetClass: "equity", Region: "domestic", Currency: "CNY",
	}
	if err := snap.EnsureInstrument(context.Background(), inst); err != nil {
		t.Fatalf("seed instrument: %v", err)
	}
}

func createTestPlan(t *testing.T, db *sql.DB) service.PlanDetail {
	t.Helper()
	svc := service.NewPlanService(
		db,
		repository.NewPlanRepo(db),
		repository.NewParametersRepo(db),
		repository.NewAllocationRepo(db),
		repository.NewScenarioRepo(db),
		repository.NewHoldingsRepo(db),
		service.NewConfigHashService(
			repository.NewPlanRepo(db),
			repository.NewParametersRepo(db),
			repository.NewAllocationRepo(db),
			repository.NewHoldingsRepo(db),
		),
		marketdata.NewSnapshotService(
			repository.NewSnapshotRepo(db),
			repository.NewInstrumentRepo(db),
			repository.NewMarketDataRepo(db),
		),
	)
	scn := "scn_builtin_near_fire"
	plan, err := svc.Create(context.Background(), service.CreatePlanRequest{
		Name: "测试计划", BaseCurrency: "CNY", ValuationDate: "2026-06-09",
		SelectedScenarioID: &scn,
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}
