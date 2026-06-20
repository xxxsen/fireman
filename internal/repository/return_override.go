package repository

import (
	"context"
	"database/sql"
	"time"
)

// PlanReturnOverride is an asset-level plan-specific override of the forward
// geometric return and/or volatility (td/061 §4.1.5). Only forward-looking
// values may be overridden; historical facts, correlation priors and the FX
// common factor are never affected. ForwardReturn / AnnualVolatility are nil
// when that dimension is not overridden.
type PlanReturnOverride struct {
	PlanID           string
	InstrumentID     string
	ForwardReturn    *float64
	AnnualVolatility *float64
	Reason           string
	ExpiresAt        string
	CreatedAt        int64
	UpdatedAt        int64
}

// ReturnOverrideRepo persists per-plan, per-instrument return overrides.
type ReturnOverrideRepo struct {
	db *sql.DB
}

func NewReturnOverrideRepo(db *sql.DB) *ReturnOverrideRepo {
	return &ReturnOverrideRepo{db: db}
}

// ListByPlan returns all overrides configured for a plan, ordered by instrument.
func (r *ReturnOverrideRepo) ListByPlan(ctx context.Context, planID string) ([]PlanReturnOverride, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT plan_id, instrument_id, forward_return, annual_volatility,
		       reason, expires_at, created_at, updated_at
		FROM plan_return_assumption_overrides
		WHERE plan_id=? ORDER BY instrument_id`, planID)
	if err != nil {
		return nil, wrapSQL("list return overrides", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PlanReturnOverride
	for rows.Next() {
		var o PlanReturnOverride
		if err := rows.Scan(&o.PlanID, &o.InstrumentID, &o.ForwardReturn, &o.AnnualVolatility,
			&o.Reason, &o.ExpiresAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, wrapSQL("scan return override", err)
		}
		out = append(out, o)
	}
	return out, wrapSQL("iterate return overrides", rows.Err())
}

// Upsert inserts or replaces an override, preserving created_at on update.
func (r *ReturnOverrideRepo) Upsert(ctx context.Context, tx *sql.Tx, o PlanReturnOverride) error {
	exec := func(q string, args ...any) error {
		var e error
		if tx != nil {
			_, e = tx.ExecContext(ctx, q, args...)
		} else {
			_, e = r.db.ExecContext(ctx, q, args...)
		}
		return wrapSQL("exec return override sql", e)
	}
	now := time.Now().UnixMilli()
	if o.CreatedAt == 0 {
		o.CreatedAt = now
	}
	err := exec(`
		INSERT INTO plan_return_assumption_overrides (
			plan_id, instrument_id, forward_return, annual_volatility,
			reason, expires_at, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(plan_id, instrument_id) DO UPDATE SET
			forward_return=excluded.forward_return,
			annual_volatility=excluded.annual_volatility,
			reason=excluded.reason,
			expires_at=excluded.expires_at,
			updated_at=excluded.updated_at`,
		o.PlanID, o.InstrumentID, o.ForwardReturn, o.AnnualVolatility,
		o.Reason, o.ExpiresAt, o.CreatedAt, now)
	return err
}

// Delete removes an override; deleting a missing override is a no-op.
func (r *ReturnOverrideRepo) Delete(ctx context.Context, tx *sql.Tx, planID, instrumentID string) error {
	exec := func(q string, args ...any) error {
		var e error
		if tx != nil {
			_, e = tx.ExecContext(ctx, q, args...)
		} else {
			_, e = r.db.ExecContext(ctx, q, args...)
		}
		return wrapSQL("exec return override sql", e)
	}
	return exec(`DELETE FROM plan_return_assumption_overrides WHERE plan_id=? AND instrument_id=?`,
		planID, instrumentID)
}
