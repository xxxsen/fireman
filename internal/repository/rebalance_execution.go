package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrRebalanceExecutionNotFound     = errors.New("rebalance execution not found")
	ErrNoActiveRebalanceExecution     = errors.New("no active rebalance execution")
	ErrActiveRebalanceExecutionExists = errors.New("active rebalance execution exists")
)

// RebalanceExecution is a multi-day rebalance task.
type RebalanceExecution struct {
	ID                         string `json:"id"`
	PlanID                     string `json:"plan_id"`
	Status                     string `json:"status"`
	CreatedAt                  int64  `json:"created_at"`
	UpdatedAt                  int64  `json:"updated_at"`
	StartedAt                  *int64 `json:"started_at,omitempty"`
	CompletedAt                *int64 `json:"completed_at,omitempty"`
	BaselineHoldingsTotalMinor int64  `json:"baseline_holdings_total_minor"`
	BaselineConfigVersion      int    `json:"baseline_config_version"`
	BaselineSnapshotJSON       string `json:"baseline_snapshot_json"`
	CashPoolMinor              int64  `json:"cash_pool_minor"`
	Note                       string `json:"note"`
}

// RebalanceExecutionLine is one asset row in an execution.
type RebalanceExecutionLine struct {
	ID                   string `json:"id"`
	ExecutionID          string `json:"execution_id"`
	HoldingID            string `json:"holding_id"`
	InstrumentID         string `json:"instrument_id"`
	InstrumentCode       string `json:"instrument_code,omitempty"`
	InstrumentName       string `json:"instrument_name,omitempty"`
	BaselineCurrentMinor int64  `json:"baseline_current_minor"`
	TargetDeltaMinor     int64  `json:"target_delta_minor"`
	ExecutedDeltaMinor   int64  `json:"executed_delta_minor"`
	RemainingDeltaMinor  int64  `json:"remaining_delta_minor"`
	ActionDirection      string `json:"action_direction"`
	ExecutionStatus      string `json:"execution_status"`
	SortOrder            int    `json:"sort_order"`
}

// RebalanceExecutionEvent is a timeline entry.
type RebalanceExecutionEvent struct {
	ID                 string `json:"id"`
	ExecutionID        string `json:"execution_id"`
	Seq                int    `json:"seq"`
	EventType          string `json:"event_type"`
	InstrumentID       string `json:"instrument_id,omitempty"`
	AmountMinor        int64  `json:"amount_minor"`
	CashPoolAfterMinor int64  `json:"cash_pool_after_minor"`
	PayloadJSON        string `json:"payload_json"`
	CreatedAt          int64  `json:"created_at"`
}

// RebalanceExecutionSummary is a list-row projection.
type RebalanceExecutionSummary struct {
	RebalanceExecution
	LineCount     int   `json:"line_count"`
	DoneLineCount int   `json:"done_line_count"`
	LastEventAt   int64 `json:"last_event_at,omitempty"`
}

// RebalanceExecutionRepo persists rebalance executions.
type RebalanceExecutionRepo struct {
	db *sql.DB
}

func NewRebalanceExecutionRepo(db *sql.DB) *RebalanceExecutionRepo {
	return &RebalanceExecutionRepo{db: db}
}

const executionSelectCols = `
	id, plan_id, status, created_at, updated_at, started_at, completed_at,
	baseline_holdings_total_minor, baseline_config_version, baseline_snapshot_json,
	cash_pool_minor, note`

func (r *RebalanceExecutionRepo) GetActiveByPlan(ctx context.Context, planID string) (*RebalanceExecution, error) {
	e, err := r.scanOne(ctx, `
		SELECT `+executionSelectCols+`
		FROM rebalance_executions
		WHERE plan_id=? AND status IN ('draft','in_progress')
		ORDER BY created_at DESC LIMIT 1`, planID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoActiveRebalanceExecution
	}
	if err != nil {
		return nil, wrapSQL("scan active rebalance execution", err)
	}
	return &e, nil
}

