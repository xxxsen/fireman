package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
)

// Research module errors.
var (
	ErrResearchCollectionNotFound      = errors.New("research collection not found")
	ErrResearchItemNotFound            = errors.New("research collection item not found")
	ErrResearchRunNotFound             = errors.New("research backtest run not found")
	ErrResearchOptimizationRunNotFound = errors.New("research optimization run not found")
)

// Research collection status values.
const (
	ResearchCollectionStatusActive   = "active"
	ResearchCollectionStatusArchived = "archived"
)

// Research run status values exposed by views. The authoritative value is
// always joined from worker_tasks; run tables do not persist task lifecycle.
const (
	ResearchRunStatusQueued    = WorkerTaskStatusPending
	ResearchRunStatusRunning   = "running"
	ResearchRunStatusSucceeded = WorkerTaskStatusComplete
	ResearchRunStatusFailed    = "failed"
	ResearchRunStatusCanceled  = "canceled"
)

// ResearchCollection mirrors a research_collections row.
type ResearchCollection struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	BaseCurrency        string  `json:"base_currency"`
	InitialAmountMinor  int64   `json:"initial_amount_minor"`
	RebalancePolicy     string  `json:"rebalance_policy"`
	RebalanceThreshold  float64 `json:"rebalance_threshold"`
	StartPolicy         string  `json:"start_policy"`
	WindowStart         string  `json:"window_start"`
	WindowEnd           string  `json:"window_end"`
	BenchmarkAssetKey   string  `json:"benchmark_asset_key,omitempty"`
	RiskFreeRate        float64 `json:"risk_free_rate"`
	TransactionCostRate float64 `json:"transaction_cost_rate"`
	TailRiskConfidence  float64 `json:"tail_risk_confidence"`
	TailRiskHorizonDays int     `json:"tail_risk_horizon_days"`
	Status              string  `json:"status"`
	TagsJSON            string  `json:"tags_json"`
	CreatedAt           int64   `json:"created_at"`
	UpdatedAt           int64   `json:"updated_at"`
}

// ResearchCollectionItem mirrors a research_collection_items row.
type ResearchCollectionItem struct {
	ID           string  `json:"id"`
	CollectionID string  `json:"collection_id"`
	AssetKey     string  `json:"asset_key"`
	Enabled      bool    `json:"enabled"`
	Weight       float64 `json:"weight"`
	WeightLocked bool    `json:"weight_locked"`
	AdjustPolicy string  `json:"adjust_policy"`
	PointType    string  `json:"point_type"`
	AssetClass   string  `json:"asset_class"`
	Region       string  `json:"region"`
	Note         string  `json:"note"`
	SortOrder    int     `json:"sort_order"`
	CreatedAt    int64   `json:"created_at"`
	UpdatedAt    int64   `json:"updated_at"`
}

// ResearchBacktestRun mirrors a research_backtest_runs row.
type ResearchBacktestRun struct {
	ID                string `json:"id"`
	CollectionID      string `json:"collection_id"`
	TaskID            string `json:"task_id"`
	InputHash         string `json:"input_hash"`
	InputSnapshotJSON string `json:"input_snapshot_json,omitempty"`
	SourceHash        string `json:"source_hash"`
	EngineVersion     string `json:"engine_version"`
	BaseCurrency      string `json:"base_currency"`
	RebalancePolicy   string `json:"rebalance_policy"`
	WindowStart       string `json:"window_start"`
	WindowEnd         string `json:"window_end"`
	Status            string `json:"status"`
	SummaryJSON       string `json:"summary_json"`
	DataQualityJSON   string `json:"data_quality_json"`
	CreatedAt         int64  `json:"created_at"`
	CompletedAt       *int64 `json:"completed_at,omitempty"`
}

// ResearchBacktestPoint mirrors a research_backtest_points row.
type ResearchBacktestPoint struct {
	RunID             string   `json:"run_id"`
	TradeDate         string   `json:"trade_date"`
	NAV               float64  `json:"nav"`
	CumulativeReturn  float64  `json:"cumulative_return"`
	PeriodReturn      float64  `json:"period_return"`
	Drawdown          float64  `json:"drawdown"`
	BenchmarkNAV      *float64 `json:"benchmark_nav,omitempty"`
	BenchmarkReturn   *float64 `json:"benchmark_return,omitempty"`
	WeightsJSON       string   `json:"weights_json"`
	ContributionsJSON string   `json:"contributions_json"`
}

// ResearchBacktestYear mirrors a research_backtest_years row.
type ResearchBacktestYear struct {
	RunID        string  `json:"run_id"`
	Year         int     `json:"year"`
	AnnualReturn float64 `json:"annual_return"`
	Volatility   float64 `json:"volatility"`
	MaxDrawdown  float64 `json:"max_drawdown"`
	StartNAV     float64 `json:"start_nav"`
	EndNAV       float64 `json:"end_nav"`
	IsPartial    bool    `json:"is_partial"`
}

// ResearchBacktestMonth mirrors a research_backtest_months row.
type ResearchBacktestMonth struct {
	RunID         string  `json:"run_id"`
	Year          int     `json:"year"`
	Month         int     `json:"month"`
	MonthlyReturn float64 `json:"monthly_return"`
}

// ResearchAssetMetrics mirrors a research_asset_metrics row: precomputed
// screener metrics for one history dimension. Nil pointers mean "metric not
// available" (never stored as 0).
type ResearchAssetMetrics struct {
	AssetKey           string   `json:"asset_key"`
	AdjustPolicy       string   `json:"adjust_policy"`
	PointType          string   `json:"point_type"`
	StartDate          string   `json:"start_date"`
	EndDate            string   `json:"end_date"`
	PointCount         int      `json:"point_count"`
	HistoryYears       float64  `json:"history_years"`
	CAGR               *float64 `json:"cagr,omitempty"`
	AnnualVolatility   *float64 `json:"annual_volatility,omitempty"`
	MaxDrawdown        *float64 `json:"max_drawdown,omitempty"`
	DownsideVolatility *float64 `json:"downside_volatility,omitempty"`
	Sharpe             *float64 `json:"sharpe,omitempty"`
	Calmar             *float64 `json:"calmar,omitempty"`
	Return1Y           *float64 `json:"return_1y,omitempty"`
	Return3Y           *float64 `json:"return_3y,omitempty"`
	Return5Y           *float64 `json:"return_5y,omitempty"`
	// ReturnDrawdownRatio is derived (total return / |max drawdown|), never
	// stored; populated when views are built.
	ReturnDrawdownRatio *float64 `json:"return_drawdown_ratio,omitempty"`
	ComputedAt          int64    `json:"computed_at"`
}

// FillReturnDrawdownRatio populates the derived return/drawdown ratio when
// CAGR, span and a negative max drawdown are all available.
func (m *ResearchAssetMetrics) FillReturnDrawdownRatio() {
	if m == nil || m.CAGR == nil || m.MaxDrawdown == nil ||
		m.HistoryYears <= 0 || *m.MaxDrawdown >= 0 {
		return
	}
	total := math.Pow(1+*m.CAGR, m.HistoryYears) - 1
	ratio := total / math.Abs(*m.MaxDrawdown)
	m.ReturnDrawdownRatio = &ratio
}

// ResearchRepo persists research collections, items, backtest runs and the
// precomputed research asset metrics projection.
type ResearchRepo struct {
	db *sql.DB
}

func NewResearchRepo(db *sql.DB) *ResearchRepo {
	return &ResearchRepo{db: db}
}

