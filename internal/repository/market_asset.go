package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrMarketAssetNotFound is returned when an asset_key has no row.
var ErrMarketAssetNotFound = errors.New("market asset not found")

// MarketAsset mirrors a market_assets row (global asset directory).
type MarketAsset struct {
	AssetKey       string `json:"asset_key"`
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	RegionCode     string `json:"region_code"`
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	Exchange       string `json:"exchange"`
	InstrumentKind string `json:"instrument_kind"`
	Currency       string `json:"currency"`
	Active         bool   `json:"active"`
	ListingStatus  string `json:"listing_status"`
	LastSeenAt     int64  `json:"last_seen_at"`
	SourceName     string `json:"source_name"`
	SourceAsOf     string `json:"source_as_of"`
	RefreshedAt    int64  `json:"refreshed_at"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

// BuildMarketAssetKey derives the canonical asset key from structured fields:
// market|instrument_type|region_code|symbol.
func BuildMarketAssetKey(market, instrumentType, regionCode, symbol string) string {
	return market + "|" + instrumentType + "|" + regionCode + "|" + symbol
}

// MarketAssetSyncState mirrors market_asset_sync_state.
type MarketAssetSyncState struct {
	Scope             string `json:"scope"`
	LastTaskID        string `json:"last_task_id"`
	LastSuccessTaskID string `json:"last_success_task_id"`
	LastSuccessAt     *int64 `json:"last_success_at,omitempty"`
	UpdatedAt         int64  `json:"updated_at"`
}

// MarketAssetPoint is one stored history observation for a market asset.
type MarketAssetPoint struct {
	AssetKey     string  `json:"asset_key"`
	AdjustPolicy string  `json:"adjust_policy"`
	PointType    string  `json:"point_type"`
	TradeDate    string  `json:"trade_date"`
	Value        float64 `json:"value"`
	SourceName   string  `json:"source_name"`
	FetchedAt    int64   `json:"fetched_at"`
}

// MarketAssetHistoryState mirrors market_asset_history_state.
type MarketAssetHistoryState struct {
	AssetKey          string `json:"asset_key"`
	AdjustPolicy      string `json:"adjust_policy"`
	PointType         string `json:"point_type"`
	LastTaskID        string `json:"last_task_id"`
	LastSuccessTaskID string `json:"last_success_task_id"`
	LastSuccessAt     *int64 `json:"last_success_at,omitempty"`
	DataAsOf          string `json:"data_as_of"`
	PointCount        int    `json:"point_count"`
	SourceName        string `json:"source_name"`
	UpdatedAt         int64  `json:"updated_at"`
}

// MarketAssetPointsSummary aggregates coverage facts used by full-replacement
// validation.
type MarketAssetPointsSummary struct {
	Count       int
	MinDate     string
	MaxDate     string
	SourceNames []string
}

// MarketAssetRepo persists the global market asset directory and its history.
type MarketAssetRepo struct {
	db *sql.DB
}

func NewMarketAssetRepo(db *sql.DB) *MarketAssetRepo {
	return &MarketAssetRepo{db: db}
}

// --- directory ---

// UpsertAssetTx inserts or refreshes one directory row, marking it active and
// seen at seenAt. Name/exchange/kind/currency/source fields are refreshed on
// conflict so renames propagate.
func (r *MarketAssetRepo) UpsertAssetTx(ctx context.Context, tx *sql.Tx, a MarketAsset, seenAt int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_assets (
			asset_key, market, instrument_type, region_code, symbol, name,
			exchange, instrument_kind, currency,
			active, listing_status, last_seen_at,
			source_name, source_as_of, refreshed_at, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,1,'active',?,?,?,?,?,?)
		ON CONFLICT(asset_key) DO UPDATE SET
			name=excluded.name,
			exchange=excluded.exchange,
			instrument_kind=excluded.instrument_kind,
			currency=excluded.currency,
			active=1,
			listing_status='active',
			last_seen_at=excluded.last_seen_at,
			source_name=excluded.source_name,
			source_as_of=excluded.source_as_of,
			refreshed_at=excluded.refreshed_at,
			updated_at=excluded.updated_at`,
		a.AssetKey, a.Market, a.InstrumentType, a.RegionCode, a.Symbol, a.Name,
		a.Exchange, a.InstrumentKind, a.Currency,
		seenAt, a.SourceName, a.SourceAsOf, seenAt, seenAt, seenAt)
	if err != nil {
		return fmt.Errorf("upsert market asset %s: %w", a.AssetKey, err)
	}
	return nil
}

