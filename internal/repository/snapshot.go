package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	SystemCashInstrumentID = "system_cash_cny"
	SystemCashSnapshotID   = "sim_snapshot_system_cash_cny"
)

// SnapshotRepo creates instrument simulation snapshots.
type SnapshotRepo struct {
	db *sql.DB
}

func NewSnapshotRepo(db *sql.DB) *SnapshotRepo {
	return &SnapshotRepo{db: db}
}

// CreatePlanSnapshot inserts a plan-specific simulation snapshot.
func (r *SnapshotRepo) CreatePlanSnapshot(ctx context.Context, tx *sql.Tx, snap SimulationSnapshot) error {
	run := func(q string, args ...any) error {
		var e error
		if tx != nil {
			_, e = tx.ExecContext(ctx, q, args...)
		} else {
			_, e = r.db.ExecContext(ctx, q, args...)
		}
		return wrapSQL("exec snapshot sql", e)
	}
	now := time.Now().UnixMilli()
	if snap.CreatedAt == 0 {
		snap.CreatedAt = now
	}
	if err := run(`
		INSERT INTO instrument_simulation_snapshots (
			id, instrument_id, plan_id, inclusion_date, as_of_date,
			window_start, window_end, complete_year_start, complete_year_end,
			complete_year_count, daily_observation_count, monthly_return_count,
			volatility_method, metrics_version, history_depth,
			historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
			expense_ratio, expense_ratio_status, fee_treatment,
			source_mode, quality_status, warnings_json, source_hash, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		snap.ID, snap.InstrumentID, snap.PlanID, snap.InclusionDate, snap.AsOfDate,
		snap.WindowStart, snap.WindowEnd, snap.CompleteYearStart, snap.CompleteYearEnd,
		snap.CompleteYearCount, snap.DailyObservationCount, snap.MonthlyReturnCount,
		snap.VolatilityMethod, snap.MetricsVersion, snap.HistoryDepth,
		snap.HistoricalCAGR, snap.ModeledAnnualReturn, snap.AnnualVolatility, snap.MaxDrawdown,
		snap.ExpenseRatio, snap.ExpenseRatioStatus, snap.FeeTreatment,
		snap.SourceMode, snap.QualityStatus, snap.WarningsJSON, snap.SourceHash, snap.CreatedAt); err != nil {
		return wrapSQL("insert simulation snapshot", err)
	}
	if err := r.replaceSnapshotYears(ctx, tx, snap.ID, snap.Years); err != nil {
		return err
	}
	return r.replaceSnapshotMonths(ctx, tx, snap.ID, snap.Months)
}

func (r *SnapshotRepo) replaceSnapshotMonths(ctx context.Context, tx *sql.Tx, snapshotID string,
	months []SnapshotMonth,
) error {
	run := func(q string, args ...any) error {
		var e error
		if tx != nil {
			_, e = tx.ExecContext(ctx, q, args...)
		} else {
			_, e = r.db.ExecContext(ctx, q, args...)
		}
		return wrapSQL("exec snapshot sql", e)
	}
	if err := run(`DELETE FROM instrument_simulation_snapshot_months WHERE snapshot_id=?`, snapshotID); err != nil {
		return wrapSQL("delete snapshot months", err)
	}
	for _, m := range months {
		if err := run(`
			INSERT INTO instrument_simulation_snapshot_months (
				snapshot_id, year, month, log_return
			) VALUES (?,?,?,?)`,
			snapshotID, m.Year, m.Month, m.LogReturn); err != nil {
			return fmt.Errorf("insert snapshot month %d-%02d: %w", m.Year, m.Month, err)
		}
	}
	return nil
}

// ListSnapshotMonths returns the frozen monthly log-return series for a snapshot
// (td/061 §4.1.6), ordered chronologically. It is loaded on demand (not by
// GetByID) so only the joint factor model build pays for it.
//
//nolint:dupl // generic query/scan loop shared with other simple list readers
func (r *SnapshotRepo) ListSnapshotMonths(ctx context.Context, snapshotID string) ([]SnapshotMonth, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT year, month, log_return
		FROM instrument_simulation_snapshot_months
		WHERE snapshot_id=? ORDER BY year, month`, snapshotID)
	if err != nil {
		return nil, wrapSQL("list snapshot months", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SnapshotMonth
	for rows.Next() {
		var m SnapshotMonth
		if err := rows.Scan(&m.Year, &m.Month, &m.LogReturn); err != nil {
			return nil, wrapSQL("scan snapshot month", err)
		}
		out = append(out, m)
	}
	return out, wrapSQL("iterate snapshot months", rows.Err())
}

func (r *SnapshotRepo) replaceSnapshotYears(ctx context.Context, tx *sql.Tx, snapshotID string,
	years []SnapshotYear,
) error {
	run := func(q string, args ...any) error {
		var e error
		if tx != nil {
			_, e = tx.ExecContext(ctx, q, args...)
		} else {
			_, e = r.db.ExecContext(ctx, q, args...)
		}
		return wrapSQL("exec snapshot sql", e)
	}
	if err := run(`DELETE FROM instrument_simulation_snapshot_years WHERE snapshot_id=?`, snapshotID); err != nil {
		return wrapSQL("delete snapshot years", err)
	}
	for _, y := range years {
		if err := run(`
			INSERT INTO instrument_simulation_snapshot_years (
				snapshot_id, year, annual_return, start_date, end_date, observations
			) VALUES (?,?,?,?,?,?)`,
			snapshotID, y.Year, y.AnnualReturn, y.StartDate, y.EndDate, y.Observations); err != nil {
			return fmt.Errorf("insert snapshot year %d: %w", y.Year, err)
		}
	}
	return nil
}

// SimulationSnapshot is a row in instrument_simulation_snapshots.
type SimulationSnapshot struct {
	ID                    string         `json:"id"`
	InstrumentID          string         `json:"instrument_id"`
	PlanID                *string        `json:"plan_id,omitempty"`
	InclusionDate         string         `json:"inclusion_date"`
	AsOfDate              string         `json:"as_of_date"`
	WindowStart           *string        `json:"window_start,omitempty"`
	WindowEnd             *string        `json:"window_end,omitempty"`
	CompleteYearStart     *int           `json:"complete_year_start,omitempty"`
	CompleteYearEnd       *int           `json:"complete_year_end,omitempty"`
	CompleteYearCount     int            `json:"complete_year_count"`
	DailyObservationCount int            `json:"daily_observation_count"`
	MonthlyReturnCount    int            `json:"monthly_return_count"`
	VolatilityMethod      string         `json:"volatility_method"`
	MetricsVersion        string         `json:"metrics_version"`
	HistoryDepth          string         `json:"history_depth"`
	HistoricalCAGR        float64        `json:"historical_cagr"`
	ModeledAnnualReturn   float64        `json:"modeled_annual_return"`
	AnnualVolatility      float64        `json:"annual_volatility"`
	MaxDrawdown           float64        `json:"max_drawdown"`
	ExpenseRatio          *float64       `json:"expense_ratio,omitempty"`
	ExpenseRatioStatus    string         `json:"expense_ratio_status"`
	FeeTreatment          string         `json:"fee_treatment"`
	SourceMode            string         `json:"source_mode"`
	QualityStatus         string         `json:"quality_status"`
	WarningsJSON          string         `json:"warnings_json"`
	SourceHash            string         `json:"source_hash"`
	CreatedAt             int64          `json:"created_at"`
	Years                 []SnapshotYear `json:"years,omitempty"`
	// Months is the frozen monthly log-return series; populated only when the
	// caller explicitly loads it (joint factor model build), never by GetByID.
	Months []SnapshotMonth `json:"months,omitempty"`
}

// SnapshotYear is one row in instrument_simulation_snapshot_years.
type SnapshotYear struct {
	Year         int     `json:"year"`
	AnnualReturn float64 `json:"annual_return"`
	StartDate    string  `json:"start_date"`
	EndDate      string  `json:"end_date"`
	Observations int     `json:"observations"`
}

// SnapshotMonth is one row in instrument_simulation_snapshot_months.
type SnapshotMonth struct {
	Year      int     `json:"year"`
	Month     int     `json:"month"`
	LogReturn float64 `json:"log_return"`
}

func (r *SnapshotRepo) GetByID(ctx context.Context, id string) (SimulationSnapshot, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, instrument_id, plan_id, inclusion_date, as_of_date,
			window_start, window_end, complete_year_start, complete_year_end,
			complete_year_count, daily_observation_count, monthly_return_count,
			volatility_method, metrics_version, history_depth,
			historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
			expense_ratio, expense_ratio_status, fee_treatment,
			source_mode, quality_status, warnings_json, source_hash, created_at
		FROM instrument_simulation_snapshots WHERE id=?`, id)
	snap, err := scanSnapshot(row)
	if err != nil {
		return SimulationSnapshot{}, err
	}
	years, err := r.listYears(ctx, id)
	if err != nil {
		return SimulationSnapshot{}, err
	}
	snap.Years = years
	return snap, nil
}

func (r *SnapshotRepo) listYears(ctx context.Context, snapshotID string) ([]SnapshotYear, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT year, annual_return, start_date, end_date, observations
		FROM instrument_simulation_snapshot_years
		WHERE snapshot_id=? ORDER BY year`, snapshotID)
	if err != nil {
		return nil, wrapSQL("list snapshot years", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SnapshotYear
	for rows.Next() {
		var y SnapshotYear
		if err := rows.Scan(&y.Year, &y.AnnualReturn, &y.StartDate, &y.EndDate, &y.Observations); err != nil {
			return nil, wrapSQL("scan snapshot year", err)
		}
		out = append(out, y)
	}
	return out, wrapSQL("iterate snapshot years", rows.Err())
}

func scanSnapshot(row *sql.Row) (SimulationSnapshot, error) {
	var snap SimulationSnapshot
	var planID sql.NullString
	var windowStart, windowEnd sql.NullString
	var yearStart, yearEnd sql.NullInt64
	var expenseRatio sql.NullFloat64
	err := row.Scan(
		&snap.ID, &snap.InstrumentID, &planID, &snap.InclusionDate, &snap.AsOfDate,
		&windowStart, &windowEnd, &yearStart, &yearEnd,
		&snap.CompleteYearCount, &snap.DailyObservationCount, &snap.MonthlyReturnCount,
		&snap.VolatilityMethod, &snap.MetricsVersion, &snap.HistoryDepth,
		&snap.HistoricalCAGR, &snap.ModeledAnnualReturn, &snap.AnnualVolatility, &snap.MaxDrawdown,
		&expenseRatio, &snap.ExpenseRatioStatus, &snap.FeeTreatment,
		&snap.SourceMode, &snap.QualityStatus, &snap.WarningsJSON, &snap.SourceHash, &snap.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SimulationSnapshot{}, ErrSnapshotNotFound
	}
	if err != nil {
		return SimulationSnapshot{}, wrapSQL("scan snapshot", err)
	}
	if planID.Valid {
		v := planID.String
		snap.PlanID = &v
	}
	if windowStart.Valid {
		v := windowStart.String
		snap.WindowStart = &v
	}
	if windowEnd.Valid {
		v := windowEnd.String
		snap.WindowEnd = &v
	}
	if yearStart.Valid {
		v := int(yearStart.Int64)
		snap.CompleteYearStart = &v
	}
	if yearEnd.Valid {
		v := int(yearEnd.Int64)
		snap.CompleteYearEnd = &v
	}
	if expenseRatio.Valid {
		v := expenseRatio.Float64
		snap.ExpenseRatio = &v
	}
	return snap, nil
}

var ErrSnapshotNotFound = errors.New("snapshot not found")

func (r *SnapshotRepo) GetSystemCashSnapshotID() string {
	return SystemCashSnapshotID
}

// ListByInstrument returns plan-specific snapshots for an instrument, newest first.
func (r *SnapshotRepo) ListByInstrument(ctx context.Context, instrumentID string) ([]SimulationSnapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, instrument_id, plan_id, inclusion_date, as_of_date,
			window_start, window_end, complete_year_start, complete_year_end,
			complete_year_count, daily_observation_count, monthly_return_count,
			volatility_method, metrics_version, history_depth,
			historical_cagr, modeled_annual_return, annual_volatility, max_drawdown,
			expense_ratio, expense_ratio_status, fee_treatment,
			source_mode, quality_status, warnings_json, source_hash, created_at
		FROM instrument_simulation_snapshots
		WHERE instrument_id=? AND plan_id IS NOT NULL
		ORDER BY created_at DESC LIMIT 50`, instrumentID)
	if err != nil {
		return nil, wrapSQL("list snapshots by instrument", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SimulationSnapshot
	for rows.Next() {
		var snap SimulationSnapshot
		var planID sql.NullString
		var windowStart, windowEnd sql.NullString
		var yearStart, yearEnd sql.NullInt64
		var expenseRatio sql.NullFloat64
		if err := rows.Scan(
			&snap.ID, &snap.InstrumentID, &planID, &snap.InclusionDate, &snap.AsOfDate,
			&windowStart, &windowEnd, &yearStart, &yearEnd,
			&snap.CompleteYearCount, &snap.DailyObservationCount, &snap.MonthlyReturnCount,
			&snap.VolatilityMethod, &snap.MetricsVersion, &snap.HistoryDepth,
			&snap.HistoricalCAGR, &snap.ModeledAnnualReturn, &snap.AnnualVolatility, &snap.MaxDrawdown,
			&expenseRatio, &snap.ExpenseRatioStatus, &snap.FeeTreatment,
			&snap.SourceMode, &snap.QualityStatus, &snap.WarningsJSON, &snap.SourceHash, &snap.CreatedAt,
		); err != nil {
			return nil, wrapSQL("scan snapshot list row", err)
		}
		if planID.Valid {
			v := planID.String
			snap.PlanID = &v
		}
		if windowStart.Valid {
			v := windowStart.String
			snap.WindowStart = &v
		}
		if windowEnd.Valid {
			v := windowEnd.String
			snap.WindowEnd = &v
		}
		if yearStart.Valid {
			v := int(yearStart.Int64)
			snap.CompleteYearStart = &v
		}
		if yearEnd.Valid {
			v := int(yearEnd.Int64)
			snap.CompleteYearEnd = &v
		}
		if expenseRatio.Valid {
			v := expenseRatio.Float64
			snap.ExpenseRatio = &v
		}
		out = append(out, snap)
	}
	return out, wrapSQL("iterate snapshots by instrument", rows.Err())
}

// WarningsToJSON serializes warning strings.
func WarningsToJSON(warnings []string) string {
	if len(warnings) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(warnings)
	return string(b)
}

// EnsureInstrumentExists is a test helper to insert a minimal instrument.
func (r *SnapshotRepo) EnsureInstrument(ctx context.Context, inst Instrument) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO instruments (
			id, code, name, market, instrument_type, asset_class, region, currency,
			provider, provider_symbol, adjust_policy, is_system,
			expense_ratio_status, fee_treatment, status, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inst.ID, inst.Code, inst.Name, inst.Market, "test_fixture",
		inst.AssetClass, inst.Region, inst.Currency,
		"fixture", inst.Code, "none", boolToInt(inst.IsSystem),
		"unavailable", "embedded", "active", now, now)
	if err != nil {
		return fmt.Errorf("ensure instrument: %w", err)
	}
	return nil
}