func (r *ResearchRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

// --- collections ---

const researchCollectionColumns = `
	id, name, description, base_currency, initial_amount_minor,
	rebalance_policy, rebalance_threshold, start_policy, window_start, window_end,
		benchmark_asset_key, risk_free_rate, transaction_cost_rate,
		tail_risk_confidence, tail_risk_horizon_days,
	status, tags_json, created_at, updated_at`

func scanResearchCollection(row rowScanner) (ResearchCollection, error) {
	var c ResearchCollection
	var benchmark sql.NullString
	err := row.Scan(
		&c.ID, &c.Name, &c.Description, &c.BaseCurrency, &c.InitialAmountMinor,
		&c.RebalancePolicy, &c.RebalanceThreshold, &c.StartPolicy, &c.WindowStart, &c.WindowEnd,
		&benchmark, &c.RiskFreeRate, &c.TransactionCostRate,
		&c.TailRiskConfidence, &c.TailRiskHorizonDays,
		&c.Status, &c.TagsJSON, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchCollection{}, ErrResearchCollectionNotFound
	}
	if err != nil {
		return ResearchCollection{}, wrapSQL("scan research collection", err)
	}
	if benchmark.Valid {
		c.BenchmarkAssetKey = benchmark.String
	}
	return c, nil
}

// CreateCollectionTx inserts one collection row. benchmark_asset_key is
// stored as NULL when empty (never as an empty string, which would break the
// market_assets foreign key).
func (r *ResearchRepo) CreateCollectionTx(ctx context.Context, tx *sql.Tx, c ResearchCollection) error {
	var benchmark any
	if c.BenchmarkAssetKey != "" {
		benchmark = c.BenchmarkAssetKey
	}
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO research_collections (
			id, name, description, base_currency, initial_amount_minor,
			rebalance_policy, rebalance_threshold, start_policy, window_start, window_end,
				benchmark_asset_key, risk_free_rate, transaction_cost_rate,
				tail_risk_confidence, tail_risk_horizon_days,
				status, tags_json, created_at, updated_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Description, c.BaseCurrency, c.InitialAmountMinor,
		c.RebalancePolicy, c.RebalanceThreshold, c.StartPolicy, c.WindowStart, c.WindowEnd,
		benchmark, c.RiskFreeRate, c.TransactionCostRate,
		c.TailRiskConfidence, c.TailRiskHorizonDays,
		c.Status, c.TagsJSON, c.CreatedAt, c.UpdatedAt)
	return wrapSQL("insert research collection", err)
}

// UpdateCollectionTx rewrites the mutable base parameters of a collection.
func (r *ResearchRepo) UpdateCollectionTx(ctx context.Context, tx *sql.Tx, c ResearchCollection) error {
	var benchmark any
	if c.BenchmarkAssetKey != "" {
		benchmark = c.BenchmarkAssetKey
	}
	res, err := r.exec(tx).ExecContext(ctx, `
		UPDATE research_collections SET
			name=?, description=?, base_currency=?, initial_amount_minor=?,
			rebalance_policy=?, rebalance_threshold=?, start_policy=?, window_start=?, window_end=?,
				benchmark_asset_key=?, risk_free_rate=?, transaction_cost_rate=?,
				tail_risk_confidence=?, tail_risk_horizon_days=?,
			status=?, tags_json=?, updated_at=?
		WHERE id=?`,
		c.Name, c.Description, c.BaseCurrency, c.InitialAmountMinor,
		c.RebalancePolicy, c.RebalanceThreshold, c.StartPolicy, c.WindowStart, c.WindowEnd,
		benchmark, c.RiskFreeRate, c.TransactionCostRate,
		c.TailRiskConfidence, c.TailRiskHorizonDays,
		c.Status, c.TagsJSON, c.UpdatedAt, c.ID)
	if err != nil {
		return wrapSQL("update research collection", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrResearchCollectionNotFound
	}
	return nil
}

// TouchCollectionTx bumps updated_at (item mutations affect the collection).
func (r *ResearchRepo) TouchCollectionTx(ctx context.Context, tx *sql.Tx, id string, now int64) error {
	res, err := r.exec(tx).ExecContext(ctx, `
		UPDATE research_collections
		SET updated_at=CASE WHEN updated_at>=? THEN updated_at+1 ELSE ? END
		WHERE id=?`, now, now, id)
	if err != nil {
		return wrapSQL("touch research collection", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrResearchCollectionNotFound
	}
	return nil
}

func (r *ResearchRepo) GetCollection(ctx context.Context, id string) (ResearchCollection, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+researchCollectionColumns+` FROM research_collections WHERE id=?`, id)
	return scanResearchCollection(row)
}

// GetCollectionTx reads one collection inside a transaction.
func (r *ResearchRepo) GetCollectionTx(ctx context.Context, tx *sql.Tx, id string) (ResearchCollection, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT `+researchCollectionColumns+` FROM research_collections WHERE id=?`, id)
	return scanResearchCollection(row)
}

// ListCollections returns collections filtered by status ("" = all), newest
// updated first.
func (r *ResearchRepo) ListCollections(ctx context.Context, status string) ([]ResearchCollection, error) {
	where := ""
	var args []any
	if status != "" {
		where = "WHERE status=?"
		args = append(args, status)
	}
	return queryCollect(
		ctx, r.db,
		`SELECT `+researchCollectionColumns+` FROM research_collections `+where+`
		 ORDER BY updated_at DESC, id ASC`, args,
		func(rows *sql.Rows) (ResearchCollection, error) { return scanResearchCollection(rows) },
		"query research collections", "scan research collection", "iterate research collections",
	)
}

// DeleteCollection removes a collection; items and runs cascade.
func (r *ResearchRepo) DeleteCollection(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM research_collections WHERE id=?`, id)
	if err != nil {
		return wrapSQL("delete research collection", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrResearchCollectionNotFound
	}
	return nil
}

// SetCollectionStatus archives or restores a collection.
func (r *ResearchRepo) SetCollectionStatus(ctx context.Context, id, status string, now int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE research_collections SET status=?, updated_at=? WHERE id=?`, status, now, id)
	if err != nil {
		return wrapSQL("set research collection status", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrResearchCollectionNotFound
	}
	return nil
}

// --- items ---

const researchItemColumns = `
	id, collection_id, asset_key, enabled, weight, weight_locked,
	adjust_policy, point_type, asset_class, region, note, sort_order,
	created_at, updated_at`

func scanResearchItem(row rowScanner) (ResearchCollectionItem, error) {
	var it ResearchCollectionItem
	var enabled, locked int
	err := row.Scan(
		&it.ID, &it.CollectionID, &it.AssetKey, &enabled, &it.Weight, &locked,
		&it.AdjustPolicy, &it.PointType, &it.AssetClass, &it.Region, &it.Note, &it.SortOrder,
		&it.CreatedAt, &it.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchCollectionItem{}, ErrResearchItemNotFound
	}
	if err != nil {
		return ResearchCollectionItem{}, wrapSQL("scan research item", err)
	}
	it.Enabled = enabled != 0
	it.WeightLocked = locked != 0
	return it, nil
}

func (r *ResearchRepo) CreateItemTx(ctx context.Context, tx *sql.Tx, it ResearchCollectionItem) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO research_collection_items (
			id, collection_id, asset_key, enabled, weight, weight_locked,
			adjust_policy, point_type, asset_class, region, note, sort_order,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		it.ID, it.CollectionID, it.AssetKey, boolToInt(it.Enabled), it.Weight, boolToInt(it.WeightLocked),
		it.AdjustPolicy, it.PointType, it.AssetClass, it.Region, it.Note, it.SortOrder,
		it.CreatedAt, it.UpdatedAt)
	return wrapSQL("insert research item", err)
}

func (r *ResearchRepo) UpdateItemTx(ctx context.Context, tx *sql.Tx, it ResearchCollectionItem) error {
	res, err := r.exec(tx).ExecContext(ctx, `
		UPDATE research_collection_items SET
			enabled=?, weight=?, weight_locked=?, adjust_policy=?, point_type=?,
			asset_class=?, region=?, note=?, sort_order=?, updated_at=?
		WHERE id=? AND collection_id=?`,
		boolToInt(it.Enabled), it.Weight, boolToInt(it.WeightLocked), it.AdjustPolicy, it.PointType,
		it.AssetClass, it.Region, it.Note, it.SortOrder, it.UpdatedAt,
		it.ID, it.CollectionID)
	if err != nil {
		return wrapSQL("update research item", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrResearchItemNotFound
	}
	return nil
}

func (r *ResearchRepo) DeleteItemTx(ctx context.Context, tx *sql.Tx, collectionID, itemID string) error {
	res, err := r.exec(tx).ExecContext(ctx,
		`DELETE FROM research_collection_items WHERE id=? AND collection_id=?`, itemID, collectionID)
	if err != nil {
		return wrapSQL("delete research item", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrResearchItemNotFound
	}
	return nil
}

func (r *ResearchRepo) GetItem(ctx context.Context, collectionID, itemID string) (ResearchCollectionItem, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+researchItemColumns+` FROM research_collection_items
		 WHERE id=? AND collection_id=?`, itemID, collectionID)
	return scanResearchItem(row)
}

func (r *ResearchRepo) ListItems(ctx context.Context, collectionID string) ([]ResearchCollectionItem, error) {
	return r.listItems(ctx, r.db, collectionID)
}

// ListItemsTx reads collection items inside a transaction.
func (r *ResearchRepo) ListItemsTx(
	ctx context.Context, tx *sql.Tx, collectionID string,
) ([]ResearchCollectionItem, error) {
	return r.listItems(ctx, tx, collectionID)
}

func (r *ResearchRepo) listItems(
	ctx context.Context, q rowQuerier, collectionID string,
) ([]ResearchCollectionItem, error) {
	return queryCollect(
		ctx, q,
		`SELECT `+researchItemColumns+` FROM research_collection_items
		 WHERE collection_id=? ORDER BY sort_order, created_at, id`, []any{collectionID},
		func(rows *sql.Rows) (ResearchCollectionItem, error) { return scanResearchItem(rows) },
		"query research items", "scan research item", "iterate research items",
	)
}

// CountItemsByCollections returns enabled/total item counts per collection.
func (r *ResearchRepo) CountItemsByCollections(
	ctx context.Context, collectionIDs []string,
) (map[string][2]int, error) {
	out := map[string][2]int{}
	if len(collectionIDs) == 0 {
		return out, nil
	}
	query, args := stringInQuery(`
		SELECT collection_id,
			COALESCE(SUM(CASE WHEN enabled=1 THEN 1 ELSE 0 END), 0),
			COUNT(*)
		FROM research_collection_items
		WHERE collection_id IN (`, collectionIDs, `)
		GROUP BY collection_id`)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQL("count research items", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var enabled, total int
		if err := rows.Scan(&id, &enabled, &total); err != nil {
			return nil, wrapSQL("scan research item count", err)
		}
		out[id] = [2]int{enabled, total}
	}
	return out, wrapSQL("iterate research item counts", rows.Err())
}

// SumEnabledWeightsByCollections returns the enabled weight sums per collection.
func (r *ResearchRepo) SumEnabledWeightsByCollections(
	ctx context.Context, collectionIDs []string,
) (map[string]float64, error) {
	out := map[string]float64{}
	if len(collectionIDs) == 0 {
		return out, nil
	}
	query, args := stringInQuery(`
		SELECT collection_id, COALESCE(SUM(weight), 0)
		FROM research_collection_items
		WHERE enabled=1 AND collection_id IN (`, collectionIDs, `)
		GROUP BY collection_id`)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQL("sum research item weights", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var sum float64
		if err := rows.Scan(&id, &sum); err != nil {
			return nil, wrapSQL("scan research weight sum", err)
		}
		out[id] = sum
	}
	return out, wrapSQL("iterate research weight sums", rows.Err())
}

// --- backtest runs ---

const researchRunColumns = `
	id, collection_id, task_id, input_hash, input_snapshot_json, source_hash,
	engine_version, base_currency, rebalance_policy, window_start, window_end,
	COALESCE((SELECT status FROM worker_tasks WHERE id=research_backtest_runs.task_id),'unknown'),
	summary_json, data_quality_json, created_at, completed_at`

// researchRunListColumns omits input_snapshot_json (potentially large) for
// listings.
const researchRunListColumns = `
	id, collection_id, task_id, input_hash, '' AS input_snapshot_json, source_hash,
	engine_version, base_currency, rebalance_policy, window_start, window_end,
	COALESCE((SELECT status FROM worker_tasks WHERE id=research_backtest_runs.task_id),'unknown'),
	summary_json, data_quality_json, created_at, completed_at`

func scanResearchRun(row rowScanner) (ResearchBacktestRun, error) {
	var run ResearchBacktestRun
	err := row.Scan(
		&run.ID, &run.CollectionID, &run.TaskID, &run.InputHash, &run.InputSnapshotJSON, &run.SourceHash,
		&run.EngineVersion, &run.BaseCurrency, &run.RebalancePolicy, &run.WindowStart, &run.WindowEnd,
		&run.Status, &run.SummaryJSON, &run.DataQualityJSON, &run.CreatedAt, &run.CompletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchBacktestRun{}, ErrResearchRunNotFound
	}
	if err != nil {
		return ResearchBacktestRun{}, wrapSQL("scan research run", err)
	}
	return run, nil
}

func (r *ResearchRepo) CreateRunTx(ctx context.Context, tx *sql.Tx, run ResearchBacktestRun) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO research_backtest_runs (
			id, collection_id, task_id, input_hash, input_snapshot_json, source_hash,
			engine_version, base_currency, rebalance_policy, window_start, window_end,
			summary_json, data_quality_json, created_at, completed_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.CollectionID, run.TaskID, run.InputHash, run.InputSnapshotJSON, run.SourceHash,
		run.EngineVersion, run.BaseCurrency, run.RebalancePolicy, run.WindowStart, run.WindowEnd,
		run.SummaryJSON, run.DataQualityJSON, run.CreatedAt, run.CompletedAt)
	return wrapSQL("insert research run", err)
}

func (r *ResearchRepo) GetRun(ctx context.Context, id string) (ResearchBacktestRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+researchRunColumns+` FROM research_backtest_runs WHERE id=?`, id)
	return scanResearchRun(row)
}

func (r *ResearchRepo) GetRunByTaskID(ctx context.Context, taskID string) (ResearchBacktestRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+researchRunColumns+` FROM research_backtest_runs WHERE task_id=?`, taskID)
	return scanResearchRun(row)
}

// ListRunsByCollection returns the newest runs of one collection without the
// input snapshot payload.
func (r *ResearchRepo) ListRunsByCollection(
	ctx context.Context, collectionID string, limit int,
) ([]ResearchBacktestRun, error) {
	if limit <= 0 {
		limit = 20
	}
	return queryCollect(
		ctx, r.db,
		`SELECT `+researchRunListColumns+` FROM research_backtest_runs
		 WHERE collection_id=? ORDER BY created_at DESC, id DESC LIMIT ?`,
		[]any{collectionID, limit},
		func(rows *sql.Rows) (ResearchBacktestRun, error) { return scanResearchRun(rows) },
		"query research runs", "scan research run", "iterate research runs",
	)
}

// ListRecentRuns returns the newest runs across all collections.
func (r *ResearchRepo) ListRecentRuns(ctx context.Context, limit int) ([]ResearchBacktestRun, error) {
	if limit <= 0 {
		limit = 10
	}
	return queryCollect(
		ctx, r.db,
		`SELECT `+researchRunListColumns+` FROM research_backtest_runs
		 ORDER BY created_at DESC, id DESC LIMIT ?`, []any{limit},
		func(rows *sql.Rows) (ResearchBacktestRun, error) { return scanResearchRun(rows) },
		"query recent research runs", "scan research run", "iterate research runs",
	)
}

// LatestRunsByCollections returns the most recent run per collection
// (any status), used to annotate the collection list.
func (r *ResearchRepo) LatestRunsByCollections(
	ctx context.Context, collectionIDs []string,
) (map[string]ResearchBacktestRun, error) {
	out := map[string]ResearchBacktestRun{}
	if len(collectionIDs) == 0 {
		return out, nil
	}
	query, args := stringInQuery(`
		SELECT `+researchRunListColumns+` FROM research_backtest_runs
		WHERE collection_id IN (`, collectionIDs, `)
		ORDER BY created_at DESC, id DESC`)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQL("query latest research runs", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		run, err := scanResearchRun(rows)
		if err != nil {
			return nil, err
		}
		if _, ok := out[run.CollectionID]; !ok {
			out[run.CollectionID] = run
		}
	}
	return out, wrapSQL("iterate latest research runs", rows.Err())
}

// FindSucceededRunByInputHash returns the succeeded run with the same
// (collection, input_hash), enabling idempotent backtest requests.
func (r *ResearchRepo) FindSucceededRunByInputHash(
	ctx context.Context, collectionID, inputHash string,
) (ResearchBacktestRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+researchRunColumns+` FROM research_backtest_runs
		 WHERE collection_id=? AND input_hash=? AND EXISTS (
		   SELECT 1 FROM worker_tasks t WHERE t.id=research_backtest_runs.task_id AND t.status=?
		 )
		 ORDER BY created_at DESC LIMIT 1`,
		collectionID, inputHash, ResearchRunStatusSucceeded)
	return scanResearchRun(row)
}

// FindActiveRunByInputHash returns a queued/running run with the same
// (collection, input_hash) so duplicate requests can poll the same run.
func (r *ResearchRepo) FindActiveRunByInputHash(
	ctx context.Context, collectionID, inputHash string,
) (ResearchBacktestRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+researchRunColumns+` FROM research_backtest_runs
		 WHERE collection_id=? AND input_hash=? AND EXISTS (
		   SELECT 1 FROM worker_tasks t WHERE t.id=research_backtest_runs.task_id AND t.status IN (?,?,?)
		 )
		 ORDER BY created_at DESC LIMIT 1`,
		collectionID, inputHash, WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete)
	return scanResearchRun(row)
}

// MarkRunRunning is retained as a no-op while processors are migrated. Task
// lifecycle is owned exclusively by worker_tasks.
func (r *ResearchRepo) MarkRunRunning(_ context.Context, _ string) error {
	return nil
}

// CompleteRunTx finalizes a successful run inside the caller's transaction.
func (r *ResearchRepo) CompleteRunTx(
	ctx context.Context, tx *sql.Tx, id, summaryJSON, dataQualityJSON string, completedAt int64,
) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE research_backtest_runs
		SET summary_json=?, data_quality_json=?, completed_at=?
		WHERE id=?`,
		summaryJSON, dataQualityJSON, completedAt, id)
	return wrapSQL("complete research run", err)
}

// FailRun marks a run failed/canceled.
func (r *ResearchRepo) FailRun(ctx context.Context, id, _ string, completedAt int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE research_backtest_runs SET completed_at=? WHERE id=?`, completedAt, id)
	return wrapSQL("fail research run", err)
}

// --- backtest points / years / months ---

func (r *ResearchRepo) ReplacePointsTx(
	ctx context.Context, tx *sql.Tx, runID string, points []ResearchBacktestPoint,
) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM research_backtest_points WHERE run_id=?`, runID); err != nil {
		return wrapSQL("clear research run points", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO research_backtest_points (
			run_id, trade_date, nav, cumulative_return, period_return, drawdown,
			benchmark_nav, benchmark_return, weights_json, contributions_json
		) VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return wrapSQL("prepare insert research run points", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, p := range points {
		if _, err := stmt.ExecContext(ctx,
			runID, p.TradeDate, p.NAV, p.CumulativeReturn, p.PeriodReturn, p.Drawdown,
			p.BenchmarkNAV, p.BenchmarkReturn, p.WeightsJSON, p.ContributionsJSON); err != nil {
			return wrapSQL("insert research run point "+p.TradeDate, err)
		}
	}
	return nil
}

func (r *ResearchRepo) ReplaceYearsTx(
	ctx context.Context, tx *sql.Tx, runID string, years []ResearchBacktestYear,
) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM research_backtest_years WHERE run_id=?`, runID); err != nil {
		return wrapSQL("clear research run years", err)
	}
	for _, y := range years {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO research_backtest_years (
				run_id, year, annual_return, volatility, max_drawdown,
				start_nav, end_nav, is_partial
			) VALUES (?,?,?,?,?,?,?,?)`,
			runID, y.Year, y.AnnualReturn, y.Volatility, y.MaxDrawdown,
			y.StartNAV, y.EndNAV, boolToInt(y.IsPartial)); err != nil {
			return wrapSQL("insert research run year", err)
		}
	}
	return nil
}

