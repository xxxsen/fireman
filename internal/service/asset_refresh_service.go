package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// AssetRefreshHoldingItem is one holding in an asset refresh submission.
// AssetClass/Region are only needed for assets that are new to the plan;
// existing holdings keep their stored classification.
type AssetRefreshHoldingItem struct {
	AssetKey           string   `json:"asset_key"`
	AssetClass         string   `json:"asset_class,omitempty"`
	Region             string   `json:"region,omitempty"`
	CurrentAmountMinor int64    `json:"current_amount_minor"`
	WeightWithinGroup  *float64 `json:"weight_within_group,omitempty"`
	SortOrder          *int     `json:"sort_order,omitempty"`
}

// AssetRefreshRequest submits updated current asset amounts.
type AssetRefreshRequest struct {
	ConfigVersion        int                       `json:"config_version"`
	ScenarioID           string                    `json:"scenario_id,omitempty"`
	Holdings             []AssetRefreshHoldingItem `json:"holdings"`
	TotalAssetsMinor     int64                     `json:"total_assets_minor"`
	SyncTotalAssetsMinor bool                      `json:"sync_total_assets_minor"`
	ConfigChanged        bool                      `json:"config_changed"`
}

// AssetRefreshService applies asset refresh submissions atomically.
type AssetRefreshService struct {
	sql        *sql.DB
	plans      *repository.PlanRepo
	params     *repository.ParametersRepo
	alloc      *repository.AllocationRepo
	scenario   *repository.ScenarioRepo
	holdings   *HoldingsService
	events     *repository.AssetRefreshEventRepo
	executions *repository.RebalanceExecutionRepo
}

func NewAssetRefreshService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	scenario *repository.ScenarioRepo,
	holdingsSvc *HoldingsService,
	events *repository.AssetRefreshEventRepo,
	executions *repository.RebalanceExecutionRepo,
) *AssetRefreshService {
	return &AssetRefreshService{
		sql: sqlDB, plans: plans, params: params, alloc: alloc, scenario: scenario,
		holdings: holdingsSvc, events: events, executions: executions,
	}
}

type assetRefreshPrepared struct {
	beforeTotal         int64
	enabledAfter        int64
	prep                *preparedHoldingsUpdate
	scenarioWeights     []repository.AssetClassTarget
	scenarioRegions     []repository.RegionTarget
	pendingVersionBumps int
}

func (s *AssetRefreshService) prepareAssetRefresh(
	ctx context.Context,
	planID string,
	req AssetRefreshRequest,
) (*assetRefreshPrepared, error) {
	active, err := s.executions.HasActiveByPlan(ctx, planID)
	if err != nil {
		return nil, wrapRepo("check active rebalance execution", err)
	}
	if active {
		return nil, newErr("rebalance_execution_in_progress",
			"asset refresh blocked while a rebalance execution is in progress; complete or cancel it first", nil)
	}

	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("load plan", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}

	out := &assetRefreshPrepared{}
	if req.ScenarioID != "" {
		scn, err := s.scenario.GetByID(ctx, req.ScenarioID)
		if err != nil {
			if errors.Is(err, repository.ErrScenarioNotFound) {
				return nil, newErr("scenario_not_found", "scenario not found", nil)
			}
			return nil, wrapRepo("load scenario", err)
		}
		out.scenarioWeights = scn.Weights
		out.scenarioRegions = scn.RegionTargets
		out.pendingVersionBumps = 1
	}

	amountByInstrument, err := validateAssetRefreshRequest(req)
	if err != nil {
		return nil, err
	}

	existing, err := s.holdings.GetHoldings(ctx, planID)
	if err != nil {
		return nil, err
	}
	out.beforeTotal = sumEnabledMinorFromHoldings(existing)

	var validationAlloc repository.PlanAllocation
	if req.ScenarioID != "" {
		validationAlloc = repository.PlanAllocation{
			AssetClassTargets: out.scenarioWeights,
			RegionTargets:     out.scenarioRegions,
		}
	} else {
		validationAlloc, err = s.alloc.Get(ctx, planID)
		if err != nil {
			return nil, wrapRepo("load allocation", err)
		}
	}

	updateReq := buildAssetRefreshHoldingsReq(req, existing, amountByInstrument, out.pendingVersionBumps)
	out.prep, err = s.holdings.prepareHoldingsUpdateWithPendingBumps(
		ctx, planID, updateReq, out.pendingVersionBumps, validationAlloc,
	)
	if err != nil {
		return nil, err
	}
	out.enabledAfter = sumEnabledFromBuilt(out.prep.built)
	return out, nil
}

