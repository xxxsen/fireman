package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

const (
	supersededByNewerAnalysisCode = "superseded_by_newer_analysis"
	supersededByNewerAnalysisMsg  = "superseded by a newer analysis run"
)

func supersedePriorAnalysis(
	ctx context.Context,
	tx *sql.Tx,
	coordinator *taskcore.Coordinator,
	analysis *repository.AnalysisRepo,
	runID, analysisType string,
) error {
	priorTaskIDs, err := analysis.TaskIDsBySimulationRunAndType(ctx, tx, runID, analysisType)
	if err != nil {
		return wrapRepo("list prior analysis tasks", err)
	}
	now := time.Now().UnixMilli()
	for _, taskID := range priorTaskIDs {
		if err := coordinator.RequestCancelTx(
			ctx, tx, taskID, supersededByNewerAnalysisCode, supersededByNewerAnalysisMsg, now,
		); err != nil {
			return wrapRepo("cancel prior analysis task", err)
		}
	}
	if err := analysis.DeleteBySimulationRunAndType(ctx, tx, runID, analysisType); err != nil {
		return wrapRepo("delete prior analysis", err)
	}
	return nil
}
