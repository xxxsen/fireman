package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

const (
	RebalanceExecutionStatusDraft      = "draft"
	RebalanceExecutionStatusInProgress = "in_progress"
	RebalanceExecutionStatusCompleted  = "completed"
	RebalanceExecutionStatusCanceled   = "canceled"

	ExecutionLineStatusNotStarted = "not_started"
	ExecutionLineStatusPartial    = "partial"
	ExecutionLineStatusDone       = "done"
	ExecutionLineStatusSkipped    = "skipped"

	ExecutionEventSellToCash   = "sell_to_cash"
	ExecutionEventBuyFromCash  = "buy_from_cash"
	ExecutionEventAdjustTarget = "adjust_target"
	ExecutionEventNote         = "note"
	ExecutionEventComplete     = "complete"
)

// RebalanceExecutionService manages multi-day rebalance executions.
type RebalanceExecutionService struct {
	sql         *sql.DB
	plans       *repository.PlanRepo
	executions  *repository.RebalanceExecutionRepo
	holdings    *repository.HoldingsRepo
	holdingsSvc *HoldingsService
	rebalance   *RebalanceService
}

func NewRebalanceExecutionService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	executions *repository.RebalanceExecutionRepo,
	holdings *repository.HoldingsRepo,
	holdingsSvc *HoldingsService,
	rebalance *RebalanceService,
) *RebalanceExecutionService {
	return &RebalanceExecutionService{
		sql: sqlDB, plans: plans, executions: executions,
		holdings: holdings, holdingsSvc: holdingsSvc, rebalance: rebalance,
	}
}

type RebalanceExecutionDetail struct {
	Execution repository.RebalanceExecution        `json:"execution"`
	Lines     []repository.RebalanceExecutionLine  `json:"lines"`
	Events    []repository.RebalanceExecutionEvent `json:"events"`
	Stats     ExecutionStats                       `json:"stats"`
}

type ExecutionStats struct {
	LineCount        int   `json:"line_count"`
	DoneLineCount    int   `json:"done_line_count"`
	SkippedLineCount int   `json:"skipped_line_count"`
	SoldTotalMinor   int64 `json:"sold_total_minor"`
	BoughtTotalMinor int64 `json:"bought_total_minor"`
}

type CreateRebalanceExecutionRequest struct {
	InstrumentIDs []string `json:"instrument_ids"`
	ForceNew      bool     `json:"force_new"`
}

type ExecutionTradeRequest struct {
	LineID      string `json:"line_id"`
	AmountMinor int64  `json:"amount_minor"`
	Note        string `json:"note"`
}

type ExecutionNoteRequest struct {
	Note string `json:"note"`
}

type ExecutionSkipRequest struct {
	LineID string `json:"line_id"`
}

type CompleteRebalanceExecutionRequest struct {
	ConfigVersion int `json:"config_version"`
}

func (s *RebalanceExecutionService) GetActive(ctx context.Context, planID string) (*RebalanceExecutionDetail, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan", err)
	}
	execution, err := s.executions.GetActiveByPlan(ctx, planID)
	if errors.Is(err, repository.ErrNoActiveRebalanceExecution) {
		return nil, repository.ErrNoActiveRebalanceExecution
	}
	if err != nil {
		return nil, wrapRepo("get active execution", err)
	}
	detail, err := s.buildDetail(ctx, *execution)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func (s *RebalanceExecutionService) List(
	ctx context.Context, planID string,
) ([]repository.RebalanceExecutionSummary, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, newErr("plan_not_found", "plan not found", nil)
		}
		return nil, wrapRepo("get plan", err)
	}
	out, err := s.executions.ListByPlan(ctx, planID)
	return out, wrapRepo("list rebalance executions", err)
}

func (s *RebalanceExecutionService) Get(
	ctx context.Context, planID, executionID string,
) (RebalanceExecutionDetail, error) {
	execution, err := s.executions.GetByID(ctx, planID, executionID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceExecutionNotFound) {
			return RebalanceExecutionDetail{}, newErr("rebalance_execution_not_found", "rebalance execution not found", nil)
		}
		return RebalanceExecutionDetail{}, wrapRepo("get rebalance execution", err)
	}
	return s.buildDetail(ctx, execution)
}

