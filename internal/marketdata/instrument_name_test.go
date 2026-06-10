package marketdata

import "testing"

func TestPreferInstrumentName(t *testing.T) {
	tests := []struct {
		code, resolved, fetched, want string
	}{
		{"007194", "长城短债A", "007194", "长城短债A"},
		{"007194", "长城短债A", "长城短债A", "长城短债A"},
		{"007194", "", "007194", "007194"},
		{"510300", "沪深300ETF", "华泰柏瑞沪深300ETF", "华泰柏瑞沪深300ETF"},
	}
	for _, tc := range tests {
		if got := PreferInstrumentName(tc.code, tc.resolved, tc.fetched); got != tc.want {
			t.Fatalf("PreferInstrumentName(%q,%q,%q)=%q want %q", tc.code, tc.resolved, tc.fetched, got, tc.want)
		}
	}
}

func TestUserClassification(t *testing.T) {
	cls, err := UserClassification("CN", "cn_mutual_fund", "bond", "foreign", "CNY")
	if err != nil {
		t.Fatal(err)
	}
	if cls.AssetClass != "bond" || cls.Region != "foreign" || cls.Currency != "CNY" {
		t.Fatalf("unexpected classification: %+v", cls)
	}
	if err := ValidateUserAssetClass("fx"); err == nil {
		t.Fatal("expected fx to be rejected")
	}
	if err := ValidateUserRegion("invalid"); err == nil {
		t.Fatal("expected invalid region to be rejected")
	}
}
