package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// AllocationUpdateRequest updates plan allocation with version check.
type AllocationUpdateRequest struct {
	ConfigVersion     int                           `json:"config_version"`
	AssetClassTargets []repository.AssetClassTarget `json:"asset_class_targets"`
	RegionTargets     []repository.RegionTarget     `json:"region_targets"`
}

// ApplyScenarioRequest applies a scenario to a plan (preview or commit).
type ApplyScenarioRequest struct {
	ScenarioID    string `json:"scenario_id"`
	ConfigVersion int    `json:"config_version"`
	DryRun        bool   `json:"dry_run"`
}

// ApplyScenarioResult shows before/after asset class weights.
type ApplyScenarioResult struct {
	ScenarioID    string                        `json:"scenario_id"`
	Before        []repository.AssetClassTarget `json:"before"`
	After         []repository.AssetClassTarget `json:"after"`
	Applied       bool                          `json:"applied"`
	ConfigVersion int                           `json:"config_version,omitempty"`
}

// ScenarioCreateRequest creates a custom scenario.
type ScenarioCreateRequest struct {
	Name          string                        `json:"name"`
	Description   string                        `json:"description"`
	Weights       []repository.AssetClassTarget `json:"weights"`
	RegionTargets []repository.RegionTarget     `json:"region_targets,omitempty"`
	CopyFromID    *string                       `json:"copy_from_id,omitempty"`
}

// AllocationService manages allocation and scenarios.
type AllocationService struct {
	sql      *sql.DB
	plans    *repository.PlanRepo
	params   *repository.ParametersRepo
	alloc    *repository.AllocationRepo
	scenario *repository.ScenarioRepo
}

func NewAllocationService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	scenario *repository.ScenarioRepo,
) *AllocationService {
	return &AllocationService{
		sql: sqlDB, plans: plans, params: params, alloc: alloc, scenario: scenario,
	}
}

func (s *AllocationService) GetAllocation(ctx context.Context, planID string) (repository.PlanAllocation, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PlanAllocation{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PlanAllocation{}, fmt.Errorf("load plan: %w", err)
	}
	out, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return repository.PlanAllocation{}, fmt.Errorf("load allocation: %w", err)
	}
	return out, nil
}

// validateAllocationWeights runs the shared domain weight checks against a
// candidate allocation, returning a plan_weights_invalid AppError on failure.
func validateAllocationWeights(alloc repository.PlanAllocation, holds []repository.PlanHolding) error {
	check := domain.ValidateAllWeights(toDomainAllocation(alloc), holdingsToDomain(holds))
	if check.Passed {
		return nil
	}
	msg := "allocation weights invalid"
	if len(check.Checks) > 0 && check.Checks[0].Message != "" {
		msg = check.Checks[0].Message
	}
	return newErr("plan_weights_invalid", msg, map[string]any{"checks": check.Checks})
}

func (s *AllocationService) UpdateAllocation(
	ctx context.Context,
	planID string,
	req AllocationUpdateRequest,
) (repository.PlanAllocation, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PlanAllocation{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PlanAllocation{}, fmt.Errorf("load plan: %w", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return repository.PlanAllocation{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}
	alloc := repository.PlanAllocation{
		AssetClassTargets: req.AssetClassTargets,
		RegionTargets:     req.RegionTargets,
	}
	holdingsRepo := repository.NewHoldingsRepo(s.sql)
	holds, _ := holdingsRepo.ListByPlan(ctx, planID)
	if err := validateAllocationWeights(alloc, holds); err != nil {
		return repository.PlanAllocation{}, err
	}
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := applyAllocationUpdateTx(ctx, tx, s.alloc, planID, alloc); err != nil {
			return err
		}
		if _, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion); err != nil {
			return fmt.Errorf("bump plan version: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return repository.PlanAllocation{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return repository.PlanAllocation{}, fmt.Errorf("update allocation tx: %w", err)
	}
	out, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return repository.PlanAllocation{}, fmt.Errorf("reload allocation: %w", err)
	}
	return out, nil
}

func (s *AllocationService) ListScenarios(ctx context.Context) ([]repository.AllocationScenario, error) {
	out, err := s.scenario.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scenarios: %w", err)
	}
	return out, nil
}

func (s *AllocationService) CreateScenario(
	ctx context.Context,
	req ScenarioCreateRequest,
) (repository.AllocationScenario, error) {
	if req.CopyFromID != nil {
		src, err := s.scenario.GetByID(ctx, *req.CopyFromID)
		if err != nil {
			if errors.Is(err, repository.ErrScenarioNotFound) {
				return repository.AllocationScenario{}, newErr("scenario_not_found", "scenario not found", nil)
			}
			return repository.AllocationScenario{}, fmt.Errorf("load source scenario: %w", err)
		}
		applyScenarioCopyDefaults(&req, src)
	}
	if req.Name == "" {
		return repository.AllocationScenario{}, newErr("validation_failed", "name is required", nil)
	}
	if err := validateScenarioWeights(req.Weights); err != nil {
		return repository.AllocationScenario{}, newErr("scenario_weights_invalid", err.Error(), nil)
	}
	regionTargets := req.RegionTargets
	if len(regionTargets) == 0 {
		regionTargets = defaultRegionTargets()
	}
	if err := validateRegionTargets(regionTargets); err != nil {
		return repository.AllocationScenario{}, newErr("scenario_region_targets_invalid", err.Error(), nil)
	}
	scn := repository.AllocationScenario{
		ID: "scn_" + uuid.New().String(), Name: req.Name,
		Description: req.Description, Weights: req.Weights, RegionTargets: regionTargets,
	}
	if err := s.scenario.Create(ctx, scn); err != nil {
		return repository.AllocationScenario{}, fmt.Errorf("create scenario: %w", err)
	}
	out, err := s.scenario.GetByID(ctx, scn.ID)
	if err != nil {
		return repository.AllocationScenario{}, fmt.Errorf("load created scenario: %w", err)
	}
	return out, nil
}

