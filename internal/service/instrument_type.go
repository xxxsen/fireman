package service

// This file is the single source of truth for instrument-type presentation:
// the Chinese label and same-code search-result ordering priority. The API
// exposes both on asset search results (instrument_type_label /
// instrument_type_priority) so the web UI never re-implements the mapping.

// instrumentTypePriority orders same-code search results: mutual funds first,
// then exchange funds and stocks, then everything else. The ordering only
// helps users distinguish choices and never changes a saved holding.
func instrumentTypePriority(t string) int {
	switch t {
	case "cn_mutual_fund":
		return 0
	case "cn_exchange_fund":
		return 1
	case "cn_exchange_stock":
		return 2
	default:
		return 3
	}
}

// instrumentTypeLabelZH renders a user-facing label for a directory
// instrument type inside backend messages and API responses. Labels match the
// web UI's fallback instrumentTypeLabel so advice and pickers use the same
// wording.
func instrumentTypeLabelZH(t string) string {
	switch t {
	case "cn_mutual_fund":
		return "公募基金"
	case "cn_exchange_fund":
		return "场内 ETF / LOF"
	case "cn_exchange_stock":
		return "A 股"
	case "hk_stock":
		return "港股"
	case "hk_etf":
		return "香港 ETF"
	case "us_stock":
		return "美国股票"
	case "us_etf":
		return "美国 ETF"
	default:
		return t
	}
}