func (r *ResearchRepo) ReplaceMonthsTx(
	ctx context.Context, tx *sql.Tx, runID string, months []ResearchBacktestMonth,
) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM research_backtest_months WHERE run_id=?`, runID); err != nil {
		return wrapSQL("clear research run months", err)
	}
	for _, m := range months {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO research_backtest_months (run_id, year, month, monthly_return)
			VALUES (?,?,?,?)`,
			runID, m.Year, m.Month, m.MonthlyReturn); err != nil {
			return wrapSQL("insert research run month", err)
		}
	}
	return nil
}

// ResearchPointsQuery narrows point reads by date range and pagination.
type ResearchPointsQuery struct {
	From   string
	To     string
	Limit  int
	Offset int
}

// ListPoints returns run curve points ordered by trade_date, optionally
// bounded by [From, To] and paginated, plus the filtered total count.
func (r *ResearchRepo) ListPoints(
	ctx context.Context, runID string, q ResearchPointsQuery,
) ([]ResearchBacktestPoint, int, error) {
	conds := []string{"run_id=?"}
	args := []any{runID}
	if q.From != "" {
		conds = append(conds, "trade_date >= ?")
		args = append(args, q.From)
	}
	if q.To != "" {
		conds = append(conds, "trade_date <= ?")
		args = append(args, q.To)
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM research_backtest_points `+where, args...).Scan(&total); err != nil {
		return nil, 0, wrapSQL("count research run points", err)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = total
		if limit == 0 {
			limit = 1
		}
	}
	pagedArgs := append(append([]any{}, args...), limit, q.Offset)
	points, err := queryCollect(
		ctx, r.db, `
		SELECT run_id, trade_date, nav, cumulative_return, period_return, drawdown,
			benchmark_nav, benchmark_return, weights_json, contributions_json
		FROM research_backtest_points `+where+`
		ORDER BY trade_date LIMIT ? OFFSET ?`, pagedArgs,
		func(rows *sql.Rows) (ResearchBacktestPoint, error) {
			var p ResearchBacktestPoint
			if err := rows.Scan(&p.RunID, &p.TradeDate, &p.NAV, &p.CumulativeReturn,
				&p.PeriodReturn, &p.Drawdown, &p.BenchmarkNAV, &p.BenchmarkReturn,
				&p.WeightsJSON, &p.ContributionsJSON); err != nil {
				return ResearchBacktestPoint{}, wrapSQL("scan research run point", err)
			}
			return p, nil
		},
		"query research run points", "scan research run point", "iterate research run points",
	)
	if err != nil {
		return nil, 0, err
	}
	return points, total, nil
}

func (r *ResearchRepo) ListYears(ctx context.Context, runID string) ([]ResearchBacktestYear, error) {
	return queryCollect(
		ctx, r.db, `
		SELECT run_id, year, annual_return, volatility, max_drawdown, start_nav, end_nav, is_partial
		FROM research_backtest_years WHERE run_id=? ORDER BY year DESC`, []any{runID},
		func(rows *sql.Rows) (ResearchBacktestYear, error) {
			var y ResearchBacktestYear
			var partial int
			if err := rows.Scan(&y.RunID, &y.Year, &y.AnnualReturn, &y.Volatility,
				&y.MaxDrawdown, &y.StartNAV, &y.EndNAV, &partial); err != nil {
				return ResearchBacktestYear{}, wrapSQL("scan research run year", err)
			}
			y.IsPartial = partial != 0
			return y, nil
		},
		"query research run years", "scan research run year", "iterate research run years",
	)
}

func (r *ResearchRepo) ListMonths(ctx context.Context, runID string) ([]ResearchBacktestMonth, error) {
	return queryCollect(
		ctx, r.db, `
		SELECT run_id, year, month, monthly_return
		FROM research_backtest_months WHERE run_id=? ORDER BY year, month`, []any{runID},
		func(rows *sql.Rows) (ResearchBacktestMonth, error) {
			var m ResearchBacktestMonth
			if err := rows.Scan(&m.RunID, &m.Year, &m.Month, &m.MonthlyReturn); err != nil {
				return ResearchBacktestMonth{}, wrapSQL("scan research run month", err)
			}
			return m, nil
		},
		"query research run months", "scan research run month", "iterate research run months",
	)
}

// --- research asset metrics projection ---

const researchMetricsColumns = `
	asset_key, adjust_policy, point_type, start_date, end_date, point_count,
	history_years, cagr, annual_volatility, max_drawdown, downside_volatility,
	sharpe, calmar, return_1y, return_3y, return_5y, computed_at`

func scanResearchMetrics(row rowScanner) (ResearchAssetMetrics, error) {
	var m ResearchAssetMetrics
	err := row.Scan(
		&m.AssetKey, &m.AdjustPolicy, &m.PointType, &m.StartDate, &m.EndDate, &m.PointCount,
		&m.HistoryYears, &m.CAGR, &m.AnnualVolatility, &m.MaxDrawdown, &m.DownsideVolatility,
		&m.Sharpe, &m.Calmar, &m.Return1Y, &m.Return3Y, &m.Return5Y, &m.ComputedAt,
	)
	if err != nil {
		return ResearchAssetMetrics{}, wrapSQL("scan research asset metrics", err)
	}
	return m, nil
}

// UpsertMetricsTx writes one metrics projection row.
func (r *ResearchRepo) UpsertMetricsTx(ctx context.Context, tx *sql.Tx, m ResearchAssetMetrics) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO research_asset_metrics (
			asset_key, adjust_policy, point_type, start_date, end_date, point_count,
			history_years, cagr, annual_volatility, max_drawdown, downside_volatility,
			sharpe, calmar, return_1y, return_3y, return_5y, computed_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(asset_key, adjust_policy, point_type) DO UPDATE SET
			start_date=excluded.start_date,
			end_date=excluded.end_date,
			point_count=excluded.point_count,
			history_years=excluded.history_years,
			cagr=excluded.cagr,
			annual_volatility=excluded.annual_volatility,
			max_drawdown=excluded.max_drawdown,
			downside_volatility=excluded.downside_volatility,
			sharpe=excluded.sharpe,
			calmar=excluded.calmar,
			return_1y=excluded.return_1y,
			return_3y=excluded.return_3y,
			return_5y=excluded.return_5y,
			computed_at=excluded.computed_at`,
		m.AssetKey, m.AdjustPolicy, m.PointType, m.StartDate, m.EndDate, m.PointCount,
		m.HistoryYears, m.CAGR, m.AnnualVolatility, m.MaxDrawdown, m.DownsideVolatility,
		m.Sharpe, m.Calmar, m.Return1Y, m.Return3Y, m.Return5Y, m.ComputedAt)
	return wrapSQL("upsert research asset metrics", err)
}

