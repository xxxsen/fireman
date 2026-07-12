package api

import (
	"database/sql"

	workerpkg "github.com/fireman/fireman/internal/worker"
)

func newTestTaskWorker(db *sql.DB, services Services) *workerpkg.Supervisor {
	return workerpkg.NewSupervisor(
		services.TaskCoordinator,
		workerpkg.NewProcessorSet(db, services.TaskCoordinator, services.Research, services.AutoUpdates),
		nil, nil,
	)
}
