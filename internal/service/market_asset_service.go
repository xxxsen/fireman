package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
	taskcore "github.com/fireman/fireman/internal/task"
)

// Directory sync scopes. A scope is a pure UI aggregation of directory sync
// units; the real task/state/version granularity is the unit's sync_key.
const (
	ScopeCNAll   = "cn_all"
	ScopeHKAll   = "hk_all"
	ScopeUSAll   = "us_all"
	ScopeFXRates = "fx_rates"
)

// DirectorySyncUnit is one directory sync unit: the real dedupe, state and
// version granularity. All directory tasks are generated from this static
// registry — arbitrary markets/instrument_types are never accepted.
type DirectorySyncUnit struct {
	SyncKey         string
	Scope           string
	Label           string
	Markets         []string
	InstrumentTypes []string
}

// directorySyncUnits is the full unit registry in display order.
var directorySyncUnits = []DirectorySyncUnit{
	{
		SyncKey: "cn_exchange_stock", Scope: ScopeCNAll, Label: "A 股股票",
		Markets: []string{"CN"}, InstrumentTypes: []string{"cn_exchange_stock"},
	},
	{
		SyncKey: "cn_exchange_fund", Scope: ScopeCNAll, Label: "场内基金（ETF/LOF）",
		Markets: []string{"CN"}, InstrumentTypes: []string{"cn_exchange_fund"},
	},
	{
		SyncKey: "cn_mutual_fund", Scope: ScopeCNAll, Label: "场外基金",
		Markets: []string{"CN"}, InstrumentTypes: []string{"cn_mutual_fund"},
	},
	{
		SyncKey: "hk_stock", Scope: ScopeHKAll, Label: "港股股票",
		Markets: []string{"HK"}, InstrumentTypes: []string{"hk_stock"},
	},
	{
		SyncKey: "hk_etf", Scope: ScopeHKAll, Label: "港股 ETF",
		Markets: []string{"HK"}, InstrumentTypes: []string{"hk_etf"},
	},
	{
		SyncKey: "us_stock", Scope: ScopeUSAll, Label: "美股股票",
		Markets: []string{"US"}, InstrumentTypes: []string{"us_stock"},
	},
	{
		SyncKey: "us_etf", Scope: ScopeUSAll, Label: "美股 ETF",
		Markets: []string{"US"}, InstrumentTypes: []string{"us_etf"},
	},
}

// DirectoryScopes lists the directory scopes in display order.
var DirectoryScopes = []string{ScopeCNAll, ScopeHKAll, ScopeUSAll}

var directoryScopeLabels = map[string]string{
	ScopeCNAll: "中国市场目录",
	ScopeHKAll: "港股市场目录",
	ScopeUSAll: "美股市场目录",
}

// DirectoryScopeLabel returns the display label for a directory scope.
func DirectoryScopeLabel(scope string) string {
	if label, ok := directoryScopeLabels[scope]; ok {
		return label
	}
	return scope
}

// DirectoryUnitBySyncKey looks up one unit in the registry.
func DirectoryUnitBySyncKey(syncKey string) (DirectorySyncUnit, bool) {
	for _, u := range directorySyncUnits {
		if u.SyncKey == syncKey {
			return u, true
		}
	}
	return DirectorySyncUnit{}, false
}

// DirectoryUnitsByScope returns a scope's units in registry order.
func DirectoryUnitsByScope(scope string) []DirectorySyncUnit {
	var out []DirectorySyncUnit
	for _, u := range directorySyncUnits {
		if u.Scope == scope {
			out = append(out, u)
		}
	}
	return out
}

// AssetDirectorySyncPayload is the worker payload for asset_directory_sync.
// sync_key identifies the directory sync unit; markets/instrument_types are
// always copied from the unit registry, never from user input.
type AssetDirectorySyncPayload struct {
	SyncKey         string   `json:"sync_key"`
	Scope           string   `json:"scope"`
	Markets         []string `json:"markets"`
	InstrumentTypes []string `json:"instrument_types"`
	Force           bool     `json:"force"`
}

// AssetHistorySyncPayload is the worker payload for asset_history_sync. Go
// derives it from the narrow frontend request; the sidecar never sees "mode".
// The identity fields (market/instrument_type/region_code/exchange/symbol/
// instrument_kind) always come from the market_assets directory row — never
// re-parsed from user input or the asset key string.
type AssetHistorySyncPayload struct {
	AssetKey           string `json:"asset_key"`
	Market             string `json:"market"`
	InstrumentType     string `json:"instrument_type"`
	RegionCode         string `json:"region_code"`
	Exchange           string `json:"exchange"`
	Symbol             string `json:"symbol"`
	InstrumentKind     string `json:"instrument_kind"`
	CanonicalSymbol    string `json:"canonical_symbol"`
	FeeMode            string `json:"fee_mode"`
	AdjustPolicy       string `json:"adjust_policy"`
	PointType          string `json:"point_type"`
	RequestedRange     string `json:"requested_range"`
	RequiredSourceName string `json:"required_source_name"`
	StartDate          string `json:"start_date,omitempty"`
	AllowSourceSwitch  bool   `json:"allow_source_switch"`
	ReplacementMode    string `json:"replacement_mode"`
}

// FXRateSyncPayload is the worker payload for fx_rate_sync.
type FXRateSyncPayload struct {
	Pairs           []string `json:"pairs"`
	RequestedRange  string   `json:"requested_range"`
	ReplacementMode string   `json:"replacement_mode"`
}

// FXPairs lists the system FX pairs the simulation/valuation stack depends on.
var FXPairs = []string{"USDCNY", "HKDCNY"}

// MarketAssetService owns the market asset directory, history views and worker
// task creation. All searches read the local DB only; remote data always flows
// through worker tasks.
type MarketAssetService struct {
	sql         *sql.DB
	tasks       *repository.WorkerTaskRepo
	assets      *repository.MarketAssetRepo
	coordinator *taskcore.Coordinator
}

func NewMarketAssetService(
	sqlDB *sql.DB,
	tasks *repository.WorkerTaskRepo,
	assets *repository.MarketAssetRepo,
	coordinators ...*taskcore.Coordinator,
) *MarketAssetService {
	var coordinator *taskcore.Coordinator
	if len(coordinators) > 0 {
		coordinator = coordinators[0]
	}
	if coordinator == nil {
		coordinator = taskcore.NewCoordinator(sqlDB, tasks, taskcore.DefaultRegistry(), nil)
	}
	return &MarketAssetService{sql: sqlDB, tasks: tasks, assets: assets, coordinator: coordinator}
}