func (s *RebalanceExecutionService) Create(
	ctx context.Context, planID string, req CreateRebalanceExecutionRequest,
) (RebalanceExecutionDetail, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return RebalanceExecutionDetail{}, newErr("plan_not_found", "plan not found", nil)
		}
		return RebalanceExecutionDetail{}, wrapRepo("get plan for execution create", err)
	}

	active, result, err := validateRebalanceExecutionCreate(ctx, s, planID, req)
	if err != nil {
		return RebalanceExecutionDetail{}, err
	}

	execution, lines := buildRebalanceExecutionRecords(planID, plan, result, req.InstrumentIDs)
	err = persistExecutionCreate(ctx, s, active, req, execution, lines)
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, getErr := s.executions.GetActiveByPlan(ctx, planID)
			if getErr == nil && existing != nil {
				return RebalanceExecutionDetail{}, newErr(
					"active_execution_exists", "an active rebalance execution already exists",
					map[string]any{
						"execution_id": existing.ID, "created_at": existing.CreatedAt,
					},
				)
			}
		}
		return RebalanceExecutionDetail{}, err
	}
	return s.Get(ctx, planID, execution.ID)
}

func (s *RebalanceExecutionService) Sell(
	ctx context.Context, planID, executionID string, req ExecutionTradeRequest,
) (RebalanceExecutionDetail, error) {
	return s.applyTrade(ctx, planID, executionID, req, true)
}

func (s *RebalanceExecutionService) Buy(
	ctx context.Context, planID, executionID string, req ExecutionTradeRequest,
) (RebalanceExecutionDetail, error) {
	return s.applyTrade(ctx, planID, executionID, req, false)
}

func (s *RebalanceExecutionService) Skip(
	ctx context.Context, planID, executionID string, req ExecutionSkipRequest,
) (RebalanceExecutionDetail, error) {
	if req.LineID == "" {
		return RebalanceExecutionDetail{}, newErr("validation_failed", "line_id required", nil)
	}
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		execution, err := s.loadEditableExecutionTx(ctx, tx, planID, executionID)
		if err != nil {
			return err
		}
		line, err := s.executions.GetLineByIDTx(ctx, tx, executionID, req.LineID)
		if err != nil {
			if errors.Is(err, repository.ErrRebalanceExecutionNotFound) {
				return newErr("validation_failed", "execution line not found", nil)
			}
			return wrapRepo("get execution line for skip", err)
		}
		if line.ExecutionStatus == ExecutionLineStatusDone || line.ExecutionStatus == ExecutionLineStatusSkipped {
			return newErr("validation_failed", "line is already finished", nil)
		}
		line.ExecutionStatus = ExecutionLineStatusSkipped
		if err := s.executions.UpdateLineTx(ctx, tx, line); err != nil {
			return wrapRepo("update execution line for skip", err)
		}
		if execution.Status == RebalanceExecutionStatusDraft {
			now := time.Now().UnixMilli()
			if err := s.executions.SetStatusTx(
				ctx, tx, executionID, RebalanceExecutionStatusInProgress, &now, nil,
			); err != nil {
				return wrapRepo("set execution in progress on skip", err)
			}
		} else if err := s.executions.TouchExecutionTx(ctx, tx, executionID); err != nil {
			return wrapRepo("touch execution on skip", err)
		}
		label := line.InstrumentName
		if label == "" {
			label = line.InstrumentCode
		}
		return s.insertExecutionEventTx(ctx, tx, execution, ExecutionEventAdjustTarget, line.InstrumentID, 0,
			execution.CashPoolMinor, map[string]any{
				"line_id": req.LineID,
				"summary": fmt.Sprintf("跳过 %s", label),
				"skipped": true,
			})
	})
	if err != nil {
		appErr := &AppError{}
		if errors.As(err, &appErr) {
			return RebalanceExecutionDetail{}, appErr
		}
		return RebalanceExecutionDetail{}, wrapRepo("skip execution line tx", err)
	}
	return s.Get(ctx, planID, executionID)
}

