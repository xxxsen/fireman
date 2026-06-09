package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// MarketDataRepo persists market_data_points.
type MarketDataRepo struct {
	db *sql.DB
}

func NewMarketDataRepo(db *sql.DB) *MarketDataRepo {
	return &MarketDataRepo{db: db}
}

// MarketDataPoint is one stored observation.
type MarketDataPoint struct {
	InstrumentID string
	TradeDate    string
	Value        float64
	PointType    string
	SourceName   string
	FetchedAt    int64
}

func (r *MarketDataRepo) UpsertBatch(ctx context.Context, tx *sql.Tx, instrumentID string, points []MarketDataPoint) error {
	exec := r.exec(tx)
	for _, p := range points {
		_, err := exec.ExecContext(ctx, `
			INSERT INTO market_data_points (instrument_id, trade_date, value, point_type, source_name, fetched_at)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(instrument_id, trade_date) DO UPDATE SET
				value=excluded.value,
				point_type=excluded.point_type,
				source_name=excluded.source_name,
				fetched_at=excluded.fetched_at`,
			instrumentID, p.TradeDate, p.Value, p.PointType, p.SourceName, p.FetchedAt)
		if err != nil {
			return fmt.Errorf("upsert point %s: %w", p.TradeDate, err)
		}
	}
	return nil
}

func (r *MarketDataRepo) ListByInstrument(ctx context.Context, instrumentID string) ([]MarketDataPoint, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT instrument_id, trade_date, value, point_type, source_name, fetched_at
		FROM market_data_points
		WHERE instrument_id=?
		ORDER BY trade_date`, instrumentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MarketDataPoint
	for rows.Next() {
		var p MarketDataPoint
		if err := rows.Scan(&p.InstrumentID, &p.TradeDate, &p.Value, &p.PointType, &p.SourceName, &p.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *MarketDataRepo) LastTradeDate(ctx context.Context, instrumentID string) (string, error) {
	var d sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT trade_date FROM market_data_points
		WHERE instrument_id=?
		ORDER BY trade_date DESC LIMIT 1`, instrumentID).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !d.Valid {
		return "", nil
	}
	return d.String, nil
}

func (r *MarketDataRepo) LastFetchedAt(ctx context.Context, instrumentID string) (int64, error) {
	var ts sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(fetched_at) FROM market_data_points WHERE instrument_id=?`, instrumentID).Scan(&ts)
	if err != nil {
		return 0, err
	}
	if !ts.Valid {
		return 0, nil
	}
	return ts.Int64, nil
}

func (r *MarketDataRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}