// --- task views ---

// WorkerTaskView is the API shape of a worker task (payload/result omitted).
type WorkerTaskView struct {
	ID              string `json:"id"`
	WorkerType      string `json:"worker_type"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	ScopeType       string `json:"scope_type"`
	ScopeID         string `json:"scope_id"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	Phase           string `json:"phase"`
	CancelRequested bool   `json:"cancel_requested"`
	AttemptCount    int    `json:"attempt_count"`
	MaxAttempts     int    `json:"max_attempts"`
	ErrorCode       string `json:"error_code"`
	ErrorMessage    string `json:"error_message"`
	CreatedAt       int64  `json:"created_at"`
	StartedAt       *int64 `json:"started_at,omitempty"`
	FinishedAt      *int64 `json:"finished_at,omitempty"`
	HeartbeatAt     *int64 `json:"heartbeat_at,omitempty"`
}

func taskToView(t repository.WorkerTask) WorkerTaskView {
	return WorkerTaskView{
		ID: t.ID, WorkerType: t.WorkerType, Type: t.Type, Status: t.Status,
		ScopeType: t.ScopeType, ScopeID: t.ScopeID,
		ProgressCurrent: t.ProgressCurrent, ProgressTotal: t.ProgressTotal, Phase: t.Phase,
		CancelRequested: t.CancelRequested, AttemptCount: t.AttemptCount, MaxAttempts: t.MaxAttempts,
		ErrorCode: t.ErrorCode, ErrorMessage: t.ErrorMessage,
		CreatedAt: t.CreatedAt, StartedAt: t.StartedAt, FinishedAt: t.FinishedAt, HeartbeatAt: t.HeartbeatAt,
	}
}

// GetTask returns a single worker task by id.
func (s *MarketAssetService) GetTask(ctx context.Context, taskID string) (WorkerTaskView, error) {
	task, err := s.tasks.GetByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return WorkerTaskView{}, newErr("task_not_found", "worker task not found", nil)
		}
		return WorkerTaskView{}, wrapRepo("load worker task", err)
	}
	return taskToView(task), nil
}

// --- directory listing ---

// MarketAssetSyncView is a single-unit sync status block (FX rates). The
// directory scopes use the aggregated DirectoryScopeSyncView instead.
type MarketAssetSyncView struct {
	Scope             string          `json:"scope"`
	Task              *WorkerTaskView `json:"task,omitempty"`
	LastSuccessAt     *int64          `json:"last_success_at,omitempty"`
	LastSuccessTaskID string          `json:"last_success_task_id"`
}

// DirectorySyncUnitView is one unit's status inside a scope aggregation.
type DirectorySyncUnitView struct {
	SyncKey           string          `json:"sync_key"`
	Label             string          `json:"label"`
	Task              *WorkerTaskView `json:"task,omitempty"`
	LastSuccessAt     *int64          `json:"last_success_at,omitempty"`
	LastSuccessTaskID string          `json:"last_success_task_id"`
}

// Directory scope aggregate statuses; computed by Go only so every page
// renders the same interpretation.
const (
	DirectoryScopeStatusRunning  = "running"
	DirectoryScopeStatusComplete = "complete"
	DirectoryScopeStatusPartial  = "partial"
	DirectoryScopeStatusFailed   = "failed"
	DirectoryScopeStatusNever    = "never"
)

// DirectoryScopeSyncView is the aggregated sync view of one directory scope:
// scope-level status plus the per-unit facts it was derived from.
type DirectoryScopeSyncView struct {
	Scope string `json:"scope"`
	Label string `json:"label"`
	// Status: running | complete | partial | failed | never.
	Status string `json:"status"`
	// LastSuccessAt is only set when every unit has succeeded at least once;
	// it is the minimum of the unit success times ("oldest full success").
	LastSuccessAt *int64                  `json:"last_success_at,omitempty"`
	Units         []DirectorySyncUnitView `json:"units"`
}

// aggregateDirectoryScope derives the scope status and full-success time from
// directory sync unit views.
func aggregateDirectoryScope(units []DirectorySyncUnitView) (string, *int64) {
	if len(units) == 0 {
		return DirectoryScopeStatusNever, nil
	}
	facts := directoryScopeFacts(units)
	lastSuccessAt := directoryScopeLastSuccess(facts.allSuccess, facts.minSuccess)
	switch {
	case facts.anyActive:
		return DirectoryScopeStatusRunning, lastSuccessAt
	case facts.allSuccess && !facts.anyFailed:
		return DirectoryScopeStatusComplete, lastSuccessAt
	case !facts.anySuccess && !facts.anyTask:
		return DirectoryScopeStatusNever, lastSuccessAt
	case !facts.anySuccess && facts.anyFailed:
		return DirectoryScopeStatusFailed, lastSuccessAt
	default:
		return DirectoryScopeStatusPartial, lastSuccessAt
	}
}

type directoryScopeStatusFacts struct {
	anyActive  bool
	anyFailed  bool
	anySuccess bool
	anyTask    bool
	allSuccess bool
	minSuccess int64
}

func directoryScopeFacts(units []DirectorySyncUnitView) directoryScopeStatusFacts {
	facts := directoryScopeStatusFacts{allSuccess: true}
	for _, u := range units {
		facts.applyUnit(u)
	}
	return facts
}

func (f *directoryScopeStatusFacts) applyUnit(u DirectorySyncUnitView) {
	if u.Task != nil {
		f.anyTask = true
		if repository.IsActiveWorkerTaskStatus(u.Task.Status) {
			f.anyActive = true
		}
		if u.Task.Status == repository.WorkerTaskStatusFailed {
			f.anyFailed = true
		}
	}
	if u.LastSuccessAt == nil {
		f.allSuccess = false
		return
	}
	f.anySuccess = true
	if f.minSuccess == 0 || *u.LastSuccessAt < f.minSuccess {
		f.minSuccess = *u.LastSuccessAt
	}
}

func directoryScopeLastSuccess(allSuccess bool, minSuccess int64) *int64 {
	if !allSuccess {
		return nil
	}
	v := minSuccess
	return &v
}

