package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrPlanNotFound    = errors.New("plan not found")
	ErrVersionConflict = errors.New("plan version conflict")
)

// PlanRepo provides plan CRUD.
type PlanRepo struct {
	db *sql.DB
}

func NewPlanRepo(db *sql.DB) *PlanRepo {
	return &PlanRepo{db: db}
}

func (r *PlanRepo) Create(ctx context.Context, p Plan) error {
	now := time.Now().UnixMilli()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	if p.UpdatedAt == 0 {
		p.UpdatedAt = now
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.ConfigVersion == 0 {
		p.ConfigVersion = 1
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO plans (id, name, base_currency, valuation_date, status, config_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.BaseCurrency, p.ValuationDate, p.Status, p.ConfigVersion, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	return nil
}

// CreateTx inserts a plan inside an existing transaction.
func (r *PlanRepo) CreateTx(ctx context.Context, tx *sql.Tx, p Plan) error {
	now := time.Now().UnixMilli()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	if p.UpdatedAt == 0 {
		p.UpdatedAt = now
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.ConfigVersion == 0 {
		p.ConfigVersion = 1
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO plans (id, name, base_currency, valuation_date, status, config_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.BaseCurrency, p.ValuationDate, p.Status, p.ConfigVersion, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	return nil
}

func (r *PlanRepo) List(ctx context.Context) ([]Plan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, base_currency, valuation_date, status, config_version, created_at, updated_at
		FROM plans ORDER BY updated_at DESC`)
	if err != nil {
		return nil, wrapSQL("list plans", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Plan
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseCurrency, &p.ValuationDate, &p.Status,
			&p.ConfigVersion, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, wrapSQL("scan plan row", err)
		}
		out = append(out, p)
	}
	return out, wrapSQL("iterate plans", rows.Err())
}

func (r *PlanRepo) GetByID(ctx context.Context, id string) (Plan, error) {
	var p Plan
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name, base_currency, valuation_date, status, config_version, created_at, updated_at
		FROM plans WHERE id = ?`, id).Scan(
		&p.ID, &p.Name, &p.BaseCurrency, &p.ValuationDate, &p.Status,
		&p.ConfigVersion, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return Plan{}, wrapSQL("scan plan", err)
	}
	return p, nil
}

func (r *PlanRepo) Update(ctx context.Context, p Plan, expectedVersion int) error {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE plans SET name=?, base_currency=?, valuation_date=?, status=?, config_version=?, updated_at=?
		WHERE id=? AND config_version=?`,
		p.Name, p.BaseCurrency, p.ValuationDate, p.Status, expectedVersion+1, now, p.ID, expectedVersion)
	if err != nil {
		return wrapSQL("update plan", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if _, err := r.GetByID(ctx, p.ID); errors.Is(err, ErrPlanNotFound) {
			return ErrPlanNotFound
		}
		return ErrVersionConflict
	}
	return nil
}

func (r *PlanRepo) BumpVersion(ctx context.Context, planID string, expectedVersion int) (int, error) {
	return r.bumpVersion(ctx, r.db, planID, expectedVersion)
}

// BumpVersionTx bumps config_version inside an existing transaction.
func (r *PlanRepo) BumpVersionTx(ctx context.Context, tx *sql.Tx, planID string, expectedVersion int) (int, error) {
	return r.bumpVersion(ctx, tx, planID, expectedVersion)
}

func (r *PlanRepo) bumpVersion(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, planID string, expectedVersion int,
) (int, error) {
	now := time.Now().UnixMilli()
	newVersion := expectedVersion + 1
	res, err := exec.ExecContext(ctx, `
		UPDATE plans SET config_version=?, updated_at=? WHERE id=? AND config_version=?`,
		newVersion, now, planID, expectedVersion)
	if err != nil {
		return 0, wrapSQL("bump plan version", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if _, err := r.GetByID(ctx, planID); errors.Is(err, ErrPlanNotFound) {
			return 0, ErrPlanNotFound
		}
		return 0, ErrVersionConflict
	}
	return newVersion, nil
}

func (r *PlanRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM plans WHERE id=?`, id)
	if err != nil {
		return wrapSQL("update plan", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrPlanNotFound
	}
	return nil
}
