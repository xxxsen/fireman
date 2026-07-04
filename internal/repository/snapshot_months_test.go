package repository

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

// TestSnapshotMonthsRoundTripAndCascade verifies that the frozen monthly
// log-return series is persisted with the snapshot, read back in order, and
// removed by ON DELETE CASCADE when the snapshot is deleted.
func TestSnapshotMonthsRoundTripAndCascade(t *testing.T) {
	db := testutil.OpenTestDB(t)
	ctx := context.Background()
	repo := NewSnapshotRepo(db)

	if err := repo.EnsureInstrument(ctx, Instrument{
		ID: "ins_m", Code: "M001", Name: "月度", Market: "CN",
		AssetClass: "equity", Region: "domestic", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}

	planID := "plan_m"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO plans (id,name,base_currency,valuation_date,status,config_version,created_at,updated_at)
		VALUES (?,'p','CNY','2026-06-09','active',1,0,0)`, planID); err != nil {
		t.Fatal(err)
	}

	snapID := "snap_m"
	months := []SnapshotMonth{
		{Year: 2024, Month: 1, LogReturn: 0.01},
		{Year: 2024, Month: 2, LogReturn: -0.02},
		{Year: 2024, Month: 3, LogReturn: 0.03},
	}
	if err := repo.CreatePlanSnapshot(ctx, nil, SimulationSnapshot{
		ID: snapID, InstrumentID: "ins_m", PlanID: &planID,
		InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
		CompleteYearCount: 5, DailyObservationCount: 100, MonthlyReturnCount: 3,
		VolatilityMethod: "monthly_log_return_sample_stddev_annualized",
		MetricsVersion:   "monthly_log_return_v1", HistoryDepth: "five_plus_years",
		HistoricalCAGR: 0.08, ModeledAnnualReturn: 0.08, AnnualVolatility: 0.15, MaxDrawdown: 0.2,
		ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
		SourceMode: "akshare_historical", QualityStatus: "available",
		WarningsJSON: "[]", SourceHash: "h", Months: months,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ListSnapshotMonths(ctx, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 months, got %d", len(got))
	}
	if got[0].Year != 2024 || got[0].Month != 1 || got[2].Month != 3 {
		t.Fatalf("months out of order: %+v", got)
	}

	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM instrument_simulation_snapshots WHERE id=?`, snapID); err != nil {
		t.Fatal(err)
	}
	after, err := repo.ListSnapshotMonths(ctx, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 0 {
		t.Fatalf("cascade delete failed, %d months remain", len(after))
	}
}