// MarketAssetListItem is one directory row plus its local history readiness
// (from market_asset_history_state) so pickers can tell whether the asset
// still needs a history sync before simulation. For assets without local
// history, HistorySyncStatus/HistorySyncError surface the latest history sync
// task so pickers can render "syncing" and "failed" states.
type MarketAssetListItem struct {
	repository.MarketAsset
	// InstrumentTypeLabel / InstrumentTypePriority are the backend-owned
	// presentation facts for the instrument type (Chinese label and
	// identity-candidate ordering); the web pickers must not re-derive them.
	InstrumentTypeLabel    string `json:"instrument_type_label"`
	InstrumentTypePriority int    `json:"instrument_type_priority"`
	HasHistory             bool   `json:"has_history"`
	HistoryDataAsOf        string `json:"history_data_as_of,omitempty"`
	HistoryPointCount      int    `json:"history_point_count,omitempty"`
	HistorySourceName      string `json:"history_source_name,omitempty"`
	HistorySyncStatus      string `json:"history_sync_status,omitempty"`
	HistorySyncError       string `json:"history_sync_error,omitempty"`
}

// MarketAssetListResult is the GET /market-assets response. Total is the
// filtered row count before pagination.
type MarketAssetListResult struct {
	Assets []MarketAssetListItem    `json:"assets"`
	Syncs  []DirectoryScopeSyncView `json:"syncs"`
	FXSync *MarketAssetSyncView     `json:"fx_sync,omitempty"`
	Total  int                      `json:"total"`
}

// MarketAssetListParams filters the directory listing. SymbolQuery matches
// symbol only; NameQuery matches name only.
type MarketAssetListParams struct {
	Market          string
	InstrumentTypes []string
	SymbolQuery     string
	NameQuery       string
	IncludeInactive bool
	Limit           int
	Offset          int
}

// BuildSyncView assembles the single-unit sync status block for one sync-state
// row (FX rates): last success facts from market_asset_sync_state plus the
// latest task read live from worker_tasks.
func (s *MarketAssetService) BuildSyncView(ctx context.Context, syncKey string) (MarketAssetSyncView, error) {
	view := MarketAssetSyncView{Scope: syncKey}
	st, ok, err := s.assets.GetSyncState(ctx, syncKey)
	if err != nil {
		return view, wrapRepo("load sync state", err)
	}
	if !ok {
		return view, nil
	}
	view.LastSuccessAt = st.LastSuccessAt
	view.LastSuccessTaskID = st.LastSuccessTaskID
	if st.LastTaskID != "" {
		task, err := s.tasks.GetByID(ctx, st.LastTaskID)
		if err == nil {
			v := taskToView(task)
			view.Task = &v
		} else if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
			return view, wrapRepo("load sync task", err)
		}
	}
	return view, nil
}

// BuildScopeSyncView assembles one directory scope's aggregated sync view
// from its units' sync-state rows and latest tasks. Shared by the asset
// listing API and the admin sync health so the two never drift apart.
func (s *MarketAssetService) BuildScopeSyncView(
	ctx context.Context, scope string,
) (DirectoryScopeSyncView, error) {
	view := DirectoryScopeSyncView{Scope: scope, Label: DirectoryScopeLabel(scope)}
	for _, unit := range DirectoryUnitsByScope(scope) {
		unitView, err := s.buildDirectoryUnitSyncView(ctx, unit)
		if err != nil {
			return view, err
		}
		view.Units = append(view.Units, unitView)
	}
	view.Status, view.LastSuccessAt = aggregateDirectoryScope(view.Units)
	return view, nil
}

func (s *MarketAssetService) buildDirectoryUnitSyncView(
	ctx context.Context, unit DirectorySyncUnit,
) (DirectorySyncUnitView, error) {
	unitView := DirectorySyncUnitView{SyncKey: unit.SyncKey, Label: unit.Label}
	st, ok, err := s.assets.GetSyncState(ctx, unit.SyncKey)
	if err != nil {
		return unitView, wrapRepo("load sync state", err)
	}
	if !ok {
		return unitView, nil
	}
	unitView.LastSuccessAt = st.LastSuccessAt
	unitView.LastSuccessTaskID = st.LastSuccessTaskID
	if st.LastTaskID == "" {
		return unitView, nil
	}
	task, err := s.tasks.GetByID(ctx, st.LastTaskID)
	if errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return unitView, nil
	}
	if err != nil {
		return unitView, wrapRepo("load sync task", err)
	}
	v := taskToView(task)
	unitView.Task = &v
	return unitView, nil
}

// ListAssets searches the local market asset directory and attaches the sync
// status for the requested market's scope (or all scopes without a market
// filter). It never triggers remote calls.
func (s *MarketAssetService) ListAssets(
	ctx context.Context, params MarketAssetListParams,
) (MarketAssetListResult, error) {
	market := strings.ToUpper(strings.TrimSpace(params.Market))
	res, err := s.assets.Search(ctx, repository.MarketAssetSearchOptions{
		Market:          market,
		InstrumentTypes: params.InstrumentTypes,
		SymbolQuery:     params.SymbolQuery,
		NameQuery:       params.NameQuery,
		IncludeInactive: params.IncludeInactive,
		Limit:           params.Limit,
		Offset:          params.Offset,
	})
	if err != nil {
		return MarketAssetListResult{}, wrapRepo("search market assets", err)
	}
	items, err := s.attachHistoryStates(ctx, res.Assets)
	if err != nil {
		return MarketAssetListResult{}, err
	}
	out := MarketAssetListResult{Assets: items, Total: res.Total}

	// Sync views always cover every directory scope: the UI sync panel is
	// fixed and must not react to list filters.
	for _, scope := range DirectoryScopes {
		view, err := s.BuildScopeSyncView(ctx, scope)
		if err != nil {
			return MarketAssetListResult{}, err
		}
		out.Syncs = append(out.Syncs, view)
	}
	fxView, err := s.BuildSyncView(ctx, ScopeFXRates)
	if err != nil {
		return MarketAssetListResult{}, err
	}
	out.FXSync = &fxView
	return out, nil
}

