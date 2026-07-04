//go:build integration

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"

	fdb "github.com/fireman/fireman/internal/db"
)

func setupFullStackIntegration(t *testing.T) (*httptest.Server, *sql.DB, *http.Client, string) {
	t.Helper()
	db, dbPath := testutil.OpenTestDBPath(t)
	services := NewServices(db, dbPath, nil)
	runner := jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db))
	analysisRunner := jobs.NewAnalysisRunner(repository.NewAnalysisRepo(db))
	worker := jobs.NewWorker(
		db, repository.NewJobRepo(db), repository.NewSimulationRepo(db),
		runner, analysisRunner, services.EventHub, nil, nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go worker.Start(ctx, 1)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, DBPath: dbPath, Services: services}))
	t.Cleanup(srv.Close)
	return srv, db, srv.Client(), dbPath
}

func TestFullPlanWorkflowIntegration(t *testing.T) {
	srv, db, client, _ := setupFullStackIntegration(t)
	planID := seedSimulationReadyPlan(t, db)

	for _, path := range []string{
		"/api/v1/plans/" + planID + "/targets",
		"/api/v1/plans/" + planID + "/rebalance",
		"/api/v1/plans/" + planID + "/dashboard",
		"/api/v1/plans/" + planID + "/export/json",
		"/api/v1/plans/" + planID + "/export/targets.csv",
		"/api/v1/plans/" + planID + "/export/rebalance.csv",
	} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, readBody(t, resp))
		}
		_ = resp.Body.Close()
	}

	scnResp, err := client.Get(srv.URL + "/api/v1/allocation-scenarios")
	if err != nil {
		t.Fatal(err)
	}
	if scnResp.StatusCode != http.StatusOK {
		t.Fatalf("scenarios status=%d", scnResp.StatusCode)
	}
	_ = scnResp.Body.Close()
}

