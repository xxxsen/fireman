package marketdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/repository"
)

// ErrAssetHistoryMissing reports that a market asset has no local history
// points, so a simulation snapshot cannot be built until a history sync runs.
var ErrAssetHistoryMissing = errors.New("market asset has no local history data")

// SnapshotService creates plan-specific simulation snapshots from the global
// market asset directory (market_assets + market_asset_points).
type SnapshotService struct {
	snapRepo  *repository.SnapshotRepo
	assetRepo *repository.MarketAssetRepo
}

func NewSnapshotService(
	snapRepo *repository.SnapshotRepo,
	assetRepo *repository.MarketAssetRepo,
) *SnapshotService {
	return &SnapshotService{snapRepo: snapRepo, assetRepo: assetRepo}
}

// BuildSnapshotForHolding computes a plan-specific simulation snapshot without
// persisting. System cash assets resolve to their immutable seeded snapshots.
func (s *SnapshotService) BuildSnapshotForHolding(
	ctx context.Context,
	planID, assetKey, valuationDate string,
) (repository.SimulationSnapshot, error) {
	return s.BuildSnapshotForHoldingTx(ctx, nil, planID, assetKey, valuationDate)
}

// BuildSnapshotForHoldingTx is BuildSnapshotForHolding with all reads routed
// through an existing transaction (tx may be nil for pool reads). Required by
// flows that must read and write in one atomic transaction on the
// single-connection pool.
func (s *SnapshotService) BuildSnapshotForHoldingTx(
	ctx context.Context,
	tx *sql.Tx,
	planID, assetKey, valuationDate string,
) (repository.SimulationSnapshot, error) {
	if cashSnapID, ok := repository.SystemCashSnapshotIDForAsset(assetKey); ok {
		return repository.SimulationSnapshot{ID: cashSnapID}, nil
	}
	asset, err := s.assetByKey(ctx, tx, assetKey)
	if err != nil {
		return repository.SimulationSnapshot{}, fmt.Errorf("load market asset: %w", err)
	}

	points, err := s.LoadAssetPointsTx(ctx, tx, asset)
	if err != nil {
		return repository.SimulationSnapshot{}, err
	}
	if len(points) == 0 {
		return repository.SimulationSnapshot{}, &SnapshotError{
			Code:    "asset_history_missing",
			Message: "该标的尚未同步历史数据，请先同步历史数据",
			Details: map[string]any{"asset_key": assetKey},
		}
	}
	pointType, sourceName := pointMeta(points)
	metrics := BuildSnapshotMetrics(points, valuationDate, pointType, sourceName)
	snap := metricsToRepositorySnapshot("", assetKey, &planID, valuationDate, metrics)
	if err := ValidateSimulationSnapshot(snap); err != nil {
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
	return metricsToRepositorySnapshot(snapID, assetKey, &planRef, valuationDate, metrics), nil
}

func metricsToRepositorySnapshot(
	id, assetKey string,
	planID *string,
	valuationDate string,
	metrics SnapshotMetrics,
) repository.SimulationSnapshot {
	return repository.SimulationSnapshot{
		ID: id, AssetKey: assetKey, PlanID: planID,
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
		// Market asset prices/NAV already embed fund fees; the directory does
		// not track expense ratios separately.
		ExpenseRatioStatus: "unavailable",
		FeeTreatment:       "embedded",
		SourceMode:         "market_asset_history",
		QualityStatus:      metrics.QualityStatus,
		WarningsJSON:       repository.WarningsToJSON(metrics.Warnings),
		SourceHash:         metrics.SourceHash,
		Years:              toRepoYears(metrics.Years),
		Months:             toRepoMonths(metrics.MonthlyReturns),
	}
}

func toRepoMonths(months []MonthlyReturn) []repository.SnapshotMonth {
	out := make([]repository.SnapshotMonth, len(months))
	for i, m := range months {
		out[i] = repository.SnapshotMonth{Year: m.Year, Month: m.Month, LogReturn: m.LogReturn}
	}
	return out
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
	planID, assetKey, valuationDate string,
) (string, error) {
	snap, err := s.BuildSnapshotForHolding(ctx, planID, assetKey, valuationDate)
	if err != nil {
		return "", err
	}
	if _, isCash := repository.SystemCashSnapshotIDForAsset(assetKey); isCash {
		return snap.ID, nil
	}
	if err := s.snapRepo.CreatePlanSnapshot(ctx, nil, snap); err != nil {
		return "", fmt.Errorf("persist plan snapshot: %w", err)
	}
	return snap.ID, nil
}

// LoadAssetPoints loads the asset's local history series for the dimension the
// simulation should use. Exchange-traded assets require an adjusted series;
// raw prices must never win merely because they contain more points.
func (s *SnapshotService) LoadAssetPoints(
	ctx context.Context, asset repository.MarketAsset,
) ([]DataPoint, error) {
	return s.LoadAssetPointsTx(ctx, nil, asset)
}

// LoadAssetPointsTx is LoadAssetPoints with reads routed through an existing
// transaction (tx may be nil for pool reads).
func (s *SnapshotService) LoadAssetPointsTx(
	ctx context.Context, tx *sql.Tx, asset repository.MarketAsset,
) ([]DataPoint, error) {
	adjustPolicy, pointType := defaultSnapshotDimension(asset)
	states, err := s.listHistoryStates(ctx, tx, asset.AssetKey)
	if err != nil {
		return nil, fmt.Errorf("list history states: %w", err)
	}
	bestCount := 0
	for _, st := range states {
		if isExchangeTradedAsset(asset) &&
			(st.AdjustPolicy == "none" || st.PointType != "adjusted_close") {
			continue
		}
		if st.PointCount > bestCount {
			bestCount = st.PointCount
			adjustPolicy, pointType = st.AdjustPolicy, st.PointType
		}
	}
	if isExchangeTradedAsset(asset) && bestCount == 0 && len(states) > 0 {
		return nil, &SnapshotError{
			Code:    "unadjusted_price_series",
			Message: "未复权收盘价不能用于 FIRE 模拟，请先同步前复权历史数据",
			Details: map[string]any{"asset_key": asset.AssetKey},
		}
	}
	rows, err := s.listPoints(ctx, tx, asset.AssetKey, adjustPolicy, pointType)
	if err != nil {
		return nil, fmt.Errorf("list market asset points: %w", err)
	}
	out := make([]DataPoint, len(rows))
	for i, r := range rows {
		out[i] = DataPoint{
			TradeDate: r.TradeDate, Value: r.Value,
			PointType: r.PointType, SourceName: r.SourceName, FetchedAt: r.FetchedAt,
		}
	}
	if asset.Market == "CN" && isCNExchangeTradedAsset(asset) {
		if discontinuity, found := FindPriceDiscontinuity(out, CNAdjustedPriceMaxDailyMove); found {
			return nil, &SnapshotError{
				Code:    "adjustment_discontinuity",
				Message: "复权价格序列存在异常断点，请全量刷新历史数据",
				Details: map[string]any{
					"asset_key": asset.AssetKey, "previous_date": discontinuity.PreviousDate,
					"date": discontinuity.Date, "return": discontinuity.Return,
				},
			}
		}
	}
	return out, nil
}

func defaultSnapshotDimension(asset repository.MarketAsset) (string, string) {
	if isExchangeTradedAsset(asset) {
		return "qfq", "adjusted_close"
	}
	return "none", defaultPointTypeForAsset(asset)
}

func isCNExchangeTradedAsset(asset repository.MarketAsset) bool {
	return asset.InstrumentType == "cn_exchange_stock" || asset.InstrumentType == "cn_exchange_fund"
}

func isExchangeTradedAsset(asset repository.MarketAsset) bool {
	switch asset.InstrumentType {
	case "cn_exchange_stock", "cn_exchange_fund", "hk_stock", "hk_etf", "us_stock", "us_etf":
		return true
	default:
		return false
	}
}

// assetByKey routes the directory read through tx when provided.
// %w keeps repository sentinel errors visible to errors.Is upstream.
func (s *SnapshotService) assetByKey(
	ctx context.Context, tx *sql.Tx, assetKey string,
) (repository.MarketAsset, error) {
	var asset repository.MarketAsset
	var err error
	if tx != nil {
		asset, err = s.assetRepo.GetByKeyTx(ctx, tx, assetKey)
	} else {
		asset, err = s.assetRepo.GetByKey(ctx, assetKey)
	}
	if err != nil {
		return repository.MarketAsset{}, fmt.Errorf("get market asset: %w", err)
	}
	return asset, nil
}

func (s *SnapshotService) listHistoryStates(
	ctx context.Context, tx *sql.Tx, assetKey string,
) ([]repository.MarketAssetHistoryState, error) {
	var states []repository.MarketAssetHistoryState
	var err error
	if tx != nil {
		states, err = s.assetRepo.ListHistoryStatesByAssetTx(ctx, tx, assetKey)
	} else {
		states, err = s.assetRepo.ListHistoryStatesByAsset(ctx, assetKey)
	}
	if err != nil {
		return nil, fmt.Errorf("list history states: %w", err)
	}
	return states, nil
}

func (s *SnapshotService) listPoints(
	ctx context.Context, tx *sql.Tx, assetKey, adjustPolicy, pointType string,
) ([]repository.MarketAssetPoint, error) {
	var points []repository.MarketAssetPoint
	var err error
	if tx != nil {
		points, err = s.assetRepo.ListPointsTx(ctx, tx, assetKey, adjustPolicy, pointType)
	} else {
		points, err = s.assetRepo.ListPoints(ctx, assetKey, adjustPolicy, pointType)
	}
	if err != nil {
		return nil, fmt.Errorf("list points: %w", err)
	}
	return points, nil
}

// defaultPointTypeForAsset mirrors the history-sync default: mutual money
// funds use NAV, other mutual funds the cumulative NAV index, exchange-traded
// assets adjusted close.
func defaultPointTypeForAsset(asset repository.MarketAsset) string {
	if asset.InstrumentType == "cn_mutual_fund" {
		if strings.Contains(asset.InstrumentKind, "货币") {
			return "nav"
		}
		return "total_return_index"
	}
	return "adjusted_close"
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
