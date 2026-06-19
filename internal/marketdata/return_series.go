package marketdata

import (
	"sort"
	"time"
)

// ReturnSeriesPoint is one normalized observation in a return curve.
type ReturnSeriesPoint struct {
	Date             string  `json:"date"`
	Value            float64 `json:"value"`
	CumulativeReturn float64 `json:"cumulative_return"`
}

// ReturnSeries is a normalized cumulative-return curve over a fixed range.
type ReturnSeries struct {
	AsOfDate   string              `json:"as_of_date"`
	Range      string              `json:"range"`
	PointType  string              `json:"point_type"`
	SourceName string              `json:"source_name"`
	Status     string              `json:"status"`
	Points     []ReturnSeriesPoint `json:"points"`
}

const (
	ReturnSeriesStatusAvailable           = "available"
	ReturnSeriesStatusInsufficientHistory = "insufficient_history"
)

var returnSeriesRanges = map[string]bool{
	"1d": true, "1w": true, "1m": true, "3m": true, "6m": true,
	"1y": true, "3y": true, "5y": true, "all": true,
}

// IsValidReturnSeriesRange reports whether a range key is supported.
func IsValidReturnSeriesRange(rangeKey string) bool {
	return returnSeriesRanges[rangeKey]
}

// ComputeReturnSeries builds a cumulative-return curve normalized to the first
// valid point in the range (0%). For "1d" it falls back to the last two
// available trading days. Returns an insufficient_history status with empty
// points when the range has fewer than two valid observations.
func ComputeReturnSeries(points []DataPoint, asOfDate, rangeKey, pointType, sourceName string) ReturnSeries {
	out := ReturnSeries{
		AsOfDate:   asOfDate,
		Range:      rangeKey,
		PointType:  pointType,
		SourceName: sourceName,
		Status:     ReturnSeriesStatusInsufficientHistory,
		Points:     []ReturnSeriesPoint{},
	}
	if len(points) == 0 {
		return out
	}
	sorted := append([]DataPoint(nil), points...)
	sortPointsByDate(sorted)
	endIdx, endPoint, ok := lastPointOnOrBefore(sorted, asOfDate)
	if !ok {
		return out
	}
	out.AsOfDate = endPoint.TradeDate

	startIdx, ok := returnSeriesStartIndex(sorted, endIdx, endPoint.TradeDate, rangeKey)
	if !ok {
		return out
	}

	anchorIdx := -1
	for i := startIdx; i <= endIdx; i++ {
		if isValidPrice(sorted[i].Value) {
			anchorIdx = i
			break
		}
	}
	if anchorIdx < 0 || anchorIdx >= endIdx {
		return out
	}

	firstValue := sorted[anchorIdx].Value
	pts := make([]ReturnSeriesPoint, 0, endIdx-anchorIdx+1)
	for i := anchorIdx; i <= endIdx; i++ {
		if !isValidPrice(sorted[i].Value) {
			continue
		}
		pts = append(pts, ReturnSeriesPoint{
			Date:             sorted[i].TradeDate,
			Value:            sorted[i].Value,
			CumulativeReturn: sorted[i].Value/firstValue - 1,
		})
	}
	if len(pts) < 2 {
		return out
	}
	out.Status = ReturnSeriesStatusAvailable
	out.Points = pts
	return out
}

func returnSeriesStartIndex(points []DataPoint, endIdx int, endDate, rangeKey string) (int, bool) {
	if rangeKey == "1d" {
		if endIdx-1 < 0 {
			return 0, false
		}
		return endIdx - 1, true
	}
	if rangeKey == "all" {
		return 0, true
	}
	endT := parseDate(endDate)
	var target time.Time
	switch rangeKey {
	case "1w":
		target = endT.AddDate(0, 0, -7)
	case "1m":
		target = subtractNaturalMonths(endT, 1)
	case "3m":
		target = subtractNaturalMonths(endT, 3)
	case "6m":
		target = subtractNaturalMonths(endT, 6)
	case "1y":
		target = subtractNaturalYears(endT, 1)
	case "3y":
		target = subtractNaturalYears(endT, 3)
	case "5y":
		target = subtractNaturalYears(endT, 5)
	default:
		return 0, false
	}
	startIdx := firstPointOnOrAfter(points, formatDate(target))
	if startIdx < 0 || startIdx > endIdx {
		return 0, false
	}
	return startIdx, true
}

func firstPointOnOrAfter(points []DataPoint, date string) int {
	idx := sort.Search(len(points), func(i int) bool {
		return points[i].TradeDate >= date
	})
	if idx >= len(points) {
		return -1
	}
	return idx
}