// MarkUnseenInactiveTx flags directory rows of (market, instrument_type) that
// were not seen during the current sync as inactive without deleting them.
func (r *MarketAssetRepo) MarkUnseenInactiveTx(
	ctx context.Context, tx *sql.Tx, market, instrumentType string, seenAt, now int64,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE market_assets
		SET active=0, listing_status='inactive', updated_at=?
		WHERE market=? AND instrument_type=? AND last_seen_at < ?`,
		now, market, instrumentType, seenAt)
	return wrapSQL("mark unseen market assets inactive", err)
}

// CountActiveByTypeSourcesTx counts active directory rows of one category that
// were produced by any of the given listing sources. The directory coverage
// gate compares like-for-like: counts from a listing source the current sync
// no longer uses (taxonomy/source migrations) must not block the sync forever.
func (r *MarketAssetRepo) CountActiveByTypeSourcesTx(
	ctx context.Context, tx *sql.Tx, market, instrumentType string, sources []string,
) (int, error) {
	if len(sources) == 0 {
		return 0, nil
	}
	ph := make([]string, len(sources))
	args := []any{market, instrumentType}
	for i, s := range sources {
		ph[i] = "?"
		args = append(args, s)
	}
	var n int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM market_assets
		WHERE market=? AND instrument_type=? AND active=1
		  AND source_name IN (`+strings.Join(ph, ",")+`)`,
		args...).Scan(&n)
	return n, wrapSQL("count active market assets by sources", err)
}

const marketAssetColumns = `
	asset_key, market, instrument_type, region_code, symbol, name,
	exchange, instrument_kind, currency, active, listing_status, last_seen_at,
	source_name, source_as_of, refreshed_at, created_at, updated_at`

func scanMarketAsset(row rowScanner) (MarketAsset, error) {
	var a MarketAsset
	var active int
	err := row.Scan(
		&a.AssetKey, &a.Market, &a.InstrumentType, &a.RegionCode, &a.Symbol, &a.Name,
		&a.Exchange, &a.InstrumentKind, &a.Currency, &active, &a.ListingStatus, &a.LastSeenAt,
		&a.SourceName, &a.SourceAsOf, &a.RefreshedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketAsset{}, ErrMarketAssetNotFound
	}
	if err != nil {
		return MarketAsset{}, wrapSQL("scan market asset", err)
	}
	a.Active = active != 0
	return a, nil
}

func (r *MarketAssetRepo) GetByKey(ctx context.Context, assetKey string) (MarketAsset, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+marketAssetColumns+` FROM market_assets WHERE asset_key=?`, assetKey)
	return scanMarketAsset(row)
}

// GetByKeyTx reads one directory row inside a transaction.
func (r *MarketAssetRepo) GetByKeyTx(ctx context.Context, tx *sql.Tx, assetKey string) (MarketAsset, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+marketAssetColumns+` FROM market_assets WHERE asset_key=?`, assetKey)
	return scanMarketAsset(row)
}

// MarketAssetSearchOptions filters directory search. Search never touches the
// network; it reads local rows only.
type MarketAssetSearchOptions struct {
	Market          string
	InstrumentTypes []string
	Query           string
	IncludeInactive bool
	Limit           int
	Offset          int
}

