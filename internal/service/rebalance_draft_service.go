package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

const (
	RebalanceDraftStatusDraft     = "draft"
	RebalanceDraftStatusCommitted = "committed"
	RebalanceDraftStatusCancelled = "cancelled"

	DraftEventStage  = "stage"
	DraftEventUndo   = "undo"
	DraftEventCommit = "commit"

	amountToleranceMinor = 100
)

// RebalanceDraftService manages rebalance plan drafts.
type RebalanceDraftService struct {
	sql         *sql.DB
	plans       *repository.PlanRepo
	drafts      *repository.RebalanceDraftRepo
	holdings    *repository.HoldingsRepo
	holdingsSvc *HoldingsService
	rebalance   *RebalanceService
	snapRepo    *repository.PortfolioSnapshotRepo
}

func NewRebalanceDraftService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	drafts *repository.RebalanceDraftRepo,
	holdings *repository.HoldingsRepo,
	holdingsSvc *HoldingsService,
	rebalance *RebalanceService,
) *RebalanceDraftService {
	return &RebalanceDraftService{
		sql: sqlDB, plans: plans, drafts: drafts, holdings: holdings,
		holdingsSvc: holdingsSvc, rebalance: rebalance,
		snapRepo: repository.NewPortfolioSnapshotRepo(sqlDB),
	}
}

type RebalanceDraftDetail struct {
	Draft    repository.RebalanceDraft        `json:"draft"`
	Lines    []repository.RebalanceDraftLine  `json:"lines"`
	Events   []repository.RebalanceDraftEvent `json:"events"`
	FundPool domain.DraftFundPool             `json:"fund_pool"`
}

type CreateRebalanceDraftRequest struct {
	ForceNew bool `json:"force_new"`
}

type PatchRebalanceDraftLinesRequest struct {
	Lines []PatchRebalanceDraftLineItem `json:"lines"`
	Stage bool                          `json:"stage"`
}

type PatchRebalanceDraftLineItem struct {
	LineID              string `json:"line_id"`
	PlannedCurrentMinor int64  `json:"planned_current_minor"`
}

type CommitRebalanceDraftRequest struct {
	ConfigVersion          int    `json:"config_version"`
	ConfirmImbalanced      bool   `json:"confirm_imbalanced"`
	SweepUnallocatedToCash bool   `json:"sweep_unallocated_to_cash"`
	AcceptScaleShrink      bool   `json:"accept_scale_shrink"`
	RecordSnapshot         bool   `json:"record_snapshot"`
	SnapshotNote           string `json:"snapshot_note"`
}

type StageEventPayload struct {
	LineID       string `json:"line_id"`
	HoldingLabel string `json:"holding_label"`
	BeforeMinor  int64  `json:"before_minor"`
	AfterMinor   int64  `json:"after_minor"`
	Summary      string `json:"summary"`
}

func (s *RebalanceDraftService) GetActive(ctx context.Context, planID string) (*RebalanceDraftDetail, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, err
	}
	draft, err := s.drafts.GetActiveByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	if draft == nil {
		return nil, nil
	}
	detail, err := s.buildDetail(ctx, *draft)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func (s *RebalanceDraftService) Get(ctx context.Context, planID, draftID string) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, err
	}
	return s.buildDetail(ctx, draft)
}

