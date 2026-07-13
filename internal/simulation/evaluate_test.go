package simulation

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestEvaluateOutcomesMatchesRun(t *testing.T) {
	in := testInputSnapshot()
	in.Parameters.SimulationRuns = 64
	full := Run(in, RunOptions{Runs: 64})
	got, err := EvaluateOutcomes(in, RunOptions{Runs: 64})
	if err != nil {
		t.Fatal(err)
	}
	wantOutcomes := make([]bool, len(full.Paths))
	for i, path := range full.Paths {
		wantOutcomes[i] = path.Succeeded
	}
	if !reflect.DeepEqual(got.Outcomes, wantOutcomes) {
		t.Fatal("compact path outcomes differ from the formal run")
	}
	if got.SuccessCount != full.SuccessCount ||
		got.SuccessProbability != full.Summary.SuccessProbability ||
		got.SuccessWilsonLow != full.Summary.SuccessWilsonLow ||
		got.SuccessWilsonHigh != full.Summary.SuccessWilsonHigh ||
		got.TerminalP50Minor != full.Summary.TerminalQuantiles["p50"] ||
		got.MaxDrawdownP95 != full.Summary.MaxDrawdownQuantiles["p95"] {
		t.Fatalf("compact aggregates do not match formal run: %#v vs %#v", got, full.Summary)
	}
}

func TestEvaluateOutcomesCancellationDoesNotReturnPartialDenominator(t *testing.T) {
	in := testInputSnapshot()
	checks := 0
	got, err := EvaluateOutcomes(in, RunOptions{Runs: 20, CancelCheck: func() bool {
		checks++
		return checks > 5
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v want context.Canceled", err)
	}
	if got.Runs != 0 || len(got.Outcomes) != 0 {
		t.Fatalf("partial result must not be reported: %#v", got)
	}
}