func (s *RebalanceExecutionService) AddNote(
	ctx context.Context, planID, executionID string, req ExecutionNoteRequest,
) (RebalanceExecutionDetail, error) {
	execution, err := s.loadEditableExecution(ctx, planID, executionID)
	if err != nil {
		return RebalanceExecutionDetail{}, err
	}
	if req.Note == "" {
		return RebalanceExecutionDetail{}, newErr("validation_failed", "note required", nil)
	}
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		return s.insertExecutionEventTx(
			ctx, tx, execution, ExecutionEventNote, "", 0, execution.CashPoolMinor,
			map[string]any{"note": req.Note},
		)
	})
	if err != nil {
		return RebalanceExecutionDetail{}, wrapRepo("add execution note tx", err)
	}
	return s.Get(ctx, planID, executionID)
}

func (s *RebalanceExecutionService) Complete(
	ctx context.Context, planID, executionID string, req CompleteRebalanceExecutionRequest,
) (RebalanceExecutionDetail, error) {
	execution, err := s.loadEditableExecution(ctx, planID, executionID)
	if err != nil {
		return RebalanceExecutionDetail{}, err
	}
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return RebalanceExecutionDetail{}, wrapRepo("get plan for complete", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return RebalanceExecutionDetail{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}
	if req.ConfigVersion != execution.BaselineConfigVersion {
		return RebalanceExecutionDetail{}, newErr("plan_version_conflict",
			"plan configuration changed since execution creation; abandon and recreate", nil)
	}

	lines, err := s.executions.ListLines(ctx, executionID)
	if err != nil {
		return RebalanceExecutionDetail{}, wrapRepo("list execution lines for complete", err)
	}
	existing, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return RebalanceExecutionDetail{}, wrapRepo("list holdings for complete", err)
	}

	finalByHolding, cashPool, err := buildExecutionFinalAmounts(lines, existing, execution.CashPoolMinor)
	if err != nil {
		return RebalanceExecutionDetail{}, err
	}
	holdingsReq := buildExecutionCompleteHoldingsRequest(req, existing, finalByHolding)

	prep, err := s.holdingsSvc.prepareHoldingsUpdate(ctx, planID, holdingsReq)
	if err != nil {
		return RebalanceExecutionDetail{}, err
	}

	err = s.completeExecutionTx(ctx, planID, executionID, execution, req, prep, cashPool)
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return RebalanceExecutionDetail{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		appErr := &AppError{}
		if errors.As(err, &appErr) {
			return RebalanceExecutionDetail{}, appErr
		}
		return RebalanceExecutionDetail{}, wrapRepo("complete execution tx", err)
	}
	return s.Get(ctx, planID, executionID)
}

func (s *RebalanceExecutionService) Cancel(ctx context.Context, planID, executionID string) error {
	execution, err := s.executions.GetByID(ctx, planID, executionID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceExecutionNotFound) {
			return newErr("rebalance_execution_not_found", "rebalance execution not found", nil)
		}
		return wrapRepo("get execution for cancel", err)
	}
	if execution.Status != RebalanceExecutionStatusDraft && execution.Status != RebalanceExecutionStatusInProgress {
		return newErr("validation_failed", "execution is not cancellable", nil)
	}
	return wrapRepo("cancel rebalance execution", s.executions.SetStatusTx(ctx, nil, executionID,
		RebalanceExecutionStatusCanceled, nil, nil))
}

func (s *RebalanceExecutionService) HasActive(ctx context.Context, planID string) (bool, error) {
	active, err := s.executions.HasActiveByPlan(ctx, planID)
	return active, wrapRepo("check active rebalance execution", err)
}

