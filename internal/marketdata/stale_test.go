package marketdata

import (
	"testing"
	"time"
)

func TestDataStaleWithinSevenDays(t *testing.T) {
	asOf := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	stale, warning := DataStale("2026-06-03", asOf)
	if stale || warning != "" {
		t.Fatalf("expected fresh data, got stale=%v warning=%q", stale, warning)
	}
}

func TestDataStaleAfterSevenDays(t *testing.T) {
	asOf := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	stale, warning := DataStale("2026-06-01", asOf)
	if !stale || warning != staleWarningMessage {
		t.Fatalf("expected stale warning, got stale=%v warning=%q", stale, warning)
	}
}
