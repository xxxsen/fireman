package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// AllocationRepo manages plan allocation targets.
type AllocationRepo struct {
	db *sql.DB
}

func NewAllocationRepo(db *sql.DB) *AllocationRepo {
	return &AllocationRepo{db: db}
}

func (r *AllocationRepo) Get(ctx context.Context, planID string) (PlanAllocation, error) {
	return r.get(ctx, r.db, planID)
}

// GetTx reads the plan allocation inside an existing transaction.
func (r *AllocationRepo) GetTx(ctx context.Context, tx *sql.Tx, planID string) (PlanAllocation, error) {
	return r.get(ctx, tx, planID)
}

func (r *AllocationRepo) get(ctx context.Context, q rowQuerier, planID string) (PlanAllocation, error) {
	acRows, err := q.QueryContext(ctx, `
		SELECT asset_class, weight FROM plan_asset_class_targets WHERE plan_id=? ORDER BY asset_class`, planID)
	if err != nil {
		return PlanAllocation{}, fmt.Errorf("query asset class targets: %w", err)
	}
	defer func() { _ = acRows.Close() }()
	var alloc PlanAllocation
	for acRows.Next() {
		var t AssetClassTarget
		if err := acRows.Scan(&t.AssetClass, &t.Weight); err != nil {
			return PlanAllocation{}, fmt.Errorf("scan asset class target: %w", err)
		}
		alloc.AssetClassTargets = append(alloc.AssetClassTargets, t)
	}
	if err := acRows.Err(); err != nil {
		return PlanAllocation{}, fmt.Errorf("iterate asset class targets: %w", err)
	}

	regRows, err := q.QueryContext(ctx, `
		SELECT asset_class, region, weight_within_class FROM plan_region_targets
		WHERE plan_id=? ORDER BY asset_class, region`, planID)
	if err != nil {
		return PlanAllocation{}, fmt.Errorf("query region targets: %w", err)
	}
	defer func() { _ = regRows.Close() }()
	for regRows.Next() {
		var t RegionTarget
		if err := regRows.Scan(&t.AssetClass, &t.Region, &t.WeightWithinClass); err != nil {
			return PlanAllocation{}, fmt.Errorf("scan region target: %w", err)
		}
		alloc.RegionTargets = append(alloc.RegionTargets, t)
	}
	if err := regRows.Err(); err != nil {
		return PlanAllocation{}, fmt.Errorf("iterate region targets: %w", err)
	}
	return alloc, nil
}

func (r *AllocationRepo) Replace(ctx context.Context, tx *sql.Tx, planID string, alloc PlanAllocation) error {
	run := func(q string, args ...any) error {
		var e error
		if tx != nil {
			_, e = tx.ExecContext(ctx, q, args...)
		} else {
			_, e = r.db.ExecContext(ctx, q, args...)
		}
		return wrapSQL("exec allocation sql", e)
	}
	if err := run(`DELETE FROM plan_asset_class_targets WHERE plan_id=?`, planID); err != nil {
		return fmt.Errorf("delete asset class targets: %w", err)
	}
	for _, t := range alloc.AssetClassTargets {
		if err := run(
			`INSERT INTO plan_asset_class_targets (plan_id, asset_class, weight) VALUES (?,?,?)`,
			planID, t.AssetClass, t.Weight,
		); err != nil {
			return fmt.Errorf("insert asset class target: %w", err)
		}
	}
	if err := run(`DELETE FROM plan_region_targets WHERE plan_id=?`, planID); err != nil {
		return fmt.Errorf("delete region targets: %w", err)
	}
	for _, t := range alloc.RegionTargets {
		if err := run(
			`INSERT INTO plan_region_targets (plan_id, asset_class, region, weight_within_class) VALUES (?,?,?,?)`,
			planID, t.AssetClass, t.Region, t.WeightWithinClass,
		); err != nil {
			return fmt.Errorf("insert region target: %w", err)
		}
	}
	return nil
}