func (s *RebalanceExecutionService) loadEditableExecutionTx(
	ctx context.Context, tx *sql.Tx, planID, executionID string,
) (repository.RebalanceExecution, error) {
	execution, err := s.executions.GetByIDTx(ctx, tx, planID, executionID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceExecutionNotFound) {
			return repository.RebalanceExecution{}, newErr(
				"rebalance_execution_not_found", "rebalance execution not found", nil,
			)
		}
		return repository.RebalanceExecution{}, wrapRepo("get execution", err)
	}
	if execution.Status != RebalanceExecutionStatusDraft && execution.Status != RebalanceExecutionStatusInProgress {
		return repository.RebalanceExecution{}, newErr("validation_failed", "execution is not editable", nil)
	}
	return execution, nil
}

func (s *RebalanceExecutionService) loadEditableExecution(
	ctx context.Context, planID, executionID string,
) (repository.RebalanceExecution, error) {
	execution, err := s.executions.GetByID(ctx, planID, executionID)
	if err != nil {
		if errors.Is(err, repository.ErrRebalanceExecutionNotFound) {
			return repository.RebalanceExecution{}, newErr(
				"rebalance_execution_not_found", "rebalance execution not found", nil,
			)
		}
		return repository.RebalanceExecution{}, wrapRepo("get execution", err)
	}
	if execution.Status != RebalanceExecutionStatusDraft && execution.Status != RebalanceExecutionStatusInProgress {
		return repository.RebalanceExecution{}, newErr("validation_failed", "execution is not editable", nil)
	}
	return execution, nil
}

func validateExecutionTrade(
	execution repository.RebalanceExecution,
	line *repository.RebalanceExecutionLine,
	req ExecutionTradeRequest,
	isSell bool,
) (string, int64, error) {
	if req.LineID == "" {
		return "", 0, newErr("validation_failed", "line_id required", nil)
	}
	if req.AmountMinor <= 0 {
		return "", 0, newErr("validation_failed", "amount must be positive", nil)
	}
	expectedDir := domain.RebalanceActionDecrease
	eventType := ExecutionEventSellToCash
	deltaSign := int64(-1)
	if !isSell {
		expectedDir = domain.RebalanceActionIncrease
		eventType = ExecutionEventBuyFromCash
		deltaSign = 1
		if req.AmountMinor > execution.CashPoolMinor {
			return "", 0, newErr("validation_failed", "buy amount exceeds cash pool balance", map[string]any{
				"cash_pool_minor": execution.CashPoolMinor,
			})
		}
	}
	if line.ActionDirection != expectedDir {
		return "", 0, newErr("validation_failed", "line action direction mismatch", nil)
	}
	if line.ExecutionStatus == ExecutionLineStatusDone || line.ExecutionStatus == ExecutionLineStatusSkipped {
		return "", 0, newErr("validation_failed", "line is already finished", nil)
	}
	maxTrade := absInt64(line.RemainingDeltaMinor)
	if req.AmountMinor > maxTrade+amountToleranceMinor {
		return "", 0, newErr("validation_failed", "amount exceeds remaining delta", map[string]any{
			"remaining_delta_minor": line.RemainingDeltaMinor,
		})
	}
	newExecuted := line.ExecutedDeltaMinor + deltaSign*req.AmountMinor
	line.ExecutedDeltaMinor = newExecuted
	line.RemainingDeltaMinor = line.TargetDeltaMinor - newExecuted
	line.ExecutionStatus = computeExecutionLineStatus(line.TargetDeltaMinor, newExecuted)
	var newCashPool int64
	if isSell {
		newCashPool = execution.CashPoolMinor + req.AmountMinor
	} else {
		newCashPool = execution.CashPoolMinor - req.AmountMinor
	}
	return eventType, newCashPool, nil
}

