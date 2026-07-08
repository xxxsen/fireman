package service

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
)

// Page is the shared pagination envelope for every /api/v1/admin/* listing.
type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Admin observation thresholds. These are display semantics only — they never
// feed business decisions — and live here so the API and any future consumer
// share one definition.
const (
	// adminStaleHeartbeat matches the sidecar janitor heartbeat timeout: a
	// running task whose heartbeat is older than this is likely stuck.
	adminStaleHeartbeat = 60 * time.Second
	// adminStaleSync matches the asset page reminder threshold for directory
	// scopes and history dimensions.
	adminStaleSync = 7 * 24 * time.Hour
	// adminStatsWindow is the lookback for the overview 24h counters.
	adminStatsWindow = 24 * time.Hour
)

// AdminService serves the read-only observation API: task/job listings,
// callback records, data versions and the overview aggregation. It only
// projects existing state — no business rules, no second copy of any state.
type AdminService struct {
	tasks        *repository.WorkerTaskRepo
	jobs         *repository.JobRepo
	records      *repository.PostProcessRecordRepo
	assets       *repository.MarketAssetRepo
	marketAssets *MarketAssetService
	resources    *resourcedb.DB
	dbPath       string
	now          func() time.Time
}

func NewAdminService(
	tasks *repository.WorkerTaskRepo,
	jobs *repository.JobRepo,
	records *repository.PostProcessRecordRepo,
	assets *repository.MarketAssetRepo,
	marketAssets *MarketAssetService,
	resources *resourcedb.DB,
	dbPath string,
) *AdminService {
	return &AdminService{
		tasks: tasks, jobs: jobs, records: records, assets: assets,
		marketAssets: marketAssets, resources: resources, dbPath: dbPath,
		now: time.Now,
	}
}

// --- overview ---

// AdminOverview is the single-request payload behind GET /admin/overview.
type AdminOverview struct {
	WorkerTasks AdminWorkerTaskStats `json:"worker_tasks"`
	Jobs        AdminJobStats        `json:"jobs"`
	Callbacks   AdminCallbackStats   `json:"callbacks"`
	SyncHealth  AdminSyncHealth      `json:"sync_health"`
	Storage     AdminStorageStats    `json:"storage"`
}

type AdminWorkerTaskStats struct {
	Active           int            `json:"active"`
	ByStatus         map[string]int `json:"by_status"`
	FailedLast24h    int            `json:"failed_last_24h"`
	CompletedLast24h int            `json:"completed_last_24h"`
	StaleRunning     int            `json:"stale_running"`
}

type AdminJobStats struct {
	Queued           int `json:"queued"`
	Running          int `json:"running"`
	FailedLast24h    int `json:"failed_last_24h"`
	SucceededLast24h int `json:"succeeded_last_24h"`
}

type AdminCallbackStats struct {
	TotalLast24h  int `json:"total_last_24h"`
	FailedLast24h int `json:"failed_last_24h"`
}

type AdminSyncHealth struct {
	DirectoryScopes   []AdminDirectoryScopeHealth      `json:"directory_scopes"`
	FXPairs           []AdminFXPairHealth              `json:"fx_pairs"`
	HistoryDimensions repository.HistoryStateAggregate `json:"history_dimensions"`
}

// AdminDirectoryScopeHealth is one scope's aggregated directory sync health
// plus the per-unit facts, so the admin never hides which unit failed.
type AdminDirectoryScopeHealth struct {
	Scope string `json:"scope"`
	Label string `json:"label"`
	// Status: running | complete | partial | failed | never (scope aggregate).
	Status string `json:"status"`
	// LastSuccessAt is the scope's oldest full-success time; null while any
	// unit has never succeeded.
	LastSuccessAt    *int64                     `json:"last_success_at"`
	ActiveTaskStatus string                     `json:"active_task_status"`
	Stale            bool                       `json:"stale"`
	Units            []AdminDirectoryUnitHealth `json:"units"`
}

