package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// HoldingWriteItem contains the client-writable fields of a plan holding.
// asset_class/region are chosen by the user (never inferred from the asset
// type). AllowInactive lets the client keep an asset that has left the
// upstream directory listing.
type HoldingWriteItem struct {
	AssetKey           string  `json:"asset_key"`
	AssetClass         string  `json:"asset_class"`
	Region             string  `json:"region"`
	Enabled            bool    `json:"enabled"`
	WeightWithinGroup  float64 `json:"weight_within_group"`
	CurrentAmountMinor int64   `json:"current_amount_minor"`
	SortOrder          int     `json:"sort_order"`
	AllowInactive      bool    `json:"allow_inactive,omitempty"`
	// Read-only field rejected if present in raw JSON — checked at API layer.
	SimulationSnapshotID *string `json:"simulation_snapshot_id,omitempty"`
}

// HoldingsUpdateRequest replaces all holdings for a plan.
type HoldingsUpdateRequest struct {
	ConfigVersion int                `json:"config_version"`
	Holdings      []HoldingWriteItem `json:"holdings"`
}

// HoldingsService manages plan holdings.
type HoldingsService struct {
	sql       *sql.DB
	plans     *repository.PlanRepo
	holdings  *repository.HoldingsRepo
	snapSvc   *marketdata.SnapshotService
	assetRepo *repository.MarketAssetRepo
}

func NewHoldingsService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	holdings *repository.HoldingsRepo,
	snapSvc *marketdata.SnapshotService,
	assetRepo *repository.MarketAssetRepo,
) *HoldingsService {
	return &HoldingsService{
		sql: sqlDB, plans: plans, holdings: holdings, snapSvc: snapSvc,
		assetRepo: assetRepo,
	}
}

func (s *HoldingsService) GetHoldings(ctx context.Context, planID string) ([]repository.PlanHolding, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, fmt.Errorf("load plan: %w", err)
	}
	out, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("list holdings: %w", err)
	}
	return out, nil
}

type preparedHoldingsUpdate struct {
	built        []repository.PlanHolding
	pendingSnaps []pendingHoldingSnap
}

type pendingHoldingSnap struct {
	snap repository.SimulationSnapshot
	skip bool
}

func (s *HoldingsService) UpdateHoldings(ctx context.Context, planID string,
	req HoldingsUpdateRequest,
) ([]repository.PlanHolding, error) {
	allocRepo := repository.NewAllocationRepo(s.sql)
	alloc, err := allocRepo.Get(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("load allocation: %w", err)
	}
	prep, err := s.prepareHoldingsUpdateWithPendingBumps(ctx, planID, req, 0, alloc)
	if err != nil {
		return nil, err
	}
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		return s.applyHoldingsUpdateTx(ctx, tx, planID, req.ConfigVersion, prep)
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return nil, fmt.Errorf("update holdings tx: %w", err)
	}
	out, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("list holdings: %w", err)
	}
	return out, nil
}

func (s *HoldingsService) prepareHoldingsUpdate(ctx context.Context, planID string,
	req HoldingsUpdateRequest,
) (*preparedHoldingsUpdate, error) {
	allocRepo := repository.NewAllocationRepo(s.sql)
	alloc, err := allocRepo.Get(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("load allocation: %w", err)
	}
	return s.prepareHoldingsUpdateWithPendingBumps(ctx, planID, req, 0, alloc)
}

func (s *HoldingsService) prepareHoldingsUpdateWithPendingBumps(ctx context.Context, planID string,
	req HoldingsUpdateRequest, pendingVersionBumps int, alloc repository.PlanAllocation,
) (*preparedHoldingsUpdate, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, fmt.Errorf("load plan: %w", err)
	}
	expectedVersion := plan.ConfigVersion + pendingVersionBumps
	if req.ConfigVersion != expectedVersion {
		return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}

	existing, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("list existing holdings: %w", err)
	}
	// Only reuse non-empty snapshots: a holding saved lazily (no history at
	// the time) retries the build on every save so it heals once history
	// arrives.
	existingSnap := make(map[string]string)
	for _, h := range existing {
		if h.SimulationSnapshotID != "" {
			existingSnap[h.AssetKey] = h.SimulationSnapshotID
		}
	}

	seen := make(map[string]struct{}, len(req.Holdings))
	pendingSnaps := make([]pendingHoldingSnap, 0)
	built := make([]repository.PlanHolding, 0, len(req.Holdings))
	for _, item := range req.Holdings {
		dupKey := item.AssetKey + "|" + item.AssetClass + "|" + item.Region
		if _, ok := seen[dupKey]; ok {
			return nil, newErr("holding_duplicate",
				"duplicate asset_key + asset_class + region within the plan",
				map[string]any{"asset_key": item.AssetKey})
		}
		seen[dupKey] = struct{}{}
		holding, pending, err := s.buildOnePreparedHolding(ctx, plan, item, existingSnap)
		if err != nil {
			return nil, err
		}
		if pending != nil {
			pendingSnaps = append(pendingSnaps, *pending)
		}
		// Rows sharing one asset_key (different classification) share the
		// snapshot; record it so later rows in this request reuse it.
		if holding.SimulationSnapshotID != "" {
			existingSnap[item.AssetKey] = holding.SimulationSnapshotID
		}
		built = append(built, holding)
	}

	da := toDomainAllocation(alloc)
	dh := holdingsToDomain(built)
	check := domain.ValidateAllWeights(da, dh)
	if !check.Passed {
		msg := "holding weights invalid"
		for _, c := range check.Checks {
			if !c.Passed && c.Message != "" {
				msg = c.Message
				break
			}
		}
		return nil, newErr("plan_weights_invalid", msg, map[string]any{"checks": check.Checks})
	}
	return &preparedHoldingsUpdate{built: built, pendingSnaps: pendingSnaps}, nil
}

func (s *HoldingsService) applyHoldingsUpdateTx(ctx context.Context, tx *sql.Tx, planID string, configVersion int,
	prep *preparedHoldingsUpdate,
) error {
	for _, ps := range prep.pendingSnaps {
		if ps.skip {
			continue
		}
		if err := s.snapSvc.CreatePlanSnapshotTx(ctx, tx, ps.snap); err != nil {
			return fmt.Errorf("create plan snapshot: %w", err)
		}
	}
	if err := s.holdings.Replace(ctx, tx, planID, prep.built); err != nil {
		return fmt.Errorf("replace holdings: %w", err)
	}
	if _, err := s.plans.BumpVersionTx(ctx, tx, planID, configVersion); err != nil {
		return fmt.Errorf("bump plan version: %w", err)
	}
	return nil
}
