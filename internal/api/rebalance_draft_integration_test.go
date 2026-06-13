package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func seedThreeHoldingsRebalancePlan(t *testing.T, db *sql.DB) (string, []string) {
	t.Helper()
	plan := createTestPlan(t, db)
	planID := plan.ID
	now := time.Now().UnixMilli()
	snapRepo := repository.NewSnapshotRepo(db)
	holdRepo := repository.NewHoldingsRepo(db)

	amounts := []int64{120_000_00, 90_000_00, 90_000_00}
	weights := []float64{0.3334, 0.3333, 0.3333}
	instIDs := []string{"ins_rbd_a", "ins_rbd_b", "ins_rbd_c"}

	for i, instID := range instIDs {
		if err := snapRepo.EnsureInstrument(context.Background(), repository.Instrument{
			ID: instID, Code: "RB" + string(rune('A'+i)), Name: "测试标的" + string(rune('A'+i)),
			Market: "CN", AssetClass: "equity", Region: "domestic", Currency: "CNY",
		}); err != nil {
			t.Fatal(err)
		}
		snapID := "snap_" + instID
		if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
			ID: snapID, InstrumentID: instID, PlanID: &planID,
			InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
			CompleteYearCount: 5, ObservationCount: 100,
			HistoricalCAGR: 0.08, ModeledAnnualReturn: 0.08, AnnualVolatility: 0.15, MaxDrawdown: 0.2,
			ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
			SourceMode: "akshare_historical", QualityStatus: "available",
			WarningsJSON: "[]", SourceHash: "fixture", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(context.Background(), `
			INSERT INTO plan_holdings (
				id, plan_id, instrument_id, enabled, asset_class, region,
				weight_within_group, current_amount_minor, simulation_snapshot_id,
				sort_order, created_at, updated_at
			) VALUES (?,?,?,1,'equity','domestic',?,?,?,?,?,?)`,
			"hold_"+instID, planID, instID, weights[i], amounts[i], snapID, i*10, now, now); err != nil {
			t.Fatal(err)
		}
	}
	_ = holdRepo

	stmts := []struct {
		query string
		args  []any
	}{
		{`UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, []any{300_000_00, planID}},
		{`UPDATE plan_asset_class_targets SET weight=1.0 WHERE plan_id=? AND asset_class='equity'`, []any{planID}},
		{`UPDATE plan_asset_class_targets SET weight=0 WHERE plan_id=? AND asset_class IN ('bond','cash')`, []any{planID}},
		{`UPDATE plan_region_targets SET weight_within_class=1.0
		 WHERE plan_id=? AND asset_class='equity' AND region='domestic'`, []any{planID}},
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(context.Background(), stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, instID := range instIDs {
		seedInstrumentMarketData(t, db, instID)
	}
	return planID, instIDs
}

func seedInstrumentMarketData(t *testing.T, db *sql.DB, instID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	for _, p := range buildTwentyYearFixturePoints() {
		if _, err := db.ExecContext(ctx, `
			INSERT OR IGNORE INTO market_data_points (instrument_id, trade_date, value, point_type, source_name, fetched_at)
			VALUES (?, ?, ?, 'adjusted_close', 'fixture', ?)`,
			instID, p.Date, p.Value, now); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRebalanceDraftCRUDFlow(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts",
		"application/json",
		bytes.NewReader([]byte(`{}`)),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create draft status=%d body=%s", resp.StatusCode, string(body))
	}
	env := decodeEnvelope(t, body)
	data := env["data"].(map[string]any)
	draft := data["draft"].(map[string]any)
	draftID := draft["id"].(string)
	lines := data["lines"].([]any)
	if len(lines) == 0 {
		t.Fatal("expected draft lines")
	}
	line0 := lines[0].(map[string]any)
	lineID := line0["id"].(string)
	frozenTarget := int64(line0["frozen_target_minor"].(float64))

	// PATCH stage: reduce first line by 20w
	planned := int64(line0["baseline_current_minor"].(float64)) - 20_000_00
	patchBody, _ := json.Marshal(map[string]any{
		"stage": true,
		"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
	})
	resp, err = client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(patchBody)))
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(body))
	}
	env = decodeEnvelope(t, body)
	patchData := env["data"].(map[string]any)
	patchLines := patchData["lines"].([]any)
	if int64(patchLines[0].(map[string]any)["planned_current_minor"].(float64)) != planned {
		t.Fatal("planned not updated")
	}
	if int64(patchLines[0].(map[string]any)["frozen_target_minor"].(float64)) != frozenTarget {
		t.Fatal("frozen target changed after edit")
	}
	events := patchData["events"].([]any)
	if len(events) == 0 {
		t.Fatal("expected stage event")
	}

	// undo
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/undo",
		"application/json",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d body=%s", resp.StatusCode, string(body))
	}

	// active draft
	resp, err = client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-drafts/active")
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("active status=%d body=%s", resp.StatusCode, string(body))
	}
	env = decodeEnvelope(t, body)
	active := env["data"].(map[string]any)
	if active["draft"].(map[string]any)["id"].(string) != draftID {
		t.Fatal("active draft mismatch")
	}

	// stage again and commit
	resp, err = client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(patchBody)))
	if err != nil {
		t.Fatal(err)
	}
	mustRead(t, resp)

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	planEnv := decodeEnvelope(t, mustRead(t, planResp))
	version := int(planEnv["data"].(map[string]any)["config_version"].(float64))

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":      version,
		"confirm_imbalanced":  true,
		"accept_scale_shrink": true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", resp.StatusCode, string(body))
	}
	commitEnv := decodeEnvelope(t, body)
	if commitEnv["data"].(map[string]any)["draft"].(map[string]any)["status"].(string) != "committed" {
		t.Fatal("draft not committed")
	}

	var committedAmount int64
	err = db.QueryRowContext(context.Background(),
		`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, instIDs[0]).Scan(&committedAmount)
	if err != nil {
		t.Fatal(err)
	}
	if committedAmount != planned {
		t.Fatalf("holdings not updated after commit: got %d want %d", committedAmount, planned)
	}

	// cancel on committed should fail — create new draft attempt
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts",
		"application/json",
		bytes.NewReader([]byte(`{}`)),
	)
	if err != nil {
		t.Fatal(err)
	}
	body = mustRead(t, resp)
	// may fail if no structural actionable after commit — that's ok
	if resp.StatusCode == http.StatusOK {
		env2 := decodeEnvelope(t, body)
		cancelID := env2["data"].(map[string]any)["draft"].(map[string]any)["id"].(string)
		req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+cancelID, nil)
		resp2, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp2.Body.Close() }()
		if resp2.StatusCode != http.StatusOK {
			t.Fatalf("cancel status=%d", resp2.StatusCode)
		}
	}
}

