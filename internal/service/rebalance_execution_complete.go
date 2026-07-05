package service

import (
	"context"
	"database/sql"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

func buildExecutionFinalAmounts(
	lines []repository.RebalanceExecutionLine,
	existing []repository.PlanHolding,
	cashPool int64,
) (map[string]int64, int64, error) {
	finalByHolding := map[string]int64{}
	for _, line := range lines {
		amount := line.BaselineCurrentMinor + line.ExecutedDeltaMinor
		if amount < 0 {
			return nil, cashPool, newErr("validation_failed", "computed holding amount is negative", map[string]any{
				"holding_id": line.HoldingID,
			})
		}
		finalByHolding[line.HoldingID] = amount
	}
	if cashPool <= amountToleranceMinor {
		return finalByHolding, cashPool, nil
	}
	cashHolding := findCashSweepHolding(existing)
	if cashHolding == nil {
		return nil, cashPool, newErr(
			"validation_failed", "cash pool balance remains but no cash holding to sweep",
			map[string]any{"cash_pool_minor": cashPool},
		)
	}
	base := cashHolding.CurrentAmountMinor
	if planned, ok := finalByHolding[cashHolding.ID]; ok {
		base = planned
	}
	finalByHolding[cashHolding.ID] = base + cashPool
	return finalByHolding, 0, nil
}

func buildExecutionCompleteHoldingsRequest(
	req CompleteRebalanceExecutionRequest,
	existing []repository.PlanHolding,
	finalByHolding map[string]int64,
) HoldingsUpdateRequest {
	holdingsReq := HoldingsUpdateRequest{
		ConfigVersion: req.ConfigVersion,
		Holdings:      make([]HoldingWriteItem, 0, len(existing)),
	}
	for _, h := range existing {
		amount := h.CurrentAmountMinor
		if planned, ok := finalByHolding[h.ID]; ok {
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

// completeExecutionTx performs the whole complete flow inside one
// transaction: re-check execution editability and plan version, read lines
// and holdings via Tx variants, derive final amounts, then apply the
// holdings update and status flip. Any interleaved trade or a second
// Complete fails the in-transaction checks instead of committing on stale
// reads.
func (s *RebalanceExecutionService) completeExecutionTx(
	ctx context.Context,
	planID, executionID string,
	req CompleteRebalanceExecutionRequest,
) error {
	return wrapRepo("complete execution tx", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		execution, err := s.loadEditableExecutionTx(ctx, tx, planID, executionID)
		if err != nil {
			return err
		}
		plan, err := s.plans.GetByIDTx(ctx, tx, planID)
		if err != nil {
			return wrapRepo("get plan for complete", err)
		}
		if req.ConfigVersion != plan.ConfigVersion {
			return newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		if req.ConfigVersion != execution.BaselineConfigVersion {
			return newErr("plan_version_conflict",
				"plan configuration changed since execution creation; abandon and recreate", nil)
		}

		lines, err := s.executions.ListLinesTx(ctx, tx, executionID)
		if err != nil {
			return wrapRepo("list execution lines for complete", err)
		}
		existing, err := s.holdings.ListByPlanTx(ctx, tx, planID)
		if err != nil {
			return wrapRepo("list holdings for complete", err)
		}

		finalByHolding, cashPool, err := buildExecutionFinalAmounts(lines, existing, execution.CashPoolMinor)
		if err != nil {
			return err
		}
		holdingsReq := buildExecutionCompleteHoldingsRequest(req, existing, finalByHolding)
		prep, err := s.holdingsSvc.prepareHoldingsUpdateTx(ctx, tx, planID, holdingsReq)
		if err != nil {
			return err
		}

		if err := s.holdingsSvc.applyHoldingsUpdateTx(ctx, tx, planID, req.ConfigVersion, prep); err != nil {
			return wrapRepo("apply holdings update for execution complete", err)
		}
		now := time.Now().UnixMilli()
		if err := s.executions.SetStatusTx(
			ctx, tx, executionID, RebalanceExecutionStatusCompleted, nil, &now,
		); err != nil {
			return wrapRepo("set execution completed", err)
		}
		if cashPool != execution.CashPoolMinor {
			if err := s.executions.UpdateCashPoolTx(ctx, tx, executionID, cashPool); err != nil {
				return wrapRepo("update execution cash pool on complete", err)
			}
		}
		return s.insertExecutionEventTx(ctx, tx, execution, ExecutionEventComplete, "", 0, cashPool, map[string]any{
			"completed_at": now,
		})
	}))
}

func (s *RebalanceExecutionService) applyExecutionTradeInTx(
	ctx context.Context,
	tx *sql.Tx,
	executionID string,
	execution repository.RebalanceExecution,
	line repository.RebalanceExecutionLine,
	req ExecutionTradeRequest,
	eventType string,
	newCashPool int64,
) error {
	if err := s.executions.UpdateLineTx(ctx, tx, line); err != nil {
		return wrapRepo("update execution line", err)
	}
	if err := s.executions.UpdateCashPoolTx(ctx, tx, executionID, newCashPool); err != nil {
		return wrapRepo("update execution cash pool", err)
	}
	if execution.Status == RebalanceExecutionStatusDraft {
		now := time.Now().UnixMilli()
		if err := s.executions.SetStatusTx(
			ctx, tx, executionID, RebalanceExecutionStatusInProgress, &now, nil,
		); err != nil {
			return wrapRepo("set execution in progress", err)
		}
	}
	payload := map[string]any{
		"line_id": req.LineID,
		"note":    req.Note,
		"summary": formatExecutionTradeSummary(line, req.AmountMinor, eventType == ExecutionEventSellToCash),
	}
	execution.CashPoolMinor = newCashPool
	return s.insertExecutionEventTx(
		ctx, tx, execution, eventType, line.AssetKey, req.AmountMinor, newCashPool, payload,
	)
}
