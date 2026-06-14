package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

//nolint:dupl // mirrors execution create validation with draft-specific active check
func validateRebalanceDraftCreate(
	ctx context.Context,
	s *RebalanceDraftService,
	planID string,
	req CreateRebalanceDraftRequest,
) (*repository.RebalanceDraft, domain.RebalanceResult, error) {
	active, err := s.drafts.GetActiveByPlan(ctx, planID)
	if err != nil && !errors.Is(err, repository.ErrNoActiveRebalanceDraft) {
		return nil, domain.RebalanceResult{}, wrapRepo("get active draft for create", err)
	}
	if active != nil && !req.ForceNew {
		return nil, domain.RebalanceResult{}, newErr("active_draft_exists", "an active rebalance draft already exists",
			map[string]any{
				"draft_id": active.ID, "created_at": active.CreatedAt,
			})
	}

	result, err := loadStructuralRebalanceForCreate(ctx, s.sql, s.rebalance, planID)
	if err != nil {
		return nil, domain.RebalanceResult{}, err
	}
	return active, result, nil
}

func buildRebalanceDraftRecords(
	planID string,
	plan repository.Plan,
	result domain.RebalanceResult,
) (repository.RebalanceDraft, []repository.RebalanceDraftLine) {
	frozen := domain.BuildFrozenDraftLines(result)
	now := time.Now().UnixMilli()
	draft := repository.RebalanceDraft{
		ID: "rbd_" + uuid.New().String(), PlanID: planID,
		Status: RebalanceDraftStatusDraft, ConfigVersion: plan.ConfigVersion,
		BaselineHoldingsTotalMinor: result.Summary.HoldingsTotalMinor,
		CreatedAt:                  now, UpdatedAt: now,
	}
	lines := make([]repository.RebalanceDraftLine, 0, len(frozen))
	for _, f := range frozen {
		lines = append(lines, repository.RebalanceDraftLine{
			ID: "rbdl_" + uuid.New().String(), DraftID: draft.ID,
			HoldingID: f.HoldingID, InstrumentID: f.InstrumentID,
			BaselineCurrentMinor: f.BaselineCurrentMinor, PlannedCurrentMinor: f.PlannedCurrentMinor,
			FrozenTargetMinor: f.FrozenTargetMinor, FrozenGapMinor: f.FrozenGapMinor,
			FrozenGapWeight: f.FrozenGapWeight, FrozenAction: f.FrozenAction,
			FrozenSuggestedTradeMinor:    f.FrozenSuggestedTradeMinor,
			RecommendedPackageDeltaMinor: f.RecommendedPackageDeltaMinor,
		})
	}
	return draft, lines
}

func (s *RebalanceDraftService) patchDraftLineItem(
	ctx context.Context,
	tx *sql.Tx,
	draftID string,
	item PatchRebalanceDraftLineItem,
	stage bool,
) error {
	if item.LineID == "" {
		return newErr("validation_failed", "line_id required", nil)
	}
	if item.PlannedCurrentMinor < 0 {
		return newErr("validation_failed", "planned amount cannot be negative", nil)
	}
	line, err := s.drafts.GetLineByID(ctx, draftID, item.LineID)
	if err != nil {
		return wrapRepo("get draft line", err)
	}
	before := line.PlannedCurrentMinor
	after := item.PlannedCurrentMinor
	var lastSaved *int64
	if stage {
		now := time.Now().UnixMilli()
		lastSaved = &now
	}
	if err := s.drafts.UpdateLinePlannedTx(ctx, tx, item.LineID, after, lastSaved); err != nil {
		return wrapRepo("update draft line planned", err)
	}
	if !stage || before == after {
		return nil
	}
	return s.insertDraftStageEvent(ctx, tx, draftID, item.LineID, line, before, after)
}

func (s *RebalanceDraftService) insertDraftStageEvent(
	ctx context.Context,
	tx *sql.Tx,
	draftID, lineID string,
	line repository.RebalanceDraftLine,
	before, after int64,
) error {
	seq, err := s.drafts.NextEventSeq(ctx, tx, draftID)
	if err != nil {
		return wrapRepo("next draft event seq", err)
	}
	label := line.InstrumentName
	if label == "" {
		label = line.InstrumentCode
	}
	payload := StageEventPayload{
		LineID: lineID, HoldingLabel: label,
		BeforeMinor: before, AfterMinor: after,
		Summary: formatStageSummary(label, before, after),
	}
	payloadJSON, _ := json.Marshal(payload)
	return wrapRepo("insert draft stage event", s.drafts.InsertEventTx(ctx, tx, repository.RebalanceDraftEvent{
		ID: "rbde_" + uuid.New().String(), DraftID: draftID,
		Seq: seq, EventType: DraftEventStage, PayloadJSON: string(payloadJSON),
	}))
}

func resolveCommitFundPool(
	net int64,
	req CommitRebalanceDraftRequest,
	existing []repository.PlanHolding,
	plannedByHolding map[string]int64,
) (int64, error) {
	if net <= amountToleranceMinor {
		return net, nil
	}
	cashHolding := findCashSweepHolding(existing)
	if req.SweepUnallocatedToCash && cashHolding != nil {
		base := cashHolding.CurrentAmountMinor
		if planned, ok := plannedByHolding[cashHolding.ID]; ok {
			base = planned
		}
		plannedByHolding[cashHolding.ID] = base + net
		return 0, nil
	}
	if !req.AcceptScaleShrink {
		code := "unallocated_to_cash_required"
		msg := "unallocated funds must be swept to cash or accept scale shrink"
		if cashHolding == nil {
			msg = "no enabled cash holding for unallocated sweep"
		}
		return net, newErr(code, msg, map[string]any{
			"net_minor": net, "has_cash_holding": cashHolding != nil,
		})
	}
	return net, nil
}

func persistDraftCreate(
	ctx context.Context,
	s *RebalanceDraftService,
	active *repository.RebalanceDraft,
	req CreateRebalanceDraftRequest,
	draft repository.RebalanceDraft,
	lines []repository.RebalanceDraftLine,
) error {
	return wrapRepo("create rebalance draft tx", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if active != nil && req.ForceNew {
			if err := s.drafts.SetStatusTx(ctx, tx, active.ID, RebalanceDraftStatusCancelled, nil); err != nil {
				return wrapRepo("cancel active draft", err)
			}
		}
		return s.drafts.CreateTx(ctx, tx, draft, lines)
	}))
}