// attachHistoryStates annotates directory rows with their local history
// readiness. When an asset has several history dimensions, the one with the
// most points wins (it is the series simulations would use).
func (s *MarketAssetService) attachHistoryStates(
	ctx context.Context, assets []repository.MarketAsset,
) ([]MarketAssetListItem, error) {
	items := make([]MarketAssetListItem, 0, len(assets))
	keys := make([]string, 0, len(assets))
	for _, a := range assets {
		items = append(items, MarketAssetListItem{
			MarketAsset:            a,
			InstrumentTypeLabel:    instrumentTypeLabelZH(a.InstrumentType),
			InstrumentTypePriority: instrumentTypePriority(a.InstrumentType),
		})
		keys = append(keys, a.AssetKey)
	}
	if len(keys) == 0 {
		return items, nil
	}
	states, err := s.assets.ListHistoryStatesByAssetKeys(ctx, keys)
	if err != nil {
		return nil, wrapRepo("list history states", err)
	}
	best := make(map[string]repository.MarketAssetHistoryState, len(states))
	// Latest history sync task per asset without points yet: exposes the
	// syncing/failed states for pickers.
	pendingTask := make(map[string]string, len(states))
	for _, st := range states {
		if st.PointCount <= 0 {
			if st.LastTaskID != "" {
				pendingTask[st.AssetKey] = st.LastTaskID
			}
			continue
		}
		if cur, ok := best[st.AssetKey]; !ok || st.PointCount > cur.PointCount {
			best[st.AssetKey] = st
		}
	}
	for i := range items {
		if st, ok := best[items[i].AssetKey]; ok {
			items[i].HasHistory = true
			items[i].HistoryDataAsOf = st.DataAsOf
			items[i].HistoryPointCount = st.PointCount
			items[i].HistorySourceName = st.SourceName
			continue
		}
		taskID, ok := pendingTask[items[i].AssetKey]
		if !ok {
			continue
		}
		task, err := s.tasks.GetByID(ctx, taskID)
		if err != nil {
			continue
		}
		items[i].HistorySyncStatus = task.Status
		if task.Status == repository.WorkerTaskStatusFailed {
			msg := task.ErrorMessage
			if msg == "" {
				msg = task.ErrorCode
			}
			items[i].HistorySyncError = msg
		}
	}
	return items, nil
}

// --- directory sync task creation ---

// DirectorySyncRequest is the POST /market-assets/sync body: either a whole
// scope (creates every unit task) or a single sync_key. Arbitrary
// markets/instrument_types are never accepted — units come from the registry.
type DirectorySyncRequest struct {
	Scope   string `json:"scope"`
	SyncKey string `json:"sync_key"`
	Force   bool   `json:"force"`
}

// TaskCreateResult reports the task the caller should poll plus whether it
// already existed (active duplicate).
type TaskCreateResult struct {
	Task    WorkerTaskView `json:"task"`
	Existed bool           `json:"existed"`
}

// DirectorySyncTaskItem reports one unit's created-or-existing task.
type DirectorySyncTaskItem struct {
	SyncKey string         `json:"sync_key"`
	Label   string         `json:"label"`
	Task    WorkerTaskView `json:"task"`
	Existed bool           `json:"existed"`
}

// DirectorySyncResult is the sync response: one entry per requested unit.
type DirectorySyncResult struct {
	Scope string                  `json:"scope"`
	Tasks []DirectorySyncTaskItem `json:"tasks"`
}

// directoryDedupeKey is asset_directory_sync|{sync_key}. force never changes
// the dedupe key: an active unit task is always returned as-is.
func directoryDedupeKey(syncKey string) string {
	return repository.WorkerTaskTypeAssetDirectorySync + "|" + syncKey
}

// SyncDirectory creates asset_directory_sync tasks for the requested scope
// (all units) or single sync_key. All units are processed in one transaction
// so worker_tasks and market_asset_sync_state.last_task_id stay consistent;
// units with an existing active task are returned with existed=true without
// blocking the other units.
func (s *MarketAssetService) SyncDirectory(
	ctx context.Context, req DirectorySyncRequest,
) (DirectorySyncResult, error) {
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	syncKey := strings.ToLower(strings.TrimSpace(req.SyncKey))

	var units []DirectorySyncUnit
	switch {
	case syncKey != "":
		unit, ok := DirectoryUnitBySyncKey(syncKey)
		if !ok {
			return DirectorySyncResult{}, newErr("invalid_request",
				"unknown sync_key "+syncKey, nil)
		}
		if scope != "" && scope != unit.Scope {
			return DirectorySyncResult{}, newErr("invalid_request",
				fmt.Sprintf("sync_key %s does not belong to scope %s", syncKey, scope), nil)
		}
		scope = unit.Scope
		units = []DirectorySyncUnit{unit}
	case scope != "":
		units = DirectoryUnitsByScope(scope)
		if len(units) == 0 {
			return DirectorySyncResult{}, newErr("invalid_request",
				"scope must be one of cn_all, hk_all, us_all", nil)
		}
	default:
		return DirectorySyncResult{}, newErr("invalid_request",
			"scope or sync_key is required", nil)
	}

	out := DirectorySyncResult{Scope: scope, Tasks: make([]DirectorySyncTaskItem, 0, len(units))}
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		for _, unit := range units {
			item, err := s.createDirectoryUnitTaskTx(ctx, tx, unit, req.Force, 100)
			if err != nil {
				return err
			}
			out.Tasks = append(out.Tasks, item)
		}
		return nil
	})
	if err != nil {
		return DirectorySyncResult{}, wrapRepo("create directory sync tasks", err)
	}
	return out, nil
}

// SyncDirectoryWithTaskHook is the scheduler-only variant used when the rule
// state and the worker task must commit atomically.
func (s *MarketAssetService) SyncDirectoryWithTaskHook(
	ctx context.Context,
	syncKey string,
	hook func(context.Context, *sql.Tx, string) error,
) (TaskCreateResult, error) {
	unit, ok := DirectoryUnitBySyncKey(strings.ToLower(strings.TrimSpace(syncKey)))
	if !ok {
		return TaskCreateResult{}, newErr("invalid_request", "unknown sync_key "+syncKey, nil)
	}
	var item DirectorySyncTaskItem
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		var err error
		item, err = s.createDirectoryUnitTaskTx(ctx, tx, unit, false, 20)
		if err != nil {
			return err
		}
		return hook(ctx, tx, item.Task.ID)
	})
	if err != nil {
		return TaskCreateResult{}, wrapRepo("create directory auto update task", err)
	}
	return TaskCreateResult{Task: item.Task, Existed: item.Existed}, nil
}