// AdminDirectoryUnitHealth is one directory sync unit's health facts.
type AdminDirectoryUnitHealth struct {
	SyncKey          string `json:"sync_key"`
	Label            string `json:"label"`
	LastSuccessAt    *int64 `json:"last_success_at"`
	ActiveTaskStatus string `json:"active_task_status"`
	LatestTaskFailed bool   `json:"latest_task_failed"`
	Stale            bool   `json:"stale"`
}

type AdminFXPairHealth struct {
	Pair          string `json:"pair"`
	LastSuccessAt *int64 `json:"last_success_at"`
}

type AdminStorageStats struct {
	MainDBBytes     int64 `json:"main_db_bytes"`
	ResourceDBBytes int64 `json:"resource_db_bytes"`
	ResourceCount   int   `json:"resource_count"`
}

// Overview aggregates every overview block in one call so the page renders
// without a request waterfall. All counts run as SQL aggregates.
func (s *AdminService) Overview(ctx context.Context) (AdminOverview, error) {
	now := s.now()
	since := now.Add(-adminStatsWindow).UnixMilli()

	var out AdminOverview

	byStatus, err := s.tasks.CountByStatus(ctx)
	if err != nil {
		return AdminOverview{}, wrapRepo("count worker tasks", err)
	}
	out.WorkerTasks.ByStatus = map[string]int{
		repository.WorkerTaskStatusPending:     byStatus[repository.WorkerTaskStatusPending],
		repository.WorkerTaskStatusRunning:     byStatus[repository.WorkerTaskStatusRunning],
		repository.WorkerTaskStatusPreComplete: byStatus[repository.WorkerTaskStatusPreComplete],
	}
	for _, n := range out.WorkerTasks.ByStatus {
		out.WorkerTasks.Active += n
	}
	if out.WorkerTasks.FailedLast24h, err = s.tasks.CountFinishedSince(
		ctx, repository.WorkerTaskStatusFailed, since); err != nil {
		return AdminOverview{}, wrapRepo("count failed worker tasks", err)
	}
	if out.WorkerTasks.CompletedLast24h, err = s.tasks.CountFinishedSince(
		ctx, repository.WorkerTaskStatusComplete, since); err != nil {
		return AdminOverview{}, wrapRepo("count completed worker tasks", err)
	}
	if out.WorkerTasks.StaleRunning, err = s.tasks.CountStaleRunning(
		ctx, now.Add(-adminStaleHeartbeat).UnixMilli()); err != nil {
		return AdminOverview{}, wrapRepo("count stale running worker tasks", err)
	}

	jobsByStatus, err := s.jobs.CountByStatus(ctx)
	if err != nil {
		return AdminOverview{}, wrapRepo("count jobs", err)
	}
	out.Jobs.Queued = jobsByStatus[repository.JobStatusQueued]
	out.Jobs.Running = jobsByStatus[repository.JobStatusRunning]
	if out.Jobs.FailedLast24h, err = s.jobs.CountFinishedSince(
		ctx, repository.JobStatusFailed, since); err != nil {
		return AdminOverview{}, wrapRepo("count failed jobs", err)
	}
	if out.Jobs.SucceededLast24h, err = s.jobs.CountFinishedSince(
		ctx, repository.JobStatusSucceeded, since); err != nil {
		return AdminOverview{}, wrapRepo("count succeeded jobs", err)
	}

	if out.Callbacks.TotalLast24h, out.Callbacks.FailedLast24h, err = s.records.CountSince(
		ctx, since); err != nil {
		return AdminOverview{}, wrapRepo("count post process records", err)
	}

	if out.SyncHealth, err = s.syncHealth(ctx, now); err != nil {
		return AdminOverview{}, err
	}

	out.Storage = s.storageStats(ctx)
	return out, nil
}

