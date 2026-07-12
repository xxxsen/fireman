package service

import (
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestValidateHistoryDimension(t *testing.T) {
	exchange := repository.MarketAsset{InstrumentType: "cn_exchange_fund"}
	mutual := repository.MarketAsset{InstrumentType: "cn_mutual_fund", InstrumentKind: "open_fund"}
	cash := repository.MarketAsset{InstrumentType: "cash"}
	fx := repository.MarketAsset{InstrumentType: "fx_rate"}
	tests := []struct {
		name   string
		asset  repository.MarketAsset
		adjust string
		point  string
		ok     bool
	}{
		{name: "raw close", asset: exchange, adjust: "none", point: "close", ok: true},
		{name: "forward adjusted", asset: exchange, adjust: "qfq", point: "adjusted_close", ok: true},
		{name: "backward adjusted", asset: exchange, adjust: "hfq", point: "adjusted_close", ok: true},
		{name: "raw mislabeled adjusted", asset: exchange, adjust: "none", point: "adjusted_close"},
		{name: "adjusted mislabeled raw", asset: exchange, adjust: "qfq", point: "close"},
		{name: "mutual total return", asset: mutual, adjust: "none", point: "total_return_index", ok: true},
		{name: "mutual cannot adjust", asset: mutual, adjust: "qfq", point: "total_return_index"},
		{name: "system cash", asset: cash, adjust: "none", point: "adjusted_close", ok: true},
		{name: "cash cannot adjust", asset: cash, adjust: "qfq", point: "adjusted_close"},
		{name: "FX rate", asset: fx, adjust: "none", point: "fx_rate", ok: true},
		{name: "FX wrong point", asset: fx, adjust: "none", point: "adjusted_close"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHistoryDimension(tt.asset, tt.adjust, tt.point)
			if (err == nil) != tt.ok {
				t.Fatalf("err=%v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestDefaultHistoryDimension(t *testing.T) {
	if got := DefaultAdjustPolicy("cn_exchange_fund"); got != "qfq" {
		t.Fatalf("exchange default=%s", got)
	}
	if got := DefaultAdjustPolicy("cn_mutual_fund"); got != "none" {
		t.Fatalf("mutual fund default=%s", got)
	}
}