func (s *RebalanceDraftService) Create(ctx context.Context, planID string, req CreateRebalanceDraftRequest) (RebalanceDraftDetail, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return RebalanceDraftDetail{}, newErr("plan_not_found", "plan not found", nil)
		}
		return RebalanceDraftDetail{}, err
	}

	active, err := s.drafts.GetActiveByPlan(ctx, planID)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	if active != nil && !req.ForceNew {
		return RebalanceDraftDetail{}, newErr("active_draft_exists", "an active rebalance draft already exists", map[string]any{
			"draft_id": active.ID, "created_at": active.CreatedAt,
		})
	}

	result, err := s.rebalance.GetRebalance(ctx, planID, domain.RebalanceModeFull, 0)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	if result.Summary.HoldingsTotalMinor <= 0 {
		return RebalanceDraftDetail{}, newErr("validation_failed", "no enabled holdings", nil)
	}
	if result.Summary.StructuralActionableCount <= 0 {
		return RebalanceDraftDetail{}, newErr("validation_failed", "no structural rebalance actions", nil)
	}
	maxGap := maxStructuralGapWeight(result.Lines)
	params, err := repository.NewParametersRepo(s.sql).Get(ctx, planID)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	if math.Abs(maxGap) <= params.RebalanceThreshold {
		return RebalanceDraftDetail{}, newErr("validation_failed", "structural gap below rebalance threshold", nil)
	}

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

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if active != nil && req.ForceNew {
			if err := s.drafts.SetStatusTx(ctx, tx, active.ID, RebalanceDraftStatusCancelled, nil); err != nil {
				return err
			}
		}
		return s.drafts.CreateTx(ctx, tx, draft, lines)
	})
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, getErr := s.drafts.GetActiveByPlan(ctx, planID)
			if getErr == nil && existing != nil {
				return RebalanceDraftDetail{}, newErr("active_draft_exists", "an active rebalance draft already exists", map[string]any{
					"draft_id": existing.ID, "created_at": existing.CreatedAt,
				})
			}
		}
		return RebalanceDraftDetail{}, err
	}
	return s.Get(ctx, planID, draft.ID)
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func (s *RebalanceDraftService) PatchLines(ctx context.Context, planID, draftID string, req PatchRebalanceDraftLinesRequest) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, err
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return RebalanceDraftDetail{}, newErr("validation_failed", "draft is not editable", nil)
	}
	if len(req.Lines) == 0 {
		return RebalanceDraftDetail{}, newErr("validation_failed", "lines required", nil)
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		for _, item := range req.Lines {
			if item.LineID == "" {
				return newErr("validation_failed", "line_id required", nil)
			}
			if item.PlannedCurrentMinor < 0 {
				return newErr("validation_failed", "planned amount cannot be negative", nil)
			}
			line, err := s.drafts.GetLineByID(ctx, draftID, item.LineID)
			if err != nil {
				return err
			}
			before := line.PlannedCurrentMinor
			after := item.PlannedCurrentMinor
			var lastSaved *int64
			if req.Stage {
				now := time.Now().UnixMilli()
				lastSaved = &now
			}
			if err := s.drafts.UpdateLinePlannedTx(ctx, tx, item.LineID, after, lastSaved); err != nil {
				return err
			}
			if req.Stage && before != after {
				seq, err := s.drafts.NextEventSeq(ctx, tx, draftID)
				if err != nil {
					return err
				}
				label := line.InstrumentName
				if label == "" {
					label = line.InstrumentCode
				}
				payload := StageEventPayload{
					LineID: item.LineID, HoldingLabel: label,
					BeforeMinor: before, AfterMinor: after,
					Summary: formatStageSummary(label, before, after),
				}
				payloadJSON, _ := json.Marshal(payload)
				if err := s.drafts.InsertEventTx(ctx, tx, repository.RebalanceDraftEvent{
					ID: "rbde_" + uuid.New().String(), DraftID: draftID,
					Seq: seq, EventType: DraftEventStage, PayloadJSON: string(payloadJSON),
				}); err != nil {
					return err
				}
			}
		}
		return s.drafts.TouchDraftTx(ctx, tx, draftID)
	})
	if err != nil {
		if appErr, ok := err.(*AppError); ok {
			return RebalanceDraftDetail{}, appErr
		}
		return RebalanceDraftDetail{}, err
	}
	return s.Get(ctx, planID, draftID)
}

func (s *RebalanceDraftService) Undo(ctx context.Context, planID, draftID string) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, err
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return RebalanceDraftDetail{}, newErr("validation_failed", "draft is not editable", nil)
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		event, err := s.drafts.DeleteLastStageEventTx(ctx, tx, draftID)
		if err != nil {
			if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
				return newErr("validation_failed", "no staged change to undo", nil)
			}
			return err
		}
		var payload StageEventPayload
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			return err
		}
		line, err := s.drafts.GetLineByID(ctx, draftID, payload.LineID)
		if err != nil {
			return err
		}
		var lastSaved *int64
		if payload.BeforeMinor != line.BaselineCurrentMinor {
			remaining, err := s.drafts.ListStageEventsTx(ctx, tx, draftID)
			if err != nil {
				return err
			}
			lastSaved = findLastSavedFromStageEvents(remaining, payload.LineID, payload.BeforeMinor)
		}
		if err := s.drafts.UpdateLinePlannedTx(ctx, tx, payload.LineID, payload.BeforeMinor, lastSaved); err != nil {
			return err
		}
		seq, err := s.drafts.NextEventSeq(ctx, tx, draftID)
		if err != nil {
			return err
		}
		undoPayload, _ := json.Marshal(map[string]any{
			"undid_event_id": event.ID, "line_id": payload.LineID,
			"reverted_to_minor": payload.BeforeMinor,
			"summary":           formatUndoSummary(payload),
		})
		if err := s.drafts.InsertEventTx(ctx, tx, repository.RebalanceDraftEvent{
			ID: "rbde_" + uuid.New().String(), DraftID: draftID,
			Seq: seq, EventType: DraftEventUndo, PayloadJSON: string(undoPayload),
		}); err != nil {
			return err
		}
		return s.drafts.TouchDraftTx(ctx, tx, draftID)
	})
	if err != nil {
		if appErr, ok := err.(*AppError); ok {
			return RebalanceDraftDetail{}, appErr
		}
		return RebalanceDraftDetail{}, err
	}
	return s.Get(ctx, planID, draftID)
}