// ListMetricsByAssetKeys returns all metrics rows for the given asset keys.
func (r *ResearchRepo) ListMetricsByAssetKeys(
	ctx context.Context, assetKeys []string,
) ([]ResearchAssetMetrics, error) {
	if len(assetKeys) == 0 {
		return nil, nil
	}
	query, args := stringInQuery(
		`SELECT `+researchMetricsColumns+` FROM research_asset_metrics WHERE asset_key IN (`,
		assetKeys,
		`) ORDER BY asset_key, adjust_policy, point_type`,
	)
	return queryCollect(
		ctx, r.db,
		query, args,
		func(rows *sql.Rows) (ResearchAssetMetrics, error) {
			m, err := scanResearchMetrics(rows)
			if err != nil {
				return ResearchAssetMetrics{}, wrapSQL("scan research asset metrics", err)
			}
			return m, nil
		},
		"query research asset metrics", "scan research asset metrics", "iterate research asset metrics",
	)
}

// --- screener search ---

// ResearchAssetSearchFilter narrows the research screener listing. Metric
// bounds are pointers so "no filter" and "filter at 0" stay distinct.
// Drawdowns are stored negative: MinMaxDrawdown = -0.3 means "drawdown no
// worse than -30%".
type ResearchAssetSearchFilter struct {
	Market          string
	InstrumentTypes []string
	Query           string
	Currencies      []string
	IncludeInactive bool
	// HistoryStatus: "", synced, missing, stale, syncing, failed.
	HistoryStatus   string
	DataAsOfMin     string
	MinHistoryYears float64
	MinCAGR         *float64
	MinReturn1Y     *float64
	MinReturn3Y     *float64
	MinReturn5Y     *float64
	MaxVolatility   *float64
	MinMaxDrawdown  *float64
	MinSharpe       *float64
	MinCalmar       *float64
	// MaxDownsideVolatility caps annualized downside deviation.
	MaxDownsideVolatility *float64
	// MinReturnDrawdownRatio filters on total return / |max drawdown|.
	MinReturnDrawdownRatio *float64
	BacktestReady          bool
	// NowDate anchors staleness (format 2006-01-02); injectable for tests.
	NowDate  string
	SortBy   string
	SortDesc bool
	Limit    int
	Offset   int
}

