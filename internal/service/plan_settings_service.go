package service

import (
	"context"
	"database/sql"
	"errors"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// PlanSettingsPlanPatch carries the optional plan metadata portion of a
// combined settings save. Only the name is editable from the settings page.
type PlanSettingsPlanPatch struct {
	Name string `json:"name"`
}

// PlanSettingsAllocationPatch carries the optional allocation portion of a
// combined settings save.
type PlanSettingsAllocationPatch struct {
	AssetClassTargets []repository.AssetClassTarget `json:"asset_class_targets"`
	RegionTargets     []repository.RegionTarget     `json:"region_targets"`
}

// PlanSettingsUpdateRequest is the combined, single-transaction settings save:
// plan name and allocation are optional, parameters are required. The whole
// request commits or rolls back atomically with exactly one config_version CAS.
type PlanSettingsUpdateRequest struct {
	ConfigVersion          int                          `json:"config_version"`
	Plan                   *PlanSettingsPlanPatch       `json:"plan,omitempty"`
	Allocation             *PlanSettingsAllocationPatch `json:"allocation,omitempty"`
	Parameters             repository.PlanParameters    `json:"parameters"`
	ApplyUnallocatedToCash bool                         `json:"apply_unallocated_to_cash,omitempty"`
}

// PlanSettingsUpdateResult returns the post-save state of everything the
// settings page edits.
type PlanSettingsUpdateResult struct {
	Plan       PlanDetail                `json:"plan"`
	Parameters repository.PlanParameters `json:"parameters"`
	Allocation repository.PlanAllocation `json:"allocation"`
}

// settingsUpdatePrep carries the validated inputs of a combined settings save
// from the read/validate phase into the write transaction.
type settingsUpdatePrep struct {
	plan       repository.Plan
	allocation *repository.PlanAllocation
	holds      []repository.PlanHolding
	gap        int64
}

// UpdateSettings persists plan name, allocation targets and FIRE parameters in
// one transaction with a single optimistic config_version bump. Any validation
// or write failure leaves the plan completely unchanged, so a client retry can
// reuse the same config_version.
func (s *PlanService) UpdateSettings(
	ctx context.Context, planID string, req PlanSettingsUpdateRequest,
) (PlanSettingsUpdateResult, error) {
	prep, err := s.prepareSettingsUpdate(ctx, planID, &req)
	if err != nil {
		return PlanSettingsUpdateResult{}, err
	}
	if err := s.applySettingsUpdate(ctx, planID, req, prep); err != nil {
		return PlanSettingsUpdateResult{}, err
	}
	return s.loadSettingsResult(ctx, planID)
}

// prepareSettingsUpdate runs every validation up front (version CAS pre-check,
// parameter rules, allocation weights) and normalizes req.Parameters in place.
func (s *PlanService) prepareSettingsUpdate(
	ctx context.Context, planID string, req *PlanSettingsUpdateRequest,
) (settingsUpdatePrep, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return settingsUpdatePrep{}, newErr("plan_not_found", "plan not found", nil)
		}
		return settingsUpdatePrep{}, wrapRepo("load plan", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return settingsUpdatePrep{}, newErr("plan_version_conflict", "plan configuration version mismatch",
			map[string]any{"expected": plan.ConfigVersion, "provided": req.ConfigVersion})
	}
	if req.Plan != nil && req.Plan.Name != "" {
		plan.Name = req.Plan.Name
	}

	req.Parameters.PlanID = planID
	// student_t_df is read-only (frozen from the assumption profile per run);
	// preserve the stored value regardless of what the client sent.
	if existing, perr := s.params.Get(ctx, planID); perr == nil {
		req.Parameters.StudentTDf = existing.StudentTDf
	}
	if err := validateParameters(req.Parameters); err != nil {
		return settingsUpdatePrep{}, newErr("parameters_invalid", err.Error(), nil)
	}
	if err := validatePinnedProfileActive(
		ctx, repository.NewAssumptionProfileRepo(s.sql), req.Parameters,
	); err != nil {
		return settingsUpdatePrep{}, newErr("parameters_invalid", err.Error(), nil)
	}

	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return settingsUpdatePrep{}, wrapRepo("list plan holdings", err)
	}
	var allocation *repository.PlanAllocation
	if req.Allocation != nil {
		a := repository.PlanAllocation{
			AssetClassTargets: req.Allocation.AssetClassTargets,
			RegionTargets:     req.Allocation.RegionTargets,
		}
		if err := validateAllocationWeights(a, holds); err != nil {
			return settingsUpdatePrep{}, err
		}
		allocation = &a
	}
	if err := validateParametersAssetsGap(req.Parameters, holds, req.ApplyUnallocatedToCash); err != nil {
		return settingsUpdatePrep{}, err
	}
	return settingsUpdatePrep{
		plan:       plan,
		allocation: allocation,
		holds:      holds,
		gap:        req.Parameters.TotalAssetsMinor - enabledHoldingsSum(holds),
	}, nil
}

func (s *PlanService) applySettingsUpdate(
	ctx context.Context, planID string, req PlanSettingsUpdateRequest, prep settingsUpdatePrep,
) error {
	paramsReq := ParametersUpdateRequest{
		ConfigVersion:          req.ConfigVersion,
		Parameters:             req.Parameters,
		ApplyUnallocatedToCash: req.ApplyUnallocatedToCash,
	}
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if req.Plan != nil {
			if err := applyPlanUpdateTx(ctx, tx, s.plans, prep.plan); err != nil {
				return err
			}
		}
		if prep.allocation != nil {
			if err := applyAllocationUpdateTx(ctx, tx, s.alloc, planID, *prep.allocation); err != nil {
				return err
			}
		}
		if err := applyParametersUpdateTx(ctx, tx, s, planID, paramsReq, prep.gap, prep.holds); err != nil {
			return err
		}
		// Single CAS: concurrent writers that bumped the version after the
		// pre-check make this fail and roll back every write above.
		if _, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion); err != nil {
			return wrapRepo("bump plan version", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return wrapRepo("update plan settings", err)
	}
	return nil
}

func (s *PlanService) loadSettingsResult(
	ctx context.Context, planID string,
) (PlanSettingsUpdateResult, error) {
	detail, err := s.Get(ctx, planID)
	if err != nil {
		return PlanSettingsUpdateResult{}, err
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return PlanSettingsUpdateResult{}, wrapRepo("load plan parameters", err)
	}
	alloc, err := s.alloc.Get(ctx, planID)
	if err != nil {
		return PlanSettingsUpdateResult{}, wrapRepo("load plan allocation", err)
	}
	return PlanSettingsUpdateResult{Plan: detail, Parameters: params, Allocation: alloc}, nil
}
