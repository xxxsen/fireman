package service

import (
	"context"
	"fmt"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// PlanInstrumentEval is the unified plan availability result for an instrument.
type PlanInstrumentEval struct {
	Status        string `json:"status"`
	QualityStatus string `json:"quality_status"`
	Available     bool   `json:"available"`
}

// IsImportableCandidate reports whether instrument_kind may be imported as instrument_type.
func IsImportableCandidate(instrumentType, instrumentKind string) bool {
	switch instrumentType {
	case "cn_exchange_fund":
		switch instrumentKind {
		case "etf", "index_etf", "lof":
			return true
		default:
			return false
		}
	case "cn_exchange_stock":
		return instrumentKind == "stock"
	case "cn_mutual_fund":
		return instrumentKind == "mutual_fund"
	case "hk_stock":
		return instrumentKind == "stock"
	case "hk_etf":
		return instrumentKind == "etf"
	case "us_stock":
		return instrumentKind == "stock"
	case "us_etf":
		return instrumentKind == "etf"
	case "fx_rate":
		return instrumentKind == "fx_rate"
	default:
		return false
	}
}

// EvaluateInstrumentForPlan checks whether an instrument can be used in a plan at valuationDate.
func EvaluateInstrumentForPlan(
	ctx context.Context,
	inst repository.InstrumentRecord,
	marketRepo *repository.MarketDataRepo,
	valuationDate string,
) (PlanInstrumentEval, error) {
	if inst.ID == repository.SystemCashInstrumentID {
		return PlanInstrumentEval{
			Status: inst.Status, QualityStatus: "available", Available: inst.Status == "active",
		}, nil
	}
	if inst.IsSystem {
		return PlanInstrumentEval{}, newErr("instrument_not_ready", "system instrument cannot be used as a plan holding", map[string]any{
			"instrument_id": inst.ID,
		})
	}
	if inst.Status != "active" {
		return PlanInstrumentEval{
				Status: inst.Status, QualityStatus: "unavailable", Available: false,
			}, newErr("instrument_not_ready", fmt.Sprintf("instrument status is %s", inst.Status), map[string]any{
				"instrument_id": inst.ID, "status": inst.Status,
			})
	}
	quality := LibraryQualityAtDate(ctx, marketRepo, inst.ID, valuationDate)
	available := quality == "available"
	eval := PlanInstrumentEval{Status: inst.Status, QualityStatus: quality, Available: available}
	if !available {
		return eval, newErr("instrument_insufficient_history", "instrument does not have enough complete years for simulation", map[string]any{
			"instrument_id": inst.ID, "quality_status": quality, "valuation_date": valuationDate,
		})
	}
	return eval, nil
}

// LibraryQualityAtDate computes library quality truncated to valuationDate.
func LibraryQualityAtDate(ctx context.Context, marketRepo *repository.MarketDataRepo, instrumentID, valuationDate string) string {
	points, err := marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil || len(points) == 0 {
		return "insufficient_history"
	}
	dp := repoToDataPoints(points)
	filtered := make([]marketdata.DataPoint, 0, len(dp))
	for _, p := range dp {
		if p.TradeDate <= valuationDate {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return "insufficient_history"
	}
	annual := marketdata.ComputeAnnualReturns(filtered)
	if marketdata.DetectDailyAnomaly(filtered) {
		return "provider_data_anomaly"
	}
	return marketdata.DetermineLibraryQuality(filtered, annual, valuationDate, false)
}