func (s *RebalanceExecutionService) applyTrade(
	ctx context.Context, planID, executionID string, req ExecutionTradeRequest, isSell bool,
) (RebalanceExecutionDetail, error) {
	if req.LineID == "" {
		return RebalanceExecutionDetail{}, newErr("validation_failed", "line_id required", nil)
	}
	if req.AmountMinor <= 0 {
		return RebalanceExecutionDetail{}, newErr("validation_failed", "amount must be positive", nil)
	}
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		execution, err := s.loadEditableExecutionTx(ctx, tx, planID, executionID)
		if err != nil {
			return err
		}
		line, err := s.executions.GetLineByIDTx(ctx, tx, executionID, req.LineID)
		if err != nil {
			if errors.Is(err, repository.ErrRebalanceExecutionNotFound) {
				return newErr("validation_failed", "execution line not found", nil)
			}
			return wrapRepo("get execution line for trade", err)
		}
		eventType, newCashPool, err := validateExecutionTrade(execution, &line, req, isSell)
		if err != nil {
			return err
		}
		return s.applyExecutionTradeInTx(ctx, tx, executionID, execution, line, req, eventType, newCashPool)
	})
	if err != nil {
		appErr := &AppError{}
		if errors.As(err, &appErr) {
			return RebalanceExecutionDetail{}, appErr
		}
		return RebalanceExecutionDetail{}, wrapRepo("apply execution trade tx", err)
	}
	return s.Get(ctx, planID, executionID)
}

func (s *RebalanceExecutionService) insertExecutionEventTx(
	ctx context.Context,
	tx *sql.Tx,
	execution repository.RebalanceExecution,
	eventType, instrumentID string,
	amountMinor, cashPoolAfter int64,
	payload map[string]any,
) error {
	seq, err := s.executions.NextEventSeq(ctx, tx, execution.ID)
	if err != nil {
		return wrapRepo("next execution event seq", err)
	}
	payloadJSON, _ := json.Marshal(payload)
	return wrapRepo("insert execution event", s.executions.InsertEventTx(ctx, tx, repository.RebalanceExecutionEvent{
		ID: "rbex_" + uuid.New().String(), ExecutionID: execution.ID,
		Seq: seq, EventType: eventType, InstrumentID: instrumentID,
		AmountMinor: amountMinor, CashPoolAfterMinor: cashPoolAfter,
		PayloadJSON: string(payloadJSON),
	}))
}

func (s *RebalanceExecutionService) buildDetail(
	ctx context.Context, execution repository.RebalanceExecution,
) (RebalanceExecutionDetail, error) {
	lines, err := s.executions.ListLines(ctx, execution.ID)
	if err != nil {
		return RebalanceExecutionDetail{}, wrapRepo("list execution lines", err)
	}
	events, err := s.executions.ListEvents(ctx, execution.ID)
	if err != nil {
		return RebalanceExecutionDetail{}, wrapRepo("list execution events", err)
	}
	stats := computeExecutionStats(lines, events)
	return RebalanceExecutionDetail{
		Execution: execution, Lines: lines, Events: events, Stats: stats,
	}, nil
}

func computeExecutionStats(
	lines []repository.RebalanceExecutionLine,
	events []repository.RebalanceExecutionEvent,
) ExecutionStats {
	stats := ExecutionStats{LineCount: len(lines)}
	for _, line := range lines {
		switch line.ExecutionStatus {
		case ExecutionLineStatusDone:
			stats.DoneLineCount++
		case ExecutionLineStatusSkipped:
			stats.SkippedLineCount++
		}
	}
	for _, event := range events {
		switch event.EventType {
		case ExecutionEventSellToCash:
			stats.SoldTotalMinor += event.AmountMinor
		case ExecutionEventBuyFromCash:
			stats.BoughtTotalMinor += event.AmountMinor
		}
	}
	return stats
}

func computeExecutionLineStatus(targetDelta, executedDelta int64) string {
	if executedDelta == 0 {
		return ExecutionLineStatusNotStarted
	}
	if absInt64(executedDelta)+amountToleranceMinor >= absInt64(targetDelta) {
		return ExecutionLineStatusDone
	}
	return ExecutionLineStatusPartial
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func formatExecutionTradeSummary(line repository.RebalanceExecutionLine, amount int64, isSell bool) string {
	label := line.InstrumentName
	if label == "" {
		label = line.InstrumentCode
	}
	action := "买入"
	if isSell {
		action = "卖出"
	}
	return fmt.Sprintf("%s %s %s", action, label, formatWan(amount))
}