func TestSimulationJobPathAndStaleIntegration(t *testing.T) {
	srv, db, client, _ := setupFullStackIntegration(t)
	planID := seedSimulationReadyPlan(t, db)

	body, _ := json.Marshal(map[string]any{"runs": 1000, "seed": "42"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/simulations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create simulation status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	env := decodeEnvelope(t, readBody(t, resp))
	runID := env["data"].(map[string]any)["run_id"].(string)
	jobID := env["data"].(map[string]any)["job_id"].(string)
	waitJobSucceeded(t, srv, jobID)

	runResp, err := client.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	runEnv := decodeEnvelope(t, readBody(t, runResp))
	run := runEnv["data"].(map[string]any)
	if run["result_stale"].(bool) {
		t.Fatal("fresh simulation should not be stale")
	}

	pathResp, err := client.Get(srv.URL + "/api/v1/simulations/" + runID + "/paths/0")
	if err != nil {
		t.Fatal(err)
	}
	if pathResp.StatusCode != http.StatusOK {
		t.Fatalf("path detail status=%d body=%s", pathResp.StatusCode, readBody(t, pathResp))
	}
	_ = pathResp.Body.Close()

	paramsResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/parameters")
	if err != nil {
		t.Fatal(err)
	}
	paramsEnv := decodeEnvelope(t, readBody(t, paramsResp))
	params := paramsEnv["data"].(map[string]any)["parameters"].(map[string]any)
	plan, err := repository.NewPlanRepo(db).GetByID(context.Background(), planID)
	if err != nil {
		t.Fatal(err)
	}
	params["annual_spending_minor"] = float64(500_000_00)
	updateBody, _ := json.Marshal(map[string]any{
		"config_version": plan.ConfigVersion,
		"parameters":     params,
	})
	upReq, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/v1/plans/"+planID+"/parameters", bytes.NewReader(updateBody))
	upReq.Header.Set("Content-Type", "application/json")
	upResp, err := client.Do(upReq)
	if err != nil {
		t.Fatal(err)
	}
	if upResp.StatusCode != http.StatusOK {
		t.Fatalf("update parameters status=%d body=%s", upResp.StatusCode, readBody(t, upResp))
	}
	_ = upResp.Body.Close()

	staleResp, err := client.Get(srv.URL + "/api/v1/simulations/" + runID)
	if err != nil {
		t.Fatal(err)
	}
	staleEnv := decodeEnvelope(t, readBody(t, staleResp))
	if !staleEnv["data"].(map[string]any)["result_stale"].(bool) {
		t.Fatal("simulation should be stale after parameter change")
	}

	dashResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	dashEnv := decodeEnvelope(t, readBody(t, dashResp))
	sim := dashEnv["data"].(map[string]any)["latest_simulation"].(map[string]any)
	if !sim["result_stale"].(bool) {
		t.Fatal("dashboard simulation should be stale")
	}
}

func TestBackupDownloadAndValidateIntegration(t *testing.T) {
	srv, db, client, _ := setupFullStackIntegration(t)
	planID := seedSimulationReadyPlan(t, db)

	backupResp, err := client.Get(srv.URL + "/api/v1/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	if backupResp.StatusCode != http.StatusOK {
		t.Fatalf("backup status=%d body=%s", backupResp.StatusCode, readBody(t, backupResp))
	}
	backupData, err := io.ReadAll(backupResp.Body)
	_ = backupResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(backupData) == 0 {
		t.Fatal("empty backup")
	}

	backupPath := filepath.Join(t.TempDir(), "fireman-backup.db")
	if err := os.WriteFile(backupPath, backupData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fdb.ValidateDatabaseFile(context.Background(), backupPath); err != nil {
		t.Fatalf("validate backup: %v", err)
	}

	restored, err := fdb.Open(context.Background(), backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var name string
	if err := restored.QueryRowContext(context.Background(),
		`SELECT name FROM plans WHERE id=?`, planID).Scan(&name); err != nil {
		t.Fatalf("plan in backup: %v", err)
	}
	if name == "" {
		t.Fatal("expected plan name in backup")
	}
}

func TestMigrationIdempotentIntegration(t *testing.T) {
	db, dbPath := testutil.OpenTestDBPath(t)
	if err := fdb.Migrate(context.Background(), db, dbPath, nil); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("expected migration records")
	}
}

func TestStressSensitivityChainIntegration(t *testing.T) {
	srv, db, client, _ := setupFullStackIntegration(t)
	planID := seedSimulationReadyPlan(t, db)

	// Stress / sensitivity attach to an existing Monte Carlo run.
	runID := createSimulationAndWait(t, srv, planID, "11")

	stressBody, _ := json.Marshal(map[string]any{"simulation_run_id": runID})
	stressReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/stress-tests",
		bytes.NewReader(stressBody))
	stressReq.Header.Set("Content-Type", "application/json")
	stressResp, err := client.Do(stressReq)
	if err != nil {
		t.Fatal(err)
	}
	if stressResp.StatusCode != http.StatusOK {
		t.Fatalf("stress create status=%d body=%s", stressResp.StatusCode, readBody(t, stressResp))
	}
	stressEnv := decodeEnvelope(t, readBody(t, stressResp))
	stressJobID := stressEnv["data"].(map[string]any)["job_id"].(string)
	waitJobSucceeded(t, srv, stressJobID)

	sensBody, _ := json.Marshal(map[string]any{"simulation_run_id": runID})
	sensReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/plans/"+planID+"/sensitivity-tests",
		bytes.NewReader(sensBody))
	sensReq.Header.Set("Content-Type", "application/json")
	sensResp, err := client.Do(sensReq)
	if err != nil {
		t.Fatal(err)
	}
	if sensResp.StatusCode != http.StatusOK {
		t.Fatalf("sensitivity create status=%d", sensResp.StatusCode)
	}
	sensEnv := decodeEnvelope(t, readBody(t, sensResp))
	sensJobID := sensEnv["data"].(map[string]any)["job_id"].(string)
	waitJobSucceeded(t, srv, sensJobID)

	listResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/stress-tests")
	if err != nil {
		t.Fatal(err)
	}
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list stress status=%d", listResp.StatusCode)
	}
	_ = listResp.Body.Close()

	dashResp, err := client.Get(srv.URL + "/api/v1/plans/" + planID + "/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	dashEnv := decodeEnvelope(t, readBody(t, dashResp))
	data := dashEnv["data"].(map[string]any)
	if data["stress_test"] == nil || data["sensitivity_test"] == nil {
		t.Fatalf("dashboard missing analysis summaries: %+v", data)
	}
}
