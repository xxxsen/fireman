package marketdata

import "testing"

func TestValidateFetchSourceCompatibility(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		asset   string
		data    *FetchData
		wantErr bool
	}{
		{
			name:  "money source allowed for cash",
			asset: "cash",
			data: &FetchData{
				SourceName: "ak.fund_money_fund_info_em",
				SourceKind: "money_fund",
			},
		},
		{
			name:  "money source rejected for equity",
			asset: "equity",
			data: &FetchData{
				SourceName: "ak.fund_money_fund_info_em",
				SourceKind: "money_fund",
			},
			wantErr: true,
		},
		{
			name:  "open source allowed for equity",
			asset: "equity",
			data: &FetchData{
				SourceName: "ak.fund_open_fund_info_em:单位净值走势",
				SourceKind: "open_fund",
			},
		},
		{
			name:  "financial source rejected for equity",
			asset: "equity",
			data: &FetchData{
				SourceName: "ak.fund_financial_fund_info_em",
				SourceKind: "financial_fund",
			},
			wantErr: true,
		},
		{
			name:  "infers money source from source_name",
			asset: "equity",
			data: &FetchData{
				SourceName: "ak.fund_money_fund_info_em",
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateFetchSourceCompatibility("cn_mutual_fund", tc.asset, tc.data)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
