package marketdata

import (
	"math"
)

// ComputeMetrics calculates snapshot metrics from selected complete years.
func ComputeMetrics(points []DataPoint, years []SimulationYear, pointType, sourceName string) SnapshotMetrics {
	m := SnapshotMetrics{
		QualityStatus:    QualityStatusInsufficientHistory,
		CAGRStatus:       MetricStatusInsufficientCompleteYears,
		VolatilityStatus: MetricStatusInsufficientCompleteYears,
		DrawdownStatus:   MetricStatusInsufficientCompleteYears,
		VolatilityMethod: VolatilityMethodMonthlyLogReturn,
		MetricsVersion:   MetricsVersionMonthlyLogReturnV1,
		HistoryDepth:     DetermineHistoryDepth(len(years)),
	}
	if len(years) == 0 {
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
	m.DailyObservationCount = len(pointSet)
	m.Years = years
	m.HistoryDepth = DetermineHistoryDepth(m.CompleteYearCount)

	monthly := BuildMonthlyReturns(points, years)
	m.MonthlyReturnCount = len(monthly)

	m.CAGRStatus, m.HistoricalCAGR, m.ModeledAnnualReturn = computeCAGRMetrics(years)
	m.VolatilityStatus, m.AnnualVolatility = computeVolatilityMetrics(monthly)
	m.DrawdownStatus, m.MaxDrawdown = computeDrawdownMetrics(points, pointSet, years, pointType, sourceName)

	m.SourceHash = ComputeMetricsSourceHash(pointSet, pointType, sourceName, years, m.MetricsVersion)
	m.QualityStatus = FinalizeQualityStatus(m.CAGRStatus, m.VolatilityStatus, m.DrawdownStatus, false)
	m.SimulationEligible = EvaluateSimulationEligibility(m, false)

	if m.CompleteYearCount == 1 && m.SimulationEligible {
		m.Warnings = append(m.Warnings, ShortHistoryWarning())
	}
	if m.CompleteYearCount >= 2 && m.CompleteYearCount <= 4 && m.SimulationEligible {
		m.Warnings = append(m.Warnings, "完整年度样本较少，估计不稳定")
	}
	return m
}

func computeCAGRMetrics(years []SimulationYear) (status string, cagr, modeled *float64) {
	if len(years) < 1 {
		return MetricStatusInsufficientCompleteYears, nil, nil
	}
	var logSum float64
	for _, y := range years {
		if y.AnnualReturn <= -1 {
			return MetricStatusInvalidMetricValue, nil, nil
		}
		logSum += math.Log(1 + y.AnnualReturn)
	}
	val := math.Exp(logSum/float64(len(years))) - 1
	if val < -0.95 || val > 1.0 || math.IsNaN(val) || math.IsInf(val, 0) {
		return MetricStatusInvalidMetricValue, nil, nil
	}
	return MetricStatusAvailable, &val, &val
}

func computeVolatilityMetrics(monthly []MonthlyReturn) (status string, annualVol *float64) {
	if len(monthly) < 12 {
		if len(monthly) == 0 {
			return MetricStatusInsufficientCompleteYears, nil
		}
		return MetricStatusInsufficientMonthlyCoverage, nil
	}
	vol := computeMonthlyAnnualVolatility(monthly)
	if vol == nil {
		return MetricStatusInvalidMetricValue, nil
	}
	if *vol < 0 || *vol > 2.0 {
		return MetricStatusInvalidMetricValue, nil
	}
	return MetricStatusAvailable, vol
}

func computeDrawdownMetrics(
	points, pointSet []DataPoint,
	years []SimulationYear,
	pointType, sourceName string,
) (status string, maxDD *float64) {
	if len(years) < 1 || len(pointSet) == 0 {
		return MetricStatusInsufficientCompleteYears, nil
	}
	dd := maxDrawdownAcrossSegments(points, pointSet, years, pointType, sourceName)
	if dd < 0 || dd > 1.0 || math.IsNaN(dd) || math.IsInf(dd, 0) {
		return MetricStatusInvalidMetricValue, nil
	}
	return MetricStatusAvailable, &dd
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

// DetermineLibraryQuality returns instrument library status from annual data.
func DetermineLibraryQuality(points []DataPoint, annual []AnnualReturnRow, inclusionDate string,
	hasAnomaly bool,
) string {
	if hasAnomaly {
		return QualityStatusProviderDataAnomaly
	}
	if len(points) == 0 {
		return QualityStatusInsufficientHistory
	}
	years := SelectSimulationYears(points, annual, inclusionDate)
	metrics := ComputeMetrics(points, years, "adjusted_close", "library")
	if metrics.SimulationEligible {
		return QualityStatusAvailable
	}
	if hasAnomaly {
		return QualityStatusProviderDataAnomaly
	}
	return QualityStatusInsufficientHistory
}
