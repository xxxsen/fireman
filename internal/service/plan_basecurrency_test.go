package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func newPlanServiceForTest(t *testing.T) *PlanService {
	t.Helper()
	db := testutil.OpenTestDB(t)
	plans := repository.NewPlanRepo(db)
	params := repository.NewParametersRepo(db)
	alloc := repository.NewAllocationRepo(db)
	scenario := repository.NewScenarioRepo(db)
	holdings := repository.NewHoldingsRepo(db)
	hash := NewConfigHashService(plans, params, alloc, holdings, repository.NewReturnOverrideRepo(db))
	snapSvc := marketdata.NewSnapshotService(
		repository.NewSnapshotRepo(db),
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
	)
	return NewPlanService(db, plans, params, alloc, scenario, holdings,
		repository.NewInstrumentRepo(db), hash, snapSvc, repository.NewMarketDataRepo(db))
}

func assertAppErrorCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", wantCode)
	}
	var ae *AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if ae.Code != wantCode {
		t.Fatalf("expected code %q, got %q (%s)", wantCode, ae.Code, ae.Message)
	}
}

// td/065 R9: the published system profile is CNY-only, so a non-CNY base currency
// must be rejected at every plan write entry point (create, wizard, metadata
// update) rather than being saved and then failing at simulation time.
func TestCreatePlanRejectsNonCNYBaseCurrency(t *testing.T) {
	svc := newPlanServiceForTest(t)
	scn := "scn_builtin_near_fire"
	_, err := svc.Create(context.Background(), CreatePlanRequest{
		Name: "usd-plan", BaseCurrency: "USD", ValuationDate: "2026-06-09",
		SelectedScenarioID: &scn,
	})
	assertAppErrorCode(t, err, "validation_failed")
}

func TestCreatePlanWizardRejectsNonCNYBaseCurrency(t *testing.T) {
	svc := newPlanServiceForTest(t)
	_, err := svc.CreateWizard(context.Background(), PlanWizardRequest{
		Name: "usd-wizard", BaseCurrency: "USD", ValuationDate: "2026-06-09",
		SelectedScenarioID: "scn_builtin_near_fire",
	})
	assertAppErrorCode(t, err, "validation_failed")
}

func TestUpdatePlanRejectsNonCNYBaseCurrency(t *testing.T) {
	svc := newPlanServiceForTest(t)
	ctx := context.Background()
	scn := "scn_builtin_near_fire"
	plan, err := svc.Create(ctx, CreatePlanRequest{
		Name: "cny-plan", BaseCurrency: "CNY", ValuationDate: "2026-06-09",
		SelectedScenarioID: &scn,
	})
	if err != nil {
		t.Fatalf("create CNY plan: %v", err)
	}

	if _, err := svc.Update(ctx, plan.ID, UpdatePlanRequest{
		ConfigVersion: plan.ConfigVersion, BaseCurrency: "USD",
	}); err == nil {
		t.Fatal("expected non-CNY update to be rejected")
	} else {
		assertAppErrorCode(t, err, "validation_failed")
	}

	// The plan's currency must be unchanged after the rejected update.
	got, err := svc.Get(ctx, plan.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if got.BaseCurrency != "CNY" {
		t.Fatalf("base currency changed after rejected update: %q", got.BaseCurrency)
	}

	// A no-op / CNY update still works.
	if _, err := svc.Update(ctx, plan.ID, UpdatePlanRequest{
		ConfigVersion: got.ConfigVersion, BaseCurrency: "CNY",
	}); err != nil {
		t.Fatalf("CNY update must still succeed: %v", err)
	}
}
