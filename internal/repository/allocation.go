package repository

import (
	"context"
	"database/sql"
)

// AllocationRepo manages plan allocation targets.
type AllocationRepo struct {
	db *sql.DB
}

func NewAllocationRepo(db *sql.DB) *AllocationRepo {
	return &AllocationRepo{db: db}
}

func (r *AllocationRepo) Get(ctx context.Context, planID string) (PlanAllocation, error) {
	acRows, err := r.db.QueryContext(ctx, `
		SELECT asset_class, weight FROM plan_asset_class_targets WHERE plan_id=? ORDER BY asset_class`, planID)
	if err != nil {
		return PlanAllocation{}, err
	}
	defer acRows.Close()
	var alloc PlanAllocation
	for acRows.Next() {
		var t AssetClassTarget
		if err := acRows.Scan(&t.AssetClass, &t.Weight); err != nil {
			return PlanAllocation{}, err
		}
		alloc.AssetClassTargets = append(alloc.AssetClassTargets, t)
	}
	if err := acRows.Err(); err != nil {
		return PlanAllocation{}, err
	}

	regRows, err := r.db.QueryContext(ctx, `
		SELECT asset_class, region, weight_within_class FROM plan_region_targets
		WHERE plan_id=? ORDER BY asset_class, region`, planID)
	if err != nil {
		return PlanAllocation{}, err
	}
	defer regRows.Close()
	for regRows.Next() {
		var t RegionTarget
		if err := regRows.Scan(&t.AssetClass, &t.Region, &t.WeightWithinClass); err != nil {
			return PlanAllocation{}, err
		}
		alloc.RegionTargets = append(alloc.RegionTargets, t)
	}
	return alloc, regRows.Err()
}

func (r *AllocationRepo) Replace(ctx context.Context, tx *sql.Tx, planID string, alloc PlanAllocation) error {
	run := func(q string, args ...any) error {
		if tx != nil {
			_, e := tx.ExecContext(ctx, q, args...)
			return e
		}
		_, e := r.db.ExecContext(ctx, q, args...)
		return e
	}
	if err := run(`DELETE FROM plan_asset_class_targets WHERE plan_id=?`, planID); err != nil {
		return err
	}
	for _, t := range alloc.AssetClassTargets {
		if err := run(`INSERT INTO plan_asset_class_targets (plan_id, asset_class, weight) VALUES (?,?,?)`,
			planID, t.AssetClass, t.Weight); err != nil {
			return err
		}
	}
	if err := run(`DELETE FROM plan_region_targets WHERE plan_id=?`, planID); err != nil {
		return err
	}
	for _, t := range alloc.RegionTargets {
		if err := run(`INSERT INTO plan_region_targets (plan_id, asset_class, region, weight_within_class) VALUES (?,?,?,?)`,
			planID, t.AssetClass, t.Region, t.WeightWithinClass); err != nil {
			return err
		}
	}
	return nil
}