// ResearchAssetRow is one screener result row: directory + best history
// dimension + latest sync task + metrics projection.
type ResearchAssetRow struct {
	Asset        MarketAsset
	HasHistory   bool
	AdjustPolicy string
	PointType    string
	DataAsOf     string
	PointCount   int
	SourceName   string
	Stale        bool
	SyncStatus   string
	SyncError    string
	Metrics      *ResearchAssetMetrics
}

// researchSortColumns whitelists screener sort keys to SQL expressions.
var researchSortColumns = map[string]string{
	"symbol":          "a.symbol",
	"name":            "a.name",
	"market":          "a.market",
	"currency":        "a.currency",
	"data_as_of":      "h.data_as_of",
	"point_count":     "h.point_count",
	"history_years":   "m.history_years",
	"cagr":            "m.cagr",
	"return_1y":       "m.return_1y",
	"return_3y":       "m.return_3y",
	"return_5y":       "m.return_5y",
	"volatility":      "m.annual_volatility",
	"max_drawdown":    "m.max_drawdown",
	"sharpe":          "m.sharpe",
	"calmar":          "m.calmar",
	"downside_vol":    "m.downside_volatility",
	"return_drawdown": researchReturnDrawdownExpr,
	"history_status":  "h.point_count",
}

// researchReturnDrawdownExpr reconstructs total return / |max drawdown| from
// the stored CAGR and history span: (1+cagr)^years == end/start exactly.
const researchReturnDrawdownExpr = `(CASE
	WHEN m.cagr IS NOT NULL AND m.history_years > 0 AND m.max_drawdown < 0
	THEN (pow(1.0 + m.cagr, m.history_years) - 1.0) / ABS(m.max_drawdown)
	END)`

