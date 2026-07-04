package simulation

import (
	"math"
	"testing"
)

// Under fixed inflation, every path's real wealth must equal
// nominal / (1+inflation)^(month/12), using the path's own realized inflation.
func TestRealWealthFixedInflationMatchesClosedForm(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.InflationMode = "fixed"
	in.Parameters.FixedInflationRate = 0.03

	ps, _ := RunPath(in, 7, PathRunOpts{CollectMonthlyWealth: true})
	if len(ps.MonthlyWealthMinor) == 0 {
		t.Fatal("expected monthly wealth series")
	}
	for m, nominal := range ps.MonthlyWealthMinor {
		want := math.Pow(1.03, float64(m+1)/12.0)
		if got := ps.MonthlyCumInflation[m]; math.Abs(got-want) > 1e-9 {
			t.Fatalf("month %d cumulative inflation = %v, want %v", m, got, want)
		}
		expectedReal := deflate(nominal, want)
		gotReal := realWealthSeries(ps.MonthlyWealthMinor, ps.MonthlyCumInflation)[m]
		if expectedReal != gotReal {
			t.Fatalf("month %d real wealth = %d, want %d", m, gotReal, expectedReal)
		}
	}
	finalFactor := math.Pow(1.03, float64(len(ps.MonthlyWealthMinor))/12.0)
	wantTerminalReal := deflate(ps.TerminalWealthMinor, finalFactor)
	if ps.RealTerminalWealthMinor != wantTerminalReal {
		t.Fatalf("real terminal = %d, want %d", ps.RealTerminalWealthMinor, wantTerminalReal)
	}
}

// Real terminal quantiles must be present and not exceed the nominal quantiles
// when inflation is positive.
func TestRealTerminalQuantilesBelowNominal(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.InflationMode = "fixed"
	in.Parameters.FixedInflationRate = 0.03
	res := Run(in, RunOptions{Runs: 200})
	if len(res.Summary.RealTerminalQuantiles) == 0 {
		t.Fatal("expected real terminal quantiles")
	}
	for _, k := range []string{"p25", "p50", "p75"} {
		if res.Summary.RealTerminalQuantiles[k] > res.Summary.TerminalQuantiles[k] {
			t.Fatalf("real %s (%d) must not exceed nominal (%d)",
				k, res.Summary.RealTerminalQuantiles[k], res.Summary.TerminalQuantiles[k])
		}
	}
	if len(res.RealQuantileSeries) != res.HorizonMonths {
		t.Fatalf("real monthly series length %d != horizon %d", len(res.RealQuantileSeries), res.HorizonMonths)
	}
}