func mustPatchRequest(t *testing.T, url string, body *bytes.Reader) *http.Request {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(http.MethodPatch, url, body)
	} else {
		req, err = http.NewRequest(http.MethodPatch, url, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func createRebalanceDraft(t *testing.T, client *http.Client, baseURL, planID string) (string, string, int) {
	t.Helper()
	resp, err := client.Post(baseURL+"/api/v1/plans/"+planID+"/rebalance-drafts", "application/json",
		bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create draft status=%d body=%s", resp.StatusCode, string(body))
	}
	env := decodeEnvelope(t, body)
	data := env["data"].(map[string]any)
	draft := data["draft"].(map[string]any)
	draftID := draft["id"].(string)
	lines := data["lines"].([]any)
	lineID := lines[0].(map[string]any)["id"].(string)

	planResp, err := client.Get(baseURL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	planEnv := decodeEnvelope(t, mustRead(t, planResp))
	version := int(planEnv["data"].(map[string]any)["config_version"].(float64))
	return draftID, lineID, version
}

func TestAssetRefreshPOST(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	newAmounts := []int64{130_000_00, 85_000_00, 85_000_00}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{"instrument_id": id, "current_amount_minor": newAmounts[i]}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": false,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh status=%d body=%s", resp.StatusCode, string(respBody))
	}
	env := decodeEnvelope(t, respBody)
	if env["data"].(map[string]any)["after_total_minor"].(float64) != float64(300_000_00) {
		t.Fatal("unexpected after total")
	}
}

func TestRebalanceDraftCommitFundPoolImbalanced(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{
				"line_id": lineID, "planned_current_minor": int64(100_000_00),
			}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":     version,
		"confirm_imbalanced": false,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected imbalanced commit to fail, body=%s", string(body))
	}
	assertErrorCode(t, body, "unallocated_to_cash_required")
}