// IsResearchSortKey reports whether the screener sort key is supported.
func IsResearchSortKey(key string) bool {
	_, ok := researchSortColumns[key]
	return ok
}

const researchStaleExpr = `(h.asset_key IS NOT NULL AND h.data_as_of <> ''
	AND julianday(?) - julianday(h.data_as_of) >
		(CASE WHEN a.instrument_type = 'cn_mutual_fund' THEN 10 ELSE 7 END))`

func buildResearchSearchWhere(f ResearchAssetSearchFilter, nowDate string) (string, []any) {
	conditions := make([]string, 0)
	args := make([]any, 0)
	if f.Market != "" {
		conditions, args = append(conditions, "a.market = ?"), append(args, strings.ToUpper(f.Market))
	}
	conditions, args = appendResearchStringIn(
		conditions, args, "a.instrument_type", f.InstrumentTypes, false,
	)
	if f.Query != "" {
		query := "%" + f.Query + "%"
		conditions = append(conditions,
			"(a.symbol LIKE ? OR a.name LIKE ? OR a.asset_key LIKE ? OR a.exchange LIKE ?)")
		args = append(args, query, query, query, query)
	}
	conditions, args = appendResearchStringIn(conditions, args, "a.currency", f.Currencies, true)
	if !f.IncludeInactive {
		conditions = append(conditions, "a.active = 1")
	}
	conditions, args = appendResearchHistoryFilters(conditions, args, f, nowDate)
	conditions, args = appendResearchMetricFilters(conditions, args, f)
	if f.BacktestReady {
		conditions = append(conditions, researchBacktestReadyExpr)
	}
	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func appendResearchStringIn(
	conditions []string,
	args []any,
	column string,
	values []string,
	uppercase bool,
) ([]string, []any) {
	if len(values) == 0 {
		return conditions, args
	}
	placeholders := make([]string, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		if uppercase {
			value = strings.ToUpper(value)
		}
		args = append(args, value)
	}
	return append(conditions, column+" IN ("+strings.Join(placeholders, ",")+")"), args
}

func appendResearchHistoryFilters(
	conditions []string,
	args []any,
	f ResearchAssetSearchFilter,
	nowDate string,
) ([]string, []any) {
	switch f.HistoryStatus {
	case "synced":
		conditions = append(conditions, "h.asset_key IS NOT NULL")
	case "missing":
		conditions = append(conditions, "h.asset_key IS NULL AND a.instrument_type <> 'cash'")
	case "stale":
		conditions, args = append(conditions, researchStaleExpr), append(args, nowDate)
	case "syncing":
		conditions = append(conditions, "t.status IN ('pending','running','pre_complete')")
	case "failed":
		conditions = append(conditions, "t.status = 'failed'")
	}
	if f.DataAsOfMin != "" {
		conditions, args = append(conditions, "h.data_as_of >= ?"), append(args, f.DataAsOfMin)
	}
	if f.MinHistoryYears > 0 {
		conditions, args = append(conditions, "m.history_years >= ?"), append(args, f.MinHistoryYears)
	}
	return conditions, args
}

func appendResearchMetricFilters(
	conditions []string, args []any, f ResearchAssetSearchFilter,
) ([]string, []any) {
	filters := []struct {
		value *float64
		expr  string
	}{
		{f.MinCAGR, "m.cagr >= ?"},
		{f.MinReturn1Y, "m.return_1y >= ?"},
		{f.MinReturn3Y, "m.return_3y >= ?"},
		{f.MinReturn5Y, "m.return_5y >= ?"},
		{f.MaxVolatility, "m.annual_volatility <= ?"},
		{f.MinMaxDrawdown, "COALESCE(m.max_drawdown, 0) >= ?"},
		{f.MinSharpe, "m.sharpe >= ?"},
		{f.MinCalmar, "m.calmar >= ?"},
		{f.MaxDownsideVolatility, "m.downside_volatility <= ?"},
		{f.MinReturnDrawdownRatio, researchReturnDrawdownExpr + " >= ?"},
	}
	for _, filter := range filters {
		if filter.value != nil {
			conditions, args = append(conditions, filter.expr), append(args, *filter.value)
		}
	}
	return conditions, args
}

const researchBacktestReadyExpr = `(
	a.instrument_type = 'cash'
	OR (h.asset_key IS NOT NULL AND (
		a.currency = 'CNY'
		OR EXISTS (
			SELECT 1 FROM instruments fi
			JOIN market_data_points fp ON fp.instrument_id = fi.id
			WHERE fi.market = 'SYSTEM' AND fi.instrument_type = 'fx_rate'
				AND fi.code = a.currency || 'CNY'
		)
	))
)`

func researchSearchOrder(sortBy string, descending bool) string {
	expression, ok := researchSortColumns[sortBy]
	if !ok {
		return "a.market, a.instrument_type, a.symbol"
	}
	direction := "ASC"
	if descending {
		direction = "DESC"
	}
	return expression + " " + direction + " NULLS LAST, a.asset_key ASC"
}

// SearchResearchAssets runs the screener query: directory rows joined with
// the best history dimension (max point_count), its metrics projection and
// the latest history sync task, filtered/sorted/paged in SQL.
func (r *ResearchRepo) SearchResearchAssets(
	ctx context.Context, f ResearchAssetSearchFilter,
) ([]ResearchAssetRow, int, error) {
	nowDate := f.NowDate
	if nowDate == "" {
		nowDate = "1970-01-01"
	}

	base := `
	FROM market_assets a
		LEFT JOIN (
			SELECT h1.asset_key, h1.adjust_policy, h1.point_type, h1.data_as_of,
				h1.point_count, h1.source_name,
				ROW_NUMBER() OVER (
					PARTITION BY h1.asset_key
					ORDER BY h1.point_count DESC, h1.point_type
				) AS rn
			FROM market_asset_history_state h1
			JOIN market_assets a1 ON a1.asset_key = h1.asset_key
			WHERE h1.point_count > 0 AND (
				(a1.instrument_type IN (
					'cn_exchange_stock', 'cn_exchange_fund', 'hk_stock', 'hk_etf', 'us_stock', 'us_etf'
				) AND h1.adjust_policy = 'hfq' AND h1.point_type = 'adjusted_close')
				OR (a1.instrument_type = 'cn_mutual_fund'
					AND h1.adjust_policy = 'none'
					AND h1.point_type IN ('nav', 'total_return_index'))
			)
		) h ON h.asset_key = a.asset_key AND h.rn = 1
	LEFT JOIN research_asset_metrics m
		ON m.asset_key = h.asset_key
		AND m.adjust_policy = h.adjust_policy
		AND m.point_type = h.point_type
	LEFT JOIN (
		SELECT hs.asset_key, wt.status, wt.error_code, wt.error_message,
			ROW_NUMBER() OVER (PARTITION BY hs.asset_key ORDER BY wt.created_at DESC) AS rn
		FROM market_asset_history_state hs
		JOIN worker_tasks wt ON wt.id = hs.last_task_id
	) t ON t.asset_key = a.asset_key AND t.rn = 1`

	where, args := buildResearchSearchWhere(f, nowDate)

	var total int
	countArgs := append([]any{}, args...)
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) "+base+" "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, wrapSQL("count research assets", err)
	}

	orderExpr := researchSearchOrder(f.SortBy, f.SortDesc)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}

	selectSQL := buildResearchAssetSelectSQL(base, where, orderExpr)
	selectArgs := append([]any{nowDate}, args...)
	selectArgs = append(selectArgs, limit, f.Offset)

	rows, err := r.db.QueryContext(ctx, selectSQL, selectArgs...)
	if err != nil {
		return nil, 0, wrapSQL("query research assets", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ResearchAssetRow
	for rows.Next() {
		row, err := scanResearchAssetRow(rows)
		if err != nil {
			return nil, 0, wrapSQL("scan research asset row", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, wrapSQL("iterate research asset rows", err)
	}
	return out, total, nil
}

func buildResearchAssetSelectSQL(base, where, order string) string {
	var query strings.Builder
	query.WriteString(`
	SELECT a.asset_key, a.market, a.instrument_type, a.region_code, a.symbol, a.name,
		a.exchange, a.instrument_kind, a.canonical_symbol, a.fee_mode,
		a.currency, a.active, a.listing_status,
		a.last_seen_at, a.source_name, a.source_as_of, a.refreshed_at, a.created_at, a.updated_at,
		h.adjust_policy, h.point_type, h.data_as_of, h.point_count, h.source_name,
		(CASE WHEN `)
	query.WriteString(researchStaleExpr)
	query.WriteString(` THEN 1 ELSE 0 END) AS stale,
		COALESCE(t.status, ''), COALESCE(t.error_code, ''), COALESCE(t.error_message, ''),
		m.asset_key, m.start_date, m.end_date, m.point_count, m.history_years,
		m.cagr, m.annual_volatility, m.max_drawdown, m.downside_volatility,
		m.sharpe, m.calmar, m.return_1y, m.return_3y, m.return_5y, m.computed_at
	`)
	query.WriteString(base)
	query.WriteByte(' ')
	query.WriteString(where)
	query.WriteString("\nORDER BY ")
	query.WriteString(order)
	query.WriteString("\nLIMIT ? OFFSET ?")
	return query.String()
}

type researchAssetScanFields struct {
	active, stale                   int
	hAdjust, hPoint, hAsOf, hSource sql.NullString
	hCount                          sql.NullInt64
	mKey, mStart, mEnd              sql.NullString
	mCount, mComputed               sql.NullInt64
	mYears                          sql.NullFloat64
	cagr, vol, dd, downside         sql.NullFloat64
	sharpe, calmar, r1, r3, r5      sql.NullFloat64
	errCode, errMsg                 string
}

func scanResearchAssetRow(rows *sql.Rows) (ResearchAssetRow, error) {
	var row ResearchAssetRow
	var fields researchAssetScanFields
	err := rows.Scan(
		&row.Asset.AssetKey, &row.Asset.Market, &row.Asset.InstrumentType,
		&row.Asset.RegionCode, &row.Asset.Symbol, &row.Asset.Name,
		&row.Asset.Exchange, &row.Asset.InstrumentKind,
		&row.Asset.CanonicalSymbol, &row.Asset.FeeMode, &row.Asset.Currency,
		&fields.active, &row.Asset.ListingStatus,
		&row.Asset.LastSeenAt, &row.Asset.SourceName, &row.Asset.SourceAsOf,
		&row.Asset.RefreshedAt, &row.Asset.CreatedAt, &row.Asset.UpdatedAt,
		&fields.hAdjust, &fields.hPoint, &fields.hAsOf, &fields.hCount, &fields.hSource,
		&fields.stale, &row.SyncStatus, &fields.errCode, &fields.errMsg,
		&fields.mKey, &fields.mStart, &fields.mEnd, &fields.mCount, &fields.mYears,
		&fields.cagr, &fields.vol, &fields.dd, &fields.downside,
		&fields.sharpe, &fields.calmar, &fields.r1, &fields.r3, &fields.r5, &fields.mComputed,
	)
	if err != nil {
		return ResearchAssetRow{}, wrapSQL("scan research asset row fields", err)
	}
	populateResearchAssetScanFields(&row, fields)
	return row, nil
}

func populateResearchAssetScanFields(row *ResearchAssetRow, fields researchAssetScanFields) {
	row.SyncError = fields.errMsg
	if row.SyncError == "" {
		row.SyncError = fields.errCode
	}
	row.Asset.Active = fields.active == 1
	if fields.hAdjust.Valid {
		row.HasHistory = true
		row.AdjustPolicy, row.PointType = fields.hAdjust.String, fields.hPoint.String
		row.DataAsOf, row.PointCount, row.SourceName = fields.hAsOf.String, int(fields.hCount.Int64), fields.hSource.String
	}
	row.Stale = fields.stale == 1
	if !fields.mKey.Valid {
		return
	}
	metrics := &ResearchAssetMetrics{
		AssetKey: fields.mKey.String, AdjustPolicy: row.AdjustPolicy, PointType: row.PointType,
		StartDate: fields.mStart.String, EndDate: fields.mEnd.String,
		PointCount: int(fields.mCount.Int64), HistoryYears: fields.mYears.Float64,
		ComputedAt: fields.mComputed.Int64,
	}
	assignResearchMetricPointers(metrics, fields)
	row.Metrics = metrics
}

func assignResearchMetricPointers(metrics *ResearchAssetMetrics, fields researchAssetScanFields) {
	assign := func(target **float64, value sql.NullFloat64) {
		if value.Valid {
			metricValue := value.Float64
			*target = &metricValue
		}
	}
	assign(&metrics.CAGR, fields.cagr)
	assign(&metrics.AnnualVolatility, fields.vol)
	assign(&metrics.MaxDrawdown, fields.dd)
	assign(&metrics.DownsideVolatility, fields.downside)
	assign(&metrics.Sharpe, fields.sharpe)
	assign(&metrics.Calmar, fields.calmar)
	assign(&metrics.Return1Y, fields.r1)
	assign(&metrics.Return3Y, fields.r3)
	assign(&metrics.Return5Y, fields.r5)
}

// StaleMetricsDimension identifies one history dimension whose metrics
// projection is missing or out of date relative to the history state.
type StaleMetricsDimension struct {
	AssetKey     string
	AdjustPolicy string
	PointType    string
}

// ListStaleMetricsDimensions returns history dimensions with stored points
// whose research metrics row is missing or computed against different
// coverage (point_count/end_date mismatch). Used by the screener's lazy
// backfill; bounded by limit.
func (r *ResearchRepo) ListStaleMetricsDimensions(
	ctx context.Context, limit int,
) ([]StaleMetricsDimension, error) {
	if limit <= 0 {
		limit = 200
	}
	return queryCollect(
		ctx, r.db, `
		SELECT h.asset_key, h.adjust_policy, h.point_type
		FROM market_asset_history_state h
		LEFT JOIN research_asset_metrics m
			ON m.asset_key = h.asset_key
			AND m.adjust_policy = h.adjust_policy
			AND m.point_type = h.point_type
		WHERE h.point_count > 0
			AND (m.asset_key IS NULL
				OR m.point_count <> h.point_count
				OR m.end_date <> h.data_as_of)
		ORDER BY h.asset_key, h.adjust_policy, h.point_type
		LIMIT ?`, []any{limit},
		func(rows *sql.Rows) (StaleMetricsDimension, error) {
			var d StaleMetricsDimension
			if err := rows.Scan(&d.AssetKey, &d.AdjustPolicy, &d.PointType); err != nil {
				return StaleMetricsDimension{}, wrapSQL("scan stale metrics dimension", err)
			}
			return d, nil
		},
		"query stale metrics dimensions", "scan stale metrics dimension", "iterate stale metrics dimensions",
	)
}

// --- optimization runs ---

// ResearchOptimizationRun mirrors a research_optimization_runs row.
type ResearchOptimizationRun struct {
	ID                string `json:"id"`
	CollectionID      string `json:"collection_id"`
	TaskID            string `json:"task_id"`
	Status            string `json:"status"`
	InputHash         string `json:"input_hash"`
	SourceHash        string `json:"source_hash"`
	EngineVersion     string `json:"engine_version"`
	BaseCurrency      string `json:"base_currency"`
	RebalancePolicy   string `json:"rebalance_policy"`
	WindowStart       string `json:"window_start"`
	WindowEnd         string `json:"window_end"`
	ConfigJSON        string `json:"config_json"`
	InputSnapshotJSON string `json:"input_snapshot_json,omitempty"`
	CandidateCount    int    `json:"candidate_count"`
	EvaluatedCount    int    `json:"evaluated_count"`
	ResultJSON        string `json:"result_json"`
	ErrorCode         string `json:"error_code,omitempty"`
	ErrorMessage      string `json:"error_message,omitempty"`
	CreatedAt         int64  `json:"created_at"`
	CompletedAt       *int64 `json:"completed_at,omitempty"`
}

const optimizationRunColumns = `
	id, collection_id, task_id,
	COALESCE((SELECT status FROM worker_tasks WHERE id=research_optimization_runs.task_id),'unknown'),
	input_hash, source_hash,
	engine_version, base_currency, rebalance_policy, window_start, window_end,
	config_json, input_snapshot_json, candidate_count,
	CASE WHEN COALESCE((SELECT status FROM worker_tasks WHERE id=research_optimization_runs.task_id),'')
	  IN ('pending','running','pre_complete')
	  THEN COALESCE((SELECT progress_current FROM worker_tasks
	                 WHERE id=research_optimization_runs.task_id),evaluated_count)
	  ELSE evaluated_count END,
	result_json,
	COALESCE((SELECT error_code FROM worker_tasks WHERE id=research_optimization_runs.task_id),''),
	COALESCE((SELECT error_message FROM worker_tasks WHERE id=research_optimization_runs.task_id),''),
	created_at, completed_at`

const optimizationRunListColumns = `
	id, collection_id, task_id,
	COALESCE((SELECT status FROM worker_tasks WHERE id=research_optimization_runs.task_id),'unknown'),
	input_hash, source_hash,
	engine_version, base_currency, rebalance_policy, window_start, window_end,
	config_json, '' AS input_snapshot_json, candidate_count,
	CASE WHEN COALESCE((SELECT status FROM worker_tasks WHERE id=research_optimization_runs.task_id),'')
	  IN ('pending','running','pre_complete')
	  THEN COALESCE((SELECT progress_current FROM worker_tasks
	                 WHERE id=research_optimization_runs.task_id),evaluated_count)
	  ELSE evaluated_count END,
	result_json,
	COALESCE((SELECT error_code FROM worker_tasks WHERE id=research_optimization_runs.task_id),''),
	COALESCE((SELECT error_message FROM worker_tasks WHERE id=research_optimization_runs.task_id),''),
	created_at, completed_at`

func scanOptimizationRun(row rowScanner) (ResearchOptimizationRun, error) {
	var run ResearchOptimizationRun
	err := row.Scan(
		&run.ID, &run.CollectionID, &run.TaskID, &run.Status,
		&run.InputHash, &run.SourceHash, &run.EngineVersion,
		&run.BaseCurrency, &run.RebalancePolicy, &run.WindowStart, &run.WindowEnd,
		&run.ConfigJSON, &run.InputSnapshotJSON, &run.CandidateCount, &run.EvaluatedCount,
		&run.ResultJSON, &run.ErrorCode, &run.ErrorMessage,
		&run.CreatedAt, &run.CompletedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchOptimizationRun{}, ErrResearchOptimizationRunNotFound
	}
	if err != nil {
		return ResearchOptimizationRun{}, wrapSQL("scan optimization run", err)
	}
	return run, nil
}

func (r *ResearchRepo) CreateOptimizationRunTx(
	ctx context.Context, tx *sql.Tx, run ResearchOptimizationRun,
) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO research_optimization_runs (
			id, collection_id, task_id, input_hash, source_hash,
			engine_version, base_currency, rebalance_policy, window_start, window_end,
			config_json, input_snapshot_json, candidate_count, evaluated_count,
			result_json, created_at, completed_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.CollectionID, run.TaskID,
		run.InputHash, run.SourceHash, run.EngineVersion,
		run.BaseCurrency, run.RebalancePolicy, run.WindowStart, run.WindowEnd,
		run.ConfigJSON, run.InputSnapshotJSON, run.CandidateCount, run.EvaluatedCount,
		run.ResultJSON, run.CreatedAt, run.CompletedAt)
	return wrapSQL("insert optimization run", err)
}

func (r *ResearchRepo) GetOptimizationRun(
	ctx context.Context, id string,
) (ResearchOptimizationRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+optimizationRunColumns+` FROM research_optimization_runs WHERE id=?`, id)
	return scanOptimizationRun(row)
}

func (r *ResearchRepo) GetOptimizationRunByTaskID(
	ctx context.Context, taskID string,
) (ResearchOptimizationRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+optimizationRunColumns+` FROM research_optimization_runs WHERE task_id=?`, taskID)
	return scanOptimizationRun(row)
}