func (s *RebalanceDraftService) Commit(ctx context.Context, planID, draftID string, req CommitRebalanceDraftRequest) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, err
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return RebalanceDraftDetail{}, newErr("validation_failed", "draft is not editable", nil)
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return RebalanceDraftDetail{}, newErr("plan_version_conflict", "plan configuration changed since draft creation; abandon and recreate", nil)
	}
	if draft.ConfigVersion != plan.ConfigVersion {
		return RebalanceDraftDetail{}, newErr("plan_version_conflict", "plan configuration changed since draft creation; abandon and recreate", nil)
	}

	detail, err := s.buildDetail(ctx, draft)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	for _, line := range detail.Lines {
		if line.PlannedCurrentMinor < 0 {
			return RebalanceDraftDetail{}, newErr("validation_failed", "planned amount cannot be negative", nil)
		}
	}
	net := detail.FundPool.NetMinor
	existing, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	plannedByHolding := make(map[string]int64, len(detail.Lines))
	for _, line := range detail.Lines {
		plannedByHolding[line.HoldingID] = line.PlannedCurrentMinor
	}

	if net > amountToleranceMinor {
		cashHolding := findCashSweepHolding(existing)
		if req.SweepUnallocatedToCash && cashHolding != nil {
			base := cashHolding.CurrentAmountMinor
			if planned, ok := plannedByHolding[cashHolding.ID]; ok {
				base = planned
			}
			plannedByHolding[cashHolding.ID] = base + net
			net = 0
		} else if !req.AcceptScaleShrink {
			code := "unallocated_to_cash_required"
			msg := "unallocated funds must be swept to cash or accept scale shrink"
			if cashHolding == nil {
				msg = "no enabled cash holding for unallocated sweep"
			}
			return RebalanceDraftDetail{}, newErr(code, msg, map[string]any{
				"net_minor": net, "has_cash_holding": cashHolding != nil,
			})
		}
	}
	if math.Abs(float64(net)) > amountToleranceMinor && !req.ConfirmImbalanced && !req.AcceptScaleShrink {
		return RebalanceDraftDetail{}, newErr("fund_pool_imbalanced", "rebalance fund pool is not balanced", map[string]any{
			"net_minor": net,
		})
	}
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
			InstrumentID: h.InstrumentID, Enabled: h.Enabled,
			WeightWithinGroup: h.WeightWithinGroup, CurrentAmountMinor: amount,
			SortOrder: h.SortOrder,
		})
	}

	prep, err := s.holdingsSvc.prepareHoldingsUpdate(ctx, planID, holdingsReq)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.holdingsSvc.applyHoldingsUpdateTx(ctx, tx, planID, req.ConfigVersion, prep); err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		if err := s.drafts.SetStatusTx(ctx, tx, draftID, RebalanceDraftStatusCommitted, &now); err != nil {
			return err
		}
		seq, err := s.drafts.NextEventSeq(ctx, tx, draftID)
		if err != nil {
			return err
		}
		commitPayload, _ := json.Marshal(map[string]any{"committed_at": now})
		return s.drafts.InsertEventTx(ctx, tx, repository.RebalanceDraftEvent{
			ID: "rbde_" + uuid.New().String(), DraftID: draftID,
			Seq: seq, EventType: DraftEventCommit, PayloadJSON: string(commitPayload),
		})
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return RebalanceDraftDetail{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return RebalanceDraftDetail{}, err
	}

	if req.RecordSnapshot {
		// Use commit-final plannedByHolding (includes cash sweep), not draft lines alone.
		items := make([]repository.PortfolioSnapshotItem, 0, len(existing))
		var total int64
		for _, h := range existing {
			amount := h.CurrentAmountMinor
			if planned, ok := plannedByHolding[h.ID]; ok {
				amount = planned
			}
			items = append(items, repository.PortfolioSnapshotItem{
				InstrumentID: h.InstrumentID, AmountMinor: amount,
			})
			total += amount
		}
		note := req.SnapshotNote
		if note == "" {
			note = "调仓计划提交后记录"
		}
		snap := repository.PortfolioSnapshot{
			ID: "psnap_" + uuid.New().String(), PlanID: planID,
			SnapshotDate: plan.ValuationDate, TotalAmountMinor: total, Note: note, Items: items,
		}
		if err := s.snapRepo.Create(ctx, snap); err != nil {
			slog.WarnContext(ctx, "rebalance draft commit snapshot failed",
				"plan_id", planID, "draft_id", draftID, "error", err)
		}
	}

	return s.Get(ctx, planID, draftID)
}

