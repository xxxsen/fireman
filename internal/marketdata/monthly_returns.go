package marketdata

import (
	"math"
	"strconv"
)

// MonthlyReturn is one monthly simple and log return in a complete year window.
type MonthlyReturn struct {
	Year         int
	Month        int
	StartDate    string
	EndDate      string
	SimpleReturn float64
	LogReturn    float64
}

// BuildMonthlyReturns computes monthly returns for consecutive complete-year segments.
func BuildMonthlyReturns(points []DataPoint, years []SimulationYear) []MonthlyReturn {
	if len(years) == 0 {
		return nil
	}
	segments := ConsecutiveYearSegments(years)
	var out []MonthlyReturn
	for _, seg := range segments {
		out = append(out, buildMonthlyReturnsForSegment(points, seg)...)
	}
	return out
}

func buildMonthlyReturnsForSegment(points []DataPoint, seg []SimulationYear) []MonthlyReturn {
	if len(seg) == 0 {
		return nil
	}
	anchor, hasAnchor := anchorBefore(points, seg[0].Year)
	if !hasAnchor || anchor.Value <= 0 {
		return nil
	}
	var out []MonthlyReturn
	prevValue := anchor.Value
	prevDate := anchor.TradeDate
	for _, y := range seg {
		for month := 1; month <= 12; month++ {
			monthEnd, ok := monthEndPoint(points, y.Year, month)
			if !ok || monthEnd.Value <= 0 {
				return nil
			}
			simple := monthEnd.Value/prevValue - 1
			if simple <= -1 {
				return nil
			}
			out = append(out, MonthlyReturn{
				Year: y.Year, Month: month,
				StartDate: prevDate, EndDate: monthEnd.TradeDate,
				SimpleReturn: simple, LogReturn: math.Log(1 + simple),
			})
			prevValue = monthEnd.Value
			prevDate = monthEnd.TradeDate
		}
	}
	return out
}

func monthEndPoint(points []DataPoint, year, month int) (DataPoint, bool) {
	var last *DataPoint
	for i := range points {
		if yearOf(points[i].TradeDate) != year {
			continue
		}
		if monthOf(points[i].TradeDate) != month {
			continue
		}
		last = &points[i]
	}
	if last == nil {
		return DataPoint{}, false
	}
	return *last, true
}

func formatYearMonth(year, month int) string {
	return strconv.Itoa(year) + "-" + padMonth(month) + "-"
}

func padMonth(month int) string {
	if month < 10 {
		return "0" + strconv.Itoa(month)
	}
	return strconv.Itoa(month)
}