func TestRebalanceDraftCommitVersionConflict(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{
				"line_id": lineID, "planned_current_minor": int64(100_000_00),
			}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	mustRead(t, resp)

	if _, err := db.ExecContext(context.Background(),
		`UPDATE plans SET config_version=config_version+1 WHERE id=?`, planID); err != nil {
		t.Fatal(err)
	}

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":      version,
		"confirm_imbalanced":  true,
		"accept_scale_shrink": true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected version conflict, body=%s", string(body))
	}
	assertErrorCode(t, body, "plan_version_conflict")
}

func TestRebalanceDraftCommitNegativeAmount(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	if _, err := db.ExecContext(context.Background(),
		`UPDATE rebalance_draft_lines SET planned_current_minor=? WHERE id=?`, -1, lineID); err != nil {
		t.Fatal(err)
	}

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":     version,
		"confirm_imbalanced": true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected negative amount rejection, body=%s", string(body))
	}
	assertErrorCode(t, body, "validation_failed")
}

func mustJSONBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRebalanceDraftPersistenceAfterGET(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, _ := createRebalanceDraft(t, client, srv.URL, planID)
	planned := int64(100_000_00)
	patchBody := mustJSONBytes(t, map[string]any{
		"stage": true,
		"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
	})
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(patchBody)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	resp, err = client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-drafts/" + draftID)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get draft status=%d body=%s", resp.StatusCode, string(body))
	}
	env := decodeEnvelope(t, body)
	lines := env["data"].(map[string]any)["lines"].([]any)
	got := int64(lines[0].(map[string]any)["planned_current_minor"].(float64))
	if got != planned {
		t.Fatalf("planned after refetch: got %d want %d", got, planned)
	}
	events := env["data"].(map[string]any)["events"].([]any)
	if len(events) == 0 {
		t.Fatal("expected staged events after refetch")
	}
}

func TestRebalanceDraftUndoRestoresStagedLineState(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, _ := createRebalanceDraft(t, client, srv.URL, planID)
	stage := func(planned int64) {
		t.Helper()
		resp, err := client.Do(mustPatchRequest(t,
			srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
			bytes.NewReader(mustJSONBytes(t, map[string]any{
				"stage": true,
				"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
			}))))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
		}
	}
	stage(110_000_00)
	stage(100_000_00)

	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/undo",
		"application/json", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	line := env["data"].(map[string]any)["lines"].([]any)[0].(map[string]any)
	if int64(line["planned_current_minor"].(float64)) != 110_000_00 {
		t.Fatalf("undo planned: got %v want 11000000", line["planned_current_minor"])
	}
	if line["last_saved_at"] == nil {
		t.Fatal("expected last_saved_at restored after undo to earlier stage")
	}
	events := env["data"].(map[string]any)["events"].([]any)
	if len(events) == 0 {
		t.Fatal("expected remaining stage events")
	}
	var undoPayload string
	for _, raw := range events {
		ev := raw.(map[string]any)
		if ev["event_type"].(string) == "undo" {
			undoPayload = ev["payload_json"].(string)
		}
	}
	if !strings.Contains(undoPayload, "summary") || !strings.Contains(undoPayload, "撤销") {
		t.Fatalf("expected undo summary in timeline payload: %s", undoPayload)
	}
}

