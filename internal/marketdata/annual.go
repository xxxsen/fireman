package marketdata

import (
	"sort"
	"strconv"
	"time"
)

func yearOf(date string) int {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return t.Year()
}

func monthOf(date string) int {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return int(t.Month())
}

// anchorBefore returns the last point strictly before year-01-01.
func anchorBefore(points []DataPoint, year int) (DataPoint, bool) {
	cutoff := strconv.Itoa(year) + "-01-01"
	var last *DataPoint
	for i := range points {
		if points[i].TradeDate < cutoff {
			last = &points[i]
			continue
		}
		break
	}
	if last == nil {
		return DataPoint{}, false
	}
	return *last, true
}

// ComputeAnnualReturns builds the full-history annual return table.
func ComputeAnnualReturns(points []DataPoint) []AnnualReturnRow {
	if len(points) == 0 {
		return nil
	}
	yearsSet := map[int]struct{}{}
	for _, p := range points {
		yearsSet[yearOf(p.TradeDate)] = struct{}{}
	}
	years := make([]int, 0, len(yearsSet))
	for y := range yearsSet {
		years = append(years, y)
	}
	sort.Ints(years)

	var out []AnnualReturnRow
	for _, y := range years {
		anchor, hasAnchor := anchorBefore(points, y)
		var yearPoints []DataPoint
		for _, p := range points {
			if yearOf(p.TradeDate) == y {
				yearPoints = append(yearPoints, p)
			}
		}
		if len(yearPoints) == 0 {
			continue
		}
		start := yearPoints[0]
		end := yearPoints[len(yearPoints)-1]
		isPartial := !hasAnchor
		startValue := start.Value
		startDate := start.TradeDate
		if hasAnchor {
			startValue = anchor.Value
			startDate = anchor.TradeDate
		}
		annualReturn := end.Value/startValue - 1
		out = append(out, AnnualReturnRow{
			Year: y, AnnualReturn: annualReturn,
			StartDate: startDate, EndDate: end.TradeDate,
			StartValue: startValue, EndValue: end.Value,
			Observations: len(yearPoints), IsPartial: isPartial,
		})
	}
	return out
}
