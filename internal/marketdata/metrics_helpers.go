package marketdata

import "math"

func computeAnnualVolatility(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	logReturns := make([]float64, len(returns))
	var mean float64
	for i, r := range returns {
		logReturns[i] = math.Log(1 + r)
		mean += logReturns[i]
	}
	mean /= float64(len(returns))
	var varSum float64
	for _, g := range logReturns {
		d := g - mean
		varSum += d * d
	}
	return math.Sqrt(varSum / float64(len(returns)-1))
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
