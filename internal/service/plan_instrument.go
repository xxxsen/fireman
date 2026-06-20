package service

import (
	"context"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// PlanInstrumentEval is the unified plan availability result for an instrument.
type PlanInstrumentEval struct {
	Status             string `json:"status"`
	QualityStatus      string `json:"quality_status"`
	SimulationEligible bool   `json:"simulation_eligible"`
	Available          bool   `json:"available"`
	CompleteYearCount  int    `json:"complete_year_count,omitempty"`
	MonthlyReturnCount int    `json:"monthly_return_count,omitempty"`
	HistoryDepth       string `json:"history_depth,omitempty"`
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
			Status: inst.Status, QualityStatus: "available", SimulationEligible: true,
			Available: inst.Status == "active",
		}, nil
	}
	if inst.IsSystem {
		return PlanInstrumentEval{}, newErr("instrument_not_ready", "system instrument cannot be used as a plan holding",
			map[string]any{
				"instrument_id": inst.ID,
			})
	}
	if inst.Status != "active" {
		return PlanInstrumentEval{
				Status: inst.Status, QualityStatus: "unavailable", Available: false,
			}, newErr("instrument_not_ready", "instrument status is not active", map[string]any{
				"instrument_id": inst.ID, "status": inst.Status,
			})
	}
	metrics, quality := libraryMetricsAtDate(ctx, marketRepo, inst.ID, valuationDate)
	available := metrics.SimulationEligible
	eval := PlanInstrumentEval{
		Status: inst.Status, QualityStatus: quality, SimulationEligible: available,
		Available: available, CompleteYearCount: metrics.CompleteYearCount,
		MonthlyReturnCount: metrics.MonthlyReturnCount, HistoryDepth: metrics.HistoryDepth,
	}
	if !available {
		return eval, newErr(
			"instrument_insufficient_history",
			insufficientHistoryMessage(metrics),
			map[string]any{
				"instrument_id": inst.ID, "quality_status": quality, "valuation_date": valuationDate,
				"complete_year_count": metrics.CompleteYearCount, "monthly_return_count": metrics.MonthlyReturnCount,
				"cagr_status": metrics.CAGRStatus, "volatility_status": metrics.VolatilityStatus,
				"drawdown_status": metrics.DrawdownStatus,
			},
		)
	}
	return eval, nil
}

func insufficientHistoryMessage(metrics marketdata.SnapshotMetrics) string {
	if metrics.CompleteYearCount < 1 {
		return "没有完整自然年度，无法生成模拟指标"
	}
	if metrics.VolatilityStatus == marketdata.MetricStatusInsufficientMonthlyCoverage {
		return "完整年度月份覆盖不足，无法计算月度年化波动率"
	}
	return "instrument does not have enough complete years for simulation"
}

// LibraryQualityAtDate computes library quality truncated to valuationDate.
func LibraryQualityAtDate(ctx context.Context, marketRepo *repository.MarketDataRepo, instrumentID,
	valuationDate string,
) string {
	_, quality := libraryMetricsAtDate(ctx, marketRepo, instrumentID, valuationDate)
	return quality
}

func libraryMetricsAtDate(
	ctx context.Context,
	marketRepo *repository.MarketDataRepo,
	instrumentID, valuationDate string,
) (marketdata.SnapshotMetrics, string) {
	points, err := marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil || len(points) == 0 {
		return marketdata.SnapshotMetrics{}, marketdata.QualityStatusInsufficientHistory
	}
	dp := repoToDataPoints(points)
	inclusionDate := valuationDate
	var filtered []marketdata.DataPoint
	if inclusionDate == "" {
		// Library list/detail without plan valuation: use full history as-of latest point.
		inclusionDate = dp[len(dp)-1].TradeDate
		filtered = dp
	} else {
		filtered = make([]marketdata.DataPoint, 0, len(dp))
		for _, p := range dp {
			if p.TradeDate <= inclusionDate {
				filtered = append(filtered, p)
			}
		}
	}
	if len(filtered) == 0 {
		return marketdata.SnapshotMetrics{}, marketdata.QualityStatusInsufficientHistory
	}
	return marketdata.ComputeLibraryMetrics(filtered, inclusionDate)
}
