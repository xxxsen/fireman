package marketdata

import "strings"

// HasCNExchangePrefix reports whether code already includes sh/sz/bj prefix.
func HasCNExchangePrefix(code string) bool {
	raw := strings.ToLower(strings.TrimSpace(code))
	return strings.HasPrefix(raw, "sh") || strings.HasPrefix(raw, "sz") || strings.HasPrefix(raw, "bj")
}

// NormalizeCNExchangeCode lowercases prefixed CN exchange codes.
func NormalizeCNExchangeCode(code string) string {
	raw := strings.ToLower(strings.TrimSpace(code))
	if HasCNExchangePrefix(raw) {
		return raw
	}
	return raw
}

// RequiresCNResolve reports whether a CN on-exchange code still needs resolve/disambiguation.
func RequiresCNResolve(market, instrumentType, code string) bool {
	if !strings.EqualFold(market, "CN") {
		return false
	}
	switch instrumentType {
	case "cn_exchange_fund", "cn_exchange_stock":
		return !HasCNExchangePrefix(code)
	default:
		return false
	}
}