func (s *RebalanceDraftService) Cancel(ctx context.Context, planID, draftID string) error {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return err
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return newErr("validation_failed", "draft is not cancellable", nil)
	}
	return s.drafts.SetStatusTx(ctx, nil, draftID, RebalanceDraftStatusCancelled, nil)
}

func (s *RebalanceDraftService) buildDetail(ctx context.Context, draft repository.RebalanceDraft) (RebalanceDraftDetail, error) {
	lines, err := s.drafts.ListLines(ctx, draft.ID)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	events, err := s.drafts.ListEvents(ctx, draft.ID)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	frozen := make([]domain.FrozenDraftLine, 0, len(lines))
	for _, line := range lines {
		frozen = append(frozen, domain.FrozenDraftLine{
			HoldingID: line.HoldingID, InstrumentID: line.InstrumentID,
			BaselineCurrentMinor: line.BaselineCurrentMinor, PlannedCurrentMinor: line.PlannedCurrentMinor,
			FrozenTargetMinor: line.FrozenTargetMinor, FrozenGapMinor: line.FrozenGapMinor,
			FrozenGapWeight: line.FrozenGapWeight, FrozenAction: line.FrozenAction,
			FrozenSuggestedTradeMinor:    line.FrozenSuggestedTradeMinor,
			RecommendedPackageDeltaMinor: line.RecommendedPackageDeltaMinor,
		})
	}
	return RebalanceDraftDetail{
		Draft: draft, Lines: lines, Events: events,
		FundPool: domain.ComputeDraftFundPool(frozen),
	}, nil
}

func maxStructuralGapWeight(lines []domain.RebalanceLine) float64 {
	var max float64
	for _, line := range lines {
		if !line.Enabled {
			continue
		}
		if w := math.Abs(line.StructuralGapWeight); w > max {
			max = w
		}
	}
	return max
}

func formatUndoSummary(payload StageEventPayload) string {
	if payload.Summary != "" {
		return "撤销：" + payload.Summary
	}
	return fmt.Sprintf("撤销 %s 的调整", payload.HoldingLabel)
}

func findLastSavedFromStageEvents(events []repository.RebalanceDraftEvent, lineID string, plannedMinor int64) *int64 {
	for i := len(events) - 1; i >= 0; i-- {
		var payload StageEventPayload
		if err := json.Unmarshal([]byte(events[i].PayloadJSON), &payload); err != nil {
			continue
		}
		if payload.LineID == lineID && payload.AfterMinor == plannedMinor {
			at := events[i].CreatedAt
			return &at
		}
	}
	return nil
}

func formatStageSummary(label string, before, after int64) string {
	action := "调整"
	if after < before {
		action = "缩减"
	} else if after > before {
		action = "增配"
	}
	return fmt.Sprintf("%s %s：%s → %s", action, label, formatWan(before), formatWan(after))
}

func formatWan(minor int64) string {
	yuan := float64(minor) / 100.0
	if yuan >= 10_000 {
		return fmt.Sprintf("%.0fw", yuan/10_000)
	}
	return fmt.Sprintf("%.0f", yuan)
}

// findCashSweepHolding picks enabled system cash, else enabled cash asset_class (min sort_order).
func findCashSweepHolding(holdings []repository.PlanHolding) *repository.PlanHolding {
	var fallback *repository.PlanHolding
	for i := range holdings {
		h := &holdings[i]
		if !h.Enabled {
			continue
		}
		if h.InstrumentID == repository.SystemCashInstrumentID {
			return h
		}
		if h.AssetClass == domain.AssetClassCash {
			if fallback == nil || h.SortOrder < fallback.SortOrder {
				fallback = h
			}
		}
	}
	return fallback
}
