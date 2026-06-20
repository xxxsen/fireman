package marketdata

import (
	"encoding/json"
	"sort"
)

// ComputeLibraryMetrics derives the as-of-inclusionDate library snapshot metrics
// and quality status from cleaned daily points. inclusionDate is the date used by
// SelectSimulationYears to decide partial/complete years; callers truncate points
// to that date beforehand (the asset-library list passes the full history with
// inclusionDate set to the last trade date). The asset-library list, detail page
// and plan eligibility share this function so their quality/eligibility stay
// identical.
func ComputeLibraryMetrics(points []DataPoint, inclusionDate string) (SnapshotMetrics, string) {
	if len(points) == 0 {
		return SnapshotMetrics{}, QualityStatusInsufficientHistory
	}
	if DetectDailyAnomaly(points) {
		return SnapshotMetrics{QualityStatus: QualityStatusProviderDataAnomaly}, QualityStatusProviderDataAnomaly
	}
	annual := ComputeAnnualReturns(points)
	pointType, sourceName := points[0].PointType, points[0].SourceName
	if pointType == "" {
		pointType = "adjusted_close"
	}
	if sourceName == "" {
		sourceName = "library"
	}
	years := SelectSimulationYears(points, annual, inclusionDate)
	metrics := ComputeMetrics(points, years, pointType, sourceName)
	quality := metrics.QualityStatus
	if metrics.SimulationEligible {
		quality = QualityStatusAvailable
	} else if quality == QualityStatusAvailable {
		quality = QualityStatusInsufficientHistory
	}
	return metrics, quality
}

// LibraryProjection is the precomputed asset-library list view for one
// instrument, derived from its full cleaned history. Trailing windows end at
// DataAsOf (the instrument's own last trade date) so a 停更 instrument still
// reports its 3/5y returns, matching the detail page that loads full history.
type LibraryProjection struct {
	DataAsOf           string
	SourceName         string
	PointType          string
	QualityStatus      string
	SimulationEligible bool
	HistoryDepth       string
	CompleteYearCount  int
	MonthlyReturnCount int
	MetricsVersion     string
	Warnings           []string
	Trailing           ListTrailingReturns
}

// ComputeLibraryProjection builds the asset-library list projection from an
// instrument's full cleaned history. ok is false when there is no usable history
// (the caller then writes no projection row and the list renders "—").
func ComputeLibraryProjection(points []DataPoint) (LibraryProjection, bool) {
	if len(points) == 0 {
		return LibraryProjection{}, false
	}
	sorted := append([]DataPoint(nil), points...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TradeDate < sorted[j].TradeDate })
	last := sorted[len(sorted)-1]
	asOf := last.TradeDate
	metrics, quality := ComputeLibraryMetrics(sorted, asOf)
	trailing := ComputeListTrailingReturns(sorted, asOf, last.PointType, last.SourceName)
	return LibraryProjection{
		DataAsOf:           asOf,
		SourceName:         last.SourceName,
		PointType:          last.PointType,
		QualityStatus:      quality,
		SimulationEligible: metrics.SimulationEligible,
		HistoryDepth:       metrics.HistoryDepth,
		CompleteYearCount:  metrics.CompleteYearCount,
		MonthlyReturnCount: metrics.MonthlyReturnCount,
		MetricsVersion:     metrics.MetricsVersion,
		Warnings:           metrics.Warnings,
		Trailing:           trailing,
	}, true
}

// WarningsJSON serializes Warnings as a JSON array string, always returning a
// valid array ("[]" when empty) for storage in the projection table.
func (p LibraryProjection) WarningsJSON() string {
	if len(p.Warnings) == 0 {
		return "[]"
	}
	b, err := json.Marshal(p.Warnings)
	if err != nil {
		return "[]"
	}
	return string(b)
}
