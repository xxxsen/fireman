package api

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	"github.com/fireman/fireman/internal/testutil"
)

// addCashHoldingForSweep adds an enabled system-cash holding so a Complete
// with leftover cash pool can sweep it instead of failing.
func addCashHoldingForSweep(t *testing.T, db *sql.DB, planID string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plan_holdings (
			id, plan_id, asset_key, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?,?,?,1,'cash','domestic',1.0,0,?,9999,?,?)`,
		"hold_sys_cash_"+planID, planID, repository.SystemCashAssetKey,
		repository.SystemCashSnapshotID, now, now); err != nil {
		t.Fatal(err)
	}
}

// replayExecutionEvents derives the expected final amount per asset_key from
// the execution event stream (the source of truth): baseline per line, plus
// executed sells/buys, plus the leftover cash-pool sweep into the cash
// holding.
func replayExecutionEvents(
	t *testing.T,
	lines []repository.RebalanceExecutionLine,
	events []repository.RebalanceExecutionEvent,
	holdingsAtCreate map[string]int64,
) map[string]int64 {
	t.Helper()
	final := make(map[string]int64, len(holdingsAtCreate))
	for key, amount := range holdingsAtCreate {
		final[key] = amount
	}
	for _, line := range lines {
		final[line.AssetKey] = line.BaselineCurrentMinor
	}
	pool := int64(0)
	for _, ev := range events {
		switch ev.EventType {
		case service.ExecutionEventSellToCash:
			final[ev.AssetKey] -= ev.AmountMinor
			pool += ev.AmountMinor
		case service.ExecutionEventBuyFromCash:
			final[ev.AssetKey] += ev.AmountMinor
			pool -= ev.AmountMinor
		}
	}
	if pool > 0 {
		final[repository.SystemCashAssetKey] += pool
	}
	return final
}

// A Sell racing a Complete must never lose the trade: either the sell commits
// before Complete and is included in the final holdings, or it is rejected
// because the execution is already completed. The final holdings must always
// equal the event-stream replay.
func TestRebalanceExecutionSellCompleteConcurrency(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	addCashHoldingForSweep(t, db, planID)
	svcs := buildServices(db)
	execSvc := svcs.RebalanceExecutions
	ctx := context.Background()

	holdingsBefore, err := repository.NewHoldingsRepo(db).ListByPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	baseline := make(map[string]int64, len(holdingsBefore))
	for _, h := range holdingsBefore {
		baseline[h.AssetKey] = h.CurrentAmountMinor
	}

	detail, err := execSvc.Create(ctx, planID, service.CreateRebalanceExecutionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	executionID := detail.Execution.ID
	var sellLine repository.RebalanceExecutionLine
	for _, line := range detail.Lines {
		if line.ActionDirection == "decrease" && line.RemainingDeltaMinor < 0 {
			sellLine = line
			break
		}
	}
	if sellLine.ID == "" {
		t.Fatal("need a decrease line")
	}
	// First sell half so the execution is in progress with a non-zero pool.
	firstSell := (-sellLine.RemainingDeltaMinor) / 2
	if firstSell <= 0 {
		t.Fatal("decrease line too small")
	}
	if _, err := execSvc.Sell(ctx, planID, executionID, service.ExecutionTradeRequest{
		LineID: sellLine.ID, AmountMinor: firstSell,
	}); err != nil {
		t.Fatal(err)
	}
	secondSell := (-sellLine.RemainingDeltaMinor) - firstSell
	if secondSell <= 0 {
		t.Fatal("no remaining amount for the racing sell")
	}

	// Race: Sell vs Complete, each retried once on failure.
	var wg sync.WaitGroup
	var sellErr, completeErr error
	wg.Go(func() {
		for attempt := 0; attempt < 2; attempt++ {
			_, sellErr = execSvc.Sell(ctx, planID, executionID, service.ExecutionTradeRequest{
				LineID: sellLine.ID, AmountMinor: secondSell,
			})
			if sellErr == nil {
				return
			}
		}
	})
	wg.Go(func() {
		for attempt := 0; attempt < 2; attempt++ {
			_, completeErr = execSvc.Complete(ctx, planID, executionID,
				service.CompleteRebalanceExecutionRequest{ConfigVersion: 1})
			if completeErr == nil {
				return
			}
		}
	})
	wg.Wait()

	if completeErr != nil {
		t.Fatalf("complete must eventually succeed: %v", completeErr)
	}
	// The racing sell either succeeded (before complete) or was rejected
	// because the execution was no longer editable — never silently dropped.
	if sellErr != nil && !strings.Contains(sellErr.Error(), "not editable") {
		t.Fatalf("unexpected sell failure: %v", sellErr)
	}

	final, err := execSvc.Get(ctx, planID, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Execution.Status != service.RebalanceExecutionStatusCompleted {
		t.Fatalf("status=%s want completed", final.Execution.Status)
	}

	want := replayExecutionEvents(t, final.Lines, final.Events, baseline)
	holdingsAfter, err := repository.NewHoldingsRepo(db).ListByPlan(ctx, planID)
	if err != nil {
		t.Fatal(err)
	}
	var totalBefore, totalAfter int64
	for _, amount := range baseline {
		totalBefore += amount
	}
	for _, h := range holdingsAfter {
		totalAfter += h.CurrentAmountMinor
		if got, wantAmount := h.CurrentAmountMinor, want[h.AssetKey]; got != wantAmount {
			t.Fatalf("holding %s = %d, event replay expects %d (lost trade?)",
				h.AssetKey, got, wantAmount)
		}
	}
	if totalAfter != totalBefore {
		t.Fatalf("total holdings changed: before=%d after=%d", totalBefore, totalAfter)
	}
}

// Two concurrent Completes: exactly one succeeds, the other fails the
// in-transaction editability re-check with validation_failed.
func TestRebalanceExecutionDoubleCompleteConcurrency(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	addCashHoldingForSweep(t, db, planID)
	svcs := buildServices(db)
	execSvc := svcs.RebalanceExecutions
	ctx := context.Background()

	detail, err := execSvc.Create(ctx, planID, service.CreateRebalanceExecutionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	executionID := detail.Execution.ID
	var sellLine repository.RebalanceExecutionLine
	for _, line := range detail.Lines {
		if line.ActionDirection == "decrease" && line.RemainingDeltaMinor < 0 {
			sellLine = line
			break
		}
	}
	if sellLine.ID == "" {
		t.Fatal("need a decrease line")
	}
	if _, err := execSvc.Sell(ctx, planID, executionID, service.ExecutionTradeRequest{
		LineID: sellLine.ID, AmountMinor: -sellLine.RemainingDeltaMinor,
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			_, errs[i] = execSvc.Complete(ctx, planID, executionID,
				service.CompleteRebalanceExecutionRequest{ConfigVersion: 1})
		})
	}
	wg.Wait()

	successCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
			continue
		}
		var appErr *service.AppError
		if !errors.As(err, &appErr) || appErr.Code != "validation_failed" ||
			!strings.Contains(appErr.Message, "not editable") {
			t.Fatalf("loser must fail with validation_failed (execution is not editable), got %v", err)
		}
	}
	if successCount != 1 {
		t.Fatalf("exactly one complete must succeed, got %d", successCount)
	}
}
