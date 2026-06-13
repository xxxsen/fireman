package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrRebalanceDraftNotFound = errors.New("rebalance draft not found")
	ErrActiveDraftExists      = errors.New("active rebalance draft exists")
	ErrNoActiveRebalanceDraft = errors.New("no active rebalance draft")
)

// RebalanceDraft is a persisted rebalance plan draft.
type RebalanceDraft struct {
	ID                         string `json:"id"`
	PlanID                     string `json:"plan_id"`
	Status                     string `json:"status"`
	ConfigVersion              int    `json:"config_version"`
	BaselineHoldingsTotalMinor int64  `json:"baseline_holdings_total_minor"`
	CreatedAt                  int64  `json:"created_at"`
	UpdatedAt                  int64  `json:"updated_at"`
	CommittedAt                *int64 `json:"committed_at,omitempty"`
	Note                       string `json:"note"`
}

// RebalanceDraftLine is one editable row in a draft.
type RebalanceDraftLine struct {
	ID                           string  `json:"id"`
	DraftID                      string  `json:"draft_id"`
	HoldingID                    string  `json:"holding_id"`
	InstrumentID                 string  `json:"instrument_id"`
	InstrumentCode               string  `json:"instrument_code,omitempty"`
	InstrumentName               string  `json:"instrument_name,omitempty"`
	BaselineCurrentMinor         int64   `json:"baseline_current_minor"`
	PlannedCurrentMinor          int64   `json:"planned_current_minor"`
	FrozenTargetMinor            int64   `json:"frozen_target_minor"`
	FrozenGapMinor               int64   `json:"frozen_gap_minor"`
	FrozenGapWeight              float64 `json:"frozen_gap_weight"`
	FrozenAction                 string  `json:"frozen_action"`
	FrozenSuggestedTradeMinor    int64   `json:"frozen_suggested_trade_minor"`
	RecommendedPackageDeltaMinor int64   `json:"recommended_package_delta_minor"`
	LastSavedAt                  *int64  `json:"last_saved_at,omitempty"`
}

// RebalanceDraftEvent is a timeline entry for staged changes.
type RebalanceDraftEvent struct {
	ID          string `json:"id"`
	DraftID     string `json:"draft_id"`
	Seq         int    `json:"seq"`
	EventType   string `json:"event_type"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   int64  `json:"created_at"`
}

// RebalanceDraftRepo persists rebalance plan drafts.
type RebalanceDraftRepo struct {
	db *sql.DB
}

func NewRebalanceDraftRepo(db *sql.DB) *RebalanceDraftRepo {
	return &RebalanceDraftRepo{db: db}
}

func (r *RebalanceDraftRepo) GetActiveByPlan(ctx context.Context, planID string) (*RebalanceDraft, error) {
	var d RebalanceDraft
	var committed sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, plan_id, status, config_version, baseline_holdings_total_minor,
			created_at, updated_at, committed_at, note
		FROM rebalance_drafts
		WHERE plan_id=? AND status='draft'
		ORDER BY created_at DESC LIMIT 1`, planID).Scan(
		&d.ID, &d.PlanID, &d.Status, &d.ConfigVersion, &d.BaselineHoldingsTotalMinor,
		&d.CreatedAt, &d.UpdatedAt, &committed, &d.Note,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoActiveRebalanceDraft
	}
	if err != nil {
		return nil, wrapSQL("scan active rebalance draft", err)
	}
	if committed.Valid {
		v := committed.Int64
		d.CommittedAt = &v
	}
	return &d, nil
}

func (r *RebalanceDraftRepo) GetByID(ctx context.Context, planID, draftID string) (RebalanceDraft, error) {
	var d RebalanceDraft
	var committed sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, plan_id, status, config_version, baseline_holdings_total_minor,
			created_at, updated_at, committed_at, note
		FROM rebalance_drafts WHERE plan_id=? AND id=?`, planID, draftID).Scan(
		&d.ID, &d.PlanID, &d.Status, &d.ConfigVersion, &d.BaselineHoldingsTotalMinor,
		&d.CreatedAt, &d.UpdatedAt, &committed, &d.Note,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceDraft{}, ErrRebalanceDraftNotFound
	}
	if err != nil {
		return RebalanceDraft{}, wrapSQL("scan rebalance draft", err)
	}
	if committed.Valid {
		v := committed.Int64
		d.CommittedAt = &v
	}
	return d, nil
}

func (r *RebalanceDraftRepo) CreateTx(
	ctx context.Context, tx *sql.Tx, draft RebalanceDraft, lines []RebalanceDraftLine,
) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if draft.CreatedAt == 0 {
		draft.CreatedAt = now
	}
	draft.UpdatedAt = now
	if _, err := exec.ExecContext(ctx, `
		INSERT INTO rebalance_drafts (
			id, plan_id, status, config_version, baseline_holdings_total_minor,
			created_at, updated_at, committed_at, note
		) VALUES (?,?,?,?,?,?,?,?,?)`,
		draft.ID, draft.PlanID, draft.Status, draft.ConfigVersion, draft.BaselineHoldingsTotalMinor,
		draft.CreatedAt, draft.UpdatedAt, draft.CommittedAt, draft.Note); err != nil {
		return fmt.Errorf("insert draft: %w", err)
	}
	for _, line := range lines {
		if _, err := exec.ExecContext(ctx, `
			INSERT INTO rebalance_draft_lines (
				id, draft_id, holding_id, instrument_id,
				baseline_current_minor, planned_current_minor,
				frozen_target_minor, frozen_gap_minor, frozen_gap_weight,
				frozen_action, frozen_suggested_trade_minor,
				recommended_package_delta_minor, last_saved_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			line.ID, draft.ID, line.HoldingID, line.InstrumentID,
			line.BaselineCurrentMinor, line.PlannedCurrentMinor,
			line.FrozenTargetMinor, line.FrozenGapMinor, line.FrozenGapWeight,
			line.FrozenAction, line.FrozenSuggestedTradeMinor,
			line.RecommendedPackageDeltaMinor, line.LastSavedAt); err != nil {
			return fmt.Errorf("insert draft line: %w", err)
		}
	}
	return nil
}

