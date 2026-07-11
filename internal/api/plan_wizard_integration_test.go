//go:build integration

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fireman/fireman/internal/service"
)

func countTable(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func wizardRegionTargets() []map[string]any {
	return []map[string]any{
		{"asset_class": "equity", "region": "domestic", "weight_within_class": 1.0},
		{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.0},
		{"asset_class": "bond", "region": "domestic", "weight_within_class": 1.0},
		{"asset_class": "bond", "region": "foreign", "weight_within_class": 0.0},
		{"asset_class": "cash", "region": "domestic", "weight_within_class": 1.0},
		{"asset_class": "cash", "region": "foreign", "weight_within_class": 0.0},
	}
}

func wizardParams(total int64) map[string]any {
	return map[string]any{
		"current_age": 30, "retirement_age": 55, "end_age": 90,
		"total_assets_minor": total, "annual_savings_minor": 200_000_00,
		"annual_savings_growth_rate": 0, "annual_spending_minor": 400_000_00,
		"terminal_wealth_floor_minor": 0,
		"inflation_mode":              "fixed_real", "fixed_inflation_rate": 0.03,
		"inflation_mu": 0.03, "inflation_phi": 0.5, "inflation_sigma": 0.01,
		"withdrawal_type": "fixed_real", "withdrawal_rate": 0.04,
		"withdrawal_floor_ratio": 0.70, "withdrawal_ceiling_ratio": 1.30,
		"withdrawal_tax_rate": 0, "taxable_withdrawal_ratio": 0,
		"rebalance_frequency": "annual", "rebalance_threshold": 0.03,
		"transaction_cost_rate": 0, "simulation_runs": 10000, "student_t_df": 7,
	}
}

func postWizard(t *testing.T, client *http.Client, baseURL string, body map[string]any) (*http.Response, []byte) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := client.Post(baseURL+"/api/v1/plans/wizard", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return resp, readBody(t, resp)
}

func TestPlanWizardSuccessIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	assetEquity := seedAssetCode(t, db, "510300")
	assetBond := seedAssetCode(t, db, "510500")

	const total = int64(10_000_000_00)
	body := map[string]any{
		"name": "向导集成测试", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": "scn_builtin_near_fire",
		"parameters":           wizardParams(total),
		"region_targets":       wizardRegionTargets(),
		"holdings": []map[string]any{
			{
				"asset_key": assetEquity, "asset_class": "equity", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 7_000_000_00,
				"sort_order": 1,
			},
			{
				"asset_key": assetBond, "asset_class": "bond", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 3_000_000_00,
				"sort_order": 2,
			},
		},
	}

	plansBefore := countTable(t, db, "plans")
	snapsBefore := countTable(t, db, "market_asset_simulation_snapshots")

	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wizard status=%d body=%s", resp.StatusCode, string(raw))
	}
	env := decodeEnvelope(t, raw)
	plan := env["data"].(map[string]any)
	planID := plan["id"].(string)
	if int(plan["config_version"].(float64)) != 1 {
		t.Fatalf("expected config_version=1, got %+v", plan["config_version"])
	}

	if countTable(t, db, "plans") != plansBefore+1 {
		t.Fatal("expected exactly one new plan")
	}
	if countTable(t, db, "market_asset_simulation_snapshots") <= snapsBefore {
		t.Fatal("expected new plan snapshots")
	}

	holdResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/holdings")
	if err != nil {
		t.Fatal(err)
	}
	holdEnv := decodeEnvelope(t, readBody(t, holdResp))
	holdings := holdEnv["data"].(map[string]any)["holdings"].([]any)

	var sum int64
	snapIDs := make(map[string]bool)
	for _, h := range holdings {
		row := h.(map[string]any)
		if row["enabled"].(bool) {
			sum += int64(row["current_amount_minor"].(float64))
		}
		snapIDs[row["simulation_snapshot_id"].(string)] = true
	}
	if sum != total {
		t.Fatalf("holdings sum=%d want %d", sum, total)
	}

	allocResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/allocation")
	if err != nil {
		t.Fatal(err)
	}
	allocEnv := decodeEnvelope(t, readBody(t, allocResp))
	targets := allocEnv["data"].(map[string]any)["asset_class_targets"].([]any)
	equityW := 0.0
	for _, tg := range targets {
		row := tg.(map[string]any)
		if row["asset_class"].(string) == "equity" {
			equityW = row["weight"].(float64)
		}
	}
	if equityW != 0.70 {
		t.Fatalf("expected equity target 0.70, got %v", equityW)
	}

	for _, h := range holdings {
		row := h.(map[string]any)
		sid := row["simulation_snapshot_id"].(string)
		if sid == "sim_snapshot_system_cash_cny" {
			continue
		}
		var planRef sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT plan_id FROM market_asset_simulation_snapshots WHERE id=?`, sid).Scan(&planRef); err != nil {
			t.Fatal(err)
		}
		if !planRef.Valid || planRef.String != planID {
			t.Fatalf("snapshot %s not tied to plan %s", sid, planID)
		}
	}
}

func TestPlanWizardAdvancedParametersIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	assetEquity := seedAssetCode(t, db, "510300")
	assetBond := seedAssetCode(t, db, "510500")

	const total = int64(10_000_000_00)
	params := wizardParams(total)
	// Advanced FIRE params chosen in the wizard's disclosure must be
	// persisted as plan parameters, not silently replaced by hard-coded defaults.
	params["inflation_mode"] = "random_ar1"
	params["inflation_mu"] = 0.025
	params["inflation_sigma"] = 0.012
	params["inflation_phi"] = 0.4
	params["withdrawal_type"] = "guardrail"
	params["withdrawal_rate"] = 0.045
	params["withdrawal_floor_ratio"] = 0.65
	params["withdrawal_ceiling_ratio"] = 1.25

	body := map[string]any{
		"name": "向导-高级参数", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": "scn_builtin_near_fire",
		"parameters":           params,
		"region_targets":       wizardRegionTargets(),
		"holdings": []map[string]any{
			{
				"asset_key": assetEquity, "asset_class": "equity", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0,
				"current_amount_minor": 7_000_000_00, "sort_order": 1,
			},
			{
				"asset_key": assetBond, "asset_class": "bond", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0,
				"current_amount_minor": 3_000_000_00, "sort_order": 2,
			},
		},
	}

	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wizard status=%d body=%s", resp.StatusCode, string(raw))
	}
	planID := decodeEnvelope(t, raw)["data"].(map[string]any)["id"].(string)

	paramsResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeEnvelope(t, readBody(t, paramsResp))["data"].(map[string]any)["parameters"].(map[string]any)

	if got["inflation_mode"] != "random_ar1" {
		t.Fatalf("inflation_mode=%v want random_ar1", got["inflation_mode"])
	}
	if got["withdrawal_type"] != "guardrail" {
		t.Fatalf("withdrawal_type=%v want guardrail", got["withdrawal_type"])
	}
	numeric := map[string]float64{
		"inflation_mu":             0.025,
		"inflation_sigma":          0.012,
		"inflation_phi":            0.4,
		"withdrawal_rate":          0.045,
		"withdrawal_floor_ratio":   0.65,
		"withdrawal_ceiling_ratio": 1.25,
	}
	for k, want := range numeric {
		if v, _ := got[k].(float64); v != want {
			t.Fatalf("parameter %s=%v want %v", k, got[k], want)
		}
	}
}

func TestPlanWizardInvalidAdvancedParametersRejected(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	assetEquity := seedAssetCode(t, db, "510300")
	assetBond := seedAssetCode(t, db, "510500")

	const total = int64(10_000_000_00)
	holdings := []map[string]any{
		{
			"asset_key": assetEquity, "asset_class": "equity", "region": "domestic",
			"enabled": true, "weight_within_group": 1.0,
			"current_amount_minor": 7_000_000_00, "sort_order": 1,
		},
		{
			"asset_key": assetBond, "asset_class": "bond", "region": "domestic",
			"enabled": true, "weight_within_group": 1.0,
			"current_amount_minor": 3_000_000_00, "sort_order": 2,
		},
	}

	// Each out-of-range advanced parameter must be rejected at wizard
	// creation, leaving no plan / holdings / snapshot rows behind.
	cases := []struct {
		name  string
		apply func(p map[string]any)
	}{
		{"tax product equals one", func(p map[string]any) {
			p["withdrawal_tax_rate"] = 1.0
			p["taxable_withdrawal_ratio"] = 1.0
		}},
		{"guardrail ceiling above two", func(p map[string]any) {
			p["withdrawal_type"] = "guardrail"
			p["withdrawal_ceiling_ratio"] = 2.5
		}},
		{"floor zero", func(p map[string]any) { p["withdrawal_floor_ratio"] = 0.0 }},
		{"fixed inflation above cap", func(p map[string]any) { p["fixed_inflation_rate"] = 0.25 }},
		{"negative transaction cost", func(p map[string]any) { p["transaction_cost_rate"] = -0.01 }},
		{"transaction cost equals one", func(p map[string]any) { p["transaction_cost_rate"] = 1.0 }},
		{"unsupported rebalance frequency", func(p map[string]any) { p["rebalance_frequency"] = "weekly" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plansBefore := countTable(t, db, "plans")
			snapsBefore := countTable(t, db, "market_asset_simulation_snapshots")
			params := wizardParams(total)
			tc.apply(params)
			body := map[string]any{
				"name": "向导-非法参数", "base_currency": "CNY", "valuation_date": "2026-06-09",
				"selected_scenario_id": "scn_builtin_near_fire",
				"parameters":           params,
				"region_targets":       wizardRegionTargets(),
				"holdings":             holdings,
			}
			resp, raw := postWizard(t, client, srv.URL, body)
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("expected rejection, got 200 body=%s", string(raw))
			}
			assertErrorCode(t, raw, "parameters_invalid")
			if got := countTable(t, db, "plans"); got != plansBefore {
				t.Fatalf("plans changed: before=%d after=%d", plansBefore, got)
			}
			if got := countTable(t, db, "market_asset_simulation_snapshots"); got != snapsBefore {
				t.Fatalf("snapshots changed: before=%d after=%d", snapsBefore, got)
			}
		})
	}

	// Sanity: a valid tax product (0.2 * 0.8) is accepted and simulable.
	params := wizardParams(total)
	params["withdrawal_tax_rate"] = 0.2
	params["taxable_withdrawal_ratio"] = 0.8
	body := map[string]any{
		"name": "向导-合法税率", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": "scn_builtin_near_fire",
		"parameters":           params,
		"region_targets":       wizardRegionTargets(),
		"holdings":             holdings,
	}
	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected valid tax product accepted, status=%d body=%s", resp.StatusCode, string(raw))
	}
}

func TestPlanWizardRegionTargetsIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	assetEquityDomestic := seedAssetCode(t, db, "510300")
	assetEquityForeign := seedAssetCode(t, db, "510500")
	assetBond := seedAssetCode(t, db, "159915")

	const total = int64(10_000_000_00)
	customTargets := []map[string]any{
		{"asset_class": "equity", "region": "domestic", "weight_within_class": 0.7},
		{"asset_class": "equity", "region": "foreign", "weight_within_class": 0.3},
		{"asset_class": "bond", "region": "domestic", "weight_within_class": 1.0},
		{"asset_class": "bond", "region": "foreign", "weight_within_class": 0.0},
		{"asset_class": "cash", "region": "domestic", "weight_within_class": 1.0},
		{"asset_class": "cash", "region": "foreign", "weight_within_class": 0.0},
	}
	body := map[string]any{
		"name": "向导-地区目标", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": "scn_builtin_near_fire",
		"parameters":           wizardParams(total),
		"region_targets":       customTargets,
		"holdings": []map[string]any{
			{
				"asset_key": assetEquityDomestic, "asset_class": "equity", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0,
				"current_amount_minor": 4_900_000_00, "sort_order": 1,
			},
			{
				"asset_key": assetEquityForeign, "asset_class": "equity", "region": "foreign",
				"enabled": true, "weight_within_group": 1.0,
				"current_amount_minor": 2_100_000_00, "sort_order": 2,
			},
			{
				"asset_key": assetBond, "asset_class": "bond", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 3_000_000_00,
				"sort_order": 3,
			},
		},
	}

	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wizard status=%d body=%s", resp.StatusCode, string(raw))
	}
	env := decodeEnvelope(t, raw)
	planID := env["data"].(map[string]any)["id"].(string)

	allocResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/allocation")
	if err != nil {
		t.Fatal(err)
	}
	allocEnv := decodeEnvelope(t, readBody(t, allocResp))
	regionTargets := allocEnv["data"].(map[string]any)["region_targets"].([]any)

	equityForeign := -1.0
	for _, tg := range regionTargets {
		row := tg.(map[string]any)
		if row["asset_class"].(string) == "equity" && row["region"].(string) == "foreign" {
			equityForeign = row["weight_within_class"].(float64)
		}
	}
	if equityForeign != 0.3 {
		t.Fatalf("expected equity foreign target 0.30, got %v", equityForeign)
	}
}

func TestPlanWizardApplyUnallocatedToCashIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	assetEquity := seedAssetCode(t, db, "510300")
	assetBond := seedAssetCode(t, db, "510500")

	const total = int64(10_000_000_00)
	const equityAmt = int64(7_000_000_00)
	const bondAmt = int64(2_000_000_00)
	body := map[string]any{
		"name": "向导-差额入现金", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id":      "scn_builtin_near_fire",
		"parameters":                wizardParams(total),
		"region_targets":            wizardRegionTargets(),
		"apply_unallocated_to_cash": true,
		"holdings": []map[string]any{
			{
				"asset_key": assetEquity, "asset_class": "equity", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": equityAmt,
				"sort_order": 1,
			},
			{
				"asset_key": assetBond, "asset_class": "bond", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": bondAmt,
				"sort_order": 2,
			},
		},
	}

	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wizard status=%d body=%s", resp.StatusCode, string(raw))
	}
	env := decodeEnvelope(t, raw)
	planID := env["data"].(map[string]any)["id"].(string)

	holdResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/holdings")
	if err != nil {
		t.Fatal(err)
	}
	holdEnv := decodeEnvelope(t, readBody(t, holdResp))
	holdings := holdEnv["data"].(map[string]any)["holdings"].([]any)

	var sum int64
	var cashMinor int64
	for _, h := range holdings {
		row := h.(map[string]any)
		if !row["enabled"].(bool) {
			continue
		}
		amt := int64(row["current_amount_minor"].(float64))
		sum += amt
		if row["asset_key"].(string) == "SYS|cash||CNY" {
			cashMinor = amt
		}
	}
	if sum != total {
		t.Fatalf("holdings sum=%d want %d", sum, total)
	}
	wantCash := total - equityAmt - bondAmt
	if cashMinor != wantCash {
		t.Fatalf("cash holding=%d want %d", cashMinor, wantCash)
	}
}

func TestPlanWizardFailureNoResidualIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	asset := seedAssetCode(t, db, "510300")

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{
			name: "group weight invalid",
			code: "plan_weights_invalid",
			body: map[string]any{
				"name": "失败-组内权重", "valuation_date": "2026-06-09",
				"selected_scenario_id":      "scn_builtin_near_fire",
				"parameters":                wizardParams(1_000_000_00),
				"region_targets":            wizardRegionTargets(),
				"apply_unallocated_to_cash": true,
				"holdings": []map[string]any{
					{
						"asset_key": asset, "asset_class": "equity", "region": "domestic",
						"enabled": true, "weight_within_group": 0.4, "current_amount_minor": 700_000_00,
						"sort_order": 1,
					},
				},
			},
		},
		{
			name: "holdings exceed total",
			code: "holdings_exceed_total",
			body: map[string]any{
				"name": "失败-超总资产", "valuation_date": "2026-06-09",
				"selected_scenario_id": "scn_builtin_near_fire",
				"parameters":           wizardParams(1_000_000_00),
				"region_targets":       wizardRegionTargets(),
				"holdings": []map[string]any{
					{
						"asset_key": asset, "asset_class": "equity", "region": "domestic",
						"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 2_000_000_00,
						"sort_order": 1,
					},
				},
			},
		},
		{
			name: "unknown asset",
			code: "market_asset_not_found",
			body: map[string]any{
				"name": "失败-未知标的", "valuation_date": "2026-06-09",
				"selected_scenario_id":      "scn_builtin_near_fire",
				"parameters":                wizardParams(1_000_000_00),
				"region_targets":            wizardRegionTargets(),
				"apply_unallocated_to_cash": true,
				"holdings": []map[string]any{
					{
						"asset_key": "cn:cn_exchange_fund:sh:999999", "asset_class": "equity", "region": "domestic",
						"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 700_000_00,
						"sort_order": 1,
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plansBefore := countTable(t, db, "plans")
			holdBefore := countTable(t, db, "plan_holdings")
			snapsBefore, err := service.CountSnapshots(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}

			resp, raw := postWizard(t, client, srv.URL, tc.body)
			if resp.StatusCode == http.StatusOK {
				t.Fatalf("expected failure, got ok body=%s", string(raw))
			}
			assertErrorCode(t, raw, tc.code)

			if countTable(t, db, "plans") != plansBefore {
				t.Fatal("plans table should have no residual row")
			}
			if countTable(t, db, "plan_holdings") != holdBefore {
				t.Fatal("holdings table should have no residual row")
			}
			snapsAfter, err := service.CountSnapshots(context.Background(), db)
			if err != nil {
				t.Fatal(err)
			}
			if snapsAfter != snapsBefore {
				t.Fatal("plan snapshots should have no residual row")
			}
		})
	}
}

// TestPlanWizardLazyHoldingIntegration: a wizard holding whose asset has no
// local history is saved lazily (empty snapshot id) instead of failing.
func TestPlanWizardLazyHoldingIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund:sh:513999"
	seed.Symbol = "513999"
	seed.Points = nil // directory row only, no history yet
	seedMarketAssetWithHistory(t, db, seed)
	assetBond := seedAssetCode(t, db, "510510")

	const total = int64(10_000_000_00)
	body := map[string]any{
		"name": "向导-懒快照", "valuation_date": "2026-06-09",
		"selected_scenario_id":      "scn_builtin_near_fire",
		"parameters":                wizardParams(total),
		"region_targets":            wizardRegionTargets(),
		"apply_unallocated_to_cash": true,
		"holdings": []map[string]any{
			{
				"asset_key": seed.AssetKey, "asset_class": "equity", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 7_000_000_00,
				"sort_order": 1,
			},
			{
				"asset_key": assetBond, "asset_class": "bond", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 3_000_000_00,
				"sort_order": 2,
			},
		},
	}
	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wizard with missing history must lazily save, status=%d body=%s", resp.StatusCode, string(raw))
	}
	planID := decodeEnvelope(t, raw)["data"].(map[string]any)["id"].(string)

	var snapID string
	if err := db.QueryRowContext(context.Background(), `
		SELECT simulation_snapshot_id FROM plan_holdings
		WHERE plan_id=? AND asset_key=?`, planID, seed.AssetKey).Scan(&snapID); err != nil {
		t.Fatal(err)
	}
	if snapID != "" {
		t.Fatalf("expected lazy holding with empty snapshot id, got %q", snapID)
	}
}
