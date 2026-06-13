package marketdata

import (
	"errors"
	"strings"
)

// ErrSourceTypeConflict indicates fetched market data source does not match instrument type.
var ErrSourceTypeConflict = errors.New("market_data_source_type_conflict")

// ValidateFetchSourceCompatibility rejects cn_mutual_fund sources that conflict with asset class.
func ValidateFetchSourceCompatibility(instrumentType, assetClass string, data *FetchData) error {
	if instrumentType != "cn_mutual_fund" || data == nil {
		return nil
	}
	kind := strings.TrimSpace(data.SourceKind)
	if kind == "" {
		kind = inferCNMutualFundSourceKind(data.SourceName)
	}
	switch kind {
	case "money_fund":
		if assetClass != "cash" {
			return ErrSourceTypeConflict
		}
	case "financial_fund":
		if assetClass == "equity" {
			return ErrSourceTypeConflict
		}
	}
	if strings.Contains(data.SourceName, "fund_money_fund") && assetClass != "cash" {
		return ErrSourceTypeConflict
	}
	if strings.Contains(data.SourceName, "fund_financial_fund") && assetClass == "equity" {
		return ErrSourceTypeConflict
	}
	return nil
}

func inferCNMutualFundSourceKind(sourceName string) string {
	switch {
	case strings.Contains(sourceName, "fund_money_fund"):
		return "money_fund"
	case strings.Contains(sourceName, "fund_financial_fund"):
		return "financial_fund"
	case strings.Contains(sourceName, "fund_lof_hist"):
		return "lof"
	case strings.Contains(sourceName, "fund_open_fund"):
		return "open_fund"
	default:
		return ""
	}
}
