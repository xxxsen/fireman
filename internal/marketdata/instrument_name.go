package marketdata

import "strings"

// PreferInstrumentName keeps the resolved display name when fetch metadata only returns the code.
func PreferInstrumentName(code, resolvedName, fetchName string) string {
	code = strings.TrimSpace(code)
	resolved := strings.TrimSpace(resolvedName)
	fetched := strings.TrimSpace(fetchName)
	if fetched != "" && fetched != code {
		return fetched
	}
	if resolved != "" && resolved != code {
		return resolved
	}
	if fetched != "" {
		return fetched
	}
	return resolved
}