func TestAssetRefreshSyncScaleAndAuditEvent(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	newAmounts := []int64{100_000_00, 100_000_00, 100_000_00}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{"instrument_id": id, "current_amount_minor": newAmounts[i]}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": true,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh status=%d body=%s", resp.StatusCode, string(respBody))
	}
	env := decodeEnvelope(t, respBody)
	if env["data"].(map[string]any)["synced_scale"].(bool) != true {
		t.Fatal("expected synced_scale true")
	}

	var paramTotal int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT total_assets_minor FROM plan_parameters WHERE plan_id=?`, planID).Scan(&paramTotal); err != nil {
		t.Fatal(err)
	}
	if paramTotal != 300_000_00 {
		t.Fatalf("parameters total: got %d want 30000000", paramTotal)
	}

	var auditCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM asset_refresh_events WHERE plan_id=?`, planID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit event, got %d", auditCount)
	}
	var syncScale, configChanged int
	if err := db.QueryRowContext(context.Background(), `
		SELECT sync_scale, config_changed FROM asset_refresh_events WHERE plan_id=? LIMIT 1`, planID).
		Scan(&syncScale, &configChanged); err != nil {
		t.Fatal(err)
	}
	if syncScale != 1 || configChanged != 1 {
		t.Fatalf("audit flags sync=%d config=%d", syncScale, configChanged)
	}
}

func TestAssetRefreshAtomicRollbackOnSyncFailure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	paramsRepo := repository.NewParametersRepo(db)
	beforeParams, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	beforeScenario := ""
	if beforeParams.SelectedScenarioID != nil {
		beforeScenario = *beforeParams.SelectedScenarioID
	}

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	amounts := []int64{120_000_00, 90_000_00, 90_000_00}
	weights := []float64{0.3334, 0.3333, 0.3333}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{
			"instrument_id":        id,
			"current_amount_minor": amounts[i],
			"weight_within_group":  weights[i],
			"sort_order":           i * 10,
		}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"scenario_id":             "scn_builtin_post_fire",
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": true,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected asset refresh to fail, body=%s", string(respBody))
	}
	assertErrorCode(t, respBody, "plan_weights_invalid")

	afterParams, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	afterScenario := ""
	if afterParams.SelectedScenarioID != nil {
		afterScenario = *afterParams.SelectedScenarioID
	}
	if afterScenario != beforeScenario {
		t.Fatalf("scenario changed after failed refresh: before=%q after=%q", beforeScenario, afterScenario)
	}

	var holdingCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM plan_holdings WHERE plan_id=?`, planID).Scan(&holdingCount); err != nil {
		t.Fatal(err)
	}
	if holdingCount != len(instIDs) {
		t.Fatalf("holdings count changed: got %d want %d", holdingCount, len(instIDs))
	}

	var disabled int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM plan_holdings WHERE plan_id=? AND enabled=0`, planID).Scan(&disabled); err != nil {
		t.Fatal(err)
	}
	if disabled != 0 {
		t.Fatalf("expected no disabled holdings after rollback, got %d", disabled)
	}

	var auditCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM asset_refresh_events WHERE plan_id=?`, planID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 0 {
		t.Fatalf("expected no audit event after rollback, got %d", auditCount)
	}
}

func TestAssetRefreshAtomicScenarioAndStructure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	bondInstID := seedBondInstrumentForPlan(t, db, planID)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	equityAmounts := []int64{82_500_00, 41_250_00, 41_250_00}
	equityWeights := []float64{0.5, 0.25, 0.25}
	holdings := make([]map[string]any, 0, len(instIDs)+2)
	for i, id := range instIDs {
		holdings = append(holdings, map[string]any{
			"instrument_id":        id,
			"current_amount_minor": equityAmounts[i],
			"weight_within_group":  equityWeights[i],
			"sort_order":           i * 10,
		})
	}
	holdings = append(holdings,
		map[string]any{
			"instrument_id":        bondInstID,
			"current_amount_minor": 105_000_00,
			"weight_within_group":  1.0,
			"sort_order":           30,
		},
		map[string]any{
			"instrument_id":        repository.SystemCashInstrumentID,
			"current_amount_minor": 30_000_00,
			"weight_within_group":  1.0,
			"sort_order":           40,
		},
	)
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"scenario_id":             "scn_builtin_post_fire",
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": true,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("asset refresh status=%d body=%s", resp.StatusCode, string(respBody))
	}

	paramsRepo := repository.NewParametersRepo(db)
	params, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if params.SelectedScenarioID == nil || *params.SelectedScenarioID != "scn_builtin_post_fire" {
		got := ""
		if params.SelectedScenarioID != nil {
			got = *params.SelectedScenarioID
		}
		t.Fatalf("expected scenario scn_builtin_post_fire, got %q", got)
	}

	var weight float64
	if err := db.QueryRowContext(context.Background(), `
		SELECT weight_within_group FROM plan_holdings
		WHERE plan_id=? AND instrument_id=?`, planID, instIDs[0]).Scan(&weight); err != nil {
		t.Fatal(err)
	}
	if weight != 0.5 {
		t.Fatalf("expected first holding weight 0.5, got %v", weight)
	}

	var auditCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM asset_refresh_events WHERE plan_id=?`, planID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 audit event, got %d", auditCount)
	}
}

