package api

import (
	"context"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

func TestSimulationSnapshotSelectsHFQEvenWhenQFQStateIsLonger(t *testing.T) {
	_, db, _ := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seedMarketAssetWithHistory(t, db, seed)

	if _, err := db.Exec(`
		INSERT INTO market_asset_history_state (
			asset_key, adjust_policy, point_type, point_count, source_name, updated_at
		) VALUES (?, 'qfq', 'adjusted_close', 999999, 'qfq_counterexample', 1)`, seed.AssetKey); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO market_asset_points (
			asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at
		) VALUES (?, 'qfq', 'adjusted_close', '2008-07-03', 10.80, 'qfq_counterexample', 1),
		         (?, 'qfq', 'adjusted_close', '2008-07-04', 7.97, 'qfq_counterexample', 1)`,
		seed.AssetKey, seed.AssetKey); err != nil {
		t.Fatal(err)
	}

	svc := marketdata.NewSnapshotService(repository.NewSnapshotRepo(db), repository.NewMarketAssetRepo(db))
	snapshot, err := svc.BuildSnapshotForHolding(context.Background(), "plan_hfq", seed.AssetKey, "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AdjustPolicy != "hfq" {
		t.Fatalf("snapshot adjust policy = %q, want hfq", snapshot.AdjustPolicy)
	}
}

func TestSimulationSnapshotDoesNotFallBackToQFQ(t *testing.T) {
	_, db, _ := testRouterWithDB(t)
	seed := cnETFAssetSeed()
	seed.Points = nil
	seedMarketAssetWithHistory(t, db, seed)
	if _, err := db.Exec(`
		INSERT INTO market_asset_history_state (
			asset_key, adjust_policy, point_type, point_count, source_name, updated_at
		) VALUES (?, 'qfq', 'adjusted_close', 2, 'qfq_counterexample', 1)`, seed.AssetKey); err != nil {
		t.Fatal(err)
	}

	svc := marketdata.NewSnapshotService(repository.NewSnapshotRepo(db), repository.NewMarketAssetRepo(db))
	_, err := svc.BuildSnapshotForHolding(context.Background(), "plan_hfq", seed.AssetKey, "2026-01-01")
	var snapshotErr *marketdata.SnapshotError
	if !errors.As(err, &snapshotErr) || snapshotErr.Code != "return_history_missing" {
		t.Fatalf("snapshot error = %v, want return_history_missing", err)
	}
}
