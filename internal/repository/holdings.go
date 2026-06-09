package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrHoldingNotFound = errors.New("holding not found")
var ErrInstrumentNotFound = errors.New("instrument not found")

// HoldingsRepo manages plan holdings.
type HoldingsRepo struct {
	db *sql.DB
}

func NewHoldingsRepo(db *sql.DB) *HoldingsRepo {
	return &HoldingsRepo{db: db}
}

func (r *HoldingsRepo) ListByPlan(ctx context.Context, planID string) ([]PlanHolding, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT h.id, h.plan_id, h.instrument_id, h.enabled, h.asset_class, h.region,
			h.weight_within_group, h.current_amount_minor, h.simulation_snapshot_id,
			h.sort_order, h.created_at, h.updated_at,
			i.code, i.name
		FROM plan_holdings h
		JOIN instruments i ON i.id = h.instrument_id
		WHERE h.plan_id=? ORDER BY h.sort_order, h.created_at`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanHoldings(rows)
}

// UpdateCurrentAmountsTx sets current_amount_minor for holdings matched by instrument_id.
func (r *HoldingsRepo) UpdateCurrentAmountsTx(ctx context.Context, tx *sql.Tx, planID string, items []PortfolioSnapshotItem) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	for _, it := range items {
		if _, err := exec.ExecContext(ctx, `
			UPDATE plan_holdings SET current_amount_minor=?, updated_at=?
			WHERE plan_id=? AND instrument_id=?`,
			it.AmountMinor, now, planID, it.InstrumentID); err != nil {
			return fmt.Errorf("update holding amount: %w", err)
		}
	}
	return nil
}

func (r *HoldingsRepo) Replace(ctx context.Context, tx *sql.Tx, planID string, holdings []PlanHolding) error {
	exec := r.exec(tx)
	if _, err := exec.ExecContext(ctx, `DELETE FROM plan_holdings WHERE plan_id=?`, planID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, h := range holdings {
		if h.CreatedAt == 0 {
			h.CreatedAt = now
		}
		h.UpdatedAt = now
		_, err := exec.ExecContext(ctx, `
			INSERT INTO plan_holdings (
				id, plan_id, instrument_id, enabled, asset_class, region,
				weight_within_group, current_amount_minor, simulation_snapshot_id,
				sort_order, created_at, updated_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, planID, h.InstrumentID, boolToInt(h.Enabled), h.AssetClass, h.Region,
			h.WeightWithinGroup, h.CurrentAmountMinor, h.SimulationSnapshotID,
			h.SortOrder, h.CreatedAt, h.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert holding: %w", err)
		}
	}
	return nil
}

func (r *HoldingsRepo) GetByID(ctx context.Context, planID, holdingID string) (PlanHolding, error) {
	var h PlanHolding
	var enabled int
	err := r.db.QueryRowContext(ctx, `
		SELECT h.id, h.plan_id, h.instrument_id, h.enabled, h.asset_class, h.region,
			h.weight_within_group, h.current_amount_minor, h.simulation_snapshot_id,
			h.sort_order, h.created_at, h.updated_at,
			i.code, i.name
		FROM plan_holdings h
		JOIN instruments i ON i.id = h.instrument_id
		WHERE h.plan_id=? AND h.id=?`, planID, holdingID).Scan(
		&h.ID, &h.PlanID, &h.InstrumentID, &enabled,
		&h.AssetClass, &h.Region, &h.WeightWithinGroup, &h.CurrentAmountMinor,
		&h.SimulationSnapshotID, &h.SortOrder, &h.CreatedAt, &h.UpdatedAt,
		&h.InstrumentCode, &h.InstrumentName)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanHolding{}, ErrHoldingNotFound
	}
	if err != nil {
		return PlanHolding{}, err
	}
	h.Enabled = enabled == 1
	return h, nil
}

func (r *HoldingsRepo) UpdateSnapshotID(ctx context.Context, tx *sql.Tx, holdingID, snapshotID string) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE plan_holdings SET simulation_snapshot_id=?, updated_at=? WHERE id=?`,
		snapshotID, now, holdingID)
	return err
}

func (r *HoldingsRepo) GetInstrument(ctx context.Context, instrumentID string) (Instrument, error) {
	var inst Instrument
	var isSystem int
	err := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, market, asset_class, region, currency, is_system
		FROM instruments WHERE id=?`, instrumentID).Scan(
		&inst.ID, &inst.Code, &inst.Name, &inst.Market,
		&inst.AssetClass, &inst.Region, &inst.Currency, &isSystem)
	if errors.Is(err, sql.ErrNoRows) {
		return Instrument{}, ErrInstrumentNotFound
	}
	if err != nil {
		return Instrument{}, err
	}
	inst.IsSystem = isSystem == 1
	return inst, nil
}

func scanHoldings(rows *sql.Rows) ([]PlanHolding, error) {
	var out []PlanHolding
	for rows.Next() {
		var h PlanHolding
		var enabled int
		if err := rows.Scan(&h.ID, &h.PlanID, &h.InstrumentID, &enabled,
			&h.AssetClass, &h.Region, &h.WeightWithinGroup, &h.CurrentAmountMinor,
			&h.SimulationSnapshotID, &h.SortOrder, &h.CreatedAt, &h.UpdatedAt,
			&h.InstrumentCode, &h.InstrumentName); err != nil {
			return nil, err
		}
		h.Enabled = enabled == 1
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *HoldingsRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

// PlanInstrumentReference links a plan holding to its simulation snapshot date.
type PlanInstrumentReference struct {
	PlanID                string `json:"plan_id"`
	PlanName              string `json:"plan_name"`
	SnapshotInclusionDate string `json:"snapshot_inclusion_date"`
}

func (r *HoldingsRepo) ListReferencingPlans(ctx context.Context, instrumentID string) ([]PlanInstrumentReference, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.name, COALESCE(s.inclusion_date, '')
		FROM plan_holdings h
		JOIN plans p ON p.id = h.plan_id
		LEFT JOIN instrument_simulation_snapshots s ON s.id = h.simulation_snapshot_id
		WHERE h.instrument_id=?
		ORDER BY p.name`, instrumentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanInstrumentReference
	for rows.Next() {
		var ref PlanInstrumentReference
		if err := rows.Scan(&ref.PlanID, &ref.PlanName, &ref.SnapshotInclusionDate); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}
