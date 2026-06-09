package marketdata

import (
	"math"
	"sort"
)

// CleanPoints normalizes ordering, duplicates, and invalid market-data values.
func CleanPoints(points []HistoricalPoint) []DataPoint {
	type row struct {
		date  string
		value float64
	}
	var rows []row
	for _, p := range points {
		if p.Date == "" || p.Value <= 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		rows = append(rows, row{date: p.Date, value: p.Value})
	}
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].date < rows[j].date })

	dedup := make(map[string]float64)
	var dates []string
	for _, r := range rows {
		if _, ok := dedup[r.date]; !ok {
			dates = append(dates, r.date)
		}
		dedup[r.date] = r.value
	}
	sort.Strings(dates)

	out := make([]DataPoint, 0, len(dates))
	for _, d := range dates {
		out = append(out, DataPoint{TradeDate: d, Value: dedup[d]})
	}
	return out
}

// DetectDailyAnomaly returns true if any consecutive daily return exceeds 95%.
func DetectDailyAnomaly(points []DataPoint) bool {
	for i := 1; i < len(points); i++ {
		prev := points[i-1].Value
		if prev <= 0 {
			continue
		}
		ret := points[i].Value/prev - 1
		if math.Abs(ret) > 0.95 {
			return true
		}
	}
	return false
}