// createDirectoryUnitTaskTx creates (or returns the existing active) task for
// one directory sync unit inside the caller's transaction.
func (s *MarketAssetService) createDirectoryUnitTaskTx(
	ctx context.Context, tx *sql.Tx, unit DirectorySyncUnit, force bool, priority int,
) (DirectorySyncTaskItem, error) {
	item := DirectorySyncTaskItem{SyncKey: unit.SyncKey, Label: unit.Label}
	taskType := repository.WorkerTaskTypeAssetDirectorySync
	dedupeKey := directoryDedupeKey(unit.SyncKey)

	existing, err := s.tasks.FindActiveByDedupeTx(ctx, tx, repository.WorkerTypeSidecar, taskType, dedupeKey)
	if err == nil {
		item.Task = taskToView(existing)
		item.Existed = true
		return item, nil
	}
	if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return item, fmt.Errorf("find active directory task: %w", err)
	}

	payload := AssetDirectorySyncPayload{
		SyncKey:         unit.SyncKey,
		Scope:           unit.Scope,
		Markets:         unit.Markets,
		InstrumentTypes: unit.InstrumentTypes,
		Force:           force,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return item, fmt.Errorf("marshal directory payload: %w", err)
	}
	task := repository.WorkerTask{
		ID:          "task_" + uuid.New().String(),
		WorkerType:  repository.WorkerTypeSidecar,
		Type:        taskType,
		Status:      repository.WorkerTaskStatusPending,
		Priority:    priority,
		ScopeType:   "system",
		ScopeID:     unit.SyncKey,
		DedupeKey:   dedupeKey,
		PayloadJSON: string(payloadJSON),
		MaxAttempts: 2,
		CreatedAt:   time.Now().UnixMilli(),
	}
	if err := s.coordinator.CreateTx(ctx, tx, &task); err != nil {
		if repository.IsWorkerTaskUniqueConstraint(err) {
			// Lost the race: another request created the active task first.
			dup, findErr := s.tasks.FindActiveByDedupeTx(ctx, tx, repository.WorkerTypeSidecar, taskType, dedupeKey)
			if findErr != nil {
				return item, fmt.Errorf("find duplicate directory task: %w", findErr)
			}
			item.Task = taskToView(dup)
			item.Existed = true
			return item, nil
		}
		return item, fmt.Errorf("create directory task: %w", err)
	}
	if err := s.assets.SetSyncLastTaskTx(ctx, tx, unit.SyncKey, unit.Scope, task.ID); err != nil {
		return item, fmt.Errorf("set directory sync last task: %w", err)
	}
	item.Task = taskToView(task)
	return item, nil
}