func (r *RebalanceDraftRepo) ListLines(ctx context.Context, draftID string) ([]RebalanceDraftLine, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT l.id, l.draft_id, l.holding_id, l.instrument_id,
			l.baseline_current_minor, l.planned_current_minor,
			l.frozen_target_minor, l.frozen_gap_minor, l.frozen_gap_weight,
			l.frozen_action, l.frozen_suggested_trade_minor,
			l.recommended_package_delta_minor, l.last_saved_at,
			i.code, i.name
		FROM rebalance_draft_lines l
		JOIN instruments i ON i.id = l.instrument_id
		WHERE l.draft_id=?
		ORDER BY l.holding_id`, draftID)
	if err != nil {
		return nil, wrapSQL("list rebalance draft lines", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RebalanceDraftLine
	for rows.Next() {
		var line RebalanceDraftLine
		var lastSaved sql.NullInt64
		if err := rows.Scan(
			&line.ID, &line.DraftID, &line.HoldingID, &line.InstrumentID,
			&line.BaselineCurrentMinor, &line.PlannedCurrentMinor,
			&line.FrozenTargetMinor, &line.FrozenGapMinor, &line.FrozenGapWeight,
			&line.FrozenAction, &line.FrozenSuggestedTradeMinor,
			&line.RecommendedPackageDeltaMinor, &lastSaved,
			&line.InstrumentCode, &line.InstrumentName,
		); err != nil {
			return nil, wrapSQL("scan rebalance draft line", err)
		}
		if lastSaved.Valid {
			v := lastSaved.Int64
			line.LastSavedAt = &v
		}
		out = append(out, line)
	}
	return out, wrapSQL("iterate rebalance draft lines", rows.Err())
}

func (r *RebalanceDraftRepo) UpdateLinePlannedTx(ctx context.Context, tx *sql.Tx, lineID string, planned int64,
	lastSaved *int64,
) error {
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE rebalance_draft_lines SET planned_current_minor=?, last_saved_at=? WHERE id=?`,
		planned, lastSaved, lineID)
	return wrapSQL("update rebalance draft line planned", err)
}

func (r *RebalanceDraftRepo) TouchDraftTx(ctx context.Context, tx *sql.Tx, draftID string) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `UPDATE rebalance_drafts SET updated_at=? WHERE id=?`, now, draftID)
	return wrapSQL("touch rebalance draft", err)
}

func (r *RebalanceDraftRepo) SetStatusTx(ctx context.Context, tx *sql.Tx, draftID, status string,
	committedAt *int64,
) error {
	now := time.Now().UnixMilli()
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE rebalance_drafts SET status=?, updated_at=?, committed_at=? WHERE id=?`,
		status, now, committedAt, draftID)
	return wrapSQL("set rebalance draft status", err)
}

func (r *RebalanceDraftRepo) ListEvents(ctx context.Context, draftID string) ([]RebalanceDraftEvent, error) {
	return queryCollect(
		ctx, r.db, `
		SELECT id, draft_id, seq, event_type, payload_json, created_at
		FROM rebalance_draft_events WHERE draft_id=? ORDER BY seq ASC`, []any{draftID},
		func(rows *sql.Rows) (RebalanceDraftEvent, error) {
			var e RebalanceDraftEvent
			if err := rows.Scan(&e.ID, &e.DraftID, &e.Seq, &e.EventType, &e.PayloadJSON, &e.CreatedAt); err != nil {
				return RebalanceDraftEvent{}, wrapSQL("scan rebalance draft event", err)
			}
			return e, nil
		},
		"list rebalance draft events", "scan rebalance draft event", "iterate rebalance draft events",
	)
}

func (r *RebalanceDraftRepo) NextEventSeq(ctx context.Context, tx *sql.Tx, draftID string) (int, error) {
	var maxSeq sql.NullInt64
	err := r.queryRow(ctx, tx, `
		SELECT MAX(seq) FROM rebalance_draft_events WHERE draft_id=?`, draftID).Scan(&maxSeq)
	if err != nil {
		return 0, wrapSQL("query max rebalance draft event seq", err)
	}
	if !maxSeq.Valid {
		return 1, nil
	}
	return int(maxSeq.Int64) + 1, nil
}

func (r *RebalanceDraftRepo) InsertEventTx(ctx context.Context, tx *sql.Tx, event RebalanceDraftEvent) error {
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().UnixMilli()
	}
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO rebalance_draft_events (id, draft_id, seq, event_type, payload_json, created_at)
		VALUES (?,?,?,?,?,?)`,
		event.ID, event.DraftID, event.Seq, event.EventType, event.PayloadJSON, event.CreatedAt)
	return wrapSQL("insert rebalance draft event", err)
}