func TestAssetRefreshScenarioSwitchRejectsMismatchedHoldings(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	planResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID)
	if err != nil {
		t.Fatal(err)
	}
	version := int(decodeEnvelope(t, mustRead(t, planResp))["data"].(map[string]any)["config_version"].(float64))

	amounts := []int64{120_000_00, 90_000_00, 90_000_00}
	weights := []float64{0.3334, 0.3333, 0.3333}
	holdings := make([]map[string]any, len(instIDs))
	for i, id := range instIDs {
		holdings[i] = map[string]any{
			"instrument_id":        id,
			"current_amount_minor": amounts[i],
			"weight_within_group":  weights[i],
			"sort_order":           i * 10,
		}
	}
	body, _ := json.Marshal(map[string]any{
		"config_version":          version,
		"scenario_id":             "scn_builtin_post_fire",
		"holdings":                holdings,
		"total_assets_minor":      300_000_00,
		"sync_total_assets_minor": false,
		"config_changed":          true,
	})
	resp, err := client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/asset-refresh",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	respBody := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected asset refresh to fail on scenario switch with equity-only holdings, body=%s", string(respBody))
	}
	assertErrorCode(t, respBody, "plan_weights_invalid")

	paramsRepo := repository.NewParametersRepo(db)
	params, err := paramsRepo.Get(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	if params.SelectedScenarioID != nil && *params.SelectedScenarioID == "scn_builtin_post_fire" {
		t.Fatal("scenario must not change after failed refresh")
	}
}

func seedBondInstrumentForPlan(t *testing.T, db *sql.DB, planID string) string {
	t.Helper()
	snapRepo := repository.NewSnapshotRepo(db)
	instID := "ins_rbd_bond"
	now := time.Now().UnixMilli()
	if err := snapRepo.EnsureInstrument(context.Background(), repository.Instrument{
		ID: instID, Code: "RBOND", Name: "测试债券基金", Market: "CN",
		AssetClass: "bond", Region: "domestic", Currency: "CNY",
	}); err != nil {
		t.Fatal(err)
	}
	snapID := "snap_" + instID
	if err := snapRepo.CreatePlanSnapshot(context.Background(), nil, repository.SimulationSnapshot{
		ID: snapID, InstrumentID: instID, PlanID: &planID,
		InclusionDate: "2026-06-09", AsOfDate: "2026-06-09",
		CompleteYearCount: 5, ObservationCount: 100,
		HistoricalCAGR: 0.04, ModeledAnnualReturn: 0.04, AnnualVolatility: 0.05, MaxDrawdown: 0.05,
		ExpenseRatioStatus: "unavailable", FeeTreatment: "embedded",
		SourceMode: "akshare_historical", QualityStatus: "available",
		WarningsJSON: "[]", SourceHash: "fixture", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	seedInstrumentMarketData(t, db, instID)
	return instID
}

func seedCashHolding(t *testing.T, db *sql.DB, planID string, amountMinor int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	holdID := "hold_system_cash"
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plan_holdings (
			id, plan_id, instrument_id, enabled, asset_class, region,
			weight_within_group, current_amount_minor, simulation_snapshot_id,
			sort_order, created_at, updated_at
		) VALUES (?,?,?,1,'cash','domestic',1.0,?,'sim_snapshot_system_cash_cny',0,?,?)`,
		holdID, planID, repository.SystemCashInstrumentID, amountMinor, now, now); err != nil {
		t.Fatal(err)
	}
}

func TestRebalanceDraftPackageDeltaFrozenOnPatch_PKG3(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, _ := createRebalanceDraft(t, client, srv.URL, planID)
	resp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-drafts/" + draftID)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	lines := env["data"].(map[string]any)["lines"].([]any)
	beforeDelta := int64(lines[0].(map[string]any)["recommended_package_delta_minor"].(float64))

	resp, err = client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{
				"line_id": lineID, "planned_current_minor": int64(100_000_00),
			}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	resp, err = client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-drafts/" + draftID)
	if err != nil {
		t.Fatal(err)
	}
	env = decodeEnvelope(t, mustRead(t, resp))
	lines = env["data"].(map[string]any)["lines"].([]any)
	afterDelta := int64(lines[0].(map[string]any)["recommended_package_delta_minor"].(float64))
	if afterDelta != beforeDelta {
		t.Fatalf("package delta changed: before=%d after=%d", beforeDelta, afterDelta)
	}
}

func TestRebalanceDraftCommitCashSweep_CS1(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	seedCashHolding(t, db, planID, 10_000_00)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	planned := int64(100_000_00)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":            version,
		"confirm_imbalanced":        true,
		"sweep_unallocated_to_cash": true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", resp.StatusCode, string(body))
	}

	var equityAmount, cashAmount int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, instIDs[0]).Scan(&equityAmount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(context.Background(),
		`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, repository.SystemCashInstrumentID).Scan(&cashAmount); err != nil {
		t.Fatal(err)
	}
	if equityAmount != planned {
		t.Fatalf("equity amount=%d want %d", equityAmount, planned)
	}
	if cashAmount != 30_000_00 {
		t.Fatalf("cash after sweep=%d want 3000000", cashAmount)
	}
}

func TestRebalanceDraftCommitNoCashBlocked_CS2(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": int64(100_000_00)}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	mustRead(t, resp)

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":     version,
		"confirm_imbalanced": true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected blocked commit, body=%s", string(body))
	}
	assertErrorCode(t, body, "unallocated_to_cash_required")
}

