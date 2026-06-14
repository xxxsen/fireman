package marketdata

import (
	"sort"
	"time"
)

// BuildExcludedYears returns years excluded from simulation with reasons.
func BuildExcludedYears(
	points []DataPoint,
	annual []AnnualReturnRow,
	selected []SimulationYear,
	inclusionDate string,
) []ExcludedYear {
	incYear, err := yearFromDate(inclusionDate)
	if err != nil {
		return nil
	}
	selectedSet := map[int]struct{}{}
	for _, y := range selected {
		selectedSet[y.Year] = struct{}{}
	}
	annualByYear := map[int]AnnualReturnRow{}
	for _, a := range annual {
		annualByYear[a.Year] = a
	}
	var years []int
	for y := range annualByYear {
		years = append(years, y)
	}
	sort.Ints(years)
	var out []ExcludedYear
	for _, y := range years {
		if _, ok := selectedSet[y]; ok {
			continue
		}
		reason := exclusionReason(points, annualByYear[y], y, incYear, inclusionDate)
		if reason == "" {
			continue
		}
		out = append(out, ExcludedYear{Year: y, Reason: reason})
	}
	return out
}

func exclusionReason(points []DataPoint, row AnnualReturnRow, year, incYear int, inclusionDate string) string {
	if year >= incYear {
		return "current_year"
	}
	if row.IsPartial {
		if _, ok := anchorBefore(points, year); !ok {
			return "missing_opening_anchor"
		}
		return "incomplete_year"
	}
	if !IsCompleteYear(points, year, inclusionDate) {
		if year == incYear-1 {
			// might still be incomplete for month coverage
		}
		if _, ok := anchorBefore(points, year); !ok {
			return "missing_opening_anchor"
		}
		if !hasFullMonthCoverage(points, year) {
			return "insufficient_monthly_coverage"
		}
		return "incomplete_year"
	}
	return "incomplete_year"
}

func hasFullMonthCoverage(points []DataPoint, year int) bool {
	var yearPoints []DataPoint
	for _, p := range points {
		if yearOf(p.TradeDate) == year {
			yearPoints = append(yearPoints, p)
		}
	}
	if len(yearPoints) < 126 {
		return false
	}
	for month := 1; month <= 12; month++ {
		if !hasMonthData(yearPoints, month) {
			return false
		}
	}
	return true
}

// FirstCompleteYear returns the earliest year in the inclusion universe.
func FirstCompleteYear(points []DataPoint, inclusionDate string) int {
	if len(points) == 0 {
		return 0
	}
	incYear, err := yearFromDate(inclusionDate)
	if err != nil {
		return yearOf(points[0].TradeDate)
	}
	for y := yearOf(points[0].TradeDate); y < incYear; y++ {
		if IsCompleteYear(points, y, inclusionDate) {
			return y
		}
	}
	return yearOf(points[0].TradeDate)
}

// IsEstablishmentYear reports whether year is the asset's first data year.
func IsEstablishmentYear(points []DataPoint, year int) bool {
	if len(points) == 0 {
		return false
	}
	first := yearOf(points[0].TradeDate)
	return year == first
}

// IsCurrentYear reports whether year is the calendar year of inclusionDate.
func IsCurrentYear(year int, inclusionDate string) bool {
	incYear, err := yearFromDate(inclusionDate)
	if err != nil {
		return false
	}
	return year == incYear
}

// ParseInclusionYear parses inclusion date year.
func ParseInclusionYear(inclusionDate string) int {
	t, err := time.Parse("2006-01-02", inclusionDate)
	if err != nil {
		return 0
	}
	return t.Year()
}