// FindActiveOptimizationByInputHash returns a queued/running optimization
// with the same (collection, input_hash) so duplicate requests can poll.
func (r *ResearchRepo) FindActiveOptimizationByInputHash(
	ctx context.Context, collectionID, inputHash string,
) (ResearchOptimizationRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+optimizationRunColumns+` FROM research_optimization_runs
		 WHERE collection_id=? AND input_hash=? AND EXISTS (
		   SELECT 1 FROM worker_tasks t WHERE t.id=research_optimization_runs.task_id AND t.status IN (?,?,?)
		 )
		 ORDER BY created_at DESC LIMIT 1`,
		collectionID, inputHash, WorkerTaskStatusPending, WorkerTaskStatusRunning, WorkerTaskStatusPreComplete)
	return scanOptimizationRun(row)
}

// FindSucceededOptimizationByInputHash returns the succeeded optimization
// with the same (collection, input_hash).
func (r *ResearchRepo) FindSucceededOptimizationByInputHash(
	ctx context.Context, collectionID, inputHash string,
) (ResearchOptimizationRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+optimizationRunColumns+` FROM research_optimization_runs
		 WHERE collection_id=? AND input_hash=? AND EXISTS (
		   SELECT 1 FROM worker_tasks t WHERE t.id=research_optimization_runs.task_id AND t.status=?
		 )
		 ORDER BY created_at DESC LIMIT 1`,
		collectionID, inputHash, ResearchRunStatusSucceeded)
	return scanOptimizationRun(row)
}