func (r *RebalanceExecutionRepo) HasActiveByPlan(ctx context.Context, planID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM rebalance_executions
		WHERE plan_id=? AND status IN ('draft','in_progress')`, planID).Scan(&count)
	if err != nil {
		return false, wrapSQL("count active rebalance executions", err)
	}
	return count > 0, nil
}

func (r *RebalanceExecutionRepo) GetByIDTx(
	ctx context.Context, tx *sql.Tx, planID, executionID string,
) (RebalanceExecution, error) {
	e, err := r.scanOneTx(ctx, tx, `
		SELECT `+executionSelectCols+`
		FROM rebalance_executions WHERE plan_id=? AND id=?`, planID, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceExecution{}, ErrRebalanceExecutionNotFound
	}
	if err != nil {
		return RebalanceExecution{}, wrapSQL("scan rebalance execution", err)
	}
	return e, nil
}

func (r *RebalanceExecutionRepo) GetByID(ctx context.Context, planID, executionID string) (RebalanceExecution, error) {
	e, err := r.scanOne(ctx, `
		SELECT `+executionSelectCols+`
		FROM rebalance_executions WHERE plan_id=? AND id=?`, planID, executionID)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceExecution{}, ErrRebalanceExecutionNotFound
	}
	if err != nil {
		return RebalanceExecution{}, wrapSQL("scan rebalance execution", err)
	}
	return e, nil
}

func (r *RebalanceExecutionRepo) scanOneTx(
	ctx context.Context, tx *sql.Tx, query string, args ...any,
) (RebalanceExecution, error) {
	var e RebalanceExecution
	var started, completed sql.NullInt64
	err := r.queryRow(ctx, tx, query, args...).Scan(
		&e.ID, &e.PlanID, &e.Status, &e.CreatedAt, &e.UpdatedAt,
		&started, &completed, &e.BaselineHoldingsTotalMinor, &e.BaselineConfigVersion,
		&e.BaselineSnapshotJSON, &e.CashPoolMinor, &e.Note,
	)
	if err != nil {
		return RebalanceExecution{}, wrapSQL("scan rebalance execution row", err)
	}
	if started.Valid {
		v := started.Int64
		e.StartedAt = &v
	}
	if completed.Valid {
		v := completed.Int64
		e.CompletedAt = &v
	}
	return e, nil
}

func (r *RebalanceExecutionRepo) scanOne(ctx context.Context, query string, args ...any) (RebalanceExecution, error) {
	var e RebalanceExecution
	var started, completed sql.NullInt64
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&e.ID, &e.PlanID, &e.Status, &e.CreatedAt, &e.UpdatedAt,
		&started, &completed, &e.BaselineHoldingsTotalMinor, &e.BaselineConfigVersion,
		&e.BaselineSnapshotJSON, &e.CashPoolMinor, &e.Note,
	)
	if err != nil {
		return RebalanceExecution{}, wrapSQL("scan rebalance execution row", err)
	}
	if started.Valid {
		v := started.Int64
		e.StartedAt = &v
	}
	if completed.Valid {
		v := completed.Int64
		e.CompletedAt = &v
	}
	return e, nil
}

func (r *RebalanceExecutionRepo) ListByPlan(ctx context.Context, planID string) ([]RebalanceExecutionSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.id, e.plan_id, e.status, e.created_at, e.updated_at, e.started_at, e.completed_at,
			e.baseline_holdings_total_minor, e.baseline_config_version, e.baseline_snapshot_json,
			e.cash_pool_minor, e.note,
			(SELECT COUNT(*) FROM rebalance_execution_lines l WHERE l.execution_id=e.id),
			(SELECT COUNT(*) FROM rebalance_execution_lines l
				WHERE l.execution_id=e.id AND l.execution_status='done'),
			COALESCE((SELECT MAX(ev.created_at) FROM rebalance_execution_events ev WHERE ev.execution_id=e.id), 0)
		FROM rebalance_executions e
		WHERE e.plan_id=?
		ORDER BY e.created_at DESC`, planID)
	if err != nil {
		return nil, wrapSQL("list rebalance executions", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RebalanceExecutionSummary
	for rows.Next() {
		var s RebalanceExecutionSummary
		var started, completed sql.NullInt64
		if err := rows.Scan(
			&s.ID, &s.PlanID, &s.Status, &s.CreatedAt, &s.UpdatedAt,
			&started, &completed, &s.BaselineHoldingsTotalMinor, &s.BaselineConfigVersion,
			&s.BaselineSnapshotJSON, &s.CashPoolMinor, &s.Note,
			&s.LineCount, &s.DoneLineCount, &s.LastEventAt,
		); err != nil {
			return nil, wrapSQL("scan rebalance execution summary", err)
		}
		if started.Valid {
			v := started.Int64
			s.StartedAt = &v
		}
		if completed.Valid {
			v := completed.Int64
			s.CompletedAt = &v
		}
		out = append(out, s)
	}
	return out, wrapSQL("iterate rebalance executions", rows.Err())
}

func (r *RebalanceExecutionRepo) CreateTx(
	ctx context.Context, tx *sql.Tx, execution RebalanceExecution, lines []RebalanceExecutionLine,
) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if execution.CreatedAt == 0 {
		execution.CreatedAt = now
	}
	execution.UpdatedAt = now
	if _, err := exec.ExecContext(
		ctx, `
		INSERT INTO rebalance_executions (
			id, plan_id, status, created_at, updated_at, started_at, completed_at,
			baseline_holdings_total_minor, baseline_config_version, baseline_snapshot_json,
			cash_pool_minor, note
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		execution.ID, execution.PlanID, execution.Status, execution.CreatedAt, execution.UpdatedAt,
		execution.StartedAt, execution.CompletedAt, execution.BaselineHoldingsTotalMinor,
		execution.BaselineConfigVersion, execution.BaselineSnapshotJSON, execution.CashPoolMinor, execution.Note,
	); err != nil {
		return fmt.Errorf("insert rebalance execution: %w", err)
	}
	for _, line := range lines {
		if _, err := exec.ExecContext(
			ctx, `
			INSERT INTO rebalance_execution_lines (
				id, execution_id, holding_id, instrument_id,
				baseline_current_minor, target_delta_minor, executed_delta_minor,
				remaining_delta_minor, action_direction, execution_status, sort_order
			) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			line.ID, execution.ID, line.HoldingID, line.InstrumentID,
			line.BaselineCurrentMinor, line.TargetDeltaMinor, line.ExecutedDeltaMinor,
			line.RemainingDeltaMinor, line.ActionDirection, line.ExecutionStatus, line.SortOrder,
		); err != nil {
			return fmt.Errorf("insert rebalance execution line: %w", err)
		}
	}
	return nil
}

