package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

func TestRebalanceExecutionFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	// Create execution
	createBody, _ := json.Marshal(map[string]any{})
	createResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions",
		"application/json", bytes.NewReader(createBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create execution status=%d", createResp.StatusCode)
	}
	var created struct {
		Data struct {
			Execution struct {
				ID string `json:"id"`
			} `json:"execution"`
			Lines []struct {
				ID                  string `json:"id"`
				ActionDirection     string `json:"action_direction"`
				RemainingDeltaMinor int64  `json:"remaining_delta_minor"`
			} `json:"lines"`
		} `json:"data"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = createResp.Body.Close()
	executionID := created.Data.Execution.ID
	if executionID == "" || len(created.Data.Lines) == 0 {
		t.Fatal("expected execution with lines")
	}

	var decreaseLineID, increaseLineID string
	var decreaseRemaining int64
	for _, line := range created.Data.Lines {
		if line.ActionDirection == "decrease" && decreaseLineID == "" {
			decreaseLineID = line.ID
			decreaseRemaining = line.RemainingDeltaMinor
		}
		if line.ActionDirection == "increase" && increaseLineID == "" {
			increaseLineID = line.ID
		}
	}
	if decreaseLineID == "" || increaseLineID == "" {
		t.Fatalf("need both decrease and increase lines, got %d lines", len(created.Data.Lines))
	}
	if decreaseRemaining >= 0 {
		t.Fatalf("expected negative remaining on decrease line, got %d", decreaseRemaining)
	}
	sellAmount := -decreaseRemaining
	if sellAmount <= 0 {
		t.Fatal("decrease remaining too small for test sell")
	}

	// Asset refresh blocked while execution active
	blockBody, _ := json.Marshal(map[string]any{
		"config_version": 1, "holdings": []any{}, "total_assets_minor": 300_000_00,
	})
	blockResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json", bytes.NewReader(blockBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if blockResp.StatusCode == http.StatusOK {
		t.Fatal("expected asset refresh to be blocked")
	}
	_ = blockResp.Body.Close()

	// Day 1: sell to cash pool
	sellBody, _ := json.Marshal(map[string]any{
		"line_id": decreaseLineID, "amount_minor": sellAmount, "note": "day1 sell",
	})
	sellResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/sell",
		"application/json", bytes.NewReader(sellBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if sellResp.StatusCode != http.StatusOK {
		t.Fatalf("sell status=%d", sellResp.StatusCode)
	}
	var afterSell struct {
		Data struct {
			Execution struct {
				Status        string `json:"status"`
				CashPoolMinor int64  `json:"cash_pool_minor"`
			} `json:"execution"`
		} `json:"data"`
	}
	if err := json.NewDecoder(sellResp.Body).Decode(&afterSell); err != nil {
		t.Fatal(err)
	}
	_ = sellResp.Body.Close()
	if afterSell.Data.Execution.Status != "in_progress" {
		t.Fatalf("expected in_progress, got %s", afterSell.Data.Execution.Status)
	}
	if afterSell.Data.Execution.CashPoolMinor != sellAmount {
		t.Fatalf("cash pool=%d want %d", afterSell.Data.Execution.CashPoolMinor, sellAmount)
	}

	// Day 2: buy half of sold amount from cash
	buyHalf := sellAmount / 2
	if buyHalf <= 0 {
		buyHalf = sellAmount
	}
	buyBody, _ := json.Marshal(map[string]any{
		"line_id": increaseLineID, "amount_minor": buyHalf,
	})
	buyResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/buy",
		"application/json", bytes.NewReader(buyBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if buyResp.StatusCode != http.StatusOK {
		t.Fatalf("buy status=%d", buyResp.StatusCode)
	}
	_ = buyResp.Body.Close()

	// Complete remaining buys and complete execution
	detailResp, err := client.Get(
		srv.URL + "/api/v1/plans/" + planID + "/rebalance-executions/" + executionID,
	)
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Data struct {
			Lines []struct {
				ID                  string `json:"id"`
				ActionDirection     string `json:"action_direction"`
				RemainingDeltaMinor int64  `json:"remaining_delta_minor"`
			} `json:"lines"`
			Execution struct {
				CashPoolMinor int64 `json:"cash_pool_minor"`
			} `json:"execution"`
		} `json:"data"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	_ = detailResp.Body.Close()

	for _, line := range detail.Data.Lines {
		if line.ActionDirection != "increase" || line.RemainingDeltaMinor <= 0 {
			continue
		}
		detailResp2, err := client.Get(
			srv.URL + "/api/v1/plans/" + planID + "/rebalance-executions/" + executionID,
		)
		if err != nil {
			t.Fatal(err)
		}
		var pool struct {
			Data struct {
				Execution struct {
					CashPoolMinor int64 `json:"cash_pool_minor"`
				} `json:"execution"`
			} `json:"data"`
		}
		if err := json.NewDecoder(detailResp2.Body).Decode(&pool); err != nil {
			t.Fatal(err)
		}
		_ = detailResp2.Body.Close()
		buyAmount := minInt64(line.RemainingDeltaMinor, pool.Data.Execution.CashPoolMinor)
		if buyAmount <= 0 {
			continue
		}
		body, _ := json.Marshal(map[string]any{
			"line_id": line.ID, "amount_minor": buyAmount,
		})
		resp, err := client.Post(
			srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/buy",
			"application/json", bytes.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("buy remaining status=%d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	completeBody, _ := json.Marshal(map[string]any{"config_version": 1})
	completeResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/complete",
		"application/json", bytes.NewReader(completeBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if completeResp.StatusCode != http.StatusOK {
		t.Fatalf("complete status=%d", completeResp.StatusCode)
	}
	_ = completeResp.Body.Close()

	// Asset refresh allowed after complete
	refreshHoldings := make([]map[string]any, 0, len(instIDs))
	for _, instID := range instIDs {
		refreshHoldings = append(refreshHoldings, map[string]any{
			"asset_key": instID, "current_amount_minor": 100_000_00,
		})
	}
	allowBody, _ := json.Marshal(map[string]any{
		"config_version": 2, "holdings": refreshHoldings, "total_assets_minor": 300_000_00,
	})
	allowResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json", bytes.NewReader(allowBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if allowResp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh after complete status=%d", allowResp.StatusCode)
	}
	_ = allowResp.Body.Close()
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func TestRebalanceExecutionCancelRestoresAssetRefresh(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	createResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions",
		"application/json", bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Data struct {
			Execution struct {
				ID string `json:"id"`
			} `json:"execution"`
		} `json:"data"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = createResp.Body.Close()
	executionID := created.Data.Execution.ID

	cancelResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/cancel",
		"application/json", bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if cancelResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status=%d", cancelResp.StatusCode)
	}
	_ = cancelResp.Body.Close()

	activeResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-executions/active")
	if err != nil {
		t.Fatal(err)
	}
	var active struct {
		Data any `json:"data"`
	}
	if err := json.NewDecoder(activeResp.Body).Decode(&active); err != nil {
		t.Fatal(err)
	}
	_ = activeResp.Body.Close()
	if active.Data != nil {
		t.Fatal("expected no active execution after cancel")
	}
}

func TestRebalanceExecutionSkipLine(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	executionID, lines := createRebalanceExecutionForTest(t, client, srv.URL, planID)
	var skipLineID string
	for _, line := range lines {
		if line.ExecutionStatus != "done" && line.ExecutionStatus != "skipped" {
			skipLineID = line.ID
			break
		}
	}
	if skipLineID == "" {
		t.Fatal("expected skippable line")
	}

	skipBody, _ := json.Marshal(map[string]any{"line_id": skipLineID})
	skipResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/skip",
		"application/json", bytes.NewReader(skipBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if skipResp.StatusCode != http.StatusOK {
		t.Fatalf("skip status=%d", skipResp.StatusCode)
	}
	var afterSkip struct {
		Data struct {
			Lines []struct {
				ID              string `json:"id"`
				ExecutionStatus string `json:"execution_status"`
			} `json:"lines"`
			Stats struct {
				DoneLineCount    int `json:"done_line_count"`
				SkippedLineCount int `json:"skipped_line_count"`
			} `json:"stats"`
		} `json:"data"`
	}
	if err := json.NewDecoder(skipResp.Body).Decode(&afterSkip); err != nil {
		t.Fatal(err)
	}
	_ = skipResp.Body.Close()

	skipped := false
	for _, line := range afterSkip.Data.Lines {
		if line.ID == skipLineID {
			if line.ExecutionStatus != "skipped" {
				t.Fatalf("expected skipped, got %s", line.ExecutionStatus)
			}
			skipped = true
		}
	}
	if !skipped {
		t.Fatal("skipped line not found in response")
	}
	if afterSkip.Data.Stats.DoneLineCount != 0 {
		t.Fatalf("expected skipped line excluded from done_line_count, got %d", afterSkip.Data.Stats.DoneLineCount)
	}
	if afterSkip.Data.Stats.SkippedLineCount != 1 {
		t.Fatalf("expected skipped_line_count=1, got %d", afterSkip.Data.Stats.SkippedLineCount)
	}

	sellBody, _ := json.Marshal(map[string]any{"line_id": skipLineID, "amount_minor": 100_00})
	sellResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/sell",
		"application/json", bytes.NewReader(sellBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if sellResp.StatusCode == http.StatusOK {
		t.Fatal("expected sell on skipped line to fail")
	}
	_ = sellResp.Body.Close()
}

func TestRebalanceExecutionBuyRejectsOverdrawnCashPool(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	executionID, lines := createRebalanceExecutionForTest(t, client, srv.URL, planID)
	var decreaseLineID, increaseLineID string
	var decreaseRemaining int64
	for _, line := range lines {
		if line.ActionDirection == "decrease" && decreaseLineID == "" {
			decreaseLineID = line.ID
			decreaseRemaining = line.RemainingDeltaMinor
		}
		if line.ActionDirection == "increase" && increaseLineID == "" {
			increaseLineID = line.ID
		}
	}
	if decreaseLineID == "" || increaseLineID == "" {
		t.Fatal("need decrease and increase lines")
	}
	sellAmount := -decreaseRemaining
	if sellAmount <= 0 {
		t.Fatal("invalid sell amount")
	}

	sellBody, _ := json.Marshal(map[string]any{"line_id": decreaseLineID, "amount_minor": sellAmount})
	sellResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/sell",
		"application/json", bytes.NewReader(sellBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if sellResp.StatusCode != http.StatusOK {
		t.Fatalf("sell status=%d", sellResp.StatusCode)
	}
	_ = sellResp.Body.Close()

	firstBuy := sellAmount / 2
	if firstBuy <= 0 {
		firstBuy = sellAmount
	}
	buyBody, _ := json.Marshal(map[string]any{"line_id": increaseLineID, "amount_minor": firstBuy})
	buyResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/buy",
		"application/json", bytes.NewReader(buyBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if buyResp.StatusCode != http.StatusOK {
		t.Fatalf("first buy status=%d", buyResp.StatusCode)
	}
	_ = buyResp.Body.Close()

	overBuy := sellAmount - firstBuy + 100_00
	overBody, _ := json.Marshal(map[string]any{"line_id": increaseLineID, "amount_minor": overBuy})
	overResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/buy",
		"application/json", bytes.NewReader(overBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if overResp.StatusCode == http.StatusOK {
		t.Fatal("expected second buy exceeding cash pool to fail")
	}
	_ = overResp.Body.Close()

	detailResp, err := client.Get(
		srv.URL + "/api/v1/plans/" + planID + "/rebalance-executions/" + executionID,
	)
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Data struct {
			Execution struct {
				CashPoolMinor int64 `json:"cash_pool_minor"`
			} `json:"execution"`
		} `json:"data"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	_ = detailResp.Body.Close()
	wantCash := sellAmount - firstBuy
	if detail.Data.Execution.CashPoolMinor != wantCash {
		t.Fatalf("cash pool=%d want %d after rejected over-buy", detail.Data.Execution.CashPoolMinor, wantCash)
	}
}

type rebalanceExecutionTestLine struct {
	ID                  string `json:"id"`
	ActionDirection     string `json:"action_direction"`
	RemainingDeltaMinor int64  `json:"remaining_delta_minor"`
	ExecutionStatus     string `json:"execution_status"`
}

func createRebalanceExecutionForTest(
	t *testing.T, client *http.Client, baseURL, planID string,
) (string, []rebalanceExecutionTestLine) {
	t.Helper()
	createResp, err := client.Post(
		baseURL+"/api/v1/plans/"+planID+"/rebalance-executions",
		"application/json", bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create execution status=%d", createResp.StatusCode)
	}
	var created struct {
		Data struct {
			Execution struct {
				ID string `json:"id"`
			} `json:"execution"`
			Lines []rebalanceExecutionTestLine `json:"lines"`
		} `json:"data"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	_ = createResp.Body.Close()
	if created.Data.Execution.ID == "" || len(created.Data.Lines) == 0 {
		t.Fatal("expected execution with lines")
	}
	return created.Data.Execution.ID, created.Data.Lines
}

func TestRebalanceExecutionSecondCreateRejected(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	firstResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions",
		"application/json", bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		t.Fatal(err)
	}
	if firstResp.StatusCode != http.StatusOK {
		t.Fatalf("first create status=%d", firstResp.StatusCode)
	}
	_ = firstResp.Body.Close()

	secondResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions",
		"application/json", bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(secondResp.Body)
	_ = secondResp.Body.Close()
	if secondResp.StatusCode == http.StatusOK {
		t.Fatal("expected second create to fail")
	}
	assertErrorCode(t, body, "active_execution_exists")

	var activeCount int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM rebalance_executions
		WHERE plan_id=? AND status IN ('draft','in_progress')`, planID).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Fatalf("active executions=%d want 1", activeCount)
	}
}

func TestRebalanceExecutionDoneLineCountOnlyCountsDone(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	executionID, lines := createRebalanceExecutionForTest(t, client, srv.URL, planID)
	if len(lines) < 3 {
		t.Fatalf("need at least 3 lines, got %d", len(lines))
	}

	var decreaseLineID, skipLineID string
	var decreaseRemaining int64
	for _, line := range lines {
		if line.ActionDirection == "decrease" && decreaseLineID == "" {
			decreaseLineID = line.ID
			decreaseRemaining = line.RemainingDeltaMinor
		}
	}
	for _, line := range lines {
		if line.ID != decreaseLineID && line.ExecutionStatus != "done" && line.ExecutionStatus != "skipped" {
			skipLineID = line.ID
			break
		}
	}
	if decreaseLineID == "" || skipLineID == "" {
		t.Fatal("need decrease and skippable lines")
	}

	sellAmount := -decreaseRemaining
	sellBody, _ := json.Marshal(map[string]any{"line_id": decreaseLineID, "amount_minor": sellAmount})
	sellResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/sell",
		"application/json", bytes.NewReader(sellBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if sellResp.StatusCode != http.StatusOK {
		t.Fatalf("sell status=%d", sellResp.StatusCode)
	}
	_ = sellResp.Body.Close()

	skipBody, _ := json.Marshal(map[string]any{"line_id": skipLineID})
	skipResp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-executions/"+executionID+"/skip",
		"application/json", bytes.NewReader(skipBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	if skipResp.StatusCode != http.StatusOK {
		t.Fatalf("skip status=%d", skipResp.StatusCode)
	}
	var after struct {
		Data struct {
			Stats struct {
				LineCount        int `json:"line_count"`
				DoneLineCount    int `json:"done_line_count"`
				SkippedLineCount int `json:"skipped_line_count"`
			} `json:"stats"`
		} `json:"data"`
	}
	if err := json.NewDecoder(skipResp.Body).Decode(&after); err != nil {
		t.Fatal(err)
	}
	_ = skipResp.Body.Close()

	if after.Data.Stats.LineCount < 3 {
		t.Fatalf("line_count=%d want >= 3", after.Data.Stats.LineCount)
	}
	if after.Data.Stats.DoneLineCount != 1 {
		t.Fatalf("done_line_count=%d want 1", after.Data.Stats.DoneLineCount)
	}
	if after.Data.Stats.SkippedLineCount != 1 {
		t.Fatalf("skipped_line_count=%d want 1", after.Data.Stats.SkippedLineCount)
	}
}
