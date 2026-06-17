package marketdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/repository"
)

var errSystemInstrumentHoldings = errors.New("system instrument cannot be added to plan holdings")

// SnapshotService creates plan-specific simulation snapshots.
type SnapshotService struct {
	snapRepo   *repository.SnapshotRepo
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
}

func NewSnapshotService(
	snapRepo *repository.SnapshotRepo,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
) *SnapshotService {
	return &SnapshotService{snapRepo: snapRepo, instRepo: instRepo, marketRepo: marketRepo}
}

// BuildSnapshotForHolding computes a plan-specific simulation snapshot without persisting.
func (s *SnapshotService) BuildSnapshotForHolding(
	ctx context.Context,
	planID, instrumentID, valuationDate string,
) (repository.SimulationSnapshot, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		return repository.SimulationSnapshot{}, fmt.Errorf("load instrument: %w", err)
	}
	if inst.ID == "system_cash_cny" {
		return repository.SimulationSnapshot{ID: s.snapRepo.GetSystemCashSnapshotID()}, nil
	}
	if inst.IsSystem {
		return repository.SimulationSnapshot{}, errSystemInstrumentHoldings
	}

	points, err := s.loadPoints(ctx, instrumentID)
	if err != nil {
		return repository.SimulationSnapshot{}, err
	}
	pointType, sourceName := pointMeta(points)
	metrics := BuildSnapshotMetrics(points, valuationDate, pointType, sourceName)
	if err := ValidateSimulationSnapshot(metricsToRepositorySnapshot("", instrumentID, &planID, valuationDate, inst, metrics)); err != nil {
		if !metrics.SimulationEligible {
			return repository.SimulationSnapshot{}, insufficientHistoryError(metrics)
		}
		return repository.SimulationSnapshot{}, &SnapshotError{
			Code:    "instrument_insufficient_history",
			Message: err.Error(),
			Details: map[string]any{
				"complete_year_count":  metrics.CompleteYearCount,
				"monthly_return_count": metrics.MonthlyReturnCount,
				"quality_status":       metrics.QualityStatus,
				"metrics_version":      metrics.MetricsVersion,
				"volatility_method":    metrics.VolatilityMethod,
			},
		}
	}
	if !metrics.SimulationEligible {
		return repository.SimulationSnapshot{}, insufficientHistoryError(metrics)
	}

	snapID := "sim_snap_" + uuid.New().String()
	planRef := planID
	return metricsToRepositorySnapshot(snapID, instrumentID, &planRef, valuationDate, inst, metrics), nil
}

func metricsToRepositorySnapshot(
	id, instrumentID string,
	planID *string,
	valuationDate string,
	inst repository.InstrumentRecord,
	metrics SnapshotMetrics,
) repository.SimulationSnapshot {
	return repository.SimulationSnapshot{
		ID: id, InstrumentID: instrumentID, PlanID: planID,
		InclusionDate: valuationDate, AsOfDate: valuationDate,
		WindowStart: metrics.WindowStart, WindowEnd: metrics.WindowEnd,
		CompleteYearStart: metrics.CompleteYearStart, CompleteYearEnd: metrics.CompleteYearEnd,
		CompleteYearCount:     metrics.CompleteYearCount,
		DailyObservationCount: metrics.DailyObservationCount,
		MonthlyReturnCount:    metrics.MonthlyReturnCount,
		VolatilityMethod:      metrics.VolatilityMethod,
		MetricsVersion:        metrics.MetricsVersion,
		HistoryDepth:          metrics.HistoryDepth,
		HistoricalCAGR:        MetricFloat(metrics.HistoricalCAGR),
		ModeledAnnualReturn:   MetricFloat(metrics.ModeledAnnualReturn),
		AnnualVolatility:      MetricFloat(metrics.AnnualVolatility),
		MaxDrawdown:           MetricFloat(metrics.MaxDrawdown),
		ExpenseRatio:          inst.ExpenseRatio, ExpenseRatioStatus: inst.ExpenseRatioStatus,
		FeeTreatment: inst.FeeTreatment, SourceMode: "akshare_historical",
		QualityStatus: metrics.QualityStatus,
		WarningsJSON:  repository.WarningsToJSON(metrics.Warnings),
		SourceHash:    metrics.SourceHash,
		Years:         toRepoYears(metrics.Years),
	}
}

func insufficientHistoryError(metrics SnapshotMetrics) *SnapshotError {
	return &SnapshotError{
		Code:    "instrument_insufficient_history",
		Message: insufficientHistoryMessage(metrics),
		Details: map[string]any{
			"complete_year_count":  metrics.CompleteYearCount,
			"monthly_return_count": metrics.MonthlyReturnCount,
			"cagr_status":          metrics.CAGRStatus,
			"volatility_status":    metrics.VolatilityStatus,
			"drawdown_status":      metrics.DrawdownStatus,
		},
	}
}

