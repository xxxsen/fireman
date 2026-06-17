package service

import (
	"context"
	"errors"

	"github.com/fireman/fireman/internal/marketdata"
)

// mapMarketProviderError converts a typed sidecar/client error into an AppError
// using structured error_code predicates only (never message substrings).
func mapMarketProviderError(err error) *AppError {
	if err == nil {
		return nil
	}
	switch {
	case marketdata.IsProviderTimeout(err) ||
		errors.Is(err, context.DeadlineExceeded):
		return newErr("market_provider_timeout", "数据源响应超时，请重试", nil)
	case marketdata.IsInstrumentNotFound(err):
		return newErr("instrument_not_found", "instrument not found", nil)
	case marketdata.IsInstrumentTypeMismatch(err):
		return newErr("instrument_type_mismatch",
			"code belongs to a different instrument type; try cn_mutual_fund for off-exchange funds",
			map[string]any{"suggested_instrument_type": "cn_mutual_fund"})
	case marketdata.IsSourceDataConflict(err):
		return newErr("market_data_source_type_conflict",
			"fetched data identity does not match the resolved instrument; existing data kept", nil)
	case marketdata.IsProviderInvalidRequest(err):
		return newErr("invalid_request", err.Error(), nil)
	default:
		return newErr("market_provider_unavailable", err.Error(), nil)
	}
}
