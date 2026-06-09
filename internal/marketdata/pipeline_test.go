package marketdata

import "testing"

func TestShouldFullReplaceOnRefresh(t *testing.T) {
	existing := []DataPoint{
		{TradeDate: "2024-01-02", SourceName: "ak.fund_etf_hist_sina"},
		{TradeDate: "2024-01-03", SourceName: "ak.fund_etf_hist_sina"},
	}

	if !ShouldFullReplaceOnRefresh(true, existing, "ak.stock_zh_a_hist_tx") {
		t.Fatal("force refresh should always full replace")
	}
	if ShouldFullReplaceOnRefresh(false, nil, "ak.stock_zh_a_hist_tx") {
		t.Fatal("empty existing should not full replace")
	}
	if !ShouldFullReplaceOnRefresh(false, existing, "ak.stock_zh_a_hist_tx") {
		t.Fatal("source change should full replace")
	}
	if ShouldFullReplaceOnRefresh(false, existing, "ak.fund_etf_hist_sina") {
		t.Fatal("same source should not full replace")
	}
	if !ShouldFullReplaceOnRefresh(false, existing, "") {
		t.Fatal("stored sina ETF history should full replace before fetch")
	}
}

func TestDominantSourceName(t *testing.T) {
	points := []DataPoint{
		{SourceName: "ak.fund_etf_hist_sina"},
		{SourceName: "ak.fund_etf_hist_sina"},
		{SourceName: "ak.stock_zh_a_hist_tx"},
	}
	if got := DominantSourceName(points); got != "ak.fund_etf_hist_sina" {
		t.Fatalf("dominant=%q", got)
	}
}
