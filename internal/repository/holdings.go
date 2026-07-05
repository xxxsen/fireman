package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrHoldingNotFound           = errors.New("holding not found")
	ErrInstrumentNotFound        = errors.New("instrument not found")
	ErrInstrumentVersionConflict = errors.New("instrument version conflict")
)

// HoldingsRepo manages plan holdings.
type HoldingsRepo struct {
	db *sql.DB
}

func NewHoldingsRepo(db *sql.DB) *HoldingsRepo {
	return &HoldingsRepo{db: db}
}

// holdingSelectColumns joins plan_holdings with the market asset directory
// (display symbol/name) and the optional simulation snapshot metadata.
const holdingSelectColumns = `
	SELECT h.id, h.plan_id, h.asset_key, h.enabled, h.asset_class, h.region,
		h.weight_within_group, h.current_amount_minor, h.simulation_snapshot_id,
		h.sort_order, h.created_at, h.updated_at,
		COALESCE(a.symbol, ''), COALESCE(a.name, ''), COALESCE(s.created_at, 0),
		COALESCE(s.complete_year_count, 0), COALESCE(s.monthly_return_count, 0),
		COALESCE(s.history_depth, ''), COALESCE(s.metrics_version, ''),
		COALESCE(s.warnings_json, '[]')
	FROM plan_holdings h
	LEFT JOIN market_assets a ON a.asset_key = h.asset_key
	LEFT JOIN market_asset_simulation_snapshots s ON s.id = h.simulation_snapshot_id`

func (r *HoldingsRepo) ListByPlan(ctx context.Context, planID string) ([]PlanHolding, error) {
	return r.listByPlan(ctx, r.db, planID)
}

// ListByPlanTx reads plan holdings inside an existing transaction so callers
// can keep read-check-write sequences atomic.
func (r *HoldingsRepo) ListByPlanTx(ctx context.Context, tx *sql.Tx, planID string) ([]PlanHolding, error) {
	return r.listByPlan(ctx, tx, planID)
}

func (r *HoldingsRepo) listByPlan(ctx context.Context, q rowQuerier, planID string) ([]PlanHolding, error) {
	rows, err := q.QueryContext(ctx, holdingSelectColumns+`
		WHERE h.plan_id=? ORDER BY h.sort_order, h.created_at`, planID)
	if err != nil {
		return nil, fmt.Errorf("query plan holdings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanHoldings(rows)
}

// UpdateCurrentAmountsTx sets current_amount_minor for holdings matched by asset_key.
func (r *HoldingsRepo) UpdateCurrentAmountsTx(ctx context.Context, tx *sql.Tx, planID string,
	items []PortfolioSnapshotItem,
) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	for _, it := range items {
		if _, err := exec.ExecContext(ctx, `
			UPDATE plan_holdings SET current_amount_minor=?, updated_at=?
			WHERE plan_id=? AND asset_key=?`,
			it.AmountMinor, now, planID, it.AssetKey); err != nil {
			return fmt.Errorf("update holding amount: %w", err)
		}
	}
	return nil
}

func (r *HoldingsRepo) Replace(ctx context.Context, tx *sql.Tx, planID string, holdings []PlanHolding) error {
	exec := r.exec(tx)
	if _, err := exec.ExecContext(ctx, `DELETE FROM plan_holdings WHERE plan_id=?`, planID); err != nil {
		return fmt.Errorf("delete plan holdings: %w", err)
	}
	now := time.Now().UnixMilli()
	for _, h := range holdings {
		if h.CreatedAt == 0 {
			h.CreatedAt = now
		}
		h.UpdatedAt = now
		_, err := exec.ExecContext(ctx, `
			INSERT INTO plan_holdings (
				id, plan_id, asset_key, enabled, asset_class, region,
				weight_within_group, current_amount_minor, simulation_snapshot_id,
				sort_order, created_at, updated_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			h.ID, planID, h.AssetKey, boolToInt(h.Enabled), h.AssetClass, h.Region,
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
	var warningsJSON string
	err := r.db.QueryRowContext(ctx, holdingSelectColumns+`
		WHERE h.plan_id=? AND h.id=?`, planID, holdingID).Scan(
		&h.ID, &h.PlanID, &h.AssetKey, &enabled,
		&h.AssetClass, &h.Region, &h.WeightWithinGroup, &h.CurrentAmountMinor,
		&h.SimulationSnapshotID, &h.SortOrder, &h.CreatedAt, &h.UpdatedAt,
		&h.InstrumentCode, &h.InstrumentName, &h.SimulationSnapshotCreatedAt,
		&h.SnapshotCompleteYearCount, &h.SnapshotMonthlyReturnCount,
		&h.SnapshotHistoryDepth, &h.SnapshotMetricsVersion, &warningsJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanHolding{}, ErrHoldingNotFound
	}
	if err != nil {
		return PlanHolding{}, fmt.Errorf("scan holding: %w", err)
	}
	h.Enabled = enabled == 1
	h.SnapshotWarnings = parseSnapshotWarningsJSON(warningsJSON)
	return h, nil
}

func (r *HoldingsRepo) UpdateSnapshotID(ctx context.Context, tx *sql.Tx, holdingID, snapshotID string) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE plan_holdings SET simulation_snapshot_id=?, updated_at=? WHERE id=?`,
		snapshotID, now, holdingID)
	if err != nil {
		return fmt.Errorf("update holding snapshot id: %w", err)
	}
	return nil
}

func scanHoldings(rows *sql.Rows) ([]PlanHolding, error) {
	var out []PlanHolding
	for rows.Next() {
		var h PlanHolding
		var enabled int
		var warningsJSON string
		if err := rows.Scan(&h.ID, &h.PlanID, &h.AssetKey, &enabled,
			&h.AssetClass, &h.Region, &h.WeightWithinGroup, &h.CurrentAmountMinor,
			&h.SimulationSnapshotID, &h.SortOrder, &h.CreatedAt, &h.UpdatedAt,
			&h.InstrumentCode, &h.InstrumentName, &h.SimulationSnapshotCreatedAt,
			&h.SnapshotCompleteYearCount, &h.SnapshotMonthlyReturnCount,
			&h.SnapshotHistoryDepth, &h.SnapshotMetricsVersion, &warningsJSON); err != nil {
			return nil, fmt.Errorf("scan holding row: %w", err)
		}
		h.Enabled = enabled == 1
		h.SnapshotWarnings = parseSnapshotWarningsJSON(warningsJSON)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate holdings: %w", err)
	}
	return out, nil
}

func parseSnapshotWarningsJSON(raw string) []string {
	var out []string
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func (r *HoldingsRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}
