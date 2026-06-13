package marketdata

import (
	"math"
)

// ComputeMetrics calculates snapshot metrics from selected complete years.
func ComputeMetrics(points []DataPoint, years []SimulationYear, pointType, sourceName string) SnapshotMetrics {
	m := SnapshotMetrics{QualityStatus: "insufficient_history"}
	if len(years) < 2 {
		if len(years) == 1 {
			m.Warnings = append(m.Warnings, "完整年度样本较少，估计不稳定")
		}
		return m
	}

	pointSet := BuildSnapshotPointSet(points, years, pointType, sourceName)
	ws, we := WindowBoundsFromPoints(pointSet)
	if ws != "" {
		m.WindowStart = &ws
	}
	if we != "" {
		m.WindowEnd = &we
	}
	startY := years[0].Year
	endY := years[len(years)-1].Year
	m.CompleteYearStart = &startY
	m.CompleteYearEnd = &endY
	m.CompleteYearCount = len(years)
	m.ObservationCount = len(pointSet)
	m.Years = years

	var logSum float64
	returns := make([]float64, len(years))
	for i, y := range years {
		returns[i] = y.AnnualReturn
		logSum += math.Log(1 + y.AnnualReturn)
	}
	m.HistoricalCAGR = math.Exp(logSum/float64(len(years))) - 1
	m.ModeledAnnualReturn = m.HistoricalCAGR
	m.AnnualVolatility = computeAnnualVolatility(returns)
	m.MaxDrawdown = maxDrawdownAcrossSegments(points, pointSet, years, pointType, sourceName)
	m.SourceHash = ComputeSourceHash(pointSet, pointType, sourceName)

	if len(years) <= 4 {
		m.Warnings = append(m.Warnings, "完整年度样本较少，估计不稳定")
	}
	if validateMetrics(m) {
		m.QualityStatus = "available"
	} else {
		m.QualityStatus = "insufficient_history"
	}
	return m
}

func blockMaxDrawdown(points []DataPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	peak := points[0].Value
	var maxDD float64
	for _, p := range points {
		if p.Value > peak {
			peak = p.Value
		}
		if peak <= 0 {
			continue
		}
		dd := 1 - p.Value/peak
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

func validateMetrics(m SnapshotMetrics) bool {
	if m.CompleteYearCount < 2 {
		return false
	}
	if m.HistoricalCAGR < -0.95 || m.HistoricalCAGR > 1.0 {
		return false
	}
	if m.AnnualVolatility < 0 || m.AnnualVolatility > 2.0 {
		return false
	}
	if m.MaxDrawdown < 0 || m.MaxDrawdown > 1.0 {
		return false
	}
	return true
}

// DetermineLibraryQuality returns instrument library status from annual data.
func DetermineLibraryQuality(points []DataPoint, annual []AnnualReturnRow, inclusionDate string,
	hasAnomaly bool,
) string {
	if hasAnomaly {
		return "provider_data_anomaly"
	}
	if len(points) == 0 {
		return "insufficient_history"
	}
	years := SelectSimulationYears(points, annual, inclusionDate)
	metrics := ComputeMetrics(points, years, "adjusted_close", "library")
	if metrics.QualityStatus == "available" {
		return "available"
	}
	return "insufficient_history"
}
