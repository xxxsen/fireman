package marketdata

import "testing"

// The sidecar may return source_name "tickflow.klines:1d" for resolved
// exchange-traded instruments. The Go pipeline treats source_name as an opaque
// string; these tests pin that the TickFlow value flows through processing,
// hashing and refresh-replacement decisions without special-casing.

const tickflowSourceName = "tickflow.klines:1d"

func tickflowFetchData() *FetchData {
	return &FetchData{
		Provider:       "akshare",
		ProviderSymbol: "sh510300",
		Name:           "沪深300ETF",
		Currency:       "CNY",
		PointType:      "adjusted_close",
		Points: []HistoricalPoint{
			{Date: "2024-01-02", Value: 3.501},
			{Date: "2024-01-03", Value: 3.512},
			{Date: "2024-01-04", Value: 3.498},
		},
		SourceName:    tickflowSourceName,
		SourceQuality: "full",
	}
}

func TestProcessProviderDataWithTickflowSource(t *testing.T) {
	result := ProcessProviderData(tickflowFetchData(), "2024-01-04")

	if len(result.Points) != 3 {
		t.Fatalf("points=%d, want 3", len(result.Points))
	}
	if result.SourceName != tickflowSourceName {
		t.Fatalf("source name=%q, want %q", result.SourceName, tickflowSourceName)
	}
	for _, p := range result.Points {
		if p.SourceName != tickflowSourceName {
			t.Fatalf("point %s source name=%q, want %q", p.TradeDate, p.SourceName, tickflowSourceName)
		}
		if p.PointType != "adjusted_close" {
			t.Fatalf("point %s point type=%q", p.TradeDate, p.PointType)
		}
	}
	if result.HasAnomaly {
		t.Fatal("small daily moves must not flag an anomaly")
	}
}

func TestComputeSourceHashWithTickflowSource(t *testing.T) {
	result := ProcessProviderData(tickflowFetchData(), "2024-01-04")

	hash := ComputeSourceHash(result.Points, result.PointType, result.SourceName)
	if len(hash) != 64 {
		t.Fatalf("hash length=%d, want 64 hex chars", len(hash))
	}
	if again := ComputeSourceHash(result.Points, result.PointType, result.SourceName); again != hash {
		t.Fatal("hash must be deterministic for identical inputs")
	}
	akshare := ComputeSourceHash(result.Points, result.PointType, "ak.fund_etf_hist_em")
	if akshare == hash {
		t.Fatal("hash must reflect the source name")
	}
}

func TestRefreshReplacementAcrossTickflowAndAkshareSources(t *testing.T) {
	stored := []DataPoint{
		{TradeDate: "2024-01-02", SourceName: "ak.fund_etf_hist_em"},
		{TradeDate: "2024-01-03", SourceName: "ak.fund_etf_hist_em"},
	}
	if !ShouldFullReplaceOnRefresh(false, stored, tickflowSourceName) {
		t.Fatal("switching akshare history to tickflow must full replace")
	}

	tickflowStored := []DataPoint{
		{TradeDate: "2024-01-02", SourceName: tickflowSourceName},
		{TradeDate: "2024-01-03", SourceName: tickflowSourceName},
	}
	if ShouldFullReplaceOnRefresh(false, tickflowStored, tickflowSourceName) {
		t.Fatal("same tickflow source must not full replace")
	}
	if !ShouldFullReplaceOnRefresh(false, tickflowStored, "ak.fund_etf_hist_em") {
		t.Fatal("switching tickflow history back to akshare must full replace")
	}
}
