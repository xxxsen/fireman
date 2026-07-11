package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrAutoUpdateRuleNotFound = errors.New("auto update rule not found")
	ErrAutoUpdateFilter       = errors.New("invalid auto update filter")
)

type MarketDataAutoUpdateRule struct {
	ID               string `json:"id"`
	TargetType       string `json:"target_type"`
	SyncKey          string `json:"sync_key"`
	AssetKey         string `json:"asset_key"`
	AdjustPolicy     string `json:"adjust_policy"`
	PointType        string `json:"point_type"`
	Enabled          bool   `json:"enabled"`
	IntervalHours    int    `json:"interval_hours"`
	NextRunAt        *int64 `json:"next_run_at,omitempty"`
	LastEnqueuedAt   *int64 `json:"last_enqueued_at,omitempty"`
	LastTaskID       string `json:"last_task_id"`
	LastSuccessAt    *int64 `json:"last_success_at,omitempty"`
	LastFailedAt     *int64 `json:"last_failed_at,omitempty"`
	LastErrorCode    string `json:"last_error_code"`
	LastErrorMessage string `json:"last_error_message"`
	Version          int64  `json:"version"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

type MarketDataAutoUpdateFilter struct {
	TargetType string
	Enabled    string
	Query      string
	Limit      int
	Offset     int
}

type MarketDataAutoUpdateRepo struct {
	db *sql.DB
}

func NewMarketDataAutoUpdateRepo(db *sql.DB) *MarketDataAutoUpdateRepo {
	return &MarketDataAutoUpdateRepo{db: db}
}

const autoUpdateCols = `
	id, target_type, sync_key, asset_key, adjust_policy, point_type,
	enabled, interval_hours, next_run_at, last_enqueued_at, last_task_id,
	last_success_at, last_failed_at, last_error_code, last_error_message,
	version, created_at, updated_at`

type autoRow interface {
	Scan(...any) error
}

func scanAutoRule(row autoRow) (MarketDataAutoUpdateRule, error) {
	var rule MarketDataAutoUpdateRule
	var enabled int
	err := row.Scan(
		&rule.ID, &rule.TargetType, &rule.SyncKey, &rule.AssetKey,
		&rule.AdjustPolicy, &rule.PointType, &enabled, &rule.IntervalHours,
		&rule.NextRunAt, &rule.LastEnqueuedAt, &rule.LastTaskID,
		&rule.LastSuccessAt, &rule.LastFailedAt, &rule.LastErrorCode,
		&rule.LastErrorMessage, &rule.Version, &rule.CreatedAt, &rule.UpdatedAt,
	)
	rule.Enabled = enabled != 0
	if errors.Is(err, sql.ErrNoRows) {
		return rule, ErrAutoUpdateRuleNotFound
	}
	if err != nil {
		return rule, fmt.Errorf("scan auto update rule: %w", err)
	}
	return rule, nil
}

func (r *MarketDataAutoUpdateRepo) GetHistory(
	ctx context.Context,
	assetKey string,
	adjustPolicy string,
	pointType string,
) (MarketDataAutoUpdateRule, error) {
	row := r.db.QueryRowContext(
		ctx, `
		SELECT `+autoUpdateCols+`
		FROM market_data_auto_update_rules
		WHERE target_type='asset_history'
		  AND asset_key=? AND adjust_policy=? AND point_type=?`,
		assetKey, adjustPolicy, pointType,
	)
	return scanAutoRule(row)
}

func (r *MarketDataAutoUpdateRepo) List(
	ctx context.Context,
	filter MarketDataAutoUpdateFilter,
) ([]MarketDataAutoUpdateRule, int, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	where, args, err := autoUpdateWhere(filter)
	if err != nil {
		return nil, 0, err
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM market_data_auto_update_rules" + clause
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count auto update rules: %w", err)
	}

	listArgs := make([]any, 0, len(args)+2)
	listArgs = append(listArgs, args...)
	listArgs = append(listArgs, filter.Limit, filter.Offset)
	rows, err := r.db.QueryContext(
		ctx, `
		SELECT `+autoUpdateCols+`
		FROM market_data_auto_update_rules`+clause+`
		ORDER BY enabled DESC, next_run_at ASC, updated_at DESC
		LIMIT ? OFFSET ?`,
		listArgs...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list auto update rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]MarketDataAutoUpdateRule, 0, filter.Limit)
	for rows.Next() {
		rule, scanErr := scanAutoRule(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate auto update rules: %w", err)
	}
	return items, total, nil
}

func autoUpdateWhere(filter MarketDataAutoUpdateFilter) ([]string, []any, error) {
	where := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if filter.TargetType != "" {
		where = append(where, "target_type=?")
		args = append(args, filter.TargetType)
	}

	switch filter.Enabled {
	case "":
	case "failed":
		where = append(where, `
			((last_failed_at IS NOT NULL AND last_failed_at > COALESCE(last_success_at, 0))
			 OR EXISTS (
				SELECT 1 FROM worker_tasks t
				WHERE t.id=market_data_auto_update_rules.last_task_id
				  AND t.status IN ('failed','canceled')
			 ))`)
	case "true", "false":
		where = append(where, "enabled=?")
		args = append(args, boolInt(filter.Enabled == "true"))
	default:
		return nil, nil, fmt.Errorf("%w: enabled", ErrAutoUpdateFilter)
	}

	if query := strings.TrimSpace(filter.Query); query != "" {
		where = append(where, `
			(sync_key LIKE ? OR asset_key LIKE ? OR EXISTS (
				SELECT 1 FROM market_assets a
				WHERE a.asset_key=market_data_auto_update_rules.asset_key
				  AND a.name LIKE ?
			))`)
		like := "%" + query + "%"
		args = append(args, like, like, like)
	}
	return where, args, nil
}

func (r *MarketDataAutoUpdateRepo) UpsertDirectory(
	ctx context.Context,
	syncKey string,
	intervalHours int,
	now int64,
) (MarketDataAutoUpdateRule, error) {
	id := "aur_" + uuid.NewString()
	_, err := r.db.ExecContext(
		ctx, `
		INSERT INTO market_data_auto_update_rules (
			id, target_type, sync_key, interval_hours,
			next_run_at, created_at, updated_at
		) VALUES (?, 'directory_unit', ?, ?, ?, ?, ?)
		ON CONFLICT(target_type, sync_key) WHERE target_type='directory_unit'
		DO UPDATE SET
			enabled=1,
			interval_hours=excluded.interval_hours,
			next_run_at=excluded.next_run_at,
			version=market_data_auto_update_rules.version+1,
			updated_at=excluded.updated_at`,
		id, syncKey, intervalHours, now, now, now,
	)
	if err != nil {
		return MarketDataAutoUpdateRule{}, fmt.Errorf("upsert directory auto update rule: %w", err)
	}
	return r.getDirectory(ctx, syncKey)
}

func (r *MarketDataAutoUpdateRepo) getDirectory(
	ctx context.Context,
	syncKey string,
) (MarketDataAutoUpdateRule, error) {
	row := r.db.QueryRowContext(
		ctx, `
		SELECT `+autoUpdateCols+`
		FROM market_data_auto_update_rules
		WHERE target_type='directory_unit' AND sync_key=?`,
		syncKey,
	)
	return scanAutoRule(row)
}

func (r *MarketDataAutoUpdateRepo) EnableHistory(
	ctx context.Context,
	assetKey string,
	adjustPolicy string,
	pointType string,
	now int64,
) (MarketDataAutoUpdateRule, error) {
	id := "aur_" + uuid.NewString()
	_, err := r.db.ExecContext(
		ctx, `
		INSERT INTO market_data_auto_update_rules (
			id, target_type, asset_key, adjust_policy, point_type,
			interval_hours, next_run_at, created_at, updated_at
		) VALUES (?, 'asset_history', ?, ?, ?, 24, ?, ?, ?)
		ON CONFLICT(target_type, asset_key, adjust_policy, point_type)
			WHERE target_type='asset_history'
		DO UPDATE SET
			enabled=1,
			next_run_at=excluded.next_run_at,
			version=market_data_auto_update_rules.version+1,
			updated_at=excluded.updated_at`,
		id, assetKey, adjustPolicy, pointType, now, now, now,
	)
	if err != nil {
		return MarketDataAutoUpdateRule{}, fmt.Errorf("enable history auto update rule: %w", err)
	}
	return r.GetHistory(ctx, assetKey, adjustPolicy, pointType)
}

func (r *MarketDataAutoUpdateRepo) Update(
	ctx context.Context,
	id string,
	version int64,
	enabled bool,
	intervalHours int,
	now int64,
) (MarketDataAutoUpdateRule, error) {
	nextAfterPeriod := now + int64(intervalHours)*3_600_000
	result, err := r.db.ExecContext(
		ctx, `
		UPDATE market_data_auto_update_rules
		SET next_run_at=CASE
				WHEN ?=0 THEN NULL
				WHEN enabled=0 THEN ?
				ELSE ?
			END,
			enabled=?, interval_hours=?, version=version+1, updated_at=?
		WHERE id=? AND version=?`,
		boolInt(enabled), now, nextAfterPeriod,
		boolInt(enabled), intervalHours, now, id, version,
	)
	if err != nil {
		return MarketDataAutoUpdateRule{}, fmt.Errorf("update auto update rule: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return MarketDataAutoUpdateRule{}, fmt.Errorf("read updated auto update rows: %w", err)
	}
	if rowsAffected == 0 {
		return MarketDataAutoUpdateRule{}, ErrAutoUpdateRuleNotFound
	}
	return r.Get(ctx, id)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (r *MarketDataAutoUpdateRepo) Get(
	ctx context.Context,
	id string,
) (MarketDataAutoUpdateRule, error) {
	row := r.db.QueryRowContext(
		ctx, `
		SELECT `+autoUpdateCols+`
		FROM market_data_auto_update_rules
		WHERE id=?`,
		id,
	)
	return scanAutoRule(row)
}

func (r *MarketDataAutoUpdateRepo) Due(
	ctx context.Context,
	now int64,
	limit int,
) ([]MarketDataAutoUpdateRule, error) {
	rows, err := r.db.QueryContext(
		ctx, `
		SELECT `+autoUpdateCols+`
		FROM market_data_auto_update_rules
		WHERE enabled=1 AND next_run_at IS NOT NULL AND next_run_at<=?
		ORDER BY next_run_at
		LIMIT ?`,
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due auto update rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]MarketDataAutoUpdateRule, 0, limit)
	for rows.Next() {
		rule, scanErr := scanAutoRule(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due auto update rules: %w", err)
	}
	return items, nil
}

func (r *MarketDataAutoUpdateRepo) BindTask(
	ctx context.Context,
	id string,
	version int64,
	taskID string,
	now int64,
	next int64,
) error {
	return r.bindTask(ctx, r.db, id, version, taskID, now, next)
}

func (r *MarketDataAutoUpdateRepo) BindTaskTx(
	ctx context.Context,
	tx *sql.Tx,
	id string,
	version int64,
	taskID string,
	now int64,
	next int64,
) error {
	return r.bindTask(ctx, tx, id, version, taskID, now, next)
}

type autoUpdateExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (r *MarketDataAutoUpdateRepo) bindTask(
	ctx context.Context,
	execer autoUpdateExecer,
	id string,
	version int64,
	taskID string,
	now int64,
	next int64,
) error {
	result, err := execer.ExecContext(
		ctx, `
		UPDATE market_data_auto_update_rules
		SET last_task_id=?, last_enqueued_at=?, next_run_at=?,
			version=version+1, updated_at=?
		WHERE id=? AND version=? AND enabled=1`,
		taskID, now, next, now, id, version,
	)
	if err != nil {
		return fmt.Errorf("bind auto update task: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read bound auto update rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrAutoUpdateRuleNotFound
	}
	return nil
}

func (r *MarketDataAutoUpdateRepo) MarkScheduleFailure(
	ctx context.Context,
	id string,
	version int64,
	code string,
	message string,
	now int64,
	next int64,
) error {
	result, err := r.db.ExecContext(
		ctx, `
		UPDATE market_data_auto_update_rules
		SET last_failed_at=?, last_error_code=?, last_error_message=?,
			next_run_at=?, version=version+1, updated_at=?
		WHERE id=? AND version=? AND enabled=1`,
		now, code, message, next, now, id, version,
	)
	if err != nil {
		return fmt.Errorf("mark auto update schedule failure: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read failed auto update rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrAutoUpdateRuleNotFound
	}
	return nil
}

func (r *MarketDataAutoUpdateRepo) MarkTaskSuccess(
	ctx context.Context,
	taskID string,
	at int64,
) error {
	_, err := r.db.ExecContext(
		ctx, `
		UPDATE market_data_auto_update_rules
		SET last_success_at=?, last_error_code='', last_error_message='', updated_at=?
		WHERE last_task_id=?`,
		at, at, taskID,
	)
	if err != nil {
		return fmt.Errorf("mark auto update task success: %w", err)
	}
	return nil
}

func (r *MarketDataAutoUpdateRepo) Reconcile(ctx context.Context, now int64) error {
	_, err := r.db.ExecContext(
		ctx, `
		UPDATE market_data_auto_update_rules
		SET last_success_at=COALESCE((
				SELECT finished_at FROM worker_tasks t
				WHERE t.id=last_task_id AND t.status='complete'
			), last_success_at),
			last_failed_at=CASE
				WHEN (SELECT status FROM worker_tasks t WHERE t.id=last_task_id)
					IN ('failed','canceled')
				THEN COALESCE((
					SELECT finished_at FROM worker_tasks t WHERE t.id=last_task_id
				), ?)
				ELSE last_failed_at
			END,
			last_error_code=CASE
				WHEN (SELECT status FROM worker_tasks t WHERE t.id=last_task_id)
					IN ('failed','canceled')
				THEN COALESCE((
					SELECT error_code FROM worker_tasks t WHERE t.id=last_task_id
				), '')
				ELSE last_error_code
			END,
			last_error_message=CASE
				WHEN (SELECT status FROM worker_tasks t WHERE t.id=last_task_id)
					IN ('failed','canceled')
				THEN COALESCE((
					SELECT error_message FROM worker_tasks t WHERE t.id=last_task_id
				), '')
				ELSE last_error_message
			END,
			updated_at=?
		WHERE last_task_id<>''`,
		now, now,
	)
	if err != nil {
		return fmt.Errorf("reconcile auto update rules: %w", err)
	}
	return nil
}
