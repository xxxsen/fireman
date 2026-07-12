package marketdata

import (
	"math"
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestChinaShenhuaBackwardAdjustedReturnRegression(t *testing.T) {
	rawReturn := 31.96/34.79 - 1
	forwardAdjustedReturn := 7.97/10.80 - 1
	backwardAdjustedReturn := 32.14/34.97 - 1

	if math.Abs(rawReturn-(-0.0813452141)) > 1e-9 {
		t.Fatalf("raw return = %.10f", rawReturn)
	}
	if math.Abs(forwardAdjustedReturn-(-0.2620370370)) > 1e-9 {
		t.Fatalf("qfq counterexample return = %.10f", forwardAdjustedReturn)
	}
	if math.Abs(backwardAdjustedReturn-(-0.0809265084)) > 1e-9 {
		t.Fatalf("hfq return = %.10f", backwardAdjustedReturn)
	}
	if math.Abs(backwardAdjustedReturn-rawReturn) > 0.001 {
		t.Fatalf("hfq return %.6f must remain close to raw market return %.6f", backwardAdjustedReturn, rawReturn)
	}
	if math.Abs(forwardAdjustedReturn-rawReturn) < 0.10 {
		t.Fatalf("fixture no longer demonstrates qfq denominator distortion")
	}
}

func TestChinaShenhuaBackwardAdjustedReturnIncludesDividend(t *testing.T) {
	const (
		previousRawClose = 48.13
		currentRawClose  = 45.68
		cashDividend     = 0.18
		currentHFQClose  = 45.86
	)
	wantHoldingReturn := (currentRawClose+cashDividend)/previousRawClose - 1
	gotHFQReturn := currentHFQClose/previousRawClose - 1
	if math.Abs(gotHFQReturn-wantHoldingReturn) > 1e-12 {
		t.Fatalf("hfq return %.10f != dividend-inclusive return %.10f", gotHFQReturn, wantHoldingReturn)
	}
}

func TestExchangeSnapshotDimensionIsAlwaysBackwardAdjusted(t *testing.T) {
	for _, instrumentType := range []string{
		"cn_exchange_stock", "cn_exchange_fund", "hk_stock", "hk_etf", "us_stock", "us_etf",
	} {
		adjustPolicy, pointType := defaultSnapshotDimension(repository.MarketAsset{InstrumentType: instrumentType})
		if adjustPolicy != "hfq" || pointType != "adjusted_close" {
			t.Fatalf("%s dimension = %s + %s", instrumentType, adjustPolicy, pointType)
		}
	}
}