// Search lists directory rows matching the options ordered by market,
// instrument_type, symbol.
func (r *MarketAssetRepo) Search(ctx context.Context, opts MarketAssetSearchOptions) ([]MarketAsset, error) {
	var (
		conds []string
		args  []any
	)
	if opts.Market != "" {
		conds = append(conds, "market = ?")
		args = append(args, opts.Market)
	}
	if len(opts.InstrumentTypes) > 0 {
		ph := make([]string, len(opts.InstrumentTypes))
		for i, t := range opts.InstrumentTypes {
			ph[i] = "?"
			args = append(args, t)
		}
		conds = append(conds, "instrument_type IN ("+strings.Join(ph, ",")+")")
	}
	if !opts.IncludeInactive {
		conds = append(conds, "active = 1")
	}
	if q := strings.TrimSpace(opts.Query); q != "" {
		like := "%" + escapeLike(q) + "%"
		conds = append(conds, `(symbol LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR (region_code || symbol) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit, opts.Offset)
	return queryCollect(ctx, r.db,
		`SELECT `+marketAssetColumns+` FROM market_assets `+where+`
		 ORDER BY market, instrument_type, symbol LIMIT ? OFFSET ?`,
		args,
		func(rows *sql.Rows) (MarketAsset, error) { return scanMarketAsset(rows) },
		"query market assets", "scan market asset", "iterate market assets",
	)
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// --- directory sync state ---

func (r *MarketAssetRepo) GetSyncState(ctx context.Context, scope string) (MarketAssetSyncState, bool, error) {
	var st MarketAssetSyncState
	err := r.db.QueryRowContext(ctx, `
		SELECT scope, last_task_id, last_success_task_id, last_success_at, updated_at
		FROM market_asset_sync_state WHERE scope=?`, scope).
		Scan(&st.Scope, &st.LastTaskID, &st.LastSuccessTaskID, &st.LastSuccessAt, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketAssetSyncState{}, false, nil
	}
	if err != nil {
		return MarketAssetSyncState{}, false, wrapSQL("query market asset sync state", err)
	}
	return st, true, nil
}

// SetSyncLastTaskTx records the most recently created directory task for scope.
func (r *MarketAssetRepo) SetSyncLastTaskTx(ctx context.Context, tx *sql.Tx, scope, taskID string) error {
	now := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_asset_sync_state (scope, last_task_id, updated_at)
		VALUES (?,?,?)
		ON CONFLICT(scope) DO UPDATE SET
			last_task_id=excluded.last_task_id,
			updated_at=excluded.updated_at`,
		scope, taskID, now)
	return wrapSQL("set sync state last task", err)
}

// SetSyncSuccessTx records a successful directory post-process for scope.
func (r *MarketAssetRepo) SetSyncSuccessTx(
	ctx context.Context, tx *sql.Tx, scope, taskID string, successAt int64,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_asset_sync_state
			(scope, last_task_id, last_success_task_id, last_success_at, updated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(scope) DO UPDATE SET
			last_task_id=excluded.last_task_id,
			last_success_task_id=excluded.last_success_task_id,
			last_success_at=excluded.last_success_at,
			updated_at=excluded.updated_at`,
		scope, taskID, taskID, successAt, successAt)
	return wrapSQL("set sync state success", err)
}

// --- history points ---

// DeletePointsTx removes all points for one (asset_key, adjust, point_type)
// history dimension. Used by full replacement.
func (r *MarketAssetRepo) DeletePointsTx(
	ctx context.Context, tx *sql.Tx, assetKey, adjustPolicy, pointType string,
) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM market_asset_points
		WHERE asset_key=? AND adjust_policy=? AND point_type=?`,
		assetKey, adjustPolicy, pointType)
	return wrapSQL("delete market asset points", err)
}

// UpsertPointsTx merges points into the history table.
func (r *MarketAssetRepo) UpsertPointsTx(ctx context.Context, tx *sql.Tx, points []MarketAssetPoint) error {
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO market_asset_points
			(asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(asset_key, adjust_policy, point_type, trade_date) DO UPDATE SET
			value=excluded.value,
			source_name=excluded.source_name,
			fetched_at=excluded.fetched_at`)
	if err != nil {
		return wrapSQL("prepare upsert market asset points", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, p := range points {
		if _, err := stmt.ExecContext(ctx,
			p.AssetKey, p.AdjustPolicy, p.PointType, p.TradeDate,
			p.Value, p.SourceName, p.FetchedAt); err != nil {
			return fmt.Errorf("upsert market asset point %s: %w", p.TradeDate, err)
		}
	}
	return nil
}

// ListPoints returns the full ordered history series for one dimension.
func (r *MarketAssetRepo) ListPoints(
	ctx context.Context, assetKey, adjustPolicy, pointType string,
) ([]MarketAssetPoint, error) {
	return queryCollect(ctx, r.db, `
		SELECT asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at
		FROM market_asset_points
		WHERE asset_key=? AND adjust_policy=? AND point_type=?
		ORDER BY trade_date`,
		[]any{assetKey, adjustPolicy, pointType},
		func(rows *sql.Rows) (MarketAssetPoint, error) {
			var p MarketAssetPoint
			if err := rows.Scan(&p.AssetKey, &p.AdjustPolicy, &p.PointType,
				&p.TradeDate, &p.Value, &p.SourceName, &p.FetchedAt); err != nil {
				return MarketAssetPoint{}, wrapSQL("scan market asset point", err)
			}
			return p, nil
		},
		"query market asset points", "scan market asset point", "iterate market asset points",
	)
}

// ListPointsTx returns the full ordered series inside a transaction.
func (r *MarketAssetRepo) ListPointsTx(
	ctx context.Context, tx *sql.Tx, assetKey, adjustPolicy, pointType string,
) ([]MarketAssetPoint, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT asset_key, adjust_policy, point_type, trade_date, value, source_name, fetched_at
		FROM market_asset_points
		WHERE asset_key=? AND adjust_policy=? AND point_type=?
		ORDER BY trade_date`,
		assetKey, adjustPolicy, pointType)
	if err != nil {
		return nil, wrapSQL("query market asset points", err)
	}
	return collectRows(rows,
		func(rows *sql.Rows) (MarketAssetPoint, error) {
			var p MarketAssetPoint
			if err := rows.Scan(&p.AssetKey, &p.AdjustPolicy, &p.PointType,
				&p.TradeDate, &p.Value, &p.SourceName, &p.FetchedAt); err != nil {
				return MarketAssetPoint{}, wrapSQL("scan market asset point", err)
			}
			return p, nil
		},
		"scan market asset point", "iterate market asset points",
	)
}

// PointsSummaryTx aggregates coverage facts (count, min/max date, distinct
// sources) for validation inside a post-process transaction.
func (r *MarketAssetRepo) PointsSummaryTx(
	ctx context.Context, tx *sql.Tx, assetKey, adjustPolicy, pointType string,
) (MarketAssetPointsSummary, error) {
	var out MarketAssetPointsSummary
	var minDate, maxDate sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(trade_date), MAX(trade_date)
		FROM market_asset_points
		WHERE asset_key=? AND adjust_policy=? AND point_type=?`,
		assetKey, adjustPolicy, pointType).Scan(&out.Count, &minDate, &maxDate)
	if err != nil {
		return out, wrapSQL("summarize market asset points", err)
	}
	out.MinDate = minDate.String
	out.MaxDate = maxDate.String
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT source_name FROM market_asset_points
		WHERE asset_key=? AND adjust_policy=? AND point_type=?
		ORDER BY source_name`,
		assetKey, adjustPolicy, pointType)
	if err != nil {
		return out, wrapSQL("query market asset point sources", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return out, wrapSQL("scan market asset point source", err)
		}
		out.SourceNames = append(out.SourceNames, s)
	}
	return out, wrapSQL("iterate market asset point sources", rows.Err())
}

