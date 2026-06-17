package service

import "testing"

func TestShouldUpgradeInstrumentName(t *testing.T) {
	cases := []struct {
		name    string
		current string
		fetched string
		code    string
		want    bool
	}{
		{"placeholder to real upgrades", "sh510300", "沪深300ETF", "sh510300", true},
		{"bare placeholder to real upgrades", "510300", "沪深300ETF", "sh510300", true},
		{"zero-stripped placeholder upgrades", "270042", "广发理财年年红", "270042", true},
		{"empty current upgrades", "", "广发理财", "270042", true},
		{"real to same real no update", "沪深300ETF", "沪深300ETF", "sh510300", false},
		{"real to different real updates", "旧名", "新名", "sh510300", true},
		{"real stays when fetch is placeholder", "沪深300ETF", "sh510300", "sh510300", false},
		{"real stays when fetch is bare code", "沪深300ETF", "510300", "sh510300", false},
		{"empty fetch never updates", "沪深300ETF", "  ", "sh510300", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldUpgradeInstrumentName(tc.current, tc.fetched, tc.code); got != tc.want {
				t.Fatalf("shouldUpgradeInstrumentName(%q,%q,%q)=%v want %v",
					tc.current, tc.fetched, tc.code, got, tc.want)
			}
		})
	}
}