func (s *AllocationService) UpdateScenario(
	ctx context.Context,
	scenarioID string,
	req ScenarioCreateRequest,
) (repository.AllocationScenario, error) {
	if err := validateScenarioWeights(req.Weights); err != nil {
		return repository.AllocationScenario{}, newErr("scenario_weights_invalid", err.Error(), nil)
	}
	regionTargets := req.RegionTargets
	if len(regionTargets) == 0 {
		existing, err := s.scenario.GetByID(ctx, scenarioID)
		if err != nil {
			if errors.Is(err, repository.ErrScenarioNotFound) {
				return repository.AllocationScenario{}, newErr("scenario_not_found", "scenario not found", nil)
			}
			return repository.AllocationScenario{}, fmt.Errorf("load scenario region targets: %w", err)
		}
		regionTargets = existing.RegionTargets
	}
	if err := validateRegionTargets(regionTargets); err != nil {
		return repository.AllocationScenario{}, newErr("scenario_region_targets_invalid", err.Error(), nil)
	}
	scn := repository.AllocationScenario{
		ID: scenarioID, Name: req.Name, Description: req.Description,
		Weights: req.Weights, RegionTargets: regionTargets,
	}
	if err := s.scenario.Update(ctx, scn); err != nil {
		if errors.Is(err, repository.ErrScenarioNotFound) {
			return repository.AllocationScenario{}, newErr("scenario_not_found", "scenario not found", nil)
		}
		return repository.AllocationScenario{}, fmt.Errorf("update scenario: %w", err)
	}
	out, err := s.scenario.GetByID(ctx, scenarioID)
	if err != nil {
		return repository.AllocationScenario{}, fmt.Errorf("load updated scenario: %w", err)
	}
	return out, nil
}

func (s *AllocationService) DeleteScenario(ctx context.Context, scenarioID string) error {
	if err := s.scenario.Delete(ctx, scenarioID); err != nil {
		switch {
		case errors.Is(err, repository.ErrScenarioNotFound):
			return newErr("scenario_not_found", "scenario not found", nil)
		case errors.Is(err, repository.ErrBuiltinScenario):
			return newErr("builtin_scenario_immutable", "builtin scenarios cannot be deleted", nil)
		case errors.Is(err, repository.ErrScenarioInUse):
			return newErr("scenario_in_use", "scenario is referenced by plans", nil)
		default:
			return fmt.Errorf("delete scenario: %w", err)
		}
	}
	return nil
}

func (s *AllocationService) ApplyScenario(
	ctx context.Context,
	planID string,
	req ApplyScenarioRequest,
) (ApplyScenarioResult, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return ApplyScenarioResult{}, newErr("plan_not_found", "plan not found", nil)
		}
		return ApplyScenarioResult{}, fmt.Errorf("load plan: %w", err)
	}
	if !req.DryRun && req.ConfigVersion != plan.ConfigVersion {
		return ApplyScenarioResult{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}
	scn, err := s.scenario.GetByID(ctx, req.ScenarioID)
	if err != nil {
		if errors.Is(err, repository.ErrScenarioNotFound) {
			return ApplyScenarioResult{}, newErr("scenario_not_found", "scenario not found", nil)
		}
		return ApplyScenarioResult{}, fmt.Errorf("load scenario: %w", err)
	}
	current, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return ApplyScenarioResult{}, fmt.Errorf("load current allocation: %w", err)
	}
	result := ApplyScenarioResult{
		ScenarioID: req.ScenarioID,
		Before:     current.AssetClassTargets,
		After:      scn.Weights,
		Applied:    false,
	}
	if req.DryRun {
		return result, nil
	}
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		// Scenario templates only carry asset-class structure; the plan's own
		// domestic/foreign split is preserved and not overwritten.
		newAlloc := repository.PlanAllocation{
			AssetClassTargets: scn.Weights,
			RegionTargets:     current.RegionTargets,
		}
		if err := s.alloc.Replace(ctx, tx, planID, newAlloc); err != nil {
			return fmt.Errorf("replace allocation: %w", err)
		}
		if err := s.params.SetSelectedScenarioID(ctx, tx, planID, req.ScenarioID); err != nil {
			return fmt.Errorf("set selected scenario: %w", err)
		}
		newVer, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion)
		if err != nil {
			return fmt.Errorf("bump plan version: %w", err)
		}
		result.ConfigVersion = newVer
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return ApplyScenarioResult{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return ApplyScenarioResult{}, fmt.Errorf("apply scenario tx: %w", err)
	}
	result.Applied = true
	return result, nil
}