func (r *RebalanceExecutionRepo) ListLines(ctx context.Context, executionID string) ([]RebalanceExecutionLine, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+executionLineSelectCols+`
		FROM rebalance_execution_lines l
		JOIN instruments i ON i.id = l.instrument_id
		WHERE l.execution_id=?
		ORDER BY l.sort_order, l.holding_id`, executionID)
	if err != nil {
		return nil, wrapSQL("list rebalance execution lines", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RebalanceExecutionLine
	for rows.Next() {
		var line RebalanceExecutionLine
		if err := rows.Scan(
			&line.ID, &line.ExecutionID, &line.HoldingID, &line.InstrumentID,
			&line.BaselineCurrentMinor, &line.TargetDeltaMinor, &line.ExecutedDeltaMinor,
			&line.RemainingDeltaMinor, &line.ActionDirection, &line.ExecutionStatus, &line.SortOrder,
			&line.InstrumentCode, &line.InstrumentName,
		); err != nil {
			return nil, wrapSQL("scan rebalance execution line", err)
		}
		out = append(out, line)
	}
	return out, wrapSQL("iterate rebalance execution lines", rows.Err())
}

const executionLineSelectCols = `
		l.id, l.execution_id, l.holding_id, l.instrument_id,
		l.baseline_current_minor, l.target_delta_minor, l.executed_delta_minor,
		l.remaining_delta_minor, l.action_direction, l.execution_status, l.sort_order,
		i.code, i.name`

func (r *RebalanceExecutionRepo) GetLineByIDTx(
	ctx context.Context, tx *sql.Tx, executionID, lineID string,
) (RebalanceExecutionLine, error) {
	var line RebalanceExecutionLine
	err := r.queryRow(ctx, tx, `
		SELECT `+executionLineSelectCols+`
		FROM rebalance_execution_lines l
		JOIN instruments i ON i.id = l.instrument_id
		WHERE l.execution_id=? AND l.id=?`, executionID, lineID).Scan(
		&line.ID, &line.ExecutionID, &line.HoldingID, &line.InstrumentID,
		&line.BaselineCurrentMinor, &line.TargetDeltaMinor, &line.ExecutedDeltaMinor,
		&line.RemainingDeltaMinor, &line.ActionDirection, &line.ExecutionStatus, &line.SortOrder,
		&line.InstrumentCode, &line.InstrumentName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceExecutionLine{}, ErrRebalanceExecutionNotFound
	}
	if err != nil {
		return RebalanceExecutionLine{}, wrapSQL("scan rebalance execution line", err)
	}
	return line, nil
}

func (r *RebalanceExecutionRepo) GetLineByID(
	ctx context.Context, executionID, lineID string,
) (RebalanceExecutionLine, error) {
	var line RebalanceExecutionLine
	err := r.db.QueryRowContext(ctx, `
		SELECT `+executionLineSelectCols+`
		FROM rebalance_execution_lines l
		JOIN instruments i ON i.id = l.instrument_id
		WHERE l.execution_id=? AND l.id=?`, executionID, lineID).Scan(
		&line.ID, &line.ExecutionID, &line.HoldingID, &line.InstrumentID,
		&line.BaselineCurrentMinor, &line.TargetDeltaMinor, &line.ExecutedDeltaMinor,
		&line.RemainingDeltaMinor, &line.ActionDirection, &line.ExecutionStatus, &line.SortOrder,
		&line.InstrumentCode, &line.InstrumentName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceExecutionLine{}, ErrRebalanceExecutionNotFound
	}
	if err != nil {
		return RebalanceExecutionLine{}, wrapSQL("scan rebalance execution line", err)
	}
	return line, nil
}

func (r *RebalanceExecutionRepo) UpdateLineTx(
	ctx context.Context, tx *sql.Tx, line RebalanceExecutionLine,
) error {
	_, err := r.exec(tx).ExecContext(
		ctx, `
		UPDATE rebalance_execution_lines SET
			executed_delta_minor=?, remaining_delta_minor=?, execution_status=?
		WHERE id=?`,
		line.ExecutedDeltaMinor, line.RemainingDeltaMinor, line.ExecutionStatus, line.ID,
	)
	return wrapSQL("update rebalance execution line", err)
}

func (r *RebalanceExecutionRepo) UpdateCashPoolTx(
	ctx context.Context, tx *sql.Tx, executionID string, cashPool int64,
) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(
		ctx, `
		UPDATE rebalance_executions SET cash_pool_minor=?, updated_at=? WHERE id=?`,
		cashPool, now, executionID,
	)
	return wrapSQL("update rebalance execution cash pool", err)
}

