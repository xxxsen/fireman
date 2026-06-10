package marketdata

import (
	"strings"
)

// NormalizeHKCode normalizes HK exchange codes to zero-padded 5-digit symbols.
func NormalizeHKCode(code string) string {
	raw := strings.TrimSpace(strings.ToUpper(code))
	if strings.HasPrefix(raw, "HK") {
		raw = raw[2:]
	}
	var digits strings.Builder
	for _, ch := range raw {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}
	normalized := digits.String()
	if normalized == "" {
		return strings.TrimSpace(code)
	}
	for len(normalized) < 5 {
		normalized = "0" + normalized
	}
	return normalized
}
