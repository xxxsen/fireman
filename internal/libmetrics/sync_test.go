package libmetrics

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func seedActiveInstrument(t *testing.T, repo *repository.InstrumentRepo, id string) {
	t.Helper()
	if err := repo.Create(context.Background(), nil, repository.InstrumentRecord{
		ID: id, Code: id, Name: id, Market: "CN", InstrumentType: "fund",
		AssetClass: "equity", Region: "domestic", Currency: "CNY",
		Provider: "akshare", ProviderSymbol: id, AdjustPolicy: "qfq",
		ExpenseRatioStatus: "unknown", FeeTreatment: "net", Status: "active", CreatedAt: 1000,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func dailyPoints(start, end string) []marketdata.DataPoint {
	startT, _ := time.Parse("2006-01-02", start)
	endT, _ := time.Parse("2006-01-02", end)
	var pts []marketdata.DataPoint
	value := 100.0
	for d := startT; !d.After(endT); d = d.AddDate(0, 0, 1) {
		pts = append(pts, marketdata.DataPoint{
			TradeDate: d.Format("2006-01-02"), Value: value,
			PointType: "adjusted_close", SourceName: "lib_src", FetchedAt: 1,
		})
		value *= 1.0003
	}
	return pts
}

func countProjection(t *testing.T, db *sql.DB, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM instrument_library_metrics WHERE instrument_id=?`, id).Scan(&n); err != nil {
		t.Fatalf("count projection: %v", err)
	}
	return n
}

// TestSyncTxUpsertsThenDeletes is the td/058 P1 guard: SyncTx upserts a
// projection from non-empty history, and on empty history deletes the stale row
// rather than leaving it behind.
func TestSyncTxUpsertsThenDeletes(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := repository.NewInstrumentRepo(db)
	libRepo := repository.NewInstrumentLibraryMetricsRepo(db)
	ctx := context.Background()

	seedActiveInstrument(t, instRepo, "ins_sync")

	// Non-empty history -> projection is written.
	if err := SyncTx(ctx, libRepo, nil, "ins_sync", dailyPoints("2020-01-01", "2024-01-01")); err != nil {
		t.Fatalf("sync non-empty: %v", err)
	}
	if got := countProjection(t, db, "ins_sync"); got != 1 {
		t.Fatalf("expected projection row after non-empty sync, got %d", got)
	}

	// Empty history (e.g. a full replace cleared every point) -> stale row deleted.
	if err := SyncTx(ctx, libRepo, nil, "ins_sync", nil); err != nil {
		t.Fatalf("sync empty: %v", err)
	}
	if got := countProjection(t, db, "ins_sync"); got != 0 {
		t.Fatalf("expected projection deleted after empty sync, got %d", got)
	}

	// Deleting an already-absent projection is a no-op, not an error.
	if err := SyncTx(ctx, libRepo, nil, "ins_sync", nil); err != nil {
		t.Fatalf("sync empty (idempotent): %v", err)
	}
}