// --- history state ---

func scanHistoryState(row rowScanner) (MarketAssetHistoryState, error) {
	var st MarketAssetHistoryState
	err := row.Scan(
		&st.AssetKey, &st.AdjustPolicy, &st.PointType,
		&st.LastTaskID, &st.LastSuccessTaskID, &st.LastSuccessAt,
		&st.DataAsOf, &st.PointCount, &st.SourceName, &st.UpdatedAt,
	)
	if err != nil {
		return MarketAssetHistoryState{}, err
	}
	return st, nil
}

const historyStateColumns = `
	asset_key, adjust_policy, point_type, last_task_id, last_success_task_id,
	last_success_at, data_as_of, point_count, source_name, updated_at`

// GetHistoryState returns the sync state for one history dimension. The bool
// reports whether a row exists.
func (r *MarketAssetRepo) GetHistoryState(
	ctx context.Context, assetKey, adjustPolicy, pointType string,
) (MarketAssetHistoryState, bool, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT `+historyStateColumns+`
		FROM market_asset_history_state
		WHERE asset_key=? AND adjust_policy=? AND point_type=?`,
		assetKey, adjustPolicy, pointType)
	st, err := scanHistoryState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketAssetHistoryState{}, false, nil
	}
	if err != nil {
		return MarketAssetHistoryState{}, false, wrapSQL("query market asset history state", err)
	}
	return st, true, nil
}

// GetHistoryStateTx reads the history state inside a transaction.
func (r *MarketAssetRepo) GetHistoryStateTx(
	ctx context.Context, tx *sql.Tx, assetKey, adjustPolicy, pointType string,
) (MarketAssetHistoryState, bool, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT `+historyStateColumns+`
		FROM market_asset_history_state
		WHERE asset_key=? AND adjust_policy=? AND point_type=?`,
		assetKey, adjustPolicy, pointType)
	st, err := scanHistoryState(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketAssetHistoryState{}, false, nil
	}
	if err != nil {
		return MarketAssetHistoryState{}, false, wrapSQL("query market asset history state", err)
	}
	return st, true, nil
}

