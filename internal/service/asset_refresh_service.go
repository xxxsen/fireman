package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// AssetRefreshHoldingItem is one holding amount in an asset refresh submission.
type AssetRefreshHoldingItem struct {
	InstrumentID       string `json:"instrument_id"`
	CurrentAmountMinor int64  `json:"current_amount_minor"`
}

// AssetRefreshRequest submits updated current asset amounts.
type AssetRefreshRequest struct {
	ConfigVersion        int                       `json:"config_version"`
	Holdings             []AssetRefreshHoldingItem `json:"holdings"`
	TotalAssetsMinor     int64                     `json:"total_assets_minor"`
	SyncTotalAssetsMinor bool                      `json:"sync_total_assets_minor"`
	ConfigChanged        bool                      `json:"config_changed"`
}

// AssetRefreshService applies asset refresh submissions atomically.
type AssetRefreshService struct {
	sql      *sql.DB
	plans    *repository.PlanRepo
	params   *repository.ParametersRepo
	holdings *HoldingsService
	events   *repository.AssetRefreshEventRepo
}

func NewAssetRefreshService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	holdingsSvc *HoldingsService,
	events *repository.AssetRefreshEventRepo,
) *AssetRefreshService {
	return &AssetRefreshService{
		sql: sqlDB, plans: plans, params: params, holdings: holdingsSvc, events: events,
	}
}

func (s *AssetRefreshService) Submit(ctx context.Context, planID string, req AssetRefreshRequest) (map[string]any, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	if len(req.Holdings) == 0 {
		return nil, newErr("validation_failed", "holdings required", nil)
	}

	var sum int64
	amountByInstrument := make(map[string]int64, len(req.Holdings))
	for _, item := range req.Holdings {
		if item.InstrumentID == "" {
			return nil, newErr("validation_failed", "instrument_id required", nil)
		}
		if item.CurrentAmountMinor < 0 {
			return nil, newErr("validation_failed", "current amount cannot be negative", nil)
		}
		sum += item.CurrentAmountMinor
		amountByInstrument[item.InstrumentID] = item.CurrentAmountMinor
	}
	if math.Abs(float64(sum-req.TotalAssetsMinor)) > amountToleranceMinor {
		return nil, newErr("validation_failed", "holdings sum does not match total assets", map[string]any{
			"holdings_sum_minor": sum, "total_assets_minor": req.TotalAssetsMinor,
		})
	}

	existing, err := s.holdings.GetHoldings(ctx, planID)
	if err != nil {
		return nil, err
	}
	beforeTotal := sumEnabledMinorFromHoldings(existing)

	updateReq := HoldingsUpdateRequest{
		ConfigVersion: req.ConfigVersion,
		Holdings:      make([]HoldingWriteItem, 0, len(existing)),
	}
	for _, h := range existing {
		amount := h.CurrentAmountMinor
		if v, ok := amountByInstrument[h.InstrumentID]; ok {
			amount = v
		}
		updateReq.Holdings = append(updateReq.Holdings, HoldingWriteItem{
			InstrumentID: h.InstrumentID, Enabled: h.Enabled,
			WeightWithinGroup: h.WeightWithinGroup, CurrentAmountMinor: amount,
			SortOrder: h.SortOrder,
		})
	}

	prep, err := s.holdings.prepareHoldingsUpdate(ctx, planID, updateReq)
	if err != nil {
		return nil, err
	}
	enabledAfter := sumEnabledFromBuilt(prep.built)

	var syncedScale bool
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.holdings.applyHoldingsUpdateTx(ctx, tx, planID, req.ConfigVersion, prep); err != nil {
			return err
		}
		versionAfterHoldings := req.ConfigVersion + 1
		if req.SyncTotalAssetsMinor {
			if err := applyTotalAssetsSyncTx(ctx, tx, s.plans, s.params, planID, versionAfterHoldings, req.TotalAssetsMinor, enabledAfter); err != nil {
				return err
			}
			syncedScale = true
		}
		return s.events.CreateTx(ctx, tx, repository.AssetRefreshEvent{
			ID: "are_" + uuid.New().String(), PlanID: planID,
			RefreshedAt:      time.Now().UnixMilli(),
			BeforeTotalMinor: beforeTotal, AfterTotalMinor: req.TotalAssetsMinor,
			SyncScale: syncedScale, ConfigChanged: req.ConfigChanged,
		})
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		if appErr, ok := err.(*AppError); ok {
			return nil, appErr
		}
		return nil, err
	}

	updated, err := s.holdings.GetHoldings(ctx, planID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"holdings":           updated,
		"before_total_minor": beforeTotal,
		"after_total_minor":  req.TotalAssetsMinor,
		"synced_scale":       syncedScale,
	}, nil
}

func sumEnabledMinorFromHoldings(holdings []repository.PlanHolding) int64 {
	var sum int64
	for _, h := range holdings {
		if h.Enabled {
			sum += h.CurrentAmountMinor
		}
	}
	return sum
}

func sumEnabledFromBuilt(holdings []repository.PlanHolding) int64 {
	var sum int64
	for _, h := range holdings {
		if h.Enabled {
			sum += h.CurrentAmountMinor
		}
	}
	return sum
}

func applyTotalAssetsSyncTx(
	ctx context.Context,
	tx *sql.Tx,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	planID string,
	configVersion int,
	totalMinor int64,
	enabledSum int64,
) error {
	gap := totalMinor - enabledSum
	if gap < -100 {
		return newErr("holdings_exceed_total", "enabled holdings exceed total assets", map[string]any{
			"total_assets_minor": totalMinor, "holdings_sum_minor": enabledSum,
		})
	}
	if gap > 100 {
		return newErr("unallocated_gap_unresolved", "unallocated gap must be resolved via holdings", map[string]any{
			"gap_minor": gap,
		})
	}
	p, err := params.Get(ctx, planID)
	if err != nil {
		return err
	}
	p.TotalAssetsMinor = totalMinor
	if err := params.Upsert(ctx, tx, p); err != nil {
		return err
	}
	_, err = plans.BumpVersionTx(ctx, tx, planID, configVersion)
	return err
}
