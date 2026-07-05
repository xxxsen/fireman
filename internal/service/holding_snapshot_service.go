package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// HoldingSnapshotService exposes plan holding simulation snapshots.
type HoldingSnapshotService struct {
	sql      *sql.DB
	plans    *repository.PlanRepo
	holdings *repository.HoldingsRepo
	snapRepo *repository.SnapshotRepo
	snapSvc  *marketdata.SnapshotService
}

func NewHoldingSnapshotService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	holdings *repository.HoldingsRepo,
	snapRepo *repository.SnapshotRepo,
	snapSvc *marketdata.SnapshotService,
) *HoldingSnapshotService {
	return &HoldingSnapshotService{
		sql: sqlDB, plans: plans, holdings: holdings, snapRepo: snapRepo, snapSvc: snapSvc,
	}
}

func (s *HoldingSnapshotService) Get(ctx context.Context, planID, holdingID string) (repository.SimulationSnapshot,
	error,
) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.SimulationSnapshot{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.SimulationSnapshot{}, fmt.Errorf("load plan: %w", err)
	}
	holding, err := s.holdings.GetByID(ctx, planID, holdingID)
	if err != nil {
		if errors.Is(err, repository.ErrHoldingNotFound) {
			return repository.SimulationSnapshot{}, newErr("holding_not_found", "holding not found", nil)
		}
		return repository.SimulationSnapshot{}, fmt.Errorf("load holding: %w", err)
	}
	snap, err := s.snapRepo.GetByID(ctx, holding.SimulationSnapshotID)
	if err != nil {
		if errors.Is(err, repository.ErrSnapshotNotFound) {
			return repository.SimulationSnapshot{}, newErr("snapshot_not_found", "simulation snapshot not found", nil)
		}
		return repository.SimulationSnapshot{}, fmt.Errorf("load snapshot: %w", err)
	}
	return snap, nil
}

type SyncSnapshotRequest struct {
	ConfigVersion int `json:"config_version"`
}

func (s *HoldingSnapshotService) Sync(ctx context.Context, planID, holdingID string,
	req SyncSnapshotRequest,
) (repository.SimulationSnapshot, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.SimulationSnapshot{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.SimulationSnapshot{}, fmt.Errorf("load plan: %w", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return repository.SimulationSnapshot{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}
	holding, err := s.holdings.GetByID(ctx, planID, holdingID)
	if err != nil {
		if errors.Is(err, repository.ErrHoldingNotFound) {
			return repository.SimulationSnapshot{}, newErr("holding_not_found", "holding not found", nil)
		}
		return repository.SimulationSnapshot{}, fmt.Errorf("load holding: %w", err)
	}

	syncDate := time.Now().Format("2006-01-02")
	snap, err := s.snapSvc.BuildSnapshotForHolding(ctx, planID, holding.AssetKey, syncDate)
	if err != nil {
		return repository.SimulationSnapshot{}, MapSnapshotError(err)
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if snap.ID != repository.SystemCashSnapshotID {
			if err := s.snapSvc.CreatePlanSnapshotTx(ctx, tx, snap); err != nil {
				return fmt.Errorf("create plan snapshot: %w", err)
			}
		}
		if err := s.holdings.UpdateSnapshotID(ctx, tx, holdingID, snap.ID); err != nil {
			return fmt.Errorf("update holding snapshot id: %w", err)
		}
		if _, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion); err != nil {
			return fmt.Errorf("bump plan version: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return repository.SimulationSnapshot{}, newErr(
				"plan_version_conflict",
				"plan configuration version mismatch",
				nil,
			)
		}
		return repository.SimulationSnapshot{}, fmt.Errorf("sync holding snapshot tx: %w", err)
	}
	return snap, nil
}
