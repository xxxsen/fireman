package marketdata

import "time"

const staleWarningMessage = "数据可能过期"

// DataStale reports whether market data is older than seven calendar days.
func DataStale(lastTradeDate string, asOf time.Time) (bool, string) {
	if lastTradeDate == "" {
		return false, ""
	}
	last, err := time.Parse("2006-01-02", lastTradeDate)
	if err != nil {
		return false, ""
	}
	days := calendarDaysBetween(last, asOf)
	if days > 7 {
		return true, staleWarningMessage
	}
	return false, ""
}

func calendarDaysBetween(from, to time.Time) int {
	fromDay := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toDay := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(toDay.Sub(fromDay).Hours() / 24)
}