func (r *RebalanceDraftRepo) DeleteLastStageEventTx(
	ctx context.Context, tx *sql.Tx, draftID string,
) (RebalanceDraftEvent, error) {
	var e RebalanceDraftEvent
	err := r.queryRow(ctx, tx, `
		SELECT id, draft_id, seq, event_type, payload_json, created_at
		FROM rebalance_draft_events
		WHERE draft_id=? AND event_type='stage'
		ORDER BY seq DESC LIMIT 1`, draftID).Scan(
		&e.ID, &e.DraftID, &e.Seq, &e.EventType, &e.PayloadJSON, &e.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceDraftEvent{}, ErrRebalanceDraftNotFound
	}
	if err != nil {
		return RebalanceDraftEvent{}, wrapSQL("scan last stage event", err)
	}
	if _, err := r.exec(tx).ExecContext(ctx, `DELETE FROM rebalance_draft_events WHERE id=?`, e.ID); err != nil {
		return RebalanceDraftEvent{}, wrapSQL("delete last stage event", err)
	}
	return e, nil
}

func (r *RebalanceDraftRepo) ListStageEventsTx(
	ctx context.Context, tx *sql.Tx, draftID string,
) ([]RebalanceDraftEvent, error) {
	rows, err := r.query(ctx, tx, `
		SELECT id, draft_id, seq, event_type, payload_json, created_at
		FROM rebalance_draft_events
		WHERE draft_id=? AND event_type='stage'
		ORDER BY seq ASC`, draftID)
	if err != nil {
		return nil, err
	}
	return collectRows(
		rows,
		func(rows *sql.Rows) (RebalanceDraftEvent, error) {
			var e RebalanceDraftEvent
			if err := rows.Scan(&e.ID, &e.DraftID, &e.Seq, &e.EventType, &e.PayloadJSON, &e.CreatedAt); err != nil {
				return RebalanceDraftEvent{}, wrapSQL("scan rebalance draft event", err)
			}
			return e, nil
		},
		"scan stage event", "iterate stage events",
	)
}

func (r *RebalanceDraftRepo) query(ctx context.Context, tx *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	if tx != nil {
		rows, err := tx.QueryContext(ctx, query, args...)
		return rows, wrapSQL("query rebalance draft tx", err)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	return rows, wrapSQL("query rebalance draft", err)
}

func (r *RebalanceDraftRepo) GetLineByID(ctx context.Context, draftID, lineID string) (RebalanceDraftLine, error) {
	var line RebalanceDraftLine
	var lastSaved sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT l.id, l.draft_id, l.holding_id, l.instrument_id,
			l.baseline_current_minor, l.planned_current_minor,
			l.frozen_target_minor, l.frozen_gap_minor, l.frozen_gap_weight,
			l.frozen_action, l.frozen_suggested_trade_minor,
			l.recommended_package_delta_minor, l.last_saved_at,
			i.code, i.name
		FROM rebalance_draft_lines l
		JOIN instruments i ON i.id = l.instrument_id
		WHERE l.draft_id=? AND l.id=?`, draftID, lineID).Scan(
		&line.ID, &line.DraftID, &line.HoldingID, &line.InstrumentID,
		&line.BaselineCurrentMinor, &line.PlannedCurrentMinor,
		&line.FrozenTargetMinor, &line.FrozenGapMinor, &line.FrozenGapWeight,
		&line.FrozenAction, &line.FrozenSuggestedTradeMinor,
		&line.RecommendedPackageDeltaMinor, &lastSaved,
		&line.InstrumentCode, &line.InstrumentName,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return RebalanceDraftLine{}, ErrRebalanceDraftNotFound
	}
	if err != nil {
		return RebalanceDraftLine{}, wrapSQL("scan rebalance draft line", err)
	}
	if lastSaved.Valid {
		v := lastSaved.Int64
		line.LastSavedAt = &v
	}
	return line, nil
}

func (r *RebalanceDraftRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *RebalanceDraftRepo) queryRow(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	if tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return r.db.QueryRowContext(ctx, query, args...)
}
