package service

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

// TestAttachTrailingReturnsBatchAndSkips covers td/056 §4.2: the asset list
// attaches 近1/3/5年年化收益 for active, non-system instruments via a single
// batched market-data query, and leaves system/inactive rows untouched (nil).
func TestAttachTrailingReturnsBatchAndSkips(t *testing.T) {
	db := testutil.OpenTestDB(t)
	instRepo := repository.NewInstrumentRepo(db)
	marketRepo := repository.NewMarketDataRepo(db)
	ctx := context.Background()

	if err := instRepo.Create(ctx, nil, repository.InstrumentRecord{
		ID: "ins_eq", Code: "EQ1", Name: "权益基金", Market: "CN", InstrumentType: "fund",
		AssetClass: "equity", Region: "domestic", Currency: "CNY",
		Provider: "akshare", ProviderSymbol: "EQ1", AdjustPolicy: "qfq",
		ExpenseRatioStatus: "unknown", FeeTreatment: "net", Status: "active", CreatedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	// Daily ~+0.02%/day across the last ~3.5 years so 1y/3y windows are available.
	end := time.Now()
	start := end.AddDate(-4, 0, 0)
	var points []repository.MarketDataPoint
	value := 100.0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		value *= 1.0002
		points = append(points, repository.MarketDataPoint{
			InstrumentID: "ins_eq", TradeDate: d.Format("2006-01-02"), Value: value,
			PointType: "adjusted_close", SourceName: "test", FetchedAt: 1,
		})
	}
	if err := marketRepo.UpsertBatch(ctx, nil, "ins_eq", points); err != nil {
		t.Fatal(err)
	}

	svc := &InstrumentService{marketRepo: marketRepo}

	items := []repository.InstrumentRecord{
		{ID: "ins_eq", Status: "active", IsSystem: false},
		{ID: "ins_inactive", Status: "delisted", IsSystem: false},
		{ID: repository.SystemCashInstrumentID, Status: "active", IsSystem: true},
	}
	svc.attachTrailingReturns(ctx, items)

	eq := items[0].TrailingReturns
	if eq == nil {
		t.Fatalf("active instrument should carry trailing returns")
	}
	if eq.OneYearAnnualizedReturn == nil || eq.ThreeYearAnnualizedReturn == nil {
		t.Fatalf("1y/3y should be available: %+v", eq)
	}
	if eq.FiveYearAnnualizedReturn != nil {
		t.Fatalf("5y should be nil with only ~4y history: %+v", eq.FiveYearAnnualizedReturn)
	}
	if items[1].TrailingReturns != nil {
		t.Fatalf("inactive instrument must stay nil")
	}
	if items[2].TrailingReturns != nil {
		t.Fatalf("system instrument must stay nil")
	}
}
