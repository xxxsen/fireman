package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// InstrumentRecord is the full instruments table row.
type InstrumentRecord struct {
	ID                 string   `json:"id"`
	Code               string   `json:"code"`
	Name               string   `json:"name"`
	Market             string   `json:"market"`
	InstrumentType     string   `json:"instrument_type"`
	AssetClass         string   `json:"asset_class"`
	Region             string   `json:"region"`
	Currency           string   `json:"currency"`
	Provider           string   `json:"provider"`
	ProviderSymbol     string   `json:"provider_symbol"`
	AdjustPolicy       string   `json:"adjust_policy"`
	IsSystem           bool     `json:"is_system"`
	ExpenseRatio       *float64 `json:"expense_ratio,omitempty"`
	ExpenseRatioStatus string   `json:"expense_ratio_status"`
	FeeTreatment       string   `json:"fee_treatment"`
	Status             string   `json:"status"`
	QualityStatus      string   `json:"quality_status,omitempty"`
	DataAsOf           string   `json:"data_as_of,omitempty"`
	DataSourceName     string   `json:"data_source_name,omitempty"`
	PointType          string   `json:"point_type,omitempty"`
	DataStale          bool     `json:"data_stale"`
	StaleWarning       string   `json:"stale_warning,omitempty"`
	CreatedAt          int64    `json:"created_at"`
	UpdatedAt          int64    `json:"updated_at"`
}

// InstrumentRepo manages the asset library.
type InstrumentRepo struct {
	db *sql.DB
}

func NewInstrumentRepo(db *sql.DB) *InstrumentRepo {
	return &InstrumentRepo{db: db}
}

func (r *InstrumentRepo) List(ctx context.Context) ([]InstrumentRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system,
			expense_ratio, expense_ratio_status, fee_treatment, status,
			created_at, updated_at
		FROM instruments
		WHERE provider='akshare' OR is_system=1
		ORDER BY is_system DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("query instruments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanInstrumentRecords(rows)
}

func (r *InstrumentRepo) GetByID(ctx context.Context, id string) (InstrumentRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system,
			expense_ratio, expense_ratio_status, fee_treatment, status,
			created_at, updated_at
		FROM instruments WHERE id=?`, id)
	return scanInstrumentRecord(row)
}

func (r *InstrumentRepo) FindByKey(ctx context.Context, market, instrumentType, code,
	adjustPolicy string,
) (InstrumentRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system,
			expense_ratio, expense_ratio_status, fee_treatment, status,
			created_at, updated_at
		FROM instruments
		WHERE market=? AND instrument_type=? AND code=? AND adjust_policy=?`,
		market, instrumentType, code, adjustPolicy)
	return scanInstrumentRecord(row)
}

func (r *InstrumentRepo) Create(ctx context.Context, tx *sql.Tx, inst InstrumentRecord) error {
	now := time.Now().UnixMilli()
	if inst.CreatedAt == 0 {
		inst.CreatedAt = now
	}
	inst.UpdatedAt = now
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system,
			expense_ratio, expense_ratio_status, fee_treatment, status,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inst.ID, inst.Code, inst.Name, inst.Market, inst.InstrumentType,
		inst.AssetClass, inst.Region, inst.Currency,
		inst.Provider, inst.ProviderSymbol, inst.AdjustPolicy, boolToInt(inst.IsSystem),
		inst.ExpenseRatio, inst.ExpenseRatioStatus, inst.FeeTreatment, inst.Status,
		inst.CreatedAt, inst.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create instrument: %w", err)
	}
	return nil
}

func (r *InstrumentRepo) UpdateStatusTx(ctx context.Context, tx *sql.Tx, id, status string) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `UPDATE instruments SET status=?, updated_at=? WHERE id=?`, status, now, id)
	if err != nil {
		return fmt.Errorf("update instrument status: %w", err)
	}
	return nil
}

func (r *InstrumentRepo) UpdateAfterFetchTx(ctx context.Context, tx *sql.Tx, inst InstrumentRecord) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE instruments SET
			name=?, asset_class=?, region=?, currency=?,
			provider_symbol=?, expense_ratio=?, expense_ratio_status=?,
			fee_treatment=?, status=?, updated_at=?
		WHERE id=?`,
		inst.Name, inst.AssetClass, inst.Region, inst.Currency,
		inst.ProviderSymbol, inst.ExpenseRatio, inst.ExpenseRatioStatus,
		inst.FeeTreatment, inst.Status, now, inst.ID)
	if err != nil {
		return fmt.Errorf("update instrument after fetch: %w", err)
	}
	return nil
}

func (r *InstrumentRepo) TouchUpdated(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := r.exec(tx).ExecContext(ctx, `UPDATE instruments SET updated_at=? WHERE id=?`, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("touch instrument updated_at: %w", err)
	}
	return nil
}

func (r *InstrumentRepo) UpdateNameTx(ctx context.Context, tx *sql.Tx, id, name string) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `UPDATE instruments SET name=?, updated_at=? WHERE id=?`, name, now, id)
	if err != nil {
		return fmt.Errorf("update instrument name: %w", err)
	}
	return nil
}

func (r *InstrumentRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM instruments WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete instrument: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInstrumentNotFound
	}
	return nil
}

func (r *InstrumentRepo) IsReferencedByPlan(ctx context.Context, instrumentID string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plan_holdings WHERE instrument_id=?`, instrumentID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count plan references: %w", err)
	}
	return n > 0, nil
}

func (r *InstrumentRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

func scanInstrumentRecord(row *sql.Row) (InstrumentRecord, error) {
	var inst InstrumentRecord
	var isSystem int
	var expenseRatio sql.NullFloat64
	err := row.Scan(
		&inst.ID, &inst.Code, &inst.Name, &inst.Market, &inst.InstrumentType,
		&inst.AssetClass, &inst.Region, &inst.Currency,
		&inst.Provider, &inst.ProviderSymbol, &inst.AdjustPolicy, &isSystem,
		&expenseRatio, &inst.ExpenseRatioStatus, &inst.FeeTreatment, &inst.Status,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return InstrumentRecord{}, ErrInstrumentNotFound
	}
	if err != nil {
		return InstrumentRecord{}, fmt.Errorf("scan instrument: %w", err)
	}
	inst.IsSystem = isSystem == 1
	if expenseRatio.Valid {
		v := expenseRatio.Float64
		inst.ExpenseRatio = &v
	}
	return inst, nil
}

func scanInstrumentRecords(rows *sql.Rows) ([]InstrumentRecord, error) {
	var out []InstrumentRecord
	for rows.Next() {
		var inst InstrumentRecord
		var isSystem int
		var expenseRatio sql.NullFloat64
		if err := rows.Scan(
			&inst.ID, &inst.Code, &inst.Name, &inst.Market, &inst.InstrumentType,
			&inst.AssetClass, &inst.Region, &inst.Currency,
			&inst.Provider, &inst.ProviderSymbol, &inst.AdjustPolicy, &isSystem,
			&expenseRatio, &inst.ExpenseRatioStatus, &inst.FeeTreatment, &inst.Status,
			&inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan instrument row: %w", err)
		}
		inst.IsSystem = isSystem == 1
		if expenseRatio.Valid {
			v := expenseRatio.Float64
			inst.ExpenseRatio = &v
		}
		out = append(out, inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate instruments: %w", err)
	}
	return out, nil
}
