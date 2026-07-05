package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

func buildServices(db *sql.DB) Services {
	return NewServices(db, "", nil, nil)
}

func testRouterWithDB(t *testing.T) (*httptest.Server, *sql.DB, *http.Client) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client()
}

// seedEquityAsset inserts a minimal active equity market asset (no history)
// so holdings can reference it; snapshots stay lazy until history arrives.
func seedEquityAsset(t *testing.T, db *sql.DB, assetKey string) {
	t.Helper()
	snap := repository.NewSnapshotRepo(db)
	if err := snap.EnsureMarketAsset(context.Background(), repository.MarketAsset{
		AssetKey: assetKey, Symbol: "TEST001", Name: "测试权益基金",
		Market: "CN", Currency: "CNY",
	}); err != nil {
		t.Fatalf("seed market asset: %v", err)
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
		repository.NewMarketAssetRepo(db),
		service.NewConfigHashService(
			repository.NewPlanRepo(db),
			repository.NewParametersRepo(db),
			repository.NewAllocationRepo(db),
			repository.NewHoldingsRepo(db),
			repository.NewReturnOverrideRepo(db),
		),
		marketdata.NewSnapshotService(
			repository.NewSnapshotRepo(db),
			repository.NewMarketAssetRepo(db),
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

// marketAssetSeed describes one directory entry plus its synced history.
type marketAssetSeed struct {
	AssetKey       string
	Market         string
	InstrumentType string
	RegionCode     string
	Symbol         string
	Name           string
	InstrumentKind string
	Currency       string
	PointType      string
	Points         []marketdata.HistoricalPoint
}

func cnETFAssetSeed() marketAssetSeed {
	return marketAssetSeed{
		AssetKey: "cn:cn_exchange_fund:sh:510300", Market: "CN",
		InstrumentType: "cn_exchange_fund", RegionCode: "sh", Symbol: "510300",
		Name: "沪深300ETF", InstrumentKind: "etf", Currency: "CNY",
		PointType: "adjusted_close", Points: buildFixturePoints(),
	}
}

func hkStockAssetSeed() marketAssetSeed {
	return marketAssetSeed{
		AssetKey: "hk:hk_stock:00700", Market: "HK",
		InstrumentType: "hk_stock", Symbol: "00700",
		Name: "腾讯控股", InstrumentKind: "stock", Currency: "HKD",
		PointType: "adjusted_close", Points: buildTwentyYearFixturePoints(),
	}
}

// seedMarketAssetWithHistory is idempotent so a test can seed the same asset
// twice without failing unique constraints.
func seedMarketAssetWithHistory(t *testing.T, db *sql.DB, seed marketAssetSeed) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO market_assets (
			asset_key, market, instrument_type, region_code, symbol, name, exchange,
			instrument_kind, currency, active, listing_status, last_seen_at,
			source_name, source_as_of, refreshed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, 1, 'active', ?, 'test_directory', '', ?, ?, ?)`,
		seed.AssetKey, seed.Market, seed.InstrumentType, seed.RegionCode, seed.Symbol,
		seed.Name, seed.InstrumentKind, seed.Currency, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if len(seed.Points) == 0 {
		return
	}
	for _, p := range seed.Points {
		if _, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO market_asset_points (
				asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at
			) VALUES (?, 'none', ?, ?, ?, 'test_fixture', ?)`,
			seed.AssetKey, seed.PointType, p.Date, p.Value, now); err != nil {
			t.Fatal(err)
		}
	}
	last := seed.Points[len(seed.Points)-1]
	if _, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO market_asset_history_state (
			asset_key, adjust_policy, point_type, last_task_id, last_success_task_id,
			last_success_at, data_as_of, point_count, source_name, updated_at
		) VALUES (?, 'none', ?, 'task_seed', 'task_seed', ?, ?, ?, 'test_fixture', ?)`,
		seed.AssetKey, seed.PointType, now, last.Date, len(seed.Points), now); err != nil {
		t.Fatal(err)
	}
}

func buildTwentyYearFixturePoints() []marketdata.HistoricalPoint {
	var out []marketdata.HistoricalPoint
	value := 100.0
	for year := 2005; year <= 2024; year++ {
		out = append(out, marketdata.HistoricalPoint{
			Date: fmt.Sprintf("%d-12-31", year-1), Value: value,
		})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				value *= 1.0004
				out = append(out, marketdata.HistoricalPoint{
					Date: formatFixtureDate(year, month, day), Value: value,
				})
			}
		}
	}
	return out
}

func buildFixturePoints() []marketdata.HistoricalPoint {
	var out []marketdata.HistoricalPoint
	value := 100.0
	for year := 2018; year <= 2024; year++ {
		out = append(out, marketdata.HistoricalPoint{
			Date: fmt.Sprintf("%d-12-31", year-1), Value: value,
		})
		for month := 1; month <= 12; month++ {
			for day := 1; day <= 11; day++ {
				value *= 1.0004
				out = append(out, marketdata.HistoricalPoint{
					Date: formatFixtureDate(year, month, day), Value: value,
				})
			}
		}
	}
	return out
}

func formatFixtureDate(y, m, d int) string {
	return sprintf2(y) + "-" + sprintf2(m) + "-" + sprintf2(d)
}

func sprintf2(n int) string {
	return fmt.Sprintf("%02d", n)
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	return mustRead(t, resp)
}
