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

func importInstrumentCode(t *testing.T, client *http.Client, baseURL, code string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund", "code": code,
	})
	resp, err := client.Post(baseURL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import %s status=%d body=%s", code, resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	return env["data"].(map[string]any)["id"].(string)
}

func TestPlanWizardSuccessIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	instEquity := importInstrumentCode(t, client, srv.URL, "510300")
	instBond := importInstrumentCode(t, client, srv.URL, "510500")
	if _, err := db.ExecContext(context.Background(),
		`UPDATE instruments SET asset_class='bond' WHERE id=?`, instBond); err != nil {
		t.Fatal(err)
	}

	const total = int64(10_000_000_00)
	body := map[string]any{
		"name": "向导集成测试", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": "scn_builtin_near_fire",
		"parameters":           wizardParams(total),
		"holdings": []map[string]any{
			{"instrument_id": instEquity, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": 7_000_000_00, "sort_order": 1},
			{"instrument_id": instBond, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": 3_000_000_00, "sort_order": 2},
		},
	}

	plansBefore := countTable(t, db, "plans")
	snapsBefore := countTable(t, db, "instrument_simulation_snapshots")

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
	if countTable(t, db, "instrument_simulation_snapshots") <= snapsBefore {
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
		if sid == "snap_system_cash" {
			continue
		}
		var planRef sql.NullString
		if err := db.QueryRowContext(context.Background(),
			`SELECT plan_id FROM instrument_simulation_snapshots WHERE id=?`, sid).Scan(&planRef); err != nil {
			t.Fatal(err)
		}
		if !planRef.Valid || planRef.String != planID {
			t.Fatalf("snapshot %s not tied to plan %s", sid, planID)
		}
	}
}

func TestPlanWizardApplyUnallocatedToCashIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)

	instEquity := importInstrumentCode(t, client, srv.URL, "510300")
	instBond := importInstrumentCode(t, client, srv.URL, "510500")
	if _, err := db.ExecContext(context.Background(),
		`UPDATE instruments SET asset_class='bond' WHERE id=?`, instBond); err != nil {
		t.Fatal(err)
	}

	const total = int64(10_000_000_00)
	const equityAmt = int64(7_000_000_00)
	const bondAmt = int64(2_000_000_00)
	body := map[string]any{
		"name": "向导-差额入现金", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id":      "scn_builtin_near_fire",
		"parameters":                wizardParams(total),
		"apply_unallocated_to_cash": true,
		"holdings": []map[string]any{
			{"instrument_id": instEquity, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": equityAmt, "sort_order": 1},
			{"instrument_id": instBond, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": bondAmt, "sort_order": 2},
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
		if row["instrument_id"].(string) == "system_cash_cny" {
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
	inst := importInstrumentCode(t, client, srv.URL, "510300")

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
				"apply_unallocated_to_cash": true,
				"holdings": []map[string]any{
					{"instrument_id": inst, "enabled": true, "weight_within_group": 0.4, "current_amount_minor": 700_000_00, "sort_order": 1},
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
				"holdings": []map[string]any{
					{"instrument_id": inst, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": 2_000_000_00, "sort_order": 1},
				},
			},
		},
		{
			name: "insufficient history",
			code: "instrument_insufficient_history",
			body: map[string]any{
				"name": "失败-历史不足", "valuation_date": "2026-06-09",
				"selected_scenario_id":      "scn_builtin_near_fire",
				"parameters":                wizardParams(1_000_000_00),
				"apply_unallocated_to_cash": true,
				"holdings": []map[string]any{
					{"instrument_id": inst, "enabled": true, "weight_within_group": 1.0, "current_amount_minor": 700_000_00, "sort_order": 1},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.code == "instrument_insufficient_history" {
				if _, err := db.ExecContext(context.Background(), `DELETE FROM market_data_points WHERE instrument_id=?`, inst); err != nil {
					t.Fatal(err)
				}
			}

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