func (r *RebalanceExecutionRepo) TouchExecutionTx(ctx context.Context, tx *sql.Tx, executionID string) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE rebalance_executions SET updated_at=? WHERE id=?`, now, executionID)
	return wrapSQL("touch rebalance execution", err)
}

func (r *RebalanceExecutionRepo) SetStatusTx(
	ctx context.Context, tx *sql.Tx, executionID, status string, startedAt, completedAt *int64,
) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(
		ctx, `
		UPDATE rebalance_executions
		SET status=?, updated_at=?, started_at=COALESCE(?, started_at), completed_at=?
		WHERE id=?`,
		status, now, startedAt, completedAt, executionID,
	)
	return wrapSQL("set rebalance execution status", err)
}

func (r *RebalanceExecutionRepo) ListEvents(
	ctx context.Context, executionID string,
) ([]RebalanceExecutionEvent, error) {
	return queryCollect(
		ctx, r.db, `
		SELECT id, execution_id, seq, event_type, instrument_id, amount_minor,
			cash_pool_after_minor, payload_json, created_at
		FROM rebalance_execution_events WHERE execution_id=? ORDER BY seq ASC`, []any{executionID},
		func(rows *sql.Rows) (RebalanceExecutionEvent, error) {
			var e RebalanceExecutionEvent
			var instID sql.NullString
			if err := rows.Scan(
				&e.ID, &e.ExecutionID, &e.Seq, &e.EventType, &instID,
				&e.AmountMinor, &e.CashPoolAfterMinor, &e.PayloadJSON, &e.CreatedAt,
			); err != nil {
				return RebalanceExecutionEvent{}, wrapSQL("scan rebalance execution event", err)
			}
			if instID.Valid {
				e.InstrumentID = instID.String
			}
			return e, nil
		},
		"list rebalance execution events", "scan rebalance execution event", "iterate rebalance execution events",
	)
}

func (r *RebalanceExecutionRepo) NextEventSeq(ctx context.Context, tx *sql.Tx, executionID string) (int, error) {
	var maxSeq sql.NullInt64
	err := r.queryRow(ctx, tx, `
		SELECT MAX(seq) FROM rebalance_execution_events WHERE execution_id=?`, executionID).Scan(&maxSeq)
	if err != nil {
		return 0, wrapSQL("query max rebalance execution event seq", err)
	}
	if !maxSeq.Valid {
		return 1, nil
	}
	return int(maxSeq.Int64) + 1, nil
}

func (r *RebalanceExecutionRepo) InsertEventTx(ctx context.Context, tx *sql.Tx, event RebalanceExecutionEvent) error {
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().UnixMilli()
	}
	var instID any
	if event.InstrumentID != "" {
		instID = event.InstrumentID
	}
	_, err := r.exec(tx).ExecContext(
		ctx, `
		INSERT INTO rebalance_execution_events (
			id, execution_id, seq, event_type, instrument_id, amount_minor,
			cash_pool_after_minor, payload_json, created_at
		) VALUES (?,?,?,?,?,?,?,?,?)`,
		event.ID, event.ExecutionID, event.Seq, event.EventType, instID, event.AmountMinor,
		event.CashPoolAfterMinor, event.PayloadJSON, event.CreatedAt,
	)
	return wrapSQL("insert rebalance execution event", err)
}

func (r *RebalanceExecutionRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *RebalanceExecutionRepo) queryRow(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	if tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return r.db.QueryRowContext(ctx, query, args...)
}