func (r *ResearchRepo) MarkOptimizationRunning(_ context.Context, _ string) error {
	return nil
}

func (r *ResearchRepo) UpdateOptimizationProgress(
	_ context.Context, _ string, _ int,
) error {
	return nil
}

func (r *ResearchRepo) CompleteOptimizationRun(
	ctx context.Context, id, resultJSON string, evaluated int, completedAt int64,
) error {
	return r.CompleteOptimizationRunTx(ctx, nil, id, resultJSON, evaluated, completedAt)
}

func (r *ResearchRepo) CompleteOptimizationRunTx(
	ctx context.Context, tx *sql.Tx, id, resultJSON string, evaluated int, completedAt int64,
) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE research_optimization_runs
		SET result_json=?, evaluated_count=?, completed_at=?
		WHERE id=?`,
		resultJSON, evaluated, completedAt, id)
	return wrapSQL("complete optimization run", err)
}

func (r *ResearchRepo) FailOptimizationRun(
	ctx context.Context, id, _, _, _ string, completedAt int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE research_optimization_runs
		SET completed_at=? WHERE id=?`, completedAt, id)
	return wrapSQL("fail optimization run", err)
}

// LatestOptimizationByCollection returns the most recent optimization run
// for a collection (any status).
func (r *ResearchRepo) LatestOptimizationByCollection(
	ctx context.Context, collectionID string,
) (ResearchOptimizationRun, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+optimizationRunListColumns+` FROM research_optimization_runs
		 WHERE collection_id=? ORDER BY created_at DESC LIMIT 1`, collectionID)
	return scanOptimizationRun(row)
}
