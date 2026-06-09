package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// AnnualReturnsRepo manages instrument_annual_returns.
type AnnualReturnsRepo struct {
	db *sql.DB
}

func NewAnnualReturnsRepo(db *sql.DB) *AnnualReturnsRepo {
	return &AnnualReturnsRepo{db: db}
}

// AnnualReturnRecord is a persisted annual return row.
type AnnualReturnRecord struct {
	InstrumentID string  `json:"instrument_id"`
	Year         int     `json:"year"`
	AnnualReturn float64 `json:"annual_return"`
	StartDate    string  `json:"start_date"`
	EndDate      string  `json:"end_date"`
	StartValue   float64 `json:"start_value"`
	EndValue     float64 `json:"end_value"`
	Observations int     `json:"observations"`
	IsPartial    bool    `json:"is_partial"`
	InSimulation bool    `json:"in_simulation,omitempty"`
}

func (r *AnnualReturnsRepo) ReplaceAll(ctx context.Context, tx *sql.Tx, instrumentID string, rows []AnnualReturnRecord) error {
	exec := r.exec(tx)
	if _, err := exec.ExecContext(ctx, `DELETE FROM instrument_annual_returns WHERE instrument_id=?`, instrumentID); err != nil {
		return err
	}
	for _, row := range rows {
		_, err := exec.ExecContext(ctx, `
			INSERT INTO instrument_annual_returns (
				instrument_id, year, annual_return, start_date, end_date,
				start_value, end_value, observations, is_partial
			) VALUES (?,?,?,?,?,?,?,?,?)`,
			instrumentID, row.Year, row.AnnualReturn, row.StartDate, row.EndDate,
			row.StartValue, row.EndValue, row.Observations, boolToInt(row.IsPartial))
		if err != nil {
			return fmt.Errorf("insert annual return %d: %w", row.Year, err)
		}
	}
	return nil
}

func (r *AnnualReturnsRepo) ListByInstrument(ctx context.Context, instrumentID string) ([]AnnualReturnRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT instrument_id, year, annual_return, start_date, end_date,
			start_value, end_value, observations, is_partial
		FROM instrument_annual_returns
		WHERE instrument_id=?
		ORDER BY year`, instrumentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnnualReturnRecord
	for rows.Next() {
		var rec AnnualReturnRecord
		var partial int
		if err := rows.Scan(
			&rec.InstrumentID, &rec.Year, &rec.AnnualReturn,
			&rec.StartDate, &rec.EndDate, &rec.StartValue, &rec.EndValue,
			&rec.Observations, &partial,
		); err != nil {
			return nil, err
		}
		rec.IsPartial = partial == 1
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *AnnualReturnsRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}
