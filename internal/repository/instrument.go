package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// InstrumentRecord is a row of the internal instruments table. Since the
// FIRE plan chain moved to the global market asset directory, this table only
// holds system rows (FX rates, legacy system cash) and is never exposed as a
// user-facing asset library.
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
	AssetKey           string   `json:"asset_key,omitempty"`
	AdjustPolicy       string   `json:"adjust_policy"`
	InstrumentKind     string   `json:"instrument_kind,omitempty"`
	IsSystem           bool     `json:"is_system"`
	ExpenseRatio       *float64 `json:"expense_ratio,omitempty"`
	ExpenseRatioStatus string   `json:"expense_ratio_status"`
	FeeTreatment       string   `json:"fee_treatment"`
	Status             string   `json:"status"`
	CreatedAt          int64    `json:"created_at"`
	UpdatedAt          int64    `json:"updated_at"`
}

// InstrumentRepo manages the internal system instruments table.
type InstrumentRepo struct {
	db *sql.DB
}

func NewInstrumentRepo(db *sql.DB) *InstrumentRepo {
	return &InstrumentRepo{db: db}
}

func (r *InstrumentRepo) GetByID(ctx context.Context, id string) (InstrumentRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+instrumentBaseColumns+`
		FROM instruments WHERE id=?`, id)
	return scanInstrumentRecord(row)
}

func (r *InstrumentRepo) FindByKey(ctx context.Context, market, instrumentType, code,
	adjustPolicy string,
) (InstrumentRecord, error) {
	return r.findByKey(ctx, r.db, market, instrumentType, code, adjustPolicy)
}

func (r *InstrumentRepo) FindByKeyTx(ctx context.Context, tx *sql.Tx, market, instrumentType, code,
	adjustPolicy string,
) (InstrumentRecord, error) {
	return r.findByKey(ctx, tx, market, instrumentType, code, adjustPolicy)
}

func (r *InstrumentRepo) findByKey(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, market, instrumentType, code, adjustPolicy string,
) (InstrumentRecord, error) {
	row := q.QueryRowContext(ctx, `
		SELECT `+instrumentBaseColumns+`
		FROM instruments
		WHERE market=? AND instrument_type=? AND code=? AND adjust_policy=?`,
		market, instrumentType, code, adjustPolicy)
	return scanInstrumentRecord(row)
}

// Create inserts a system instrument row (test fixtures / migrations only).
func (r *InstrumentRepo) Create(ctx context.Context, tx *sql.Tx, inst InstrumentRecord) error {
	now := time.Now().UnixMilli()
	if inst.CreatedAt == 0 {
		inst.CreatedAt = now
	}
	inst.UpdatedAt = now
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, asset_key, adjust_policy, instrument_kind, is_system,
			expense_ratio, expense_ratio_status, fee_treatment, status,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inst.ID, inst.Code, inst.Name, inst.Market, inst.InstrumentType,
		inst.AssetClass, inst.Region, inst.Currency,
		inst.Provider, inst.ProviderSymbol, inst.AssetKey, inst.AdjustPolicy,
		inst.InstrumentKind, boolToInt(inst.IsSystem),
		inst.ExpenseRatio, inst.ExpenseRatioStatus, inst.FeeTreatment, inst.Status,
		inst.CreatedAt, inst.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create instrument: %w", err)
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

func (r *InstrumentRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

// instrumentBaseColumns is the unaliased instruments column list in the exact
// order scanInstrumentRecord expects.
const instrumentBaseColumns = `id, code, name, market, instrument_type, asset_class, region, currency,
		provider, provider_symbol, asset_key, adjust_policy, instrument_kind, is_system,
		expense_ratio, expense_ratio_status, fee_treatment, status,
		created_at, updated_at`

func scanInstrumentRecord(row *sql.Row) (InstrumentRecord, error) {
	var inst InstrumentRecord
	var isSystem int
	var expenseRatio sql.NullFloat64
	err := row.Scan(
		&inst.ID, &inst.Code, &inst.Name, &inst.Market, &inst.InstrumentType,
		&inst.AssetClass, &inst.Region, &inst.Currency,
		&inst.Provider, &inst.ProviderSymbol, &inst.AssetKey, &inst.AdjustPolicy, &inst.InstrumentKind, &isSystem,
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
