package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
)

// SimulationRunner is a focused persistence adapter used by deterministic
// engine integration tests. Production task execution uses ProcessorSet.
type SimulationRunner struct {
	db   *sql.DB
	sims *repository.SimulationRepo
}

func NewSimulationRunner(db *sql.DB, sims *repository.SimulationRepo) *SimulationRunner {
	return &SimulationRunner{db: db, sims: sims}
}

//nolint:wrapcheck // Repository writes share a transaction and preserve the original rollback cause.
func (r *SimulationRunner) RunSimulation(
	ctx context.Context, _, runID string, snapshot *simulation.InputSnapshot,
	cancelCheck func() bool, progress func(done, total int, phase string),
) error {
	result := simulation.Run(snapshot, simulation.RunOptions{
		Runs: snapshot.Parameters.SimulationRuns, Progress: progress, CancelCheck: cancelCheck,
	})
	if cancelCheck != nil && cancelCheck() {
		return context.Canceled
	}
	summary, err := json.Marshal(result.Summary)
	if err != nil {
		return fmt.Errorf("marshal simulation summary: %w", err)
	}
	representative := make(map[int]string, len(result.Representative))
	for label, pathNo := range result.Representative {
		representative[pathNo] = label
	}
	paths := make([]repository.PathIndexRow, 0, len(result.Paths))
	for _, path := range result.Paths {
		paths = append(paths, repository.PathIndexRow{
			RunID: runID, PathNo: path.PathNo, PathSeed: path.PathSeed, Succeeded: path.Succeeded,
			FailureMonth: path.FailureMonth, TerminalWealthMinor: path.TerminalWealthMinor,
			MaxDrawdown: path.MaxDrawdown, RepresentativePercentile: representative[path.PathNo],
		})
	}
	return fdb.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		if err := r.sims.Complete(ctx, tx, runID, result.SuccessCount, result.FailureCount, summary); err != nil {
			return err
		}
		if err := r.sims.ReplacePathIndex(ctx, tx, runID, paths); err != nil {
			return err
		}
		if err := r.sims.ReplaceQuantileSeries(ctx, tx, runID, quantileRows(runID, result.QuantileSeries)); err != nil {
			return err
		}
		return r.sims.ReplaceRealQuantileSeries(ctx, tx, runID, quantileRows(runID, result.RealQuantileSeries))
	})
}