// ListHistoryStatesByAsset lists every history dimension state for an asset.
func (r *MarketAssetRepo) ListHistoryStatesByAsset(
	ctx context.Context, assetKey string,
) ([]MarketAssetHistoryState, error) {
	return queryCollect(ctx, r.db, `
		SELECT `+historyStateColumns+`
		FROM market_asset_history_state
		WHERE asset_key=?
		ORDER BY adjust_policy, point_type`,
		[]any{assetKey},
		func(rows *sql.Rows) (MarketAssetHistoryState, error) {
			st, err := scanHistoryState(rows)
			if err != nil {
				return MarketAssetHistoryState{}, wrapSQL("scan market asset history state", err)
			}
			return st, nil
		},
		"query market asset history states", "scan market asset history state",
		"iterate market asset history states",
	)
}

// SetHistoryLastTaskTx records the most recently created history task.
func (r *MarketAssetRepo) SetHistoryLastTaskTx(
	ctx context.Context, tx *sql.Tx, assetKey, adjustPolicy, pointType, taskID string,
) error {
	now := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_asset_history_state
			(asset_key, adjust_policy, point_type, last_task_id, updated_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT(asset_key, adjust_policy, point_type) DO UPDATE SET
			last_task_id=excluded.last_task_id,
			updated_at=excluded.updated_at`,
		assetKey, adjustPolicy, pointType, taskID, now)
	return wrapSQL("set history state last task", err)
}

// SetHistorySuccessTx records a successful history post-process outcome.
func (r *MarketAssetRepo) SetHistorySuccessTx(
	ctx context.Context, tx *sql.Tx, st MarketAssetHistoryState,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_asset_history_state
			(asset_key, adjust_policy, point_type, last_task_id, last_success_task_id,
			 last_success_at, data_as_of, point_count, source_name, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(asset_key, adjust_policy, point_type) DO UPDATE SET
			last_task_id=excluded.last_task_id,
			last_success_task_id=excluded.last_success_task_id,
			last_success_at=excluded.last_success_at,
			data_as_of=excluded.data_as_of,
			point_count=excluded.point_count,
			source_name=excluded.source_name,
			updated_at=excluded.updated_at`,
		st.AssetKey, st.AdjustPolicy, st.PointType, st.LastTaskID, st.LastSuccessTaskID,
		st.LastSuccessAt, st.DataAsOf, st.PointCount, st.SourceName, st.UpdatedAt)
	return wrapSQL("set history state success", err)
}

