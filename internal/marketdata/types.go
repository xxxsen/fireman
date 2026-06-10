package marketdata

// FetchRequest is the market-provider fetch request body.
type FetchRequest struct {
	Market         string  `json:"market"`
	InstrumentType string  `json:"instrument_type"`
	SourceCode     string  `json:"source_code"`
	StartDate      *string `json:"start_date"`
	EndDate        string  `json:"end_date"`
	AdjustPolicy   string  `json:"adjust_policy"`
}

// HistoricalPoint is one cleaned daily observation.
type HistoricalPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// FetchData is the sidecar data payload.
type FetchData struct {
	Provider               string            `json:"provider"`
	ProviderSymbol         string            `json:"provider_symbol"`
	Name                   string            `json:"name"`
	AssetClass             string            `json:"asset_class"`
	Currency               string            `json:"currency"`
	PointType              string            `json:"point_type"`
	ExpenseRatioStatus     string            `json:"expense_ratio_status"`
	ExpenseRatioComponents map[string]any    `json:"expense_ratio_components"`
	Points                 []HistoricalPoint `json:"points"`
	SourceName             string            `json:"source_name"`
	SourceQuality          string            `json:"source_quality"`
}

// FetchResponse is the sidecar envelope.
type FetchResponse struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    FetchData `json:"data"`
}

// ResolveRequest is the market-provider resolve request body.
type ResolveRequest struct {
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	Code           string `json:"code"`
}

// ResolveCandidate is one resolved instrument option.
type ResolveCandidate struct {
	Code           string `json:"code"`
	ProviderSymbol string `json:"provider_symbol"`
	Name           string `json:"name"`
	Exchange       string `json:"exchange"`
	InstrumentKind string `json:"instrument_kind"`
}

// ResolveData is the resolve payload.
type ResolveData struct {
	Ambiguous  bool               `json:"ambiguous"`
	Resolved   *ResolveCandidate  `json:"resolved,omitempty"`
	Candidates []ResolveCandidate `json:"candidates,omitempty"`
}

// ResolveResponse is the sidecar resolve envelope.
type ResolveResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    ResolveData `json:"data"`
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

// SnapshotMetrics holds computed simulation parameters.
type SnapshotMetrics struct {
	WindowStart         *string
	WindowEnd           *string
	CompleteYearStart   *int
	CompleteYearEnd     *int
	CompleteYearCount   int
	ObservationCount    int
	HistoricalCAGR      float64
	ModeledAnnualReturn float64
	AnnualVolatility    float64
	MaxDrawdown         float64
	SourceHash          string
	QualityStatus       string
	Warnings            []string
	Years               []SimulationYear
}
