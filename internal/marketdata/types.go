package marketdata

// HistoricalPoint is one cleaned daily observation.
type HistoricalPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// FetchData is the normalized history payload produced by the sidecar worker
// (uploaded as a resource and consumed by Go post-process; formerly
// the synchronous /v1/instruments/fetch response body).
type FetchData struct {
	Provider               string            `json:"provider"`
	ProviderSymbol         string            `json:"provider_symbol"`
	Name                   string            `json:"name"`
	Currency               string            `json:"currency"`
	PointType              string            `json:"point_type"`
	ExpenseRatioStatus     string            `json:"expense_ratio_status"`
	ExpenseRatioComponents map[string]any    `json:"expense_ratio_components"`
	Points                 []HistoricalPoint `json:"points"`
	SourceName             string            `json:"source_name"`
	SourceQuality          string            `json:"source_quality"`
	SourceKind             string            `json:"source_kind,omitempty"`
}

// DataPoint is a persisted market observation.
type DataPoint struct {
	TradeDate  string
	Value      float64
	PointType  string
	SourceName string
	FetchedAt  int64
}

// AnnualReturnRow is one calendar-year return.
type AnnualReturnRow struct {
	Year         int
	AnnualReturn float64
	StartDate    string
	EndDate      string
	StartValue   float64
	EndValue     float64
	Observations int
	IsPartial    bool
}

// SimulationYear is a complete year selected for snapshot metrics.
type SimulationYear struct {
	Year         int
	AnnualReturn float64
	StartDate    string
	EndDate      string
	Observations int
}

// ExcludedYear describes why a calendar year is excluded from simulation metrics.
type ExcludedYear struct {
	Year   int    `json:"year"`
	Reason string `json:"reason"`
}

// SnapshotMetrics holds computed simulation parameters.
type SnapshotMetrics struct {
	WindowStart           *string
	WindowEnd             *string
	CompleteYearStart     *int
	CompleteYearEnd       *int
	CompleteYearCount     int
	DailyObservationCount int
	MonthlyReturnCount    int

	HistoricalCAGR      *float64
	ModeledAnnualReturn *float64
	AnnualVolatility    *float64
	MaxDrawdown         *float64

	CAGRStatus         string
	VolatilityStatus   string
	DrawdownStatus     string
	QualityStatus      string
	SimulationEligible bool
	HistoryDepth       string
	VolatilityMethod   string
	MetricsVersion     string

	SourceHash string
	Warnings   []string
	Years      []SimulationYear
	// MonthlyReturns is the complete-year monthly return series; frozen alongside
	// the snapshot so the joint factor model can estimate historical correlations.
	MonthlyReturns []MonthlyReturn
}