// --- detail projections ---

// MarketAssetDetailProjection stores commit-time computed detail metrics.
type MarketAssetDetailProjection struct {
	AssetKey            string `json:"asset_key"`
	AdjustPolicy        string `json:"adjust_policy"`
	PointType           string `json:"point_type"`
	AnnualReturnsJSON   string `json:"annual_returns_json"`
	TrailingReturnsJSON string `json:"trailing_returns_json"`
	ComputedAt          int64  `json:"computed_at"`
}

// SetDetailProjectionTx upserts the commit-time detail projection.
func (r *MarketAssetRepo) SetDetailProjectionTx(
	ctx context.Context, tx *sql.Tx, p MarketAssetDetailProjection,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_asset_detail_projections
			(asset_key, adjust_policy, point_type, annual_returns_json, trailing_returns_json, computed_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(asset_key, adjust_policy, point_type) DO UPDATE SET
			annual_returns_json=excluded.annual_returns_json,
			trailing_returns_json=excluded.trailing_returns_json,
			computed_at=excluded.computed_at`,
		p.AssetKey, p.AdjustPolicy, p.PointType,
		p.AnnualReturnsJSON, p.TrailingReturnsJSON, p.ComputedAt)
	return wrapSQL("set market asset detail projection", err)
}

// GetDetailProjection loads the stored projection. The bool reports existence.
func (r *MarketAssetRepo) GetDetailProjection(
	ctx context.Context, assetKey, adjustPolicy, pointType string,
) (MarketAssetDetailProjection, bool, error) {
	var p MarketAssetDetailProjection
	err := r.db.QueryRowContext(ctx, `
		SELECT asset_key, adjust_policy, point_type, annual_returns_json, trailing_returns_json, computed_at
		FROM market_asset_detail_projections
		WHERE asset_key=? AND adjust_policy=? AND point_type=?`,
		assetKey, adjustPolicy, pointType).
		Scan(&p.AssetKey, &p.AdjustPolicy, &p.PointType,
			&p.AnnualReturnsJSON, &p.TrailingReturnsJSON, &p.ComputedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MarketAssetDetailProjection{}, false, nil
	}
	if err != nil {
		return MarketAssetDetailProjection{}, false, wrapSQL("query market asset detail projection", err)
	}
	return p, true, nil
}

// --- market data versions (post-process re-entrancy) ---

// GetDataVersionTx returns the stored version for a version_key (0 when none).
func (r *MarketAssetRepo) GetDataVersionTx(ctx context.Context, tx *sql.Tx, versionKey string) (int64, error) {
	var v int64
	err := tx.QueryRowContext(ctx,
		`SELECT version_no FROM market_data_versions WHERE version_key=?`, versionKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, wrapSQL("query market data version", err)
}

// SetDataVersionTx upserts the processed version for a version_key.
func (r *MarketAssetRepo) SetDataVersionTx(
	ctx context.Context, tx *sql.Tx, versionKey string, versionNo int64, taskID string,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO market_data_versions (version_key, version_no, task_id, updated_at)
		VALUES (?,?,?,?)
		ON CONFLICT(version_key) DO UPDATE SET
			version_no=excluded.version_no,
			task_id=excluded.task_id,
			updated_at=excluded.updated_at`,
		versionKey, versionNo, taskID, time.Now().UnixMilli())
	return wrapSQL("set market data version", err)
}
