//go:build integration

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func setupHKIntegration(t *testing.T) (*httptest.Server, *sql.DB, *http.Client) {
	t.Helper()
	provider := mockHKProviderServer(t)
	t.Cleanup(provider.Close)
	db := testutil.OpenTestDB(t)
	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: buildServices(db, provider.URL)}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client()
}

func TestHKInstrumentImportPreviewAndImportIntegration(t *testing.T) {
	srv, db, client := setupHKIntegration(t)
	_ = db

	payload, _ := json.Marshal(map[string]any{
		"market": "HK", "instrument_type": "hk_stock", "code": "700",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import/preview", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	previewEnv := decodeEnvelope(t, readBody(t, resp))
	preview := previewEnv["data"].(map[string]any)
	instPreview := preview["instrument"].(map[string]any)
	if instPreview["currency"] != "HKD" {
		t.Fatalf("preview currency=%v want HKD", instPreview["currency"])
	}

	resp, err = client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	inst := env["data"].(map[string]any)
	instID := inst["id"].(string)
	if inst["currency"] != "HKD" {
		t.Fatalf("imported currency=%v want HKD", inst["currency"])
	}
	if inst["region"] != "foreign" {
		t.Fatalf("imported region=%v want foreign", inst["region"])
	}

	var pointCount int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM market_data_points WHERE instrument_id=?`, instID).Scan(&pointCount); err != nil {
		t.Fatal(err)
	}
	if pointCount == 0 {
		t.Fatal("expected full history saved for HK import")
	}

	resp, err = client.Get(srv.URL + "/api/v1/instruments/" + instID + "/annual-returns")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("annual returns status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
}

func TestHKSimulationSnapshotWithHKDCNYIntegration(t *testing.T) {
	srv, db, client := setupHKIntegration(t)
	seedHKDCNYMarketData(t, db)

	payload, _ := json.Marshal(map[string]any{
		"market": "HK", "instrument_type": "hk_stock", "code": "700",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	instID := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)["id"].(string)

	plan := createPlanWithValuationDate(t, db, "2026-06-09")
	version := setForeignEquityAllocation(t, client, srv.URL, plan.ID, plan.ConfigVersion)
	holdingID, _ := addForeignEquityHolding(t, db, client, srv.URL, plan.ID, instID, version)

	if _, err := db.ExecContext(context.Background(), `
		UPDATE plan_parameters SET total_assets_minor=? WHERE plan_id=?`, 10_000_000_00, plan.ID); err != nil {
		t.Fatal(err)
	}

	resp, err = client.Get(srv.URL + "/api/v1/plans/" + plan.ID + "/holdings/" + holdingID + "/simulation-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holding snapshot status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}

	services := buildServices(db, "")
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	worker := jobs.NewWorker(db, repository.NewJobRepo(db), repository.NewSimulationRepo(db), runner, jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db)), services.EventHub, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "42"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+plan.ID+"/simulations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create simulation status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	data := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	jobID := data["job_id"].(string)
	runID := data["run_id"].(string)
	waitJobSucceeded(t, srv, jobID)

	run, err := repository.NewSimulationRepo(db).GetByID(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Assets []struct {
			Currency        string  `json:"currency"`
			FXSnapshotID    string  `json:"fx_snapshot_id"`
			FXModeledReturn float64 `json:"fx_modeled_return"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snap); err != nil {
		t.Fatal(err)
	}
	foundHK := false
	for _, a := range snap.Assets {
		if a.Currency != "HKD" {
			continue
		}
		foundHK = true
		if a.FXSnapshotID == "" {
			t.Fatal("expected HKD asset to include HKDCNY fx_snapshot_id in frozen snapshot")
		}
		if a.FXModeledReturn == 0 {
			t.Fatal("expected non-zero fx_modeled_return for HKDCNY conversion")
		}
	}
	if !foundHK {
		t.Fatal("expected HKD asset in simulation input snapshot")
	}
}

func seedHKDCNYMarketData(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	points := buildTwentyYearFixturePoints()
	now := time.Now().UnixMilli()
	for _, p := range points {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO market_data_points (instrument_id, trade_date, value, point_type, source_name, fetched_at)
			VALUES ('system_fx_hkdcny', ?, ?, 'fx_rate', 'system_seed', ?)`,
			p.Date, p.Value*0.9, now); err != nil {
			t.Fatal(err)
		}
	}
}

func setForeignEquityAllocation(t *testing.T, client *http.Client, baseURL, planID string, version int) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"asset_class_targets": []map[string]any{
			{"asset_class": "equity", "weight": 1.0},
			{"asset_class": "bond", "weight": 0.0},
			{"asset_class": "cash", "weight": 0.0},
		},
		"region_targets": []map[string]any{
			{"asset_class": "equity", "region": "domestic", "weight_within_class": 0.0},
			{"asset_class": "equity", "region": "foreign", "weight_within_class": 1.0},
			{"asset_class": "bond", "region": "domestic", "weight_within_class": 1.0},
			{"asset_class": "bond", "region": "foreign", "weight_within_class": 0.0},
			{"asset_class": "cash", "region": "domestic", "weight_within_class": 1.0},
			{"asset_class": "cash", "region": "foreign", "weight_within_class": 0.0},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/v1/plans/"+planID+"/allocation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("allocation status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	_ = readBody(t, resp)
	return version + 1
}

func addForeignEquityHolding(t *testing.T, db *sql.DB, client *http.Client, baseURL, planID, instrumentID string, version int) (holdingID string, newVersion int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"config_version": version,
		"holdings": []map[string]any{
			{
				"instrument_id": instrumentID, "enabled": true,
				"weight_within_group": 1.0, "current_amount_minor": 10_000_000_00, "sort_order": 1,
			},
		},
	})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/v1/plans/"+planID+"/holdings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holdings status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	holdings := env["data"].(map[string]any)["holdings"].([]any)
	holding := holdings[0].(map[string]any)
	return holding["id"].(string), planConfigVersion(t, db, planID)
}
