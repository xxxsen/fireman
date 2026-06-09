package marketdata

import (
	"sort"
	"time"
)

// IsCompleteYear checks whether a natural year has an opening anchor and sufficient observations.
func IsCompleteYear(points []DataPoint, year int, inclusionDate string) bool {
	incYear, err := yearFromDate(inclusionDate)
	if err != nil {
		return false
	}
	if year >= incYear {
		return false
	}
	anchor, ok := anchorBefore(points, year)
	if !ok {
		return false
	}
	_ = anchor

	var yearPoints []DataPoint
	for _, p := range points {
		if yearOf(p.TradeDate) == year {
			yearPoints = append(yearPoints, p)
		}
	}
	if len(yearPoints) < 126 {
		return false
	}
	if monthOf(yearPoints[0].TradeDate) > 2 {
		return false
	}
	if monthOf(yearPoints[len(yearPoints)-1].TradeDate) < 11 {
		return false
	}
	return true
}

func yearFromDate(date string) (int, error) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0, err
	}
	return t.Year(), nil
}

// SelectSimulationYears picks up to 20 complete years before inclusion date.
func SelectSimulationYears(points []DataPoint, annual []AnnualReturnRow, inclusionDate string) []SimulationYear {
	incYear, err := yearFromDate(inclusionDate)
	if err != nil {
		return nil
	}
	annualByYear := map[int]AnnualReturnRow{}
	for _, a := range annual {
		annualByYear[a.Year] = a
	}

	var eligible []int
	for y := incYear - 1; y >= 1900; y-- {
		if !IsCompleteYear(points, y, inclusionDate) {
			continue
		}
		row, ok := annualByYear[y]
		if !ok || row.IsPartial {
			continue
		}
		if row.AnnualReturn <= -1 {
			continue
		}
		eligible = append(eligible, y)
	}
	if len(eligible) > 20 {
		eligible = eligible[:20]
	}
	sort.Ints(eligible)

	out := make([]SimulationYear, 0, len(eligible))
	for _, y := range eligible {
		row := annualByYear[y]
		out = append(out, SimulationYear{
			Year: y, AnnualReturn: row.AnnualReturn,
			StartDate: row.StartDate, EndDate: row.EndDate,
			Observations: row.Observations,
		})
	}
	return out
}

// WindowBounds returns window_start and window_end for selected years.
func WindowBounds(points []DataPoint, years []SimulationYear) (start, end string) {
	if len(years) == 0 {
		return "", ""
	}
	first := years[0].Year
	anchor, ok := anchorBefore(points, first)
	if ok {
		start = anchor.TradeDate
	} else {
		start = years[0].StartDate
	}
	end = years[len(years)-1].EndDate
	return start, end
}

// PointsForSimulationWindow returns daily points used for drawdown within years.
func PointsForSimulationWindow(points []DataPoint, years []SimulationYear) []DataPoint {
	if len(years) == 0 {
		return nil
	}
	yearSet := map[int]struct{}{}
	for _, y := range years {
		yearSet[y.Year] = struct{}{}
	}
	firstYear := years[0].Year
	anchor, ok := anchorBefore(points, firstYear)
	var out []DataPoint
	if ok {
		out = append(out, anchor)
	}
	for _, p := range points {
		if _, ok := yearSet[yearOf(p.TradeDate)]; ok {
			out = append(out, p)
		}
	}
	return out
}

// ConsecutiveYearSegments splits simulation years at gaps.
func ConsecutiveYearSegments(years []SimulationYear) [][]SimulationYear {
	if len(years) == 0 {
		return nil
	}
	var segments [][]SimulationYear
	cur := []SimulationYear{years[0]}
	for i := 1; i < len(years); i++ {
		if years[i].Year-years[i-1].Year > 1 {
			segments = append(segments, cur)
			cur = []SimulationYear{years[i]}
			continue
		}
		cur = append(cur, years[i])
	}
	segments = append(segments, cur)
	return segments
}

// CountObservationsInWindow counts points between start and end inclusive.
func CountObservationsInWindow(points []DataPoint, start, end string) int {
	n := 0
	for _, p := range points {
		if p.TradeDate >= start && p.TradeDate <= end {
			n++
		}
	}
	return n
}
