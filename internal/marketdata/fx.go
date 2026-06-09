package marketdata

import (
	"context"
	"fmt"

	"github.com/fireman/fireman/internal/repository"
)

// FXPairCode returns the system FX instrument code for converting assetCurrency into baseCurrency.
func FXPairCode(assetCurrency, baseCurrency string) (string, error) {
	if assetCurrency == baseCurrency {
		return "", nil
	}
	if baseCurrency != "CNY" {
		return "", fmt.Errorf("unsupported base currency %q for FX conversion", baseCurrency)
	}
	switch assetCurrency {
	case "USD":
		return "USDCNY", nil
	case "HKD":
		return "HKDCNY", nil
	default:
		return "", fmt.Errorf("unsupported foreign currency %q", assetCurrency)
	}
}

// FXResolver loads system FX metrics for simulation input snapshots.
type FXResolver struct {
	inst   *repository.InstrumentRepo
	market *repository.MarketDataRepo
}

func NewFXResolver(inst *repository.InstrumentRepo, market *repository.MarketDataRepo) *FXResolver {
	return &FXResolver{inst: inst, market: market}
}

// Metrics returns FX snapshot metrics for a foreign-currency asset.
func (r *FXResolver) Metrics(ctx context.Context, assetCurrency, baseCurrency, asOfDate string) (SnapshotMetrics, error) {
	code, err := FXPairCode(assetCurrency, baseCurrency)
	if err != nil {
		return SnapshotMetrics{}, err
	}
	if code == "" {
		return SnapshotMetrics{}, nil
	}

	inst, err := r.inst.FindByKey(ctx, "SYSTEM", "fx_rate", code, "none")
	if err != nil {
		return SnapshotMetrics{}, fmt.Errorf("fx instrument %s: %w", code, err)
	}
	rows, err := r.market.ListByInstrument(ctx, inst.ID)
	if err != nil {
		return SnapshotMetrics{}, err
	}
	points := make([]DataPoint, len(rows))
	for i, row := range rows {
		points[i] = DataPoint{
			TradeDate: row.TradeDate, Value: row.Value,
			PointType: row.PointType, SourceName: row.SourceName, FetchedAt: row.FetchedAt,
		}
	}
	pointType, sourceName := "fx_rate", "system"
	if len(points) > 0 {
		pointType = points[0].PointType
		sourceName = points[0].SourceName
	}
	return BuildSnapshotMetrics(points, asOfDate, pointType, sourceName), nil
}