// createTask creates a worker task with dedupe semantics: when an active task
// with the same (type, dedupe_key) exists, it is returned unchanged and no new
// task is created. onBound runs in the same transaction for both a new task
// and an existing active task.
func (s *MarketAssetService) createTask(
	ctx context.Context, priority int,
	taskType, scopeType, scopeID, dedupeKey, payloadJSON string,
	onBound func(ctx context.Context, tx *sql.Tx, taskID string) error,
) (TaskCreateResult, error) {
	task := repository.WorkerTask{
		ID:          "task_" + uuid.New().String(),
		WorkerType:  repository.WorkerTypeSidecar,
		Type:        taskType,
		Status:      repository.WorkerTaskStatusPending,
		Priority:    priority,
		ScopeType:   scopeType,
		ScopeID:     scopeID,
		DedupeKey:   dedupeKey,
		PayloadJSON: payloadJSON,
		MaxAttempts: 2,
		CreatedAt:   time.Now().UnixMilli(),
	}
	var bound repository.WorkerTask
	var existed bool
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		var bindErr error
		bound, existed, bindErr = s.createOrReuseTaskTx(ctx, tx, task)
		if bindErr != nil {
			return bindErr
		}
		if onBound != nil {
			if err := onBound(ctx, tx, bound.ID); err != nil {
				return fmt.Errorf("run worker task binding hook: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return TaskCreateResult{}, wrapRepo("create worker task", err)
	}
	return TaskCreateResult{Task: taskToView(bound), Existed: existed}, nil
}

func (s *MarketAssetService) createOrReuseTaskTx(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
) (repository.WorkerTask, bool, error) {
	existing, err := s.tasks.FindActiveByDedupeTx(
		ctx, tx, task.WorkerType, task.Type, task.DedupeKey,
	)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return repository.WorkerTask{}, false, fmt.Errorf("find active task: %w", err)
	}
	if err := s.coordinator.CreateTx(ctx, tx, &task); err == nil {
		return task, false, nil
	} else if !repository.IsWorkerTaskUniqueConstraint(err) {
		return repository.WorkerTask{}, false, fmt.Errorf("create worker task: %w", err)
	}
	duplicate, err := s.tasks.FindActiveByDedupeTx(
		ctx, tx, task.WorkerType, task.Type, task.DedupeKey,
	)
	if err != nil {
		return repository.WorkerTask{}, false, fmt.Errorf("find duplicate active task: %w", err)
	}
	return duplicate, true, nil
}

// --- asset detail ---

// MarketAssetHistoryView is the history status block of the detail response.
type MarketAssetHistoryView struct {
	AdjustPolicy      string          `json:"adjust_policy"`
	PointType         string          `json:"point_type"`
	DataAsOf          string          `json:"data_as_of"`
	PointCount        int             `json:"point_count"`
	SourceName        string          `json:"source_name"`
	LastSuccessAt     *int64          `json:"last_success_at,omitempty"`
	LastSuccessTaskID string          `json:"last_success_task_id"`
	Task              *WorkerTaskView `json:"task,omitempty"`
	// CanSwitchSource reports that the latest same-source incremental refresh
	// failed with source_unavailable, enabling the switch-source-full action.
	CanSwitchSource bool                                 `json:"can_switch_source"`
	AutoUpdate      *repository.MarketDataAutoUpdateRule `json:"auto_update"`
}

// MarketAssetPointView is one (date, value) observation for charts.
type MarketAssetPointView struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// MarketAssetDetail is the GET /market-assets/by-key response.
type MarketAssetDetail struct {
	Asset           repository.MarketAsset `json:"asset"`
	History         MarketAssetHistoryView `json:"history"`
	Points          []MarketAssetPointView `json:"points"`
	AnnualReturns   json.RawMessage        `json:"annual_returns"`
	TrailingReturns json.RawMessage        `json:"trailing_returns,omitempty"`
}

// DefaultPointType picks the history point type for an asset. Mutual money
// funds only publish NAV; other mutual funds use the cumulative NAV series and
// exchange-traded assets use adjusted close.
func DefaultPointType(instrumentType, instrumentKind string) string {
	if instrumentType == "cn_mutual_fund" {
		if strings.Contains(instrumentKind, "货币") {
			return "nav"
		}
		return "total_return_index"
	}
	return "adjusted_close"
}

// DefaultAdjustPolicy returns the return-analysis-safe history policy.
func DefaultAdjustPolicy(instrumentType string) string {
	if instrumentType == "cn_mutual_fund" || instrumentType == "cash" || instrumentType == "fx_rate" {
		return "none"
	}
	return "hfq"
}

// ValidateHistoryDimension prevents raw and adjusted prices from sharing a key.
func ValidateHistoryDimension(asset repository.MarketAsset, adjustPolicy, pointType string) error {
	if adjustPolicy == "qfq" {
		return newErr("unsupported_adjust_policy",
			"qfq is not supported; use hfq + adjusted_close for return analysis", nil)
	}
	switch adjustPolicy {
	case "none", "hfq":
	default:
		return newErr("invalid_request", "adjust_policy must be none or hfq", nil)
	}
	if asset.InstrumentType == "cn_mutual_fund" {
		if adjustPolicy != "none" {
			return newErr("invalid_request", "mutual fund history only supports adjust_policy none", nil)
		}
		want := DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
		if pointType != want {
			return newErr("invalid_request", fmt.Sprintf("point_type must be %s for this asset", want), nil)
		}
		return nil
	}
	if asset.InstrumentType == "cash" {
		if adjustPolicy != "none" {
			return newErr("invalid_request", "cash only supports adjust_policy none", nil)
		}
		return nil
	}
	if asset.InstrumentType == "fx_rate" {
		if adjustPolicy != "none" || pointType != "fx_rate" {
			return newErr("invalid_request", "FX history requires none + fx_rate", nil)
		}
		return nil
	}
	if !isExchangeInstrumentType(asset.InstrumentType) {
		return newErr("invalid_request", "unsupported history dimension for this asset type", nil)
	}
	want := "adjusted_close"
	if adjustPolicy == "none" {
		want = "close"
	}
	if pointType != want {
		message := fmt.Sprintf("point_type must be %s when adjust_policy is %s", want, adjustPolicy)
		return newErr("invalid_request", message, nil)
	}
	return nil
}

func validateHistoryDimensionPair(adjustPolicy, pointType string) error {
	if (adjustPolicy == "") != (pointType == "") {
		return newErr("invalid_request",
			"adjust_policy and point_type must be provided together", nil)
	}
	return nil
}

func isExchangeInstrumentType(instrumentType string) bool {
	switch instrumentType {
	case "cn_exchange_stock", "cn_exchange_fund", "hk_stock", "hk_etf", "us_stock", "us_etf":
		return true
	default:
		return false
	}
}

// GetDetail loads the asset, its history state/points and the commit-time
// detail projection for one history dimension.
func (s *MarketAssetService) GetDetail(
	ctx context.Context, assetKey, adjustPolicy, pointType string,
) (MarketAssetDetail, error) {
	assetKey = strings.TrimSpace(assetKey)
	if assetKey == "" {
		return MarketAssetDetail{}, newErr("invalid_request", "asset_key is required", nil)
	}
	asset, err := s.assets.GetByKey(ctx, assetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return MarketAssetDetail{}, newErr("market_asset_not_found", "market asset not found", nil)
		}
		return MarketAssetDetail{}, wrapRepo("load market asset", err)
	}

	adjustPolicy, pointType, err = resolveHistoryDimension(asset, adjustPolicy, pointType)
	if err != nil {
		return MarketAssetDetail{}, err
	}
	if err := ValidateHistoryDimension(asset, adjustPolicy, pointType); err != nil {
		return MarketAssetDetail{}, err
	}

	detail := MarketAssetDetail{
		Asset: asset,
		History: MarketAssetHistoryView{
			AdjustPolicy: adjustPolicy,
			PointType:    pointType,
		},
		Points:        []MarketAssetPointView{},
		AnnualReturns: json.RawMessage("[]"),
	}

	st, ok, err := s.assets.GetHistoryState(ctx, assetKey, adjustPolicy, pointType)
	if err != nil {
		return MarketAssetDetail{}, wrapRepo("load history state", err)
	}
	if ok {
		if err := s.attachDetailHistoryTask(ctx, &detail.History, st); err != nil {
			return MarketAssetDetail{}, err
		}
	}

	points, err := s.assets.ListPoints(ctx, assetKey, adjustPolicy, pointType)
	if err != nil {
		return MarketAssetDetail{}, wrapRepo("list market asset points", err)
	}
	for _, p := range points {
		detail.Points = append(detail.Points, MarketAssetPointView{Date: p.TradeDate, Value: p.Value})
	}

	proj, ok, err := s.assets.GetDetailProjection(ctx, assetKey, adjustPolicy, pointType)
	if err != nil {
		return MarketAssetDetail{}, wrapRepo("load detail projection", err)
	}
	if ok {
		applyDetailProjection(&detail, proj)
	}
	return detail, nil
}

func (s *MarketAssetService) attachDetailHistoryTask(
	ctx context.Context,
	history *MarketAssetHistoryView,
	st repository.MarketAssetHistoryState,
) error {
	history.DataAsOf = st.DataAsOf
	history.PointCount = st.PointCount
	history.SourceName = st.SourceName
	history.LastSuccessAt = st.LastSuccessAt
	history.LastSuccessTaskID = st.LastSuccessTaskID
	if st.LastTaskID == "" {
		return nil
	}
	task, err := s.tasks.GetByID(ctx, st.LastTaskID)
	if errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return nil
	}
	if err != nil {
		return wrapRepo("load history task", err)
	}
	v := taskToView(task)
	history.Task = &v
	history.CanSwitchSource = canSwitchSource(task)
	return nil
}

