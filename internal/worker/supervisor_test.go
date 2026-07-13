package worker

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
	"github.com/fireman/fireman/internal/testutil"
)

func createSupervisorTask(t *testing.T, db *sql.DB, coordinator *taskcore.Coordinator, id string) {
	t.Helper()
	err := fdb.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		return coordinator.CreateTx(context.Background(), tx, &repository.WorkerTask{
			ID: id, WorkerType: repository.WorkerTypeGo, Type: repository.WorkerTaskTypeSimulation,
			Status: repository.WorkerTaskStatusPending, ScopeType: "test", ScopeID: id,
			DedupeKey: id, PayloadJSON: `{}`,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorTaskEventCancelsProcessorImmediately(t *testing.T) {
	db := testutil.OpenTestDB(t)
	coordinator := taskcore.NewCoordinator(db, repository.NewWorkerTaskRepo(db),
		taskcore.DefaultRegistry(), taskcore.NewEventHub())
	createSupervisorTask(t, db, coordinator, "task_supervisor_cancel")

	started := make(chan struct{})
	stopped := make(chan struct{})
	processors := &ProcessorSet{processors: map[string]func(context.Context, repository.WorkerTask, Attempt) error{
		repository.WorkerTaskTypeSimulation: func(ctx context.Context, _ repository.WorkerTask, _ Attempt) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		},
	}}
	supervisor := NewSupervisor(coordinator, processors,
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	supervisor.workerID = "go_worker:supervisor-test"
	done := make(chan bool, 1)
	go func() { done <- supervisor.tryExecute(context.Background(), "task_supervisor_cancel") }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("processor did not start")
	}
	if _, err := coordinator.RequestCancel(context.Background(), "task_supervisor_cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("processor did not observe cancellation event within one second")
	}
	select {
	case executed := <-done:
		if !executed {
			t.Fatal("claimed task reported as not executed")
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish canceled attempt")
	}
	item, err := coordinator.Get(context.Background(), "task_supervisor_cancel")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != repository.WorkerTaskStatusCanceled || item.AttemptCount != 1 {
		t.Fatalf("task after cancellation=%#v", item)
	}
}

func TestAttemptHeartbeatLeaseLossCancelsProcessorContext(t *testing.T) {
	db := testutil.OpenTestDB(t)
	coordinator := taskcore.NewCoordinator(db, repository.NewWorkerTaskRepo(db),
		taskcore.DefaultRegistry(), taskcore.NewEventHub())
	createSupervisorTask(t, db, coordinator, "task_supervisor_heartbeat")
	const workerID = "go_worker:heartbeat-test"
	const token = "heartbeat-test-token-0001"
	if _, err := coordinator.Claim(context.Background(), "task_supervisor_heartbeat", taskcore.ClaimRequest{
		WorkerType: repository.WorkerTypeGo, WorkerID: workerID, ClaimToken: token,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &attemptState{
		coordinator: coordinator, taskID: "task_supervisor_heartbeat", workerID: workerID,
		token: token, cancel: cancel,
	}
	if _, err := coordinator.RequestCancel(context.Background(), "task_supervisor_heartbeat"); err != nil {
		t.Fatal(err)
	}
	state.heartbeat(context.Background())
	if !state.leaseLost.Load() {
		t.Fatal("terminal task did not fence heartbeat ownership")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("lease loss did not cancel processor context")
	}
}
