package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/frontier"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/fireman/fireman/internal/testutil"
)

func TestSystemBackupDownload(t *testing.T) {
	db, dbPath := testutil.OpenTestDBPath(t)
	services := NewServices(db, dbPath, nil, nil, time.UTC)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, DBPath: dbPath, Services: services}))
	defer srv.Close()

	planBody := []byte(`{"name":"backup-test","valuation_date":"2024-01-01"}`)
	resp, err := http.Post(srv.URL+"/api/v1/plans", "application/json", bytes.NewReader(planBody))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create plan status=%d", resp.StatusCode)
	}

	backupResp, err := http.Get(srv.URL + "/api/v1/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	if backupResp.StatusCode != http.StatusOK {
		t.Fatalf("backup status=%d body=%s", backupResp.StatusCode, readBody(t, backupResp))
	}
	backupData, err := io.ReadAll(backupResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(backupData) == 0 {
		t.Fatal("empty backup")
	}

	badReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/system/restore", bytes.NewReader([]byte("not-sqlite")))
	badReq.Header.Set("Content-Type", "application/octet-stream")
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = badResp.Body.Close() }()
	if badResp.StatusCode == http.StatusOK {
		t.Fatal("expected invalid backup to fail")
	}
}

func TestSystemBackupPreservesFireFrontierRunTaskAndApplication(t *testing.T) {
	db, dbPath := testutil.OpenTestDBPath(t)
	services := NewServices(db, dbPath, nil, nil, time.UTC)
	plan := createTestPlan(t, db)
	ctx := context.Background()
	completedAt := int64(1_784_000_000_123)
	result := json.RawMessage(`{"algorithm_version":"fire_frontier_v1","sentinel":[1,2,3]}`)
	input := `{"source_snapshot":{"engine_version":"` + simulation.EngineVersion +
		`","assets":[{"asset_key":"equity","initial_minor":123456}]},"config":{"evaluation_runs":1000}}`
	application := repository.FireFrontierApplication{
		ID: "ffa_backup", FrontierRunID: "ffr_backup", PointID: "fpt_backup", PlanID: plan.ID,
		BeforeConfigVersion: 7, AfterConfigVersion: 8, PreviewHash: "sha256:preview",
		BeforeJSON: `{"retirement_age":55,"annual_spending_minor":40000000}`,
		AfterJSON:  `{"retirement_age":57,"annual_spending_minor":45000000}`,
		AppliedAt:  completedAt + 1,
	}
	err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		if err := services.TaskCoordinator.CreateTx(ctx, tx, &repository.WorkerTask{
			ID: "task_frontier_backup", WorkerType: repository.WorkerTypeGo,
			Type: repository.WorkerTaskTypeFireFrontier, Status: repository.WorkerTaskStatusComplete,
			ScopeType: "plan", ScopeID: plan.ID, InputHash: "sha256:input",
			PayloadJSON: `{"run_id":"ffr_backup"}`, ProgressCurrent: 9, ProgressTotal: 9,
			Phase: "complete", ResultKey: "fire_frontier_run:ffr_backup",
		}); err != nil {
			return err
		}
		if err := repository.NewFireFrontierRepo(db).CreateTx(ctx, tx, &repository.FireFrontierRun{
			ID: "ffr_backup", TaskID: "task_frontier_backup", PlanID: plan.ID,
			SourceSimulationRunID: "sim_pruned", InputHash: "sha256:input",
			AlgorithmVersion:    frontier.AlgorithmVersion,
			FrontierType:        frontier.TypeRetirementAgeMaxSpending,
			SourceEngineVersion: simulation.EngineVersion, SourceConfigHash: "sha256:config",
			SourceMarketHash: "sha256:market", EvaluationRuns: 1000,
			ConfigJSON: `{"evaluation_runs":1000}`, InputSnapshotJSON: input,
			ResultJSON: result, CompletedAt: &completedAt,
		}); err != nil {
			return err
		}
		return repository.NewFireFrontierRepo(db).CreateApplicationTx(ctx, tx, application)
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewRouter(ctx, Deps{DB: db, DBPath: dbPath, Services: services}))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/api/v1/system/backup")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("backup status=%d err=%v body=%s", resp.StatusCode, err, backup)
	}
	restorePath := filepath.Join(t.TempDir(), "frontier-restore.db")
	if err := os.WriteFile(restorePath, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fdb.ValidateDatabaseFile(ctx, restorePath); err != nil {
		t.Fatalf("validate restored backup: %v", err)
	}
	restored, err := fdb.Open(ctx, restorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()

	task, err := repository.NewWorkerTaskRepo(restored).GetByID(ctx, "task_frontier_backup")
	if err != nil || task.Status != repository.WorkerTaskStatusComplete || task.Phase != "complete" {
		t.Fatalf("restored task=%#v err=%v", task, err)
	}
	run, err := repository.NewFireFrontierRepo(restored).GetByID(ctx, "ffr_backup")
	if err != nil || run.InputSnapshotJSON != input || string(run.ResultJSON) != string(result) ||
		run.CompletedAt == nil || *run.CompletedAt != completedAt {
		t.Fatalf("restored run=%#v err=%v", run, err)
	}
	restoredApplication, err := repository.NewFireFrontierRepo(restored).GetApplication(ctx, "ffr_backup")
	if err != nil || !reflect.DeepEqual(restoredApplication, application) {
		t.Fatalf("restored application=%#v want=%#v err=%v", restoredApplication, application, err)
	}
	wantHash := sha256.Sum256(result)
	gotHash := sha256.Sum256(run.ResultJSON)
	if hex.EncodeToString(gotHash[:]) != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("result hash changed across restore: got=%x want=%x", gotHash, wantHash)
	}
}

func TestPlanExportEndpoints(t *testing.T) {
	db := testutil.OpenTestDB(t)
	planID := seedSimulationReadyPlan(t, db)
	srv := httptest.NewServer(NewRouter(context.Background(), Deps{DB: db, Services: buildServices(db)}))
	defer srv.Close()

	for _, path := range []string{
		"/api/v1/plans/" + planID + "/export/json",
		"/api/v1/plans/" + planID + "/export/targets.csv",
		"/api/v1/plans/" + planID + "/export/rebalance.csv",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, readBody(t, resp))
		}
	}
}
