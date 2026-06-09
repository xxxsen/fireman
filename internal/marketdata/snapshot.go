package marketdata

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/repository"
)

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

// CreateForHolding returns a simulation snapshot ID for a new plan holding.
func (s *SnapshotService) CreateForHolding(ctx context.Context, planID, instrumentID, valuationDate string) (string, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		return "", err
	}
	if inst.ID == "system_cash_cny" {
		return s.snapRepo.GetSystemCashSnapshotID(), nil
	}
	if inst.IsSystem {
		return "", fmt.Errorf("system instrument cannot be added to plan holdings")
	}

	points, err := s.loadPoints(ctx, instrumentID)
	if err != nil {
		return "", err
	}
	pointType, sourceName := pointMeta(points)
	metrics := BuildSnapshotMetrics(points, valuationDate, pointType, sourceName)
	if metrics.QualityStatus != "available" {
		return "", &SnapshotError{Code: "instrument_insufficient_history", Message: "instrument does not have enough complete years for simulation"}
	}

	snapID := "sim_snap_" + uuid.New().String()
	planRef := planID
	snap := repository.SimulationSnapshot{
		ID: snapID, InstrumentID: instrumentID, PlanID: &planRef,
		InclusionDate: valuationDate, AsOfDate: valuationDate,
		WindowStart: metrics.WindowStart, WindowEnd: metrics.WindowEnd,
		CompleteYearStart: metrics.CompleteYearStart, CompleteYearEnd: metrics.CompleteYearEnd,
		CompleteYearCount: metrics.CompleteYearCount, ObservationCount: metrics.ObservationCount,
		HistoricalCAGR: metrics.HistoricalCAGR, ModeledAnnualReturn: metrics.ModeledAnnualReturn,
		AnnualVolatility: metrics.AnnualVolatility, MaxDrawdown: metrics.MaxDrawdown,
		ExpenseRatio: inst.ExpenseRatio, ExpenseRatioStatus: inst.ExpenseRatioStatus,
		FeeTreatment: inst.FeeTreatment, SourceMode: "akshare_historical",
		QualityStatus: metrics.QualityStatus,
		WarningsJSON:  repository.WarningsToJSON(metrics.Warnings),
		SourceHash:    metrics.SourceHash,
		Years:         toRepoYears(metrics.Years),
	}
	if err := s.snapRepo.CreatePlanSnapshot(ctx, nil, snap); err != nil {
		return "", err
	}
	return snapID, nil
}

// SyncForHolding rebuilds snapshot for an existing holding.
func (s *SnapshotService) SyncForHolding(ctx context.Context, planID, instrumentID, holdingID, syncDate string) (repository.SimulationSnapshot, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		return repository.SimulationSnapshot{}, err
	}
	points, err := s.loadPoints(ctx, instrumentID)
	if err != nil {
		return repository.SimulationSnapshot{}, err
	}
	pointType, sourceName := pointMeta(points)
	metrics := BuildSnapshotMetrics(points, syncDate, pointType, sourceName)
	if metrics.QualityStatus != "available" {
		return repository.SimulationSnapshot{}, &SnapshotError{Code: "instrument_insufficient_history", Message: "instrument does not have enough complete years for simulation"}
	}

	snapID := "sim_snap_" + uuid.New().String()
	planRef := planID
	snap := repository.SimulationSnapshot{
		ID: snapID, InstrumentID: instrumentID, PlanID: &planRef,
		InclusionDate: syncDate, AsOfDate: syncDate,
		WindowStart: metrics.WindowStart, WindowEnd: metrics.WindowEnd,
		CompleteYearStart: metrics.CompleteYearStart, CompleteYearEnd: metrics.CompleteYearEnd,
		CompleteYearCount: metrics.CompleteYearCount, ObservationCount: metrics.ObservationCount,
		HistoricalCAGR: metrics.HistoricalCAGR, ModeledAnnualReturn: metrics.ModeledAnnualReturn,
		AnnualVolatility: metrics.AnnualVolatility, MaxDrawdown: metrics.MaxDrawdown,
		ExpenseRatio: inst.ExpenseRatio, ExpenseRatioStatus: inst.ExpenseRatioStatus,
		FeeTreatment: inst.FeeTreatment, SourceMode: "akshare_historical",
		QualityStatus: metrics.QualityStatus,
		WarningsJSON:  repository.WarningsToJSON(metrics.Warnings),
		SourceHash:    metrics.SourceHash,
		Years:         toRepoYears(metrics.Years),
	}
	_ = holdingID
	if err := s.snapRepo.CreatePlanSnapshot(ctx, nil, snap); err != nil {
		return repository.SimulationSnapshot{}, err
	}
	return s.snapRepo.GetByID(ctx, snapID)
}

func (s *SnapshotService) loadPoints(ctx context.Context, instrumentID string) ([]DataPoint, error) {
	rows, err := s.marketRepo.ListByInstrument(ctx, instrumentID)
	if err != nil {
		return nil, err
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

func pointMeta(points []DataPoint) (pointType, sourceName string) {
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
}

func (e *SnapshotError) Error() string { return e.Message }

// MarshalWarnings is a helper for tests.
func MarshalWarnings(w []string) string {
	b, _ := json.Marshal(w)
	return string(b)
}