func applyDetailProjection(detail *MarketAssetDetail, proj repository.MarketAssetDetailProjection) {
	if proj.AnnualReturnsJSON != "" {
		detail.AnnualReturns = json.RawMessage(proj.AnnualReturnsJSON)
	}
	if proj.TrailingReturnsJSON != "" && proj.TrailingReturnsJSON != "{}" {
		detail.TrailingReturns = json.RawMessage(proj.TrailingReturnsJSON)
	}
}

// resolveHistoryDimension uses the canonical type default when both parameters
// are omitted. Existing history states never redefine the calculation policy.
func resolveHistoryDimension(
	asset repository.MarketAsset, adjustPolicy, pointType string,
) (string, string, error) {
	adjustPolicy = strings.TrimSpace(adjustPolicy)
	pointType = strings.TrimSpace(pointType)
	if err := validateHistoryDimensionPair(adjustPolicy, pointType); err != nil {
		return "", "", err
	}
	if adjustPolicy != "" && pointType != "" {
		return adjustPolicy, pointType, nil
	}
	return DefaultAdjustPolicy(asset.InstrumentType),
		DefaultPointType(asset.InstrumentType, asset.InstrumentKind), nil
}

// canSwitchSource reports whether the failed task permits the
// switch_source_full escape hatch.
func canSwitchSource(task repository.WorkerTask) bool {
	if task.Type != repository.WorkerTaskTypeAssetHistorySync {
		return false
	}
	if task.Status != repository.WorkerTaskStatusFailed || task.ErrorCode != "source_unavailable" {
		return false
	}
	var payload AssetHistorySyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return false
	}
	return payload.ReplacementMode == "merge"
}

// --- history sync task creation ---

// HistorySyncRequest is the POST /market-assets/history-sync body. The
// frontend only submits the narrow mode; Go derives all execution parameters.
type HistorySyncRequest struct {
	AssetKey     string `json:"asset_key"`
	AdjustPolicy string `json:"adjust_policy"`
	PointType    string `json:"point_type"`
	Mode         string `json:"mode"`
}

const (
	historyModeDefaultRefresh   = "default_refresh"
	historyModeSwitchSourceFull = "switch_source_full"
)

func historyDedupeKey(p AssetHistorySyncPayload) string {
	allowSwitch := "false"
	if p.AllowSourceSwitch {
		allowSwitch = "true"
	}
	return strings.Join([]string{
		repository.WorkerTaskTypeAssetHistorySync,
		p.AssetKey,
		p.AdjustPolicy,
		p.PointType,
		p.RequestedRange,
		p.RequiredSourceName,
		p.StartDate,
		allowSwitch,
		p.ReplacementMode,
	}, "|")
}

// SyncHistory creates (or returns the existing active) asset_history_sync
// task for one history dimension.
func (s *MarketAssetService) SyncHistory(
	ctx context.Context, req HistorySyncRequest,
) (TaskCreateResult, error) {
	return s.syncHistory(ctx, req, 100, nil)
}

// SyncHistoryWithTaskHook uses the same payload and dedupe logic as manual
// refresh while allowing scheduler state to be written in the task transaction.
func (s *MarketAssetService) SyncHistoryWithTaskHook(
	ctx context.Context,
	req HistorySyncRequest,
	hook func(context.Context, *sql.Tx, string) error,
) (TaskCreateResult, error) {
	return s.syncHistory(ctx, req, 20, hook)
}

func (s *MarketAssetService) syncHistory(
	ctx context.Context, req HistorySyncRequest, priority int,
	hook func(context.Context, *sql.Tx, string) error,
) (TaskCreateResult, error) {
	var err error
	req, err = normalizeHistorySyncRequest(req)
	if err != nil {
		return TaskCreateResult{}, err
	}

	asset, err := s.assets.GetByKey(ctx, req.AssetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return TaskCreateResult{}, newErr("market_asset_not_found",
				"market asset not found; sync the asset directory first", nil)
		}
		return TaskCreateResult{}, wrapRepo("load market asset", err)
	}
	if req.AdjustPolicy == "" {
		req.AdjustPolicy = DefaultAdjustPolicy(asset.InstrumentType)
	}
	if req.PointType == "" {
		if req.AdjustPolicy == "none" && asset.InstrumentType != "cn_mutual_fund" {
			req.PointType = "close"
		} else {
			req.PointType = DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
		}
	}
	if err := ValidateHistoryDimension(asset, req.AdjustPolicy, req.PointType); err != nil {
		return TaskCreateResult{}, err
	}

	payload, err := s.buildHistoryPayload(ctx, asset, req)
	if err != nil {
		return TaskCreateResult{}, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return TaskCreateResult{}, fmt.Errorf("marshal history payload: %w", err)
	}
	return s.createTask(ctx, priority, repository.WorkerTaskTypeAssetHistorySync,
		"market_asset", req.AssetKey,
		historyDedupeKey(payload), string(payloadJSON),
		func(ctx context.Context, tx *sql.Tx, taskID string) error {
			if err := s.assets.SetHistoryLastTaskTx(
				ctx, tx, req.AssetKey, req.AdjustPolicy, req.PointType, taskID,
			); err != nil {
				return fmt.Errorf("set history last task: %w", err)
			}
			if hook != nil {
				return hook(ctx, tx, taskID)
			}
			return nil
		})
}

func normalizeHistorySyncRequest(req HistorySyncRequest) (HistorySyncRequest, error) {
	req.AssetKey = strings.TrimSpace(req.AssetKey)
	req.AdjustPolicy = strings.TrimSpace(req.AdjustPolicy)
	req.PointType = strings.TrimSpace(req.PointType)
	req.Mode = strings.TrimSpace(req.Mode)
	if req.AssetKey == "" {
		return HistorySyncRequest{}, newErr("invalid_request", "asset_key is required", nil)
	}
	if err := validateHistoryDimensionPair(req.AdjustPolicy, req.PointType); err != nil {
		return HistorySyncRequest{}, err
	}
	if req.Mode != historyModeDefaultRefresh && req.Mode != historyModeSwitchSourceFull {
		return HistorySyncRequest{}, newErr("invalid_request",
			"mode must be default_refresh or switch_source_full", nil)
	}
	return req, nil
}

