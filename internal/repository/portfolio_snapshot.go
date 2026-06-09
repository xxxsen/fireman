package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PortfolioSnapshotRepo manages portfolio snapshots.
type PortfolioSnapshotRepo struct {
	db *sql.DB
}

func NewPortfolioSnapshotRepo(db *sql.DB) *PortfolioSnapshotRepo {
	return &PortfolioSnapshotRepo{db: db}
}

func (r *PortfolioSnapshotRepo) Create(ctx context.Context, snap PortfolioSnapshot) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.createTx(ctx, tx, snap); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateTx inserts a portfolio snapshot inside an existing transaction.
func (r *PortfolioSnapshotRepo) CreateTx(ctx context.Context, tx *sql.Tx, snap PortfolioSnapshot) error {
	return r.createTx(ctx, tx, snap)
}

func (r *PortfolioSnapshotRepo) createTx(ctx context.Context, tx *sql.Tx, snap PortfolioSnapshot) error {
	now := time.Now().UnixMilli()
	if snap.CreatedAt == 0 {
		snap.CreatedAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO portfolio_snapshots (id, plan_id, snapshot_date, total_amount_minor, note, created_at)
		VALUES (?,?,?,?,?,?)`,
		snap.ID, snap.PlanID, snap.SnapshotDate, snap.TotalAmountMinor, snap.Note, snap.CreatedAt); err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}
	for _, item := range snap.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO portfolio_snapshot_items (snapshot_id, instrument_id, amount_minor) VALUES (?,?,?)`,
			snap.ID, item.InstrumentID, item.AmountMinor); err != nil {
			return err
		}
	}
	return nil
}

func (r *PortfolioSnapshotRepo) ListByPlan(ctx context.Context, planID string) ([]PortfolioSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, snapshot_date, total_amount_minor, note, created_at
		FROM portfolio_snapshots WHERE plan_id=? ORDER BY created_at DESC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortfolioSnapshot
	for rows.Next() {
		var s PortfolioSnapshot
		if err := rows.Scan(&s.ID, &s.PlanID, &s.SnapshotDate, &s.TotalAmountMinor, &s.Note, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
