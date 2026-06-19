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
type HoldingWriteItem struct {
	InstrumentID       string  `json:"instrument_id"`
	Enabled            bool    `json:"enabled"`
	WeightWithinGroup  float64 `json:"weight_within_group"`
	CurrentAmountMinor int64   `json:"current_amount_minor"`
	SortOrder          int     `json:"sort_order"`
	// Read-only fields rejected if present in raw JSON — checked at API layer.
	AssetClass           *string `json:"asset_class,omitempty"`
	Region               *string `json:"region,omitempty"`
	SimulationSnapshotID *string `json:"simulation_snapshot_id,omitempty"`
}

// HoldingsUpdateRequest replaces all holdings for a plan.
type HoldingsUpdateRequest struct {
	ConfigVersion int                `json:"config_version"`
	Holdings      []HoldingWriteItem `json:"holdings"`
}

// HoldingsService manages plan holdings.
type HoldingsService struct {
	sql        *sql.DB
	plans      *repository.PlanRepo
	holdings   *repository.HoldingsRepo
	snapSvc    *marketdata.SnapshotService
	instRepo   *repository.InstrumentRepo
	marketRepo *repository.MarketDataRepo
}

func NewHoldingsService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	holdings *repository.HoldingsRepo,
	snapSvc *marketdata.SnapshotService,
	instRepo *repository.InstrumentRepo,
	marketRepo *repository.MarketDataRepo,
) *HoldingsService {
	return &HoldingsService{
		sql: sqlDB, plans: plans, holdings: holdings, snapSvc: snapSvc,
		instRepo: instRepo, marketRepo: marketRepo,
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
	existingSnap := make(map[string]string)
	existingClass := make(map[string]frozenClassification)
	for _, h := range existing {
		existingSnap[h.InstrumentID] = h.SimulationSnapshotID
		existingClass[h.InstrumentID] = frozenClassification{assetClass: h.AssetClass, region: h.Region}
	}

	pendingSnaps := make([]pendingHoldingSnap, 0)
	built := make([]repository.PlanHolding, 0, len(req.Holdings))
	for _, item := range req.Holdings {
		holding, pending, err := s.buildOnePreparedHolding(ctx, plan, item, existingSnap, existingClass)
		if err != nil {
			return nil, err
		}
		if pending != nil {
			pendingSnaps = append(pendingSnaps, *pending)
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
