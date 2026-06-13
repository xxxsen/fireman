package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

const (
	RebalanceDraftStatusDraft     = "draft"
	RebalanceDraftStatusCommitted = "committed"
	RebalanceDraftStatusCancelled = "canceled"

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
		return nil, wrapRepo("get plan", err)
	}
	draft, err := s.drafts.GetActiveByPlan(ctx, planID)
	if errors.Is(err, repository.ErrNoActiveRebalanceDraft) {
		return nil, repository.ErrNoActiveRebalanceDraft
	}
	if err != nil {
		return nil, wrapRepo("get active draft", err)
	}
	detail, err := s.buildDetail(ctx, *draft)
	if err != nil {
		return nil, wrapRepo("build rebalance draft detail", err)
	}
	return &detail, nil
}

func (s *RebalanceDraftService) Get(ctx context.Context, planID, draftID string) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, wrapRepo("get rebalance draft", err)
	}
	return s.buildDetail(ctx, draft)
}

func (s *RebalanceDraftService) Create(ctx context.Context, planID string,
	req CreateRebalanceDraftRequest,
) (RebalanceDraftDetail, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return RebalanceDraftDetail{}, newErr("plan_not_found", "plan not found", nil)
		}
		return RebalanceDraftDetail{}, wrapRepo("get plan for draft create", err)
	}

	active, result, err := validateRebalanceDraftCreate(ctx, s, planID, req)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}

	draft, lines := buildRebalanceDraftRecords(planID, plan, result)
	err = persistDraftCreate(ctx, s, active, req, draft, lines)
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, getErr := s.drafts.GetActiveByPlan(ctx, planID)
			if getErr == nil && existing != nil {
				return RebalanceDraftDetail{}, newErr("active_draft_exists", "an active rebalance draft already exists",
					map[string]any{
						"draft_id": existing.ID, "created_at": existing.CreatedAt,
					})
			}
		}
		return RebalanceDraftDetail{}, wrapRepo("create rebalance draft tx", err)
	}
	return s.Get(ctx, planID, draft.ID)
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func (s *RebalanceDraftService) PatchLines(ctx context.Context, planID, draftID string,
	req PatchRebalanceDraftLinesRequest,
) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, wrapRepo("get draft for patch", err)
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return RebalanceDraftDetail{}, newErr("validation_failed", "draft is not editable", nil)
	}
	if len(req.Lines) == 0 {
		return RebalanceDraftDetail{}, newErr("validation_failed", "lines required", nil)
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		for _, item := range req.Lines {
			if err := s.patchDraftLineItem(ctx, tx, draftID, item, req.Stage); err != nil {
				return err
			}
		}
		return s.drafts.TouchDraftTx(ctx, tx, draftID)
	})
	if err != nil {
		appErr := &AppError{}
		if errors.As(err, &appErr) {
			return RebalanceDraftDetail{}, appErr
		}
		return RebalanceDraftDetail{}, wrapRepo("patch draft lines tx", err)
	}
	return s.Get(ctx, planID, draftID)
}

func (s *RebalanceDraftService) Undo(ctx context.Context, planID, draftID string) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, wrapRepo("get draft for undo", err)
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return RebalanceDraftDetail{}, newErr("validation_failed", "draft is not editable", nil)
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.undoDraftStageTx(ctx, tx, draftID); err != nil {
			return err
		}
		return s.drafts.TouchDraftTx(ctx, tx, draftID)
	})
	if err != nil {
		appErr := &AppError{}
		if errors.As(err, &appErr) {
			return RebalanceDraftDetail{}, appErr
		}
		return RebalanceDraftDetail{}, wrapRepo("undo draft tx", err)
	}
	return s.Get(ctx, planID, draftID)
}

func (s *RebalanceDraftService) Commit(ctx context.Context, planID, draftID string,
	req CommitRebalanceDraftRequest,
) (RebalanceDraftDetail, error) {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return RebalanceDraftDetail{}, newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return RebalanceDraftDetail{}, wrapRepo("get draft for commit", err)
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return RebalanceDraftDetail{}, newErr("validation_failed", "draft is not editable", nil)
	}
	state, err := s.loadCommitDraftState(ctx, planID, draftID, req, draft)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}
	if math.Abs(float64(state.net)) > amountToleranceMinor && !req.ConfirmImbalanced && !req.AcceptScaleShrink {
		return RebalanceDraftDetail{}, newErr("fund_pool_imbalanced", "rebalance fund pool is not balanced", map[string]any{
			"net_minor": state.net,
		})
	}
	holdingsReq := buildCommitHoldingsRequest(req, state.existing, state.plannedByHolding)

	prep, err := s.holdingsSvc.prepareHoldingsUpdate(ctx, planID, holdingsReq)
	if err != nil {
		return RebalanceDraftDetail{}, err
	}

	err = commitDraftHoldingsTx(ctx, s, planID, draftID, req, prep)
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return RebalanceDraftDetail{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return RebalanceDraftDetail{}, wrapRepo("commit draft tx", err)
	}

	if req.RecordSnapshot {
		s.recordCommitSnapshot(ctx, planID, draftID, state.plan, req, state.existing, state.plannedByHolding)
	}

	return s.Get(ctx, planID, draftID)
}

func (s *RebalanceDraftService) Cancel(ctx context.Context, planID, draftID string) error {
	draft, err := s.drafts.GetByID(ctx, planID, draftID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceDraftNotFound) {
			return newErr("rebalance_draft_not_found", "rebalance draft not found", nil)
		}
		return wrapRepo("get draft for cancel", err)
	}
	if draft.Status != RebalanceDraftStatusDraft {
		return newErr("validation_failed", "draft is not cancellable", nil)
	}
	return wrapRepo("cancel rebalance draft", s.drafts.SetStatusTx(ctx, nil, draftID, RebalanceDraftStatusCancelled, nil))
}

func (s *RebalanceDraftService) buildDetail(ctx context.Context, draft repository.RebalanceDraft) (RebalanceDraftDetail,
	error,
) {
	lines, err := s.drafts.ListLines(ctx, draft.ID)
	if err != nil {
		return RebalanceDraftDetail{}, wrapRepo("list draft lines", err)
	}
	events, err := s.drafts.ListEvents(ctx, draft.ID)
	if err != nil {
		return RebalanceDraftDetail{}, wrapRepo("list draft events", err)
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
	var maxGap float64
	for _, line := range lines {
		if !line.Enabled {
			continue
		}
		if w := math.Abs(line.StructuralGapWeight); w > maxGap {
			maxGap = w
		}
	}
	return maxGap
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