// requireDirectoryIdentity rejects history sync for assets whose directory
// row lacks the definite exchange identity the sidecar needs. CN on-exchange
// history must never be fetched by re-inferring the exchange from the code.
func requireDirectoryIdentity(asset repository.MarketAsset) error {
	if !strings.EqualFold(asset.Market, "CN") {
		return nil
	}
	switch asset.InstrumentType {
	case "cn_exchange_stock", "cn_exchange_fund":
		if strings.TrimSpace(asset.RegionCode) == "" && strings.TrimSpace(asset.Exchange) == "" {
			return newErr("asset_identity_incomplete",
				"市场资产目录缺少该场内资产的交易所信息，无法创建历史同步任务；请先修正资产目录数据",
				map[string]any{"asset_key": asset.AssetKey})
		}
	}
	return nil
}

func (s *MarketAssetService) buildHistoryPayload(
	ctx context.Context, asset repository.MarketAsset, req HistorySyncRequest,
) (AssetHistorySyncPayload, error) {
	base := AssetHistorySyncPayload{
		AssetKey:        asset.AssetKey,
		Market:          asset.Market,
		InstrumentType:  asset.InstrumentType,
		RegionCode:      asset.RegionCode,
		Exchange:        asset.Exchange,
		Symbol:          asset.Symbol,
		InstrumentKind:  asset.InstrumentKind,
		CanonicalSymbol: asset.CanonicalSymbol,
		FeeMode:         asset.FeeMode,
		AdjustPolicy:    req.AdjustPolicy,
		PointType:       req.PointType,
	}
	if err := requireDirectoryIdentity(asset); err != nil {
		return base, err
	}

	st, hasState, err := s.assets.GetHistoryState(ctx, req.AssetKey, req.AdjustPolicy, req.PointType)
	if err != nil {
		return base, wrapRepo("load history state", err)
	}
	hasHistory := hasState && st.SourceName != "" && st.PointCount > 0 && st.DataAsOf != ""

	if req.Mode == historyModeSwitchSourceFull {
		return s.buildSwitchSourcePayload(ctx, base, st, hasState)
	}

	// default_refresh
	if !hasHistory {
		base.RequestedRange = "full"
		base.RequiredSourceName = ""
		base.AllowSourceSwitch = true
		base.ReplacementMode = "full"
		return base, nil
	}

	mixed, err := s.hasMixedSources(ctx, req.AssetKey, req.AdjustPolicy, req.PointType)
	if err != nil {
		return base, err
	}
	if mixed {
		// Mixed-source history must be repaired by a full replacement; merge
		// refreshes are forbidden until a single source is restored.
		base.RequestedRange = "full"
		base.RequiredSourceName = ""
		base.AllowSourceSwitch = true
		base.ReplacementMode = "full"
		return base, nil
	}

	startDate, err := incrementalStartDate(st.DataAsOf)
	if err != nil {
		return base, newErr("invalid_request",
			fmt.Sprintf("history state data_as_of %q is invalid", st.DataAsOf), nil)
	}
	base.RequestedRange = "incremental"
	base.RequiredSourceName = st.SourceName
	base.StartDate = startDate
	base.AllowSourceSwitch = false
	base.ReplacementMode = "merge"
	return base, nil
}

func (s *MarketAssetService) buildSwitchSourcePayload(
	ctx context.Context,
	base AssetHistorySyncPayload,
	st repository.MarketAssetHistoryState,
	hasState bool,
) (AssetHistorySyncPayload, error) {
	if !hasState || st.LastTaskID == "" {
		return base, newErr("invalid_request",
			"switch_source_full requires a prior failed same-source refresh", nil)
	}
	lastTask, err := s.tasks.GetByID(ctx, st.LastTaskID)
	if errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return base, newErr("invalid_request",
			"switch_source_full requires a prior failed same-source refresh", nil)
	}
	if err != nil {
		return base, wrapRepo("load last history task", err)
	}
	if !canSwitchSource(lastTask) {
		return base, newErr("invalid_request",
			"switch_source_full is only allowed after a source_unavailable failure", nil)
	}
	base.RequestedRange = "full"
	base.RequiredSourceName = ""
	base.AllowSourceSwitch = true
	base.ReplacementMode = "full"
	return base, nil
}

func (s *MarketAssetService) hasMixedSources(
	ctx context.Context, assetKey, adjustPolicy, pointType string,
) (bool, error) {
	var mixed bool
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		summary, err := s.assets.PointsSummaryTx(ctx, tx, assetKey, adjustPolicy, pointType)
		if err != nil {
			return fmt.Errorf("points summary: %w", err)
		}
		mixed = len(summary.SourceNames) > 1
		return nil
	})
	if err != nil {
		return false, wrapRepo("summarize points", err)
	}
	return mixed, nil
}

// incrementalStartDate is data_as_of minus 10 calendar days.
func incrementalStartDate(dataAsOf string) (string, error) {
	t, err := time.Parse("2006-01-02", dataAsOf)
	if err != nil {
		return "", fmt.Errorf("parse data_as_of: %w", err)
	}
	return t.AddDate(0, 0, -10).Format("2006-01-02"), nil
}

// --- fx sync task creation ---

func fxDedupeKey(p FXRateSyncPayload) string {
	return strings.Join([]string{
		repository.WorkerTaskTypeFXRateSync,
		strings.Join(p.Pairs, ","),
		p.RequestedRange,
		p.ReplacementMode,
	}, "|")
}

// SyncFXRates creates (or returns the existing active) fx_rate_sync task for
// the system FX pairs.
func (s *MarketAssetService) SyncFXRates(ctx context.Context) (TaskCreateResult, error) {
	pairs := append([]string(nil), FXPairs...)
	sort.Strings(pairs)
	payload := FXRateSyncPayload{
		Pairs:           pairs,
		RequestedRange:  "full",
		ReplacementMode: "full",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return TaskCreateResult{}, fmt.Errorf("marshal fx payload: %w", err)
	}
	return s.createTask(ctx, 100, repository.WorkerTaskTypeFXRateSync,
		"system", ScopeFXRates,
		fxDedupeKey(payload), string(payloadJSON),
		func(ctx context.Context, tx *sql.Tx, taskID string) error {
			return s.assets.SetSyncLastTaskTx(ctx, tx, ScopeFXRates, ScopeFXRates, taskID)
		})
}
