package marketdata

import "math"

func computeMonthlyAnnualVolatility(monthly []MonthlyReturn) *float64 {
	n := len(monthly)
	if n < 2 {
		if n == 1 && monthly[0].LogReturn == 0 {
			zero := 0.0
			return &zero
		}
		return nil
	}
	logReturns := make([]float64, n)
	var mean float64
	for i, m := range monthly {
		logReturns[i] = m.LogReturn
		mean += m.LogReturn
	}
	mean /= float64(n)
	var varSum float64
	for _, g := range logReturns {
		d := g - mean
		varSum += d * d
	}
	monthlyVol := math.Sqrt(varSum / float64(n-1))
	annual := monthlyVol * math.Sqrt(12)
	if math.IsNaN(annual) || math.IsInf(annual, 0) {
		return nil
	}
	return &annual
}

func segmentPointsWithAnchor(
	points, pointSet []DataPoint,
	seg []SimulationYear,
	pointType, sourceName string,
) []DataPoint {
	segYears := make(map[int]struct{}, len(seg))
	for _, y := range seg {
		segYears[y.Year] = struct{}{}
	}
	var segPoints []DataPoint
	for _, p := range pointSet {
		if _, ok := segYears[yearOf(p.TradeDate)]; ok {
			segPoints = append(segPoints, p)
		}
	}
	anchor, hasAnchor := anchorBefore(points, seg[0].Year)
	if !hasAnchor {
		return segPoints
	}
	for _, p := range segPoints {
		if p.TradeDate == anchor.TradeDate {
			return segPoints
		}
	}
	ap := anchor
	if ap.PointType == "" {
		ap.PointType = pointType
	}
	if ap.SourceName == "" {
		ap.SourceName = sourceName
	}
	return append([]DataPoint{ap}, segPoints...)
}

func maxDrawdownAcrossSegments(
	points, pointSet []DataPoint,
	years []SimulationYear,
	pointType, sourceName string,
) float64 {
	segments := ConsecutiveYearSegments(years)
	var maxDD float64
	for _, seg := range segments {
		segPoints := segmentPointsWithAnchor(points, pointSet, seg, pointType, sourceName)
		if dd := blockMaxDrawdown(segPoints); dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

// MetricFloat returns a metric pointer value or zero for persisted snapshots.
func MetricFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
