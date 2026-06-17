package marketdata

// DetermineHistoryDepth maps complete year count to a history depth label.
func DetermineHistoryDepth(completeYearCount int) string {
	switch {
	case completeYearCount >= 5:
		return HistoryDepthFivePlusYears
	case completeYearCount >= 2:
		return HistoryDepthTwoToFourYears
	case completeYearCount == 1:
		return HistoryDepthOneYear
	default:
		return HistoryDepthInsufficient
	}
}

// EvaluateSimulationEligibility applies the unified simulation admission rule.
func EvaluateSimulationEligibility(m SnapshotMetrics, hasAnomaly bool) bool {
	if hasAnomaly {
		return false
	}
	if m.QualityStatus != QualityStatusAvailable {
		return false
	}
	if m.MetricsVersion != MetricsVersionMonthlyLogReturnV1 {
		return false
	}
	if m.VolatilityMethod != VolatilityMethodMonthlyLogReturn {
		return false
	}
	if m.CompleteYearCount < 1 {
		return false
	}
	if m.MonthlyReturnCount != m.CompleteYearCount*12 {
		return false
	}
	if m.CAGRStatus != MetricStatusAvailable {
		return false
	}
	if m.VolatilityStatus != MetricStatusAvailable {
		return false
	}
	if m.DrawdownStatus != MetricStatusAvailable {
		return false
	}
	return true
}

// FinalizeQualityStatus derives overall quality from per-metric statuses.
func FinalizeQualityStatus(cagrStatus, volStatus, ddStatus string, hasAnomaly bool) string {
	if hasAnomaly {
		return QualityStatusProviderDataAnomaly
	}
	if cagrStatus == MetricStatusAvailable &&
		volStatus == MetricStatusAvailable &&
		ddStatus == MetricStatusAvailable {
		return QualityStatusAvailable
	}
	return QualityStatusInsufficientHistory
}

// ShortHistoryWarning returns the standard one-year history warning.
func ShortHistoryWarning() string {
	return "仅有 1 个完整自然年度，收益与风险估计的不确定性较高"
}
