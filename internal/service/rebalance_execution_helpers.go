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

//nolint:dupl // mirrors draft create validation with execution-specific active check
func validateRebalanceExecutionCreate(
	ctx context.Context,
	s *RebalanceExecutionService,
	planID string,
	req CreateRebalanceExecutionRequest,
) (*repository.RebalanceExecution, domain.RebalanceResult, error) {
	active, err := s.executions.GetActiveByPlan(ctx, planID)
	if err != nil && !errors.Is(err, repository.ErrNoActiveRebalanceExecution) {
		return nil, domain.RebalanceResult{}, wrapRepo("get active execution for create", err)
	}
	if active != nil && !req.ForceNew {
		return nil, domain.RebalanceResult{}, newErr(
			"active_execution_exists", "an active rebalance execution already exists",
			map[string]any{
				"execution_id": active.ID, "created_at": active.CreatedAt,
			},
		)
	}

	result, err := loadStructuralRebalanceForCreate(ctx, s.sql, s.rebalance, planID)
	if err != nil {
		return nil, domain.RebalanceResult{}, err
	}
	return active, result, nil
}

func buildRebalanceExecutionRecords(
	planID string,
	plan repository.Plan,
	result domain.RebalanceResult,
	filterInstrumentIDs []string,
) (repository.RebalanceExecution, []repository.RebalanceExecutionLine) {
	filter := map[string]bool{}
	for _, id := range filterInstrumentIDs {
		filter[id] = true
	}
	useFilter := len(filter) > 0

	snapshot, _ := json.Marshal(map[string]any{
		"holdings_total_minor": result.Summary.HoldingsTotalMinor,
		"actionable_count":     result.Summary.StructuralActionableCount,
	})

	now := time.Now().UnixMilli()
	execution := repository.RebalanceExecution{
		ID: "rbx_" + uuid.New().String(), PlanID: planID,
		Status:    RebalanceExecutionStatusDraft,
		CreatedAt: now, UpdatedAt: now,
		BaselineHoldingsTotalMinor: result.Summary.HoldingsTotalMinor,
		BaselineConfigVersion:      plan.ConfigVersion,
		BaselineSnapshotJSON:       string(snapshot),
		CashPoolMinor:              0,
	}

	lines := make([]repository.RebalanceExecutionLine, 0)
	sortOrder := 0
	for _, line := range result.Lines {
		if !line.Enabled {
			continue
		}
		if line.Action == domain.RebalanceActionHold {
			continue
		}
		if useFilter && !filter[line.InstrumentID] {
			continue
		}
		targetDelta := line.StructuralGapAmountMinor
		lines = append(lines, repository.RebalanceExecutionLine{
			ID: "rbxl_" + uuid.New().String(), ExecutionID: execution.ID,
			HoldingID: line.HoldingID, InstrumentID: line.InstrumentID,
			BaselineCurrentMinor: line.CurrentAmountMinor,
			TargetDeltaMinor:     targetDelta,
			ExecutedDeltaMinor:   0,
			RemainingDeltaMinor:  targetDelta,
			ActionDirection:      line.Action,
			ExecutionStatus:      ExecutionLineStatusNotStarted,
			SortOrder:            sortOrder,
		})
		sortOrder += 10
	}
	return execution, lines
}

func persistExecutionCreate(
	ctx context.Context,
	s *RebalanceExecutionService,
	active *repository.RebalanceExecution,
	req CreateRebalanceExecutionRequest,
	execution repository.RebalanceExecution,
	lines []repository.RebalanceExecutionLine,
) error {
	if len(lines) == 0 {
		return newErr("validation_failed", "no execution lines selected", nil)
	}
	return wrapRepo("create rebalance execution tx", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if active != nil && req.ForceNew {
			if err := s.executions.SetStatusTx(ctx, tx, active.ID, RebalanceExecutionStatusCanceled, nil, nil); err != nil {
				return wrapRepo("cancel active execution", err)
			}
		}
		return s.executions.CreateTx(ctx, tx, execution, lines)
	}))
}
