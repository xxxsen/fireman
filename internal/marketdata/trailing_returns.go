package marketdata

import (
	"math"
	"sort"
	"time"
)

// TrailingReturnPeriod is one rolling return window.
type TrailingReturnPeriod struct {
	Status           string
	TargetStartDate  string
	StartDate        *string
	EndDate          string
	ActualDays       *int
	CumulativeReturn *float64
	AnnualizedReturn *float64
}

// TrailingReturns holds six fixed trailing return periods.
type TrailingReturns struct {
	AsOfDate   string
	PointType  string
	SourceName string
	Periods    map[string]TrailingReturnPeriod
}

var trailingPeriodOffsets = map[string]struct {
	months int
	years  int
}{
	"1m": {months: 1},
	"3m": {months: 3},
	"6m": {months: 6},
	"1y": {years: 1},
	"3y": {years: 3},
	"5y": {years: 5},
}

var trailingPeriodOrder = []string{"1m", "3m", "6m", "1y", "3y", "5y"}

// ComputeTrailingReturns calculates rolling cumulative returns from cleaned daily points.
func ComputeTrailingReturns(points []DataPoint, asOfDate, pointType, sourceName string) TrailingReturns {
	out := TrailingReturns{
		AsOfDate:   asOfDate,
		PointType:  pointType,
		SourceName: sourceName,
		Periods:    make(map[string]TrailingReturnPeriod, len(trailingPeriodOrder)),
	}
	if len(points) == 0 {
		for _, key := range trailingPeriodOrder {
			out.Periods[key] = unavailableTrailingPeriod(asOfDate, key)
		}
		return out
	}
	sorted := append([]DataPoint(nil), points...)
	sortPointsByDate(sorted)
	endIdx, endPoint, ok := lastPointOnOrBefore(sorted, asOfDate)
	if !ok {
		for _, key := range trailingPeriodOrder {
			out.Periods[key] = unavailableTrailingPeriod(asOfDate, key)
		}
		return out
	}
	endDate := endPoint.TradeDate
	out.AsOfDate = endDate
	for _, key := range trailingPeriodOrder {
		out.Periods[key] = computeTrailingPeriod(sorted, endIdx, endDate, key)
	}
	return out
}

func unavailableTrailingPeriod(asOfDate, key string) TrailingReturnPeriod {
	cfg := trailingPeriodOffsets[key]
	target := subtractTrailingOffset(parseDate(endOrAsOf(asOfDate)), cfg)
	return TrailingReturnPeriod{
		Status:          TrailingStatusInsufficientHistory,
		TargetStartDate: formatDate(target),
		EndDate:         asOfDate,
	}
}

func endOrAsOf(asOfDate string) string {
	return asOfDate
}

func computeTrailingPeriod(points []DataPoint, endIdx int, endDate, key string) TrailingReturnPeriod {
	cfg := trailingPeriodOffsets[key]
	endT := parseDate(endDate)
	target := subtractTrailingOffset(endT, cfg)
	targetStr := formatDate(target)
	period := TrailingReturnPeriod{
		TargetStartDate: targetStr,
		EndDate:         endDate,
	}
	startIdx, startPoint, ok := lastPointOnOrBefore(points, targetStr)
	if !ok {
		period.Status = TrailingStatusInsufficientHistory
		return period
	}
	startT := parseDate(startPoint.TradeDate)
	gapDays := daysBetween(startT, target)
	if gapDays > 10 {
		period.Status = TrailingStatusStartPointTooStale
		return period
	}
	if endIdx < startIdx {
		period.Status = TrailingStatusInsufficientHistory
		return period
	}
	if !isValidPrice(startPoint.Value) || !isValidPrice(points[endIdx].Value) {
		period.Status = TrailingStatusInvalidValue
		return period
	}
	cum := points[endIdx].Value/startPoint.Value - 1
	startDate := startPoint.TradeDate
	period.StartDate = &startDate
	actualDays := daysBetween(startT, endT)
	period.ActualDays = &actualDays
	period.CumulativeReturn = &cum
	period.Status = TrailingStatusAvailable
	if (key == "3y" || key == "5y") && actualDays > 0 {
		ann := math.Pow(points[endIdx].Value/startPoint.Value, 365.2425/float64(actualDays)) - 1
		period.AnnualizedReturn = &ann
	}
	return period
}

func subtractTrailingOffset(end time.Time, cfg struct {
	months int
	years  int
}) time.Time {
	if cfg.years > 0 {
		return subtractNaturalYears(end, cfg.years)
	}
	return subtractNaturalMonths(end, cfg.months)
}

func subtractNaturalMonths(t time.Time, months int) time.Time {
	year, month, day := t.Date()
	m := int(month) - months
	for m <= 0 {
		m += 12
		year--
	}
	newMonth := time.Month(m)
	last := lastDayOfMonth(year, newMonth)
	if day > last {
		day = last
	}
	return time.Date(year, newMonth, day, 0, 0, 0, 0, time.UTC)
}

func subtractNaturalYears(t time.Time, years int) time.Time {
	year, month, day := t.Date()
	year -= years
	last := lastDayOfMonth(year, month)
	if day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func lastDayOfMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func parseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func daysBetween(start, end time.Time) int {
	if start.IsZero() || end.IsZero() {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}

func isValidPrice(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func sortPointsByDate(points []DataPoint) {
	sort.Slice(points, func(i, j int) bool {
		return points[i].TradeDate < points[j].TradeDate
	})
}

func lastPointOnOrBefore(points []DataPoint, date string) (int, DataPoint, bool) {
	idx := sort.Search(len(points), func(i int) bool {
		return points[i].TradeDate > date
	}) - 1
	if idx < 0 {
		return -1, DataPoint{}, false
	}
	return idx, points[idx], true
}
