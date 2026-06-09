package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

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
	sql      *sql.DB
	plans    *repository.PlanRepo
	holdings *repository.HoldingsRepo
	snapSvc  *marketdata.SnapshotService
}

func NewHoldingsService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	holdings *repository.HoldingsRepo,
	snapSvc *marketdata.SnapshotService,
) *HoldingsService {
	return &HoldingsService{sql: sqlDB, plans: plans, holdings: holdings, snapSvc: snapSvc}
}

func (s *HoldingsService) GetHoldings(ctx context.Context, planID string) ([]repository.PlanHolding, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	return s.holdings.ListByPlan(ctx, planID)
}

func (s *HoldingsService) UpdateHoldings(ctx context.Context, planID string, req HoldingsUpdateRequest) ([]repository.PlanHolding, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}

	existing, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	existingSnap := make(map[string]string)
	for _, h := range existing {
		existingSnap[h.InstrumentID] = h.SimulationSnapshotID
	}

	type pendingSnap struct {
		snap repository.SimulationSnapshot
		skip bool
	}
	pendingSnaps := make([]pendingSnap, 0)

	var built []repository.PlanHolding
	for _, item := range req.Holdings {
		if item.AssetClass != nil || item.Region != nil || item.SimulationSnapshotID != nil {
			return nil, newErr("holding_fields_read_only", "asset_class, region and simulation_snapshot_id are read-only", nil)
		}
		inst, err := s.holdings.GetInstrument(ctx, item.InstrumentID)
		if err != nil {
			if errors.Is(err, repository.ErrInstrumentNotFound) {
				return nil, newErr("instrument_not_found", "instrument not found", map[string]any{"instrument_id": item.InstrumentID})
			}
			return nil, err
		}
		snapID, ok := existingSnap[item.InstrumentID]
		if !ok {
			snap, err := s.snapSvc.BuildSnapshotForHolding(ctx, planID, item.InstrumentID, plan.ValuationDate)
			if err != nil {
				return nil, MapSnapshotError(err)
			}
			snapID = snap.ID
			pendingSnaps = append(pendingSnaps, pendingSnap{
				snap: snap,
				skip: snap.ID == repository.SystemCashSnapshotID,
			})
		}
		built = append(built, repository.PlanHolding{
			ID: "hold_" + uuid.New().String(), PlanID: planID,
			InstrumentID: item.InstrumentID, Enabled: item.Enabled,
			AssetClass: inst.AssetClass, Region: inst.Region,
			WeightWithinGroup: item.WeightWithinGroup, CurrentAmountMinor: item.CurrentAmountMinor,
			SimulationSnapshotID: snapID, SortOrder: item.SortOrder,
		})
	}

	allocRepo := repository.NewAllocationRepo(s.sql)
	alloc, err := allocRepo.Get(ctx, planID)
	if err != nil {
		return nil, err
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

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		for _, ps := range pendingSnaps {
			if ps.skip {
				continue
			}
			if err := s.snapSvc.CreatePlanSnapshotTx(ctx, tx, ps.snap); err != nil {
				return err
			}
		}
		if err := s.holdings.Replace(ctx, tx, planID, built); err != nil {
			return err
		}
		_, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion)
		return err
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return nil, err
	}
	return s.holdings.ListByPlan(ctx, planID)
}