func insufficientHistoryMessage(metrics SnapshotMetrics) string {
	if metrics.CompleteYearCount < 1 {
		return "没有完整自然年度，无法生成模拟指标"
	}
	if metrics.VolatilityStatus == MetricStatusInsufficientMonthlyCoverage {
		return "完整年度月份覆盖不足，无法计算月度年化波动率"
	}
	return "instrument does not have enough complete years for simulation"
}

// CreatePlanSnapshotTx persists a snapshot within an existing transaction.
func (s *SnapshotService) CreatePlanSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	snap repository.SimulationSnapshot,
) error {
	if err := s.snapRepo.CreatePlanSnapshot(ctx, tx, snap); err != nil {
		return fmt.Errorf("create plan snapshot: %w", err)
	}
	return nil
}

// CreateForHolding returns a simulation snapshot ID for a new plan holding.
func (s *SnapshotService) CreateForHolding(
	ctx context.Context,
	planID, instrumentID, valuationDate string,
) (string, error) {
	snap, err := s.BuildSnapshotForHolding(ctx, planID, instrumentID, valuationDate)
	if err != nil {
		return "", err
	}
	if snap.ID == s.snapRepo.GetSystemCashSnapshotID() {
		return snap.ID, nil
	}
	if err := s.snapRepo.CreatePlanSnapshot(ctx, nil, snap); err != nil {
		return "", fmt.Errorf("persist plan snapshot: %w", err)
	}
	return snap.ID, nil
}

// SyncForHolding rebuilds snapshot for an existing holding.
func (s *SnapshotService) SyncForHolding(
	ctx context.Context,
	planID, instrumentID, holdingID, syncDate string,
) (repository.SimulationSnapshot, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		return repository.SimulationSnapshot{}, fmt.Errorf("load instrument: %w", err)
	}
	points, err := s.loadPoints(ctx, instrumentID)
	if err != nil {
		return repository.SimulationSnapshot{}, err
	}
	pointType, sourceName := pointMeta(points)
	metrics := BuildSnapshotMetrics(points, syncDate, pointType, sourceName)
	planRef := planID
	if err := ValidateSimulationSnapshot(metricsToRepositorySnapshot("", instrumentID, &planRef, syncDate, inst, metrics)); err != nil {
		if !metrics.SimulationEligible {
			return repository.SimulationSnapshot{}, insufficientHistoryError(metrics)
		}
		return repository.SimulationSnapshot{}, &SnapshotError{
			Code:    "instrument_insufficient_history",
			Message: err.Error(),
			Details: map[string]any{
				"complete_year_count":  metrics.CompleteYearCount,
				"monthly_return_count": metrics.MonthlyReturnCount,
				"quality_status":       metrics.QualityStatus,
				"metrics_version":      metrics.MetricsVersion,
				"volatility_method":    metrics.VolatilityMethod,
			},
		}
	}
	if !metrics.SimulationEligible {
		return repository.SimulationSnapshot{}, insufficientHistoryError(metrics)
	}

	snapID := "sim_snap_" + uuid.New().String()
	snap := metricsToRepositorySnapshot(snapID, instrumentID, &planRef, syncDate, inst, metrics)
	_ = holdingID
	if err := s.snapRepo.CreatePlanSnapshot(ctx, nil, snap); err != nil {
		return repository.SimulationSnapshot{}, fmt.Errorf("persist synced snapshot: %w", err)
	}
	out, err := s.snapRepo.GetByID(ctx, snapID)
	if err != nil {
		return repository.SimulationSnapshot{}, fmt.Errorf("load synced snapshot: %w", err)
	}
	return out, nil
}

func (s *SnapshotService) loadPoints(ctx context.Context, instrumentID string) ([]DataPoint, error) {
	rows, err := s.marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil {
		return nil, fmt.Errorf("list market data points: %w", err)
	}
	out := make([]DataPoint, len(rows))
	for i, r := range rows {
		out[i] = DataPoint{
			TradeDate: r.TradeDate, Value: r.Value,
			PointType: r.PointType, SourceName: r.SourceName, FetchedAt: r.FetchedAt,
		}
	}
	return out, nil
}

func pointMeta(points []DataPoint) (string, string) {
	if len(points) == 0 {
		return "adjusted_close", "akshare"
	}
	return points[0].PointType, points[0].SourceName
}

func toRepoYears(years []SimulationYear) []repository.SnapshotYear {
	out := make([]repository.SnapshotYear, len(years))
	for i, y := range years {
		out[i] = repository.SnapshotYear{
			Year: y.Year, AnnualReturn: y.AnnualReturn,
			StartDate: y.StartDate, EndDate: y.EndDate, Observations: y.Observations,
		}
	}
	return out
}

// SnapshotError is returned when snapshot cannot be created.
type SnapshotError struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *SnapshotError) Error() string { return e.Message }

// MarshalWarnings is a helper for tests.
func MarshalWarnings(w []string) string {
	b, _ := json.Marshal(w)
	return string(b)
}
