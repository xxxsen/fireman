package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

func (r *MarketDataRepo) DeleteAllTx(ctx context.Context, tx *sql.Tx, instrumentID string) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM market_data_points WHERE instrument_id=?`, instrumentID)
	return wrapSQL("delete market data points", err)
}

func (r *MarketDataRepo) UpsertBatch(ctx context.Context, tx *sql.Tx, instrumentID string,
	points []MarketDataPoint,
) error {
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
	return queryCollect(
		ctx, r.db, `
		SELECT instrument_id, trade_date, value, point_type, source_name, fetched_at
		FROM market_data_points
		WHERE instrument_id=?
		ORDER BY trade_date`, []any{instrumentID},
		func(rows *sql.Rows) (MarketDataPoint, error) {
			var p MarketDataPoint
			if err := rows.Scan(
				&p.InstrumentID, &p.TradeDate, &p.Value,
				&p.PointType, &p.SourceName, &p.FetchedAt,
			); err != nil {
				return MarketDataPoint{}, wrapSQL("scan market data point", err)
			}
			return p, nil
		},
		"query market data points", "scan market data point", "iterate market data points",
	)
}

// ListPointsByInstruments returns market data points for many instruments in a
// single grouped query (chunked to respect SQL parameter limits). When sinceDate
// is non-empty only points on or after it are returned, bounding the result for
// trailing-return windows. Points are grouped by instrument id, each ordered by
// trade_date ascending.
func (r *MarketDataRepo) ListPointsByInstruments(
	ctx context.Context, instrumentIDs []string, sinceDate string,
) (map[string][]MarketDataPoint, error) {
	out := make(map[string][]MarketDataPoint, len(instrumentIDs))
	const chunkSize = 400
	for start := 0; start < len(instrumentIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(instrumentIDs) {
			end = len(instrumentIDs)
		}
		chunk := instrumentIDs[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]any, 0, len(chunk)+1)
		for i, id := range chunk {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where := []string{"instrument_id IN (" + strings.Join(placeholders, ",") + ")"}
		if sinceDate != "" {
			where = append(where, "trade_date >= ?")
			args = append(args, sinceDate)
		}
		whereSQL := strings.Join(where, " AND ")
		//nolint:gosec // G202: whereSQL only joins fixed "?" placeholders; every value is a bound arg.
		rows, err := r.db.QueryContext(ctx, `
			SELECT instrument_id, trade_date, value, point_type, source_name, fetched_at
			FROM market_data_points
			WHERE `+whereSQL+`
			ORDER BY instrument_id, trade_date`, args...)
		if err != nil {
			return nil, wrapSQL("query market data points batch", err)
		}
		err = func() error {
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var p MarketDataPoint
				if err := rows.Scan(
					&p.InstrumentID, &p.TradeDate, &p.Value,
					&p.PointType, &p.SourceName, &p.FetchedAt,
				); err != nil {
					return wrapSQL("scan market data point batch", err)
				}
				out[p.InstrumentID] = append(out[p.InstrumentID], p)
			}
			return wrapSQL("iterate market data points batch", rows.Err())
		}()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
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
		return "", wrapSQL("query last trade date", err)
	}
	if !d.Valid {
		return "", nil
	}
	return d.String, nil
}

// LatestPointMeta returns metadata from the most recent stored observation.
func (r *MarketDataRepo) LatestPointMeta(ctx context.Context, instrumentID string) (string, string, error) {
	var sourceName, pointType string
	err := r.db.QueryRowContext(ctx, `
		SELECT source_name, point_type FROM market_data_points
		WHERE instrument_id=?
		ORDER BY trade_date DESC LIMIT 1`, instrumentID).Scan(&sourceName, &pointType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return sourceName, pointType, wrapSQL("scan latest point meta", err)
}

func (r *MarketDataRepo) LastFetchedAt(ctx context.Context, instrumentID string) (int64, error) {
	var ts sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(fetched_at) FROM market_data_points WHERE instrument_id=?`, instrumentID).Scan(&ts)
	if err != nil {
		return 0, wrapSQL("query last fetched at", err)
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
