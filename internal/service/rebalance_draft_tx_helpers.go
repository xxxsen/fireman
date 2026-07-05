package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

func (s *RebalanceDraftService) undoDraftStageTx(
	ctx context.Context,
	tx *sql.Tx,
	draftID string,
) error {
	event, err := s.drafts.DeleteLastStageEventTx(ctx, tx, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return newErr("validation_failed", "no staged change to undo", nil)
		}
		return wrapRepo("delete last stage event", err)
	}
	var payload StageEventPayload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		return wrapRepo("unmarshal undo payload", err)
	}
	line, err := s.drafts.GetLineByID(ctx, tx, draftID, payload.LineID)
	if err != nil {
		return wrapRepo("get line for undo", err)
	}
	var lastSaved *int64
	if payload.BeforeMinor != line.BaselineCurrentMinor {
		remaining, err := s.drafts.ListStageEventsTx(ctx, tx, draftID)
		if err != nil {
			return wrapRepo("list stage events for undo", err)
		}
		lastSaved = findLastSavedFromStageEvents(remaining, payload.LineID, payload.BeforeMinor)
	}
	if err := s.drafts.UpdateLinePlannedTx(ctx, tx, payload.LineID, payload.BeforeMinor, lastSaved); err != nil {
		return wrapRepo("revert draft line for undo", err)
	}
	return s.insertUndoEvent(ctx, tx, draftID, event.ID, payload)
}

func (s *RebalanceDraftService) insertUndoEvent(
	ctx context.Context,
	tx *sql.Tx,
	draftID, undidEventID string,
	payload StageEventPayload,
) error {
	seq, err := s.drafts.NextEventSeq(ctx, tx, draftID)
	if err != nil {
		return wrapRepo("next undo event seq", err)
	}
	undoPayload, _ := json.Marshal(map[string]any{
		"undid_event_id": undidEventID, "line_id": payload.LineID,
		"reverted_to_minor": payload.BeforeMinor,
		"summary":           formatUndoSummary(payload),
	})
	return wrapRepo("insert undo event", s.drafts.InsertEventTx(ctx, tx, repository.RebalanceDraftEvent{
		ID: "rbde_" + uuid.New().String(), DraftID: draftID,
		Seq: seq, EventType: DraftEventUndo, PayloadJSON: string(undoPayload),
	}))
}

func validateCommitPlanVersions(
	req CommitRebalanceDraftRequest,
	plan repository.Plan,
	draft repository.RebalanceDraft,
) error {
	if req.ConfigVersion != plan.ConfigVersion {
		return newErr("plan_version_conflict",
			"plan configuration changed since draft creation; abandon and recreate", nil)
	}
	if draft.ConfigVersion != plan.ConfigVersion {
		return newErr("plan_version_conflict",
			"plan configuration changed since draft creation; abandon and recreate", nil)
	}
	return nil
}

func buildCommitHoldingsRequest(
	req CommitRebalanceDraftRequest,
	existing []repository.PlanHolding,
	plannedByHolding map[string]int64,
) HoldingsUpdateRequest {
	holdingsReq := HoldingsUpdateRequest{
		ConfigVersion: req.ConfigVersion,
		Holdings:      make([]HoldingWriteItem, 0, len(existing)),
	}
	for _, h := range existing {
		amount := h.CurrentAmountMinor
		if planned, ok := plannedByHolding[h.ID]; ok {
			amount = planned
		}
		holdingsReq.Holdings = append(holdingsReq.Holdings, HoldingWriteItem{
			AssetKey: h.AssetKey, Enabled: h.Enabled,
			AssetClass: h.AssetClass, Region: h.Region,
			WeightWithinGroup: h.WeightWithinGroup, CurrentAmountMinor: amount,
			SortOrder: h.SortOrder,
		})
	}
	return holdingsReq
}

func (s *PlanService) saveWizardPlanTx(
	ctx context.Context,
	plan repository.Plan,
	params repository.PlanParameters,
	alloc repository.PlanAllocation,
	pending []wizardPendingSnap,
	built []repository.PlanHolding,
) error {
	return wrapRepo("create wizard tx", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.plans.CreateTx(ctx, tx, plan); err != nil {
			return wrapRepo("create plan in wizard", err)
		}
		for _, ps := range pending {
			if ps.skip {
				continue
			}
			if err := s.snapSvc.CreatePlanSnapshotTx(ctx, tx, ps.snap); err != nil {
				return wrapRepo("create wizard snapshot", err)
			}
		}
		if err := s.params.Upsert(ctx, tx, params); err != nil {
			return wrapRepo("upsert wizard parameters", err)
		}
		if err := s.alloc.Replace(ctx, tx, plan.ID, alloc); err != nil {
			return wrapRepo("replace wizard allocation", err)
		}
		return s.holdings.Replace(ctx, tx, plan.ID, built)
	}))
}

func commitDraftHoldingsTx(
	ctx context.Context,
	s *RebalanceDraftService,
	planID, draftID string,
	req CommitRebalanceDraftRequest,
	prep *preparedHoldingsUpdate,
) error {
	return wrapRepo("commit draft tx", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.holdingsSvc.applyHoldingsUpdateTx(ctx, tx, planID, req.ConfigVersion, prep); err != nil {
			return wrapRepo("apply holdings update for commit", err)
		}
		now := time.Now().UnixMilli()
		if err := s.drafts.SetStatusTx(ctx, tx, draftID, RebalanceDraftStatusCommitted, &now); err != nil {
			return wrapRepo("set draft committed status", err)
		}
		seq, err := s.drafts.NextEventSeq(ctx, tx, draftID)
		if err != nil {
			return wrapRepo("next commit event seq", err)
		}
		commitPayload, _ := json.Marshal(map[string]any{"committed_at": now})
		return wrapRepo("insert commit event", s.drafts.InsertEventTx(ctx, tx, repository.RebalanceDraftEvent{
			ID: "rbde_" + uuid.New().String(), DraftID: draftID,
			Seq: seq, EventType: DraftEventCommit, PayloadJSON: string(commitPayload),
		}))
	}))
}