func TestRebalanceDraftCommitScaleShrink_CS3(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	planned := int64(100_000_00)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	mustRead(t, resp)

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":      version,
		"confirm_imbalanced":  true,
		"accept_scale_shrink": true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", resp.StatusCode, string(body))
	}

	var total int64
	for _, instID := range instIDs {
		var amount int64
		if err := db.QueryRowContext(context.Background(),
			`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
			planID, instID).Scan(&amount); err != nil {
			t.Fatal(err)
		}
		total += amount
	}
	if total != 280_000_00 {
		t.Fatalf("total after shrink=%d want 28000000", total)
	}
}

func TestRebalanceDraftPatchSingleLineOthersUnchanged_APP1(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, _ := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, _ := createRebalanceDraft(t, client, srv.URL, planID)
	resp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-drafts/" + draftID)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, mustRead(t, resp))
	lines := env["data"].(map[string]any)["lines"].([]any)
	if len(lines) < 2 {
		t.Fatal("expected at least 2 draft lines")
	}
	before := make(map[string]map[string]int64, len(lines))
	for _, raw := range lines {
		line := raw.(map[string]any)
		id := line["id"].(string)
		before[id] = map[string]int64{
			"planned": int64(line["planned_current_minor"].(float64)),
			"delta":   int64(line["recommended_package_delta_minor"].(float64)),
		}
	}

	resp, err = client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{
				"line_id": lineID, "planned_current_minor": int64(100_000_00),
			}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	resp, err = client.Get(srv.URL + "/api/v1/plans/" + planID + "/rebalance-drafts/" + draftID)
	if err != nil {
		t.Fatal(err)
	}
	env = decodeEnvelope(t, mustRead(t, resp))
	lines = env["data"].(map[string]any)["lines"].([]any)
	for _, raw := range lines {
		line := raw.(map[string]any)
		lineIDGot := line["id"].(string)
		if lineIDGot == lineID {
			continue
		}
		prev := before[lineIDGot]
		planned := int64(line["planned_current_minor"].(float64))
		delta := int64(line["recommended_package_delta_minor"].(float64))
		if planned != prev["planned"] {
			t.Fatalf("line %s planned changed: before=%d after=%d", lineIDGot, prev["planned"], planned)
		}
		if delta != prev["delta"] {
			t.Fatalf("line %s package delta changed: before=%d after=%d", lineIDGot, prev["delta"], delta)
		}
	}
}

func TestRebalanceDraftCommitCashSweepPostDraft_CS5(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	seedCashHolding(t, db, planID, 10_000_00)

	planned := int64(100_000_00)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":            version,
		"confirm_imbalanced":        true,
		"sweep_unallocated_to_cash": true,
		"record_snapshot":           true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", resp.StatusCode, string(body))
	}

	var cashAmount int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, repository.SystemCashInstrumentID).Scan(&cashAmount); err != nil {
		t.Fatal(err)
	}
	if cashAmount != 30_000_00 {
		t.Fatalf("cash after post-draft sweep=%d want 3000000", cashAmount)
	}

	var cashSnapAmount int64
	if err := db.QueryRowContext(context.Background(), `
		SELECT psi.amount_minor FROM portfolio_snapshot_items psi
		JOIN portfolio_snapshots ps ON ps.id = psi.snapshot_id
		WHERE ps.plan_id=? AND psi.instrument_id=?
		ORDER BY ps.created_at DESC LIMIT 1`,
		planID, repository.SystemCashInstrumentID).Scan(&cashSnapAmount); err != nil {
		t.Fatal(err)
	}
	if cashSnapAmount != 30_000_00 {
		t.Fatalf("snapshot cash after post-draft sweep=%d want 3000000", cashSnapAmount)
	}

	var equityAmount int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, instIDs[0]).Scan(&equityAmount); err != nil {
		t.Fatal(err)
	}
	if equityAmount != planned {
		t.Fatalf("equity amount=%d want %d", equityAmount, planned)
	}
}

func TestRebalanceDraftCommitCashSweepSnapshot_CS4(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID, instIDs := seedThreeHoldingsRebalancePlan(t, db)
	seedCashHolding(t, db, planID, 10_000_00)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db}))
	defer srv.Close()
	client := srv.Client()

	draftID, lineID, version := createRebalanceDraft(t, client, srv.URL, planID)
	planned := int64(100_000_00)
	resp, err := client.Do(mustPatchRequest(t,
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/lines",
		bytes.NewReader(mustJSONBytes(t, map[string]any{
			"stage": true,
			"lines": []map[string]any{{"line_id": lineID, "planned_current_minor": planned}},
		}))))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, string(mustRead(t, resp)))
	}

	commitBody, _ := json.Marshal(map[string]any{
		"config_version":            version,
		"confirm_imbalanced":        true,
		"sweep_unallocated_to_cash": true,
		"record_snapshot":           true,
	})
	resp, err = client.Post(
		srv.URL+"/api/v1/plans/"+planID+"/rebalance-drafts/"+draftID+"/commit",
		"application/json",
		bytes.NewReader(commitBody),
	)
	if err != nil {
		t.Fatal(err)
	}
	body := mustRead(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("commit status=%d body=%s", resp.StatusCode, string(body))
	}

	var snapTotal int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT total_amount_minor FROM portfolio_snapshots WHERE plan_id=? ORDER BY created_at DESC LIMIT 1`,
		planID).Scan(&snapTotal); err != nil {
		t.Fatal(err)
	}
	if snapTotal != 310_000_00 {
		t.Fatalf("snapshot total=%d want 31000000", snapTotal)
	}

	var cashSnapAmount int64
	if err := db.QueryRowContext(context.Background(), `
		SELECT psi.amount_minor FROM portfolio_snapshot_items psi
		JOIN portfolio_snapshots ps ON ps.id = psi.snapshot_id
		WHERE ps.plan_id=? AND psi.instrument_id=?
		ORDER BY ps.created_at DESC LIMIT 1`,
		planID, repository.SystemCashInstrumentID).Scan(&cashSnapAmount); err != nil {
		t.Fatal(err)
	}
	if cashSnapAmount != 30_000_00 {
		t.Fatalf("snapshot cash amount=%d want 3000000", cashSnapAmount)
	}

	var equityAmount int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT current_amount_minor FROM plan_holdings WHERE plan_id=? AND instrument_id=?`,
		planID, instIDs[0]).Scan(&equityAmount); err != nil {
		t.Fatal(err)
	}
	if equityAmount != planned {
		t.Fatalf("equity amount=%d want %d", equityAmount, planned)
	}
}
