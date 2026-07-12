package repository

import (
	"context"
	"database/sql"
	"testing"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/testutil"
)

func seedPlanRow(t *testing.T, db *sql.DB, planID string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO plans (id, name, valuation_date, created_at, updated_at)
		VALUES (?, ?, '2026-06-19', 0, 0)`, planID, planID); err != nil {
		t.Fatalf("seed plan %s: %v", planID, err)
	}
}

func seedRunRow(t *testing.T, db *sql.DB, sims *SimulationRepo, planID, runID string, createdAt int64) {
	t.Helper()
	jobID := "job_" + runID
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO jobs (id, plan_id, type, status, input_hash, created_at)
		VALUES (?, ?, 'simulation', 'succeeded', 'h', ?)`, jobID, planID, createdAt); err != nil {
		t.Fatalf("seed job for %s: %v", runID, err)
	}
	if err := sims.CreatePending(context.Background(), nil, SimulationRun{
		ID: runID, JobID: jobID, PlanID: planID, InputHash: "h",
		InputSnapshotJSON: "{}", MarketSnapshotHash: "m", EngineVersion: "v1",
		Runs: 1, Seed: 1, HorizonMonths: 12, CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("seed run %s: %v", runID, err)
	}
}

func seedAnalysisRow(t *testing.T, db *sql.DB, analysis *AnalysisRepo, planID, runID, jobSuffix, typ string) {
	t.Helper()
	jobID := "ajob_" + jobSuffix
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO jobs (id, plan_id, type, status, input_hash, created_at)
		VALUES (?, ?, ?, 'succeeded', 'h', 0)`, jobID, planID, typ); err != nil {
		t.Fatalf("seed analysis job %s: %v", jobID, err)
	}
	if err := analysis.CreatePending(context.Background(), nil, AnalysisResult{
		JobID: jobID, PlanID: planID, Type: typ, InputHash: "h",
		SimulationRunID: runID, ResultJSON: "{}",
	}); err != nil {
		t.Fatalf("seed analysis %s: %v", jobID, err)
	}
}

func TestSimulationRepoPruneByPlanKeepsNewest(t *testing.T) {
	db := testutil.OpenTestDB(t)
	sims := NewSimulationRepo(db)
	analysis := NewAnalysisRepo(db)
	ctx := context.Background()
	planID := "plan_prune"
	seedPlanRow(t, db, planID)

	// 8 runs, run_8 newest. Attach an analysis result to the oldest run.
	for i := 1; i <= 8; i++ {
		runID := "simrun_" + string(rune('0'+i))
		seedRunRow(t, db, sims, planID, runID, int64(i*1000))
	}
	seedAnalysisRow(t, db, analysis, planID, "simrun_1", "old_stress", AnalysisTypeStress)

	var pruned []string
	if err := fdb.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		pruned, err = sims.PruneByPlan(ctx, tx, planID, 7)
		if err != nil {
			return err
		}
		return analysis.DeleteBySimulationRunIDs(ctx, tx, pruned)
	}); err != nil {
		t.Fatal(err)
	}

	if len(pruned) != 1 || pruned[0] != "simrun_1" {
		t.Fatalf("expected to prune simrun_1, got %v", pruned)
	}
	remaining, err := sims.ListByPlan(ctx, planID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 7 {
		t.Fatalf("expected 7 runs after prune, got %d", len(remaining))
	}
	for _, r := range remaining {
		if r.ID == "simrun_1" {
			t.Fatalf("simrun_1 should have been pruned")
		}
	}
	// Analysis attached to the pruned run is gone.
	gone, err := analysis.ListBySimulationRun(ctx, "simrun_1", AnalysisTypeStress, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Fatalf("expected analysis of pruned run removed, got %d", len(gone))
	}
}

func TestSimulationRepoReadsJoinedJobTerminalState(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewSimulationRepo(db)
	ctx := context.Background()
	planID := "plan_run_states"
	seedPlanRow(t, db, planID)
	statuses := []string{
		JobStatusQueued, JobStatusRunning, JobStatusSucceeded, JobStatusFailed, JobStatusCanceled,
	}
	for i, status := range statuses {
		jobID := "job_state_" + status
		if _, err := db.ExecContext(ctx, `
			INSERT INTO jobs (id, plan_id, type, status, input_hash, error_code, error_message, created_at)
			VALUES (?, ?, 'simulation', ?, 'h', ?, ?, ?)`,
			jobID, planID, status, "code_"+status, "message_"+status, i); err != nil {
			t.Fatal(err)
		}
		if err := repo.CreatePending(ctx, nil, SimulationRun{
			ID: "run_state_" + status, JobID: jobID, PlanID: planID, InputHash: "h",
			InputSnapshotJSON: "{}", MarketSnapshotHash: "m", EngineVersion: "v1",
			Runs: 1, Seed: 1, HorizonMonths: 12, CreatedAt: int64(i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := repo.ListByPlan(ctx, planID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != len(statuses) {
		t.Fatalf("runs = %d, want %d", len(runs), len(statuses))
	}
	for _, run := range runs {
		wantStatus := run.ID[len("run_state_"):]
		if run.JobStatus != wantStatus || run.JobErrorCode != "code_"+wantStatus ||
			run.JobErrorMessage != "message_"+wantStatus {
			t.Fatalf("joined state = %+v", run)
		}
	}
}

func TestAnalysisRepoListAndDeleteBySimulationRun(t *testing.T) {
	db := testutil.OpenTestDB(t)
	sims := NewSimulationRepo(db)
	analysis := NewAnalysisRepo(db)
	ctx := context.Background()
	planID := "plan_analysis"
	seedPlanRow(t, db, planID)
	seedRunRow(t, db, sims, planID, "simrun_a", 1000)
	seedRunRow(t, db, sims, planID, "simrun_b", 2000)

	seedAnalysisRow(t, db, analysis, planID, "simrun_a", "a_stress", AnalysisTypeStress)
	seedAnalysisRow(t, db, analysis, planID, "simrun_b", "b_stress", AnalysisTypeStress)
	seedAnalysisRow(t, db, analysis, planID, "simrun_b", "b_sens", AnalysisTypeSensitivity)

	// Only run b's stress is returned when filtering by run b + stress.
	got, err := analysis.ListBySimulationRun(ctx, "simrun_b", AnalysisTypeStress, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].JobID != "ajob_b_stress" {
		t.Fatalf("expected only run b stress, got %+v", got)
	}

	// Deleting run b's stress leaves run a's stress and run b's sensitivity intact.
	if err := analysis.DeleteBySimulationRunAndType(ctx, nil, "simrun_b", AnalysisTypeStress); err != nil {
		t.Fatal(err)
	}
	if got, _ := analysis.ListBySimulationRun(ctx, "simrun_b", AnalysisTypeStress, 10); len(got) != 0 {
		t.Fatalf("run b stress should be deleted")
	}
	if got, _ := analysis.ListBySimulationRun(ctx, "simrun_a", AnalysisTypeStress, 10); len(got) != 1 {
		t.Fatalf("run a stress should remain")
	}
	if got, _ := analysis.ListBySimulationRun(ctx, "simrun_b", AnalysisTypeSensitivity, 10); len(got) != 1 {
		t.Fatalf("run b sensitivity should remain")
	}
}
