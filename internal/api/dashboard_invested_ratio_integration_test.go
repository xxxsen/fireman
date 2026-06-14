package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func seedInvestedRatioDashboardPlan(t *testing.T, db *sql.DB) string {
	t.Helper()
	plan := createTestPlan(t, db)
	planID := plan.ID
	now := time.Now().UnixMilli()
	snapRepo := repository.NewSnapshotRepo(db)

	type holdingSpec struct {
		id, instID, assetClass string
		amount                 int64
	}
	specs := []holdingSpec{
		{id: "hold_eq_a", instID: "ins_eq_a", assetClass: "equity", amount: 200_000_00},
		{id: "hold_eq_b", instID: "ins_eq_b", assetClass: "equity", amount: 120_000_00},
		{id: "hold_cash", instID: "ins_cash", assetClass: "cash", amount: 80_000_00},
	}
	for i, spec := range specs {
		if err := snapRepo.EnsureInstrument(context.Background(), repository.Instrument{
			ID: spec.instID, Code: "IR" + string(rune('A'+i)), Name: "测试" + string(rune('A'+i)),
			Market: "CN", AssetClass: spec.assetClass, Region: "domestic", Currency: "CNY",
		}); err != nil {
			t.Fatal(err)
		}
		snapID := "snap_" + spec.instID
		if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
			ID: snapID, InstrumentID: spec.instID, PlanID: &planID,
			InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
			CompleteYearCount: 5, ObservationCount: 100,
			HistoricalCAGR: 0.08, ModeledAnnualReturn: 0.08, AnnualVolatility: 0.15, MaxDrawdown: 0.2,
			ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
			SourceMode: "akshare_historical", QualityStatus: "available",
			WarningsJSON: "[]", SourceHash: "fixture", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO plan_holdings (
				id, plan_id, instrument_id, enabled, asset_class, region,
				weight_within_group, current_amount_minor, simulation_snapshot_id,
				sort_order, created_at, updated_at
			) VALUES (?,?,?,1,?,?,1,?,?,?,?,?)`,
			spec.id, planID, spec.instID, spec.assetClass, "domestic",
			spec.amount, snapID, i*10, now, now); err != nil {
			t.Fatal(err)
		}
		seedInstrumentMarketData(t, db, spec.instID)
	}
	if _, err := db.ExecContext(context.Background(),
		`UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, 500_000_00, planID); err != nil {
		t.Fatal(err)
	}
	return planID
}

func TestDashboardInvestedRatioUsesPlanTotalAssets(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedInvestedRatioDashboardPlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/plans/" + planID + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status=%d", resp.StatusCode)
	}
	var out struct {
		Data struct {
			InvestedMinor int64   `json:"invested_minor"`
			InvestedRatio float64 `json:"invested_ratio"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Data.InvestedMinor != 320_000_00 {
		t.Fatalf("invested_minor=%d want 32000000", out.Data.InvestedMinor)
	}
	if out.Data.InvestedRatio != 0.64 {
		t.Fatalf("invested_ratio=%v want 0.64", out.Data.InvestedRatio)
	}
}
