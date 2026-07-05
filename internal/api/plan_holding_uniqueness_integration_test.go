//go:build integration

// One market asset (asset_key) may only be owned by a single
// asset_class/region within a plan. These tests bypass the frontend and
// verify the backend rejects duplicates on both write paths.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestPlanWizardDuplicateAssetKeyRejectedIntegration submits the same
// asset_key under equity and bond; the wizard must reject it with
// holding_duplicate and leave no residual plan.
func TestPlanWizardDuplicateAssetKeyRejectedIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	assetKey := seedAssetCode(t, db, "159007")

	const total = int64(10_000_000_00)
	body := map[string]any{
		"name": "重复资产向导", "base_currency": "CNY", "valuation_date": "2026-06-09",
		"selected_scenario_id": "scn_builtin_near_fire",
		"parameters":           wizardParams(total),
		"region_targets":       wizardRegionTargets(),
		"holdings": []map[string]any{
			{
				"asset_key": assetKey, "asset_class": "equity", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 7_000_000_00,
				"sort_order": 1,
			},
			{
				"asset_key": assetKey, "asset_class": "bond", "region": "domestic",
				"enabled": true, "weight_within_group": 1.0, "current_amount_minor": 3_000_000_00,
				"sort_order": 2,
			},
		},
	}

	plansBefore := countTable(t, db, "plans")

	resp, raw := postWizard(t, client, srv.URL, body)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected duplicate asset_key to be rejected, got 200 body=%s", string(raw))
	}
	assertErrorCode(t, raw, "holding_duplicate")
	errBody := decodeErrorBody(t, raw)
	msg, _ := errBody["message"].(string)
	if !strings.Contains(msg, "duplicate asset_key within the plan") {
		t.Fatalf("expected plan-level duplicate message, got %q", msg)
	}
	if countTable(t, db, "plans") != plansBefore {
		t.Fatal("rejected wizard request must not create a plan")
	}
}

// TestHoldingsUpdateDuplicateAssetKeyRejectedIntegration submits the same
// asset_key as domestic and foreign rows on the holdings update endpoint;
// asset_key alone is the uniqueness key, so this must fail.
func TestHoldingsUpdateDuplicateAssetKeyRejectedIntegration(t *testing.T) {
	srv, db, client := setupInstrumentIntegration(t)
	assetKey := seedFixtureAsset(t, db)

	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setEquityOnlyAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)

	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"asset_key": assetKey, "enabled": true,
				"asset_class": "equity", "region": "domestic",
				"weight_within_group": 0.5, "current_amount_minor": 5_000_000_00, "sort_order": 1,
			},
			{
				"asset_key": assetKey, "enabled": true,
				"asset_class": "equity", "region": "foreign",
				"weight_within_group": 0.5, "current_amount_minor": 5_000_000_00, "sort_order": 2,
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+plan.ID+"/holdings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw := readBody(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected duplicate asset_key to be rejected, got 200 body=%s", string(raw))
	}
	assertErrorCode(t, raw, "holding_duplicate")

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM plan_holdings WHERE plan_id = ?`, plan.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected holdings update must not persist rows, got %d", count)
	}
}
