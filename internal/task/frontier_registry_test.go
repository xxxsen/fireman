package task

import (
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestFrontierRetryClassifierOnlyAcceptsTransientInfrastructure(t *testing.T) {
	definition, ok := DefaultRegistry().Lookup(repository.WorkerTypeGo, repository.WorkerTaskTypeFireFrontier)
	if !ok || definition.ResultPrefix != "fire_frontier_run:" || definition.MaxAttempts != 2 {
		t.Fatalf("frontier definition=%+v ok=%v", definition, ok)
	}
	for _, test := range []struct {
		err  error
		want bool
	}{
		{errors.New("database is locked"), true},
		{errors.New("temporary i/o failure"), true},
		{errors.New("frontier monotonicity violated"), false},
		{errors.New("invalid frozen input"), false},
		{NewError(ErrPayloadInvalid, "bad payload", nil), false},
	} {
		if got := definition.RetryClassifier(test.err); got != test.want {
			t.Errorf("error=%q retry=%v want %v", test.err, got, test.want)
		}
	}
}
