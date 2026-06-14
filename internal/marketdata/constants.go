package marketdata

const (
	MetricsVersionMonthlyLogReturnV1 = "monthly_log_return_v1"
	VolatilityMethodMonthlyLogReturn  = "monthly_log_return_sample_stddev_annualized"
	VolatilityMethodNotApplicable     = "not_applicable"
	MetricsVersionSystemCashV1          = "system_cash_v1"

	HistoryDepthOneYear         = "one_year"
	HistoryDepthTwoToFourYears  = "two_to_four_years"
	HistoryDepthFivePlusYears   = "five_plus_years"
	HistoryDepthSystem          = "system"

	MetricStatusAvailable                    = "available"
	MetricStatusInsufficientCompleteYears    = "insufficient_complete_years"
	MetricStatusInsufficientMonthlyCoverage  = "insufficient_monthly_coverage"
	MetricStatusProviderDataAnomaly          = "provider_data_anomaly"
	MetricStatusInvalidMetricValue           = "invalid_metric_value"
	MetricStatusNotApplicable                = "not_applicable"

	QualityStatusAvailable            = "available"
	QualityStatusInsufficientHistory  = "insufficient_history"
	QualityStatusProviderDataAnomaly  = "provider_data_anomaly"

	TrailingStatusAvailable            = "available"
	TrailingStatusInsufficientHistory  = "insufficient_history"
	TrailingStatusStartPointTooStale   = "start_point_too_stale"
	TrailingStatusInvalidValue         = "invalid_value"
	TrailingStatusNotApplicable        = "not_applicable"
)