func (s *AdminService) syncHealth(ctx context.Context, now time.Time) (AdminSyncHealth, error) {
	var health AdminSyncHealth
	staleBefore := now.Add(-adminStaleSync).UnixMilli()

	directoryScopes, err := s.directoryScopeHealth(ctx, staleBefore)
	if err != nil {
		return AdminSyncHealth{}, err
	}
	health.DirectoryScopes = directoryScopes

	fxPairs, err := s.fxPairHealth(ctx)
	if err != nil {
		return AdminSyncHealth{}, err
	}
	health.FXPairs = fxPairs

	agg, err := s.assets.AggregateHistoryStates(ctx, staleBefore)
	if err != nil {
		return AdminSyncHealth{}, wrapRepo("aggregate history states", err)
	}
	health.HistoryDimensions = agg
	return health, nil
}

func (s *AdminService) directoryScopeHealth(
	ctx context.Context, staleBefore int64,
) ([]AdminDirectoryScopeHealth, error) {
	out := make([]AdminDirectoryScopeHealth, 0, len(DirectoryScopes))
	for _, scope := range DirectoryScopes {
		view, err := s.marketAssets.BuildScopeSyncView(ctx, scope)
		if err != nil {
			return nil, err
		}
		item := AdminDirectoryScopeHealth{
			Scope:         scope,
			Label:         view.Label,
			Status:        view.Status,
			LastSuccessAt: view.LastSuccessAt,
		}
		for _, u := range view.Units {
			unit := AdminDirectoryUnitHealth{
				SyncKey:       u.SyncKey,
				Label:         u.Label,
				LastSuccessAt: u.LastSuccessAt,
			}
			if u.Task != nil {
				if repository.IsActiveWorkerTaskStatus(u.Task.Status) {
					unit.ActiveTaskStatus = u.Task.Status
					if item.ActiveTaskStatus == "" {
						item.ActiveTaskStatus = u.Task.Status
					}
				}
				unit.LatestTaskFailed = u.Task.Status == repository.WorkerTaskStatusFailed
			}
			if u.LastSuccessAt != nil && *u.LastSuccessAt < staleBefore {
				unit.Stale = true
				item.Stale = true
			}
			item.Units = append(item.Units, unit)
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *AdminService) fxPairHealth(ctx context.Context) ([]AdminFXPairHealth, error) {
	fxVersions, _, err := s.assets.ListDataVersions(ctx, "fx_rate|", len(FXPairs)+8, 0)
	if err != nil {
		return nil, wrapRepo("list fx data versions", err)
	}
	fxByPair := make(map[string]int64, len(fxVersions))
	for _, v := range fxVersions {
		fxByPair[v.VersionKey] = v.UpdatedAt
	}
	out := make([]AdminFXPairHealth, 0, len(FXPairs))
	for _, pair := range FXPairs {
		item := AdminFXPairHealth{Pair: pair}
		if at, ok := fxByPair["fx_rate|"+pair]; ok {
			v := at
			item.LastSuccessAt = &v
		}
		out = append(out, item)
	}
	return out, nil
}

// storageStats never fails the overview: sizes degrade to zero when a file or
// the resource database is unavailable.
func (s *AdminService) storageStats(ctx context.Context) AdminStorageStats {
	var st AdminStorageStats
	if s.dbPath != "" {
		if info, err := os.Stat(s.dbPath); err == nil {
			st.MainDBBytes = info.Size()
		}
	}
	if s.resources != nil {
		if rs, err := s.resources.StatsSummary(ctx); err == nil {
			st.ResourceDBBytes = rs.TotalBytes
			st.ResourceCount = rs.Count
		}
	}
	return st
}

// --- worker tasks ---

// AdminWorkerTaskListParams filters GET /admin/worker-tasks.
type AdminWorkerTaskListParams struct {
	Type   string
	Status string
	Query  string
	Limit  int
	Offset int
}

// AdminWorkerTaskItem is the slim listing projection: payload_json and
// result_data never travel in list responses.
type AdminWorkerTaskItem struct {
	ID                  string `json:"id"`
	Type                string `json:"type"`
	Status              string `json:"status"`
	DedupeKey           string `json:"dedupe_key"`
	ErrorCode           string `json:"error_code"`
	ErrorMessage        string `json:"error_message"`
	PostProcessAttempts int    `json:"post_process_attempts"`
	CreatedAt           int64  `json:"created_at"`
	StartedAt           *int64 `json:"started_at"`
	FinishedAt          *int64 `json:"finished_at"`
	DurationMs          *int64 `json:"duration_ms"`
}

var adminWorkerTaskTypes = map[string]bool{
	repository.WorkerTaskTypeAssetDirectorySync: true,
	repository.WorkerTaskTypeAssetHistorySync:   true,
	repository.WorkerTaskTypeFXRateSync:         true,
}

var adminWorkerTaskStatuses = map[string]bool{
	repository.WorkerTaskStatusPending:     true,
	repository.WorkerTaskStatusRunning:     true,
	repository.WorkerTaskStatusPreComplete: true,
	repository.WorkerTaskStatusComplete:    true,
	repository.WorkerTaskStatusFailed:      true,
	repository.WorkerTaskStatusCanceled:    true,
}

// activeWorkerTaskStatuses is the expansion of the "active" pseudo status.
var activeWorkerTaskStatuses = []string{
	repository.WorkerTaskStatusPending,
	repository.WorkerTaskStatusRunning,
	repository.WorkerTaskStatusPreComplete,
}

// durationMs derives finished-started; nil while the span is still open.
func durationMs(startedAt, finishedAt *int64) *int64 {
	if startedAt == nil || finishedAt == nil {
		return nil
	}
	d := *finishedAt - *startedAt
	return &d
}

// ListWorkerTasks returns one filtered task page ordered by created_at DESC.
func (s *AdminService) ListWorkerTasks(
	ctx context.Context, params AdminWorkerTaskListParams,
) (Page[AdminWorkerTaskItem], error) {
	var zero Page[AdminWorkerTaskItem]
	if params.Type != "" && !adminWorkerTaskTypes[params.Type] {
		return zero, newErr("invalid_request",
			"type must be one of asset_directory_sync, asset_history_sync, fx_rate_sync", nil)
	}
	var statuses []string
	switch {
	case params.Status == "":
	case params.Status == "active":
		statuses = activeWorkerTaskStatuses
	case adminWorkerTaskStatuses[params.Status]:
		statuses = []string{params.Status}
	default:
		return zero, newErr("invalid_request",
			"status must be a worker task status or active", nil)
	}

	limit, offset := normalizePage(params.Limit, params.Offset)
	tasks, total, err := s.tasks.List(ctx, repository.WorkerTaskFilter{
		Type: params.Type, Statuses: statuses, Query: params.Query,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return zero, wrapRepo("list worker tasks", err)
	}
	items := make([]AdminWorkerTaskItem, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, AdminWorkerTaskItem{
			ID:                  t.ID,
			Type:                t.Type,
			Status:              t.Status,
			DedupeKey:           t.DedupeKey,
			ErrorCode:           t.ErrorCode,
			ErrorMessage:        t.ErrorMessage,
			PostProcessAttempts: t.PostProcessAttempts,
			CreatedAt:           t.CreatedAt,
			StartedAt:           t.StartedAt,
			FinishedAt:          t.FinishedAt,
			DurationMs:          durationMs(t.StartedAt, t.FinishedAt),
		})
	}
	return Page[AdminWorkerTaskItem]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// AdminTaskTimelinePhase is one derived execution timeline node. Phases whose
// timestamp column is NULL are omitted entirely — the frontend renders, it
// never reasons about time.
type AdminTaskTimelinePhase struct {
	Phase  string `json:"phase"`
	At     int64  `json:"at"`
	Status string `json:"status,omitempty"`
}

// AdminTaskHeartbeat reports the latest heartbeat and whether it is stale
// (only meaningful while the task is running).
type AdminTaskHeartbeat struct {
	At    int64 `json:"at"`
	Stale bool  `json:"stale"`
}

// AdminWorkerTaskDetail is the full task detail: raw row (payload/result as
// opaque strings), derived timeline, heartbeat and callback records.
type AdminWorkerTaskDetail struct {
	Task               repository.WorkerTask          `json:"task"`
	Timeline           []AdminTaskTimelinePhase       `json:"timeline"`
	Heartbeat          *AdminTaskHeartbeat            `json:"heartbeat,omitempty"`
	PostProcessRecords []repository.PostProcessRecord `json:"post_process_records"`
}

// taskTimeline derives the execution timeline from the row's timestamps.
func taskTimeline(t repository.WorkerTask) []AdminTaskTimelinePhase {
	timeline := []AdminTaskTimelinePhase{{Phase: "created", At: t.CreatedAt}}
	if t.StartedAt != nil {
		timeline = append(timeline, AdminTaskTimelinePhase{Phase: "started", At: *t.StartedAt})
	}
	if t.PreCompletedAt != nil {
		timeline = append(timeline, AdminTaskTimelinePhase{Phase: "pre_complete", At: *t.PreCompletedAt})
	}
	if t.FinishedAt != nil {
		timeline = append(timeline, AdminTaskTimelinePhase{
			Phase: "finished", At: *t.FinishedAt, Status: t.Status,
		})
	}
	return timeline
}

// GetWorkerTaskDetail loads one task with its timeline and callback records.
func (s *AdminService) GetWorkerTaskDetail(
	ctx context.Context, taskID string,
) (AdminWorkerTaskDetail, error) {
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return AdminWorkerTaskDetail{}, newErr("task_not_found", "worker task not found", nil)
		}
		return AdminWorkerTaskDetail{}, wrapRepo("load worker task", err)
	}
	records, err := s.records.ListByTask(ctx, taskID)
	if err != nil {
		return AdminWorkerTaskDetail{}, wrapRepo("list post process records", err)
	}
	if records == nil {
		records = []repository.PostProcessRecord{}
	}
	detail := AdminWorkerTaskDetail{
		Task:               task,
		Timeline:           taskTimeline(task),
		PostProcessRecords: records,
	}
	if task.HeartbeatAt != nil {
		detail.Heartbeat = &AdminTaskHeartbeat{
			At: *task.HeartbeatAt,
			Stale: task.Status == repository.WorkerTaskStatusRunning &&
				*task.HeartbeatAt < s.now().Add(-adminStaleHeartbeat).UnixMilli(),
		}
	}
	return detail, nil
}

// --- jobs ---

// AdminJobListParams filters GET /admin/jobs.
type AdminJobListParams struct {
	Type   string
	Status string
	PlanID string
	Limit  int
	Offset int
}

// AdminJobItem is one computed job listing row. The list already carries
// progress and error facts; deep-diving a single job reuses GET /jobs/{id}.
type AdminJobItem struct {
	ID              string `json:"id"`
	PlanID          string `json:"plan_id"`
	PlanName        string `json:"plan_name"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	Phase           string `json:"phase"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	ErrorCode       string `json:"error_code"`
	ErrorMessage    string `json:"error_message"`
	CreatedAt       int64  `json:"created_at"`
	StartedAt       *int64 `json:"started_at"`
	FinishedAt      *int64 `json:"finished_at"`
	DurationMs      *int64 `json:"duration_ms"`
}

var adminJobTypes = map[string]bool{
	repository.JobTypeSimulation:           true,
	repository.JobTypeStress:               true,
	repository.JobTypeSensitivity:          true,
	repository.JobTypeResearchBacktest:     true,
	repository.JobTypeResearchOptimization: true,
}

var adminJobStatuses = map[string]bool{
	repository.JobStatusQueued:    true,
	repository.JobStatusRunning:   true,
	repository.JobStatusSucceeded: true,
	repository.JobStatusFailed:    true,
	repository.JobStatusCanceled:  true,
}

// ListJobs returns one filtered job page ordered by created_at DESC.
func (s *AdminService) ListJobs(
	ctx context.Context, params AdminJobListParams,
) (Page[AdminJobItem], error) {
	var zero Page[AdminJobItem]
	if params.Type != "" && !adminJobTypes[params.Type] {
		return zero, newErr("invalid_request",
			"type must be one of simulation, stress, sensitivity, research_backtest, research_optimization_backtest", nil)
	}
	var statuses []string
	switch {
	case params.Status == "":
	case params.Status == "active":
		statuses = []string{repository.JobStatusQueued, repository.JobStatusRunning}
	case adminJobStatuses[params.Status]:
		statuses = []string{params.Status}
	default:
		return zero, newErr("invalid_request", "status must be a job status or active", nil)
	}

	limit, offset := normalizePage(params.Limit, params.Offset)
	jobs, total, err := s.jobs.List(ctx, repository.JobFilter{
		Type: params.Type, Statuses: statuses, PlanID: params.PlanID,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return zero, wrapRepo("list jobs", err)
	}
	items := make([]AdminJobItem, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, AdminJobItem{
			ID:              j.ID,
			PlanID:          j.PlanID,
			PlanName:        j.PlanName,
			Type:            j.Type,
			Status:          j.Status,
			Phase:           j.Phase,
			ProgressCurrent: j.ProgressCurrent,
			ProgressTotal:   j.ProgressTotal,
			ErrorCode:       j.ErrorCode,
			ErrorMessage:    j.ErrorMessage,
			CreatedAt:       j.CreatedAt,
			StartedAt:       j.StartedAt,
			FinishedAt:      j.FinishedAt,
			DurationMs:      durationMs(j.StartedAt, j.FinishedAt),
		})
	}
	return Page[AdminJobItem]{Items: items, Total: total, Limit: limit, Offset: offset}, nil
}

// --- post process records ---

// AdminPostProcessRecordParams filters GET /admin/post-process-records.
type AdminPostProcessRecordParams struct {
	TaskID   string
	Result   string
	TaskType string
	Limit    int
	Offset   int
}

var adminCallbackResults = map[string]bool{
	PostProcessSuccess:        true,
	PostProcessRetryableError: true,
	PostProcessPermanentError: true,
}

// ListPostProcessRecords returns one filtered callback record page.
func (s *AdminService) ListPostProcessRecords(
	ctx context.Context, params AdminPostProcessRecordParams,
) (Page[repository.PostProcessRecord], error) {
	var zero Page[repository.PostProcessRecord]
	if params.Result != "" && !adminCallbackResults[params.Result] {
		return zero, newErr("invalid_request",
			"result must be one of success, retryable_error, permanent_error", nil)
	}
	if params.TaskType != "" && !adminWorkerTaskTypes[params.TaskType] {
		return zero, newErr("invalid_request",
			"task_type must be one of asset_directory_sync, asset_history_sync, fx_rate_sync", nil)
	}
	limit, offset := normalizePage(params.Limit, params.Offset)
	items, total, err := s.records.List(ctx, repository.PostProcessRecordFilter{
		TaskID: params.TaskID, Result: params.Result, TaskType: params.TaskType,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return zero, wrapRepo("list post process records", err)
	}
	if items == nil {
		items = []repository.PostProcessRecord{}
	}
	return Page[repository.PostProcessRecord]{
		Items: items, Total: total, Limit: limit, Offset: offset,
	}, nil
}

// --- data versions ---

// ListDataVersions returns one page of market_data_versions filtered by an
// optional version_key prefix (asset_directory / asset_history / fx_rate).
func (s *AdminService) ListDataVersions(
	ctx context.Context, prefix string, limit, offset int,
) (Page[repository.MarketDataVersion], error) {
	limit, offset = normalizePage(limit, offset)
	items, total, err := s.assets.ListDataVersions(ctx, prefix, limit, offset)
	if err != nil {
		return Page[repository.MarketDataVersion]{}, wrapRepo("list market data versions", err)
	}
	if items == nil {
		items = []repository.MarketDataVersion{}
	}
	return Page[repository.MarketDataVersion]{
		Items: items, Total: total, Limit: limit, Offset: offset,
	}, nil
}

// normalizePage applies the admin pagination convention: limit defaults to
// 20 and caps at 100; offset floors at 0.
func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
