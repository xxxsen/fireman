package service

import (
	"context"
	"database/sql"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// applyPlanUpdateTx writes plan metadata fields inside an existing transaction
// without bumping config_version; callers bump the version once per save.
func applyPlanUpdateTx(ctx context.Context, tx *sql.Tx, plans *repository.PlanRepo, plan repository.Plan) error {
	return wrapRepo("update plan fields", plans.UpdateFieldsTx(ctx, tx, plan))
}

// applyAllocationUpdateTx replaces the plan allocation targets inside an
// existing transaction without bumping config_version.
func applyAllocationUpdateTx(
	ctx context.Context,
	tx *sql.Tx,
	alloc *repository.AllocationRepo,
	planID string,
	allocation repository.PlanAllocation,
) error {
	return wrapRepo("replace plan allocation", alloc.Replace(ctx, tx, planID, allocation))
}

// applyParametersUpdateTx applies the gap-to-cash sweep and the parameters
// upsert inside an existing transaction without bumping config_version.
func applyParametersUpdateTx(
	ctx context.Context,
	tx *sql.Tx,
	s *PlanService,
	planID string,
	req ParametersUpdateRequest,
	gap int64,
	holds []repository.PlanHolding,
) error {
	if req.ApplyUnallocatedToCash && gap > 100 {
		if err := applyUnallocatedGapToCashTx(ctx, tx, s.holdings, planID, holds, gap); err != nil {
			return wrapRepo("apply unallocated gap to cash", err)
		}
	}
	return wrapRepo("upsert plan parameters", s.params.Upsert(ctx, tx, req.Parameters))
}

func applyParametersUpdateInTx(
	ctx context.Context,
	s *PlanService,
	planID string,
	req ParametersUpdateRequest,
	gap int64,
	holds []repository.PlanHolding,
) error {
	return wrapRepo("update plan parameters", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := applyParametersUpdateTx(ctx, tx, s, planID, req, gap, holds); err != nil {
			return err
		}
		if _, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion); err != nil {
			return wrapRepo("bump plan version", err)
		}
		return nil
	}))
}