func (s *AssetRefreshService) applyAssetRefreshTx(
	ctx context.Context,
	tx *sql.Tx,
	planID string,
	req AssetRefreshRequest,
	prep *assetRefreshPrepared,
	configVersion int,
) (int, bool, error) {
	if req.ScenarioID != "" {
		newAlloc := repository.PlanAllocation{
			AssetClassTargets: prep.scenarioWeights,
			RegionTargets:     prep.scenarioRegions,
		}
		if err := s.alloc.Replace(ctx, tx, planID, newAlloc); err != nil {
			return configVersion, false, fmt.Errorf("replace allocation: %w", err)
		}
		if err := s.params.SetSelectedScenarioID(ctx, tx, planID, req.ScenarioID); err != nil {
			return configVersion, false, fmt.Errorf("set selected scenario: %w", err)
		}
		var err error
		configVersion, err = s.plans.BumpVersionTx(ctx, tx, planID, configVersion)
		if err != nil {
			return configVersion, false, fmt.Errorf("bump plan version after scenario: %w", err)
		}
	}

	if err := s.holdings.applyHoldingsUpdateTx(ctx, tx, planID, configVersion, prep.prep); err != nil {
		return configVersion, false, err
	}

	var syncedScale bool
	versionAfterHoldings := configVersion + 1
	if req.SyncTotalAssetsMinor {
		if err := applyTotalAssetsSyncTx(ctx, tx, s.plans, s.params, planID, versionAfterHoldings, req.TotalAssetsMinor,
			prep.enabledAfter); err != nil {
			return configVersion, false, err
		}
		syncedScale = true
	}
	if err := s.events.CreateTx(ctx, tx, repository.AssetRefreshEvent{
		ID: "are_" + uuid.New().String(), PlanID: planID,
		RefreshedAt:      time.Now().UnixMilli(),
		BeforeTotalMinor: prep.beforeTotal, AfterTotalMinor: req.TotalAssetsMinor,
		SyncScale: syncedScale, ConfigChanged: req.ConfigChanged,
	}); err != nil {
		return configVersion, false, fmt.Errorf("create asset refresh event: %w", err)
	}
	return configVersion, syncedScale, nil
}

func (s *AssetRefreshService) Submit(ctx context.Context, planID string, req AssetRefreshRequest) (map[string]any,
	error,
) {
	prepared, err := s.prepareAssetRefresh(ctx, planID, req)
	if err != nil {
		return nil, err
	}

	var syncedScale bool
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		var txErr error
		_, syncedScale, txErr = s.applyAssetRefreshTx(ctx, tx, planID, req, prepared, req.ConfigVersion)
		return txErr
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		appErr := &AppError{}
		if errors.As(err, &appErr) {
			return nil, appErr
		}
		return nil, wrapRepo("submit asset refresh", err)
	}

	updated, err := s.holdings.GetHoldings(ctx, planID)
	if err != nil {
		return nil, wrapRepo("load updated holdings", err)
	}
	return map[string]any{
		"holdings":           updated,
		"before_total_minor": prepared.beforeTotal,
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
	if err := params.SetTotalAssetsMinor(ctx, tx, planID, totalMinor); err != nil {
		return wrapRepo("set total assets minor", err)
	}
	_, err := plans.BumpVersionTx(ctx, tx, planID, configVersion)
	return wrapRepo("bump plan version", err)
}
