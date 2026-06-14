package service

import (
	"strings"

	"github.com/fireman/fireman/internal/marketdata"
)

func mapMarketProviderError(err error) *AppError {
	if err == nil {
		return nil
	}
	if marketdata.IsProviderTimeout(err) {
		return newErr("market_provider_timeout", "数据源响应超时，请重试", nil)
	}
	msg := err.Error()
	if strings.Contains(msg, "504") ||
		strings.Contains(msg, "upstream timeout") ||
		strings.Contains(msg, "context deadline exceeded") {
		return newErr("market_provider_timeout", "数据源响应超时，请重试", nil)
	}
	return newErr("market_provider_unavailable", msg, nil)
}
