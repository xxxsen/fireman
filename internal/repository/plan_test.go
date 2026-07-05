package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

func TestPlanRepo_VersionConflict(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewPlanRepo(db)
	ctx := context.Background()

	p := Plan{ID: "plan_test", Name: "t", BaseCurrency: "CNY", ValuationDate: "2026-01-01", ConfigVersion: 1}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "updated"
	if err := repo.UpdateFieldsTx(ctx, tx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BumpVersionTx(ctx, tx, p.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigVersion != 2 {
		t.Fatalf("version=%d", got.ConfigVersion)
	}
	if got.Name != "updated" {
		t.Fatalf("name=%s", got.Name)
	}
	if _, err := repo.BumpVersion(ctx, p.ID, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestPlanDelete_Cascade(t *testing.T) {
	db := testutil.OpenTestDB(t)
	plans := NewPlanRepo(db)
	params := NewParametersRepo(db)
	ctx := context.Background()

	planID := "plan_cascade"
	if err := plans.Create(ctx, Plan{
		ID: planID, Name: "c", BaseCurrency: "CNY",
		ValuationDate: "2026-01-01",
	}); err != nil {
		t.Fatal(err)
	}
	p := defaultTestParams(planID)
	if err := params.Upsert(ctx, nil, p); err != nil {
		t.Fatal(err)
	}
	if err := plans.Delete(ctx, planID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_parameters WHERE plan_id=?`,
		planID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("expected cascade delete of parameters")
	}
}

func defaultTestParams(planID string) PlanParameters {
	return PlanParameters{
		PlanID: planID, CurrentAge: 30, RetirementAge: 55, EndAge: 90,
		TotalAssetsMinor: 1_000_000_00, AnnualSavingsMinor: 100_000_00,
		AnnualSpendingMinor: 400_000_00, InflationMode: "fixed_real",
		WithdrawalType: "fixed_real", RebalanceFrequency: "annual",
		RebalanceThreshold: 0.03, SimulationRuns: 10000, StudentTDf: 7,
	}
}
