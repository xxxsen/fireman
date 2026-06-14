package service

import (
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestComputeExecutionStatsDoneLineCountExcludesSkipped(t *testing.T) {
	stats := computeExecutionStats([]repository.RebalanceExecutionLine{
		{ExecutionStatus: ExecutionLineStatusDone},
		{ExecutionStatus: ExecutionLineStatusPartial},
		{ExecutionStatus: ExecutionLineStatusSkipped},
	}, nil)
	if stats.LineCount != 3 {
		t.Fatalf("line_count=%d want 3", stats.LineCount)
	}
	if stats.DoneLineCount != 1 {
		t.Fatalf("done_line_count=%d want 1", stats.DoneLineCount)
	}
	if stats.SkippedLineCount != 1 {
		t.Fatalf("skipped_line_count=%d want 1", stats.SkippedLineCount)
	}
}
