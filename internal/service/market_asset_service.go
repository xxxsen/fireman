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
)

// Directory sync scopes. Each scope normalizes to a fixed markets +
// instrument_types combination so synonymous requests share one dedupe key.
const (
	ScopeCNAll = "cn_all"
	ScopeHKAll = "hk_all"
	ScopeUSAll = "us_all"
)

// scopeDefinition pins the normalized markets and required instrument types
// for one directory scope. Every listed instrument type is a required
// category: if the upstream listing for any of them fails, the whole task
// fails (no partial success).
type scopeDefinition struct {
	Markets         []string
	InstrumentTypes []string
}

var scopeDefinitions = map[string]scopeDefinition{
	ScopeCNAll: {
		Markets:         []string{"CN"},
		InstrumentTypes: []string{"cn_exchange_stock", "cn_exchange_fund", "cn_mutual_fund"},
	},
	ScopeHKAll: {
		Markets:         []string{"HK"},
		InstrumentTypes: []string{"hk_stock"},
	},
	ScopeUSAll: {
		Markets:         []string{"US"},
		InstrumentTypes: []string{"us_stock"},
	},
}

// ScopeForMarket maps a market code to its directory scope.
func ScopeForMarket(market string) string {
	switch strings.ToUpper(strings.TrimSpace(market)) {
	case "CN":
		return ScopeCNAll
	case "HK":
		return ScopeHKAll
	case "US":
		return ScopeUSAll
	}
	return ""
}

// AssetDirectorySyncPayload is the worker payload for asset_directory_sync.
type AssetDirectorySyncPayload struct {
	Scope           string   `json:"scope"`
	Markets         []string `json:"markets"`
	InstrumentTypes []string `json:"instrument_types"`
	Force           bool     `json:"force"`
}

// AssetHistorySyncPayload is the worker payload for asset_history_sync. Go
// derives it from the narrow frontend request; the sidecar never sees "mode".
type AssetHistorySyncPayload struct {
	AssetKey           string `json:"asset_key"`
	Market             string `json:"market"`
	InstrumentType     string `json:"instrument_type"`
	RegionCode         string `json:"region_code"`
	Symbol             string `json:"symbol"`
	InstrumentKind     string `json:"instrument_kind"`
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
	sql    *sql.DB
	tasks  *repository.WorkerTaskRepo
	assets *repository.MarketAssetRepo
}

func NewMarketAssetService(
	sqlDB *sql.DB,
	tasks *repository.WorkerTaskRepo,
	assets *repository.MarketAssetRepo,
) *MarketAssetService {
	return &MarketAssetService{sql: sqlDB, tasks: tasks, assets: assets}
}

// --- task views ---

// WorkerTaskView is the API shape of a worker task (payload/result omitted).
type WorkerTaskView struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    int64  `json:"created_at"`
	StartedAt    *int64 `json:"started_at,omitempty"`
	FinishedAt   *int64 `json:"finished_at,omitempty"`
	HeartbeatAt  *int64 `json:"heartbeat_at,omitempty"`
}

func taskToView(t repository.WorkerTask) WorkerTaskView {
	return WorkerTaskView{
		ID:           t.ID,
		Type:         t.Type,
		Status:       t.Status,
		ErrorCode:    t.ErrorCode,
		ErrorMessage: t.ErrorMessage,
		CreatedAt:    t.CreatedAt,
		StartedAt:    t.StartedAt,
		FinishedAt:   t.FinishedAt,
		HeartbeatAt:  t.HeartbeatAt,
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

// MarketAssetSyncView is the directory sync status block returned to the UI.
type MarketAssetSyncView struct {
	Scope             string          `json:"scope"`
	Task              *WorkerTaskView `json:"task,omitempty"`
	LastSuccessAt     *int64          `json:"last_success_at,omitempty"`
	LastSuccessTaskID string          `json:"last_success_task_id"`
}

// MarketAssetListResult is the GET /market-assets response.
type MarketAssetListResult struct {
	Assets []repository.MarketAsset `json:"assets"`
	Sync   *MarketAssetSyncView     `json:"sync,omitempty"`
	Syncs  []MarketAssetSyncView    `json:"syncs"`
	Total  int                      `json:"total"`
}

// MarketAssetListParams filters the directory listing.
type MarketAssetListParams struct {
	Market          string
	InstrumentTypes []string
	Query           string
	IncludeInactive bool
	Limit           int
	Offset          int
}

func (s *MarketAssetService) buildSyncView(ctx context.Context, scope string) (MarketAssetSyncView, error) {
	view := MarketAssetSyncView{Scope: scope}
	st, ok, err := s.assets.GetSyncState(ctx, scope)
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

// ListAssets searches the local market asset directory and attaches the sync
// status for the requested market's scope (or all scopes without a market
// filter). It never triggers remote calls.
func (s *MarketAssetService) ListAssets(
	ctx context.Context, params MarketAssetListParams,
) (MarketAssetListResult, error) {
	market := strings.ToUpper(strings.TrimSpace(params.Market))
	assets, err := s.assets.Search(ctx, repository.MarketAssetSearchOptions{
		Market:          market,
		InstrumentTypes: params.InstrumentTypes,
		Query:           params.Query,
		IncludeInactive: params.IncludeInactive,
		Limit:           params.Limit,
		Offset:          params.Offset,
	})
	if err != nil {
		return MarketAssetListResult{}, wrapRepo("search market assets", err)
	}
	out := MarketAssetListResult{Assets: assets, Total: len(assets)}
	if assets == nil {
		out.Assets = []repository.MarketAsset{}
	}

	scopes := []string{ScopeCNAll, ScopeHKAll, ScopeUSAll}
	if scope := ScopeForMarket(market); scope != "" {
		scopes = []string{scope}
	}
	for _, scope := range scopes {
		view, err := s.buildSyncView(ctx, scope)
		if err != nil {
			return MarketAssetListResult{}, err
		}
		out.Syncs = append(out.Syncs, view)
	}
	if len(out.Syncs) > 0 {
		out.Sync = &out.Syncs[0]
	}
	return out, nil
}

// --- directory sync task creation ---

// DirectorySyncRequest is the POST /market-assets/sync body.
type DirectorySyncRequest struct {
	Scope           string   `json:"scope"`
	Markets         []string `json:"markets"`
	InstrumentTypes []string `json:"instrument_types"`
	Force           bool     `json:"force"`
}

// TaskCreateResult reports the task the caller should poll plus whether it
// already existed (active duplicate).
type TaskCreateResult struct {
	Task    WorkerTaskView `json:"task"`
	Existed bool           `json:"existed"`
}

func directoryDedupeKey(p AssetDirectorySyncPayload) string {
	return strings.Join([]string{
		repository.WorkerTaskTypeAssetDirectorySync,
		p.Scope,
		strings.Join(p.Markets, ","),
		strings.Join(p.InstrumentTypes, ","),
	}, "|")
}

// SyncDirectory creates (or returns the existing active) asset_directory_sync
// task for the requested scope.
func (s *MarketAssetService) SyncDirectory(
	ctx context.Context, req DirectorySyncRequest,
) (TaskCreateResult, error) {
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	def, ok := scopeDefinitions[scope]
	if !ok {
		return TaskCreateResult{}, newErr("invalid_request",
			"scope must be one of cn_all, hk_all, us_all", nil)
	}
	payload := AssetDirectorySyncPayload{
		Scope:           scope,
		Markets:         def.Markets,
		InstrumentTypes: def.InstrumentTypes,
		Force:           req.Force,
	}
	dedupeKey := directoryDedupeKey(payload)
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return TaskCreateResult{}, fmt.Errorf("marshal directory payload: %w", err)
	}

	return s.createTask(ctx, repository.WorkerTaskTypeAssetDirectorySync, dedupeKey, string(payloadJSON),
		func(ctx context.Context, tx *sql.Tx, taskID string) error {
			return s.assets.SetSyncLastTaskTx(ctx, tx, scope, taskID)
		})
}

// createTask creates a worker task with dedupe semantics: when an active task
// with the same (type, dedupe_key) exists, it is returned unchanged and no new
// task is created. onCreated runs in the same transaction as the insert.
func (s *MarketAssetService) createTask(
	ctx context.Context,
	taskType, dedupeKey, payloadJSON string,
	onCreated func(ctx context.Context, tx *sql.Tx, taskID string) error,
) (TaskCreateResult, error) {
	if existing, err := s.tasks.FindActiveByDedupe(ctx, taskType, dedupeKey); err == nil {
		return TaskCreateResult{Task: taskToView(existing), Existed: true}, nil
	} else if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
		return TaskCreateResult{}, wrapRepo("find active task", err)
	}

	task := repository.WorkerTask{
		ID:          "wt_" + uuid.New().String(),
		Type:        taskType,
		Status:      repository.WorkerTaskStatusPending,
		DedupeKey:   dedupeKey,
		PayloadJSON: payloadJSON,
		CreatedAt:   time.Now().UnixMilli(),
	}
	var duplicate repository.WorkerTask
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.tasks.CreateTx(ctx, tx, &task); err != nil {
			if repository.IsWorkerTaskUniqueConstraint(err) {
				// Lost the race: another request created the active task first.
				existing, findErr := s.tasks.FindActiveByDedupeTx(ctx, tx, taskType, dedupeKey)
				if findErr != nil {
					return fmt.Errorf("find duplicate active task: %w", findErr)
				}
				duplicate = existing
				return nil
			}
			return err
		}
		if onCreated != nil {
			return onCreated(ctx, tx, task.ID)
		}
		return nil
	})
	if err != nil {
		return TaskCreateResult{}, wrapRepo("create worker task", err)
	}
	if duplicate.ID != "" {
		return TaskCreateResult{Task: taskToView(duplicate), Existed: true}, nil
	}
	created, err := s.tasks.GetByID(ctx, task.ID)
	if err != nil {
		return TaskCreateResult{}, wrapRepo("load created task", err)
	}
	return TaskCreateResult{Task: taskToView(created)}, nil
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
	CanSwitchSource bool `json:"can_switch_source"`
}

// MarketAssetPointView is one (date, value) observation for charts.
type MarketAssetPointView struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// MarketAssetDetail is the GET /market-assets/by-key response.
type MarketAssetDetail struct {
	Asset           repository.MarketAsset  `json:"asset"`
	History         MarketAssetHistoryView  `json:"history"`
	Points          []MarketAssetPointView  `json:"points"`
	AnnualReturns   json.RawMessage         `json:"annual_returns"`
	TrailingReturns json.RawMessage         `json:"trailing_returns,omitempty"`
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

	adjustPolicy, pointType, err = s.resolveHistoryDimension(ctx, asset, adjustPolicy, pointType)
	if err != nil {
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
		detail.History.DataAsOf = st.DataAsOf
		detail.History.PointCount = st.PointCount
		detail.History.SourceName = st.SourceName
		detail.History.LastSuccessAt = st.LastSuccessAt
		detail.History.LastSuccessTaskID = st.LastSuccessTaskID
		if st.LastTaskID != "" {
			task, err := s.tasks.GetByID(ctx, st.LastTaskID)
			if err == nil {
				v := taskToView(task)
				detail.History.Task = &v
				detail.History.CanSwitchSource = canSwitchSource(task)
			} else if !errors.Is(err, repository.ErrWorkerTaskNotFound) {
				return MarketAssetDetail{}, wrapRepo("load history task", err)
			}
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
		if proj.AnnualReturnsJSON != "" {
			detail.AnnualReturns = json.RawMessage(proj.AnnualReturnsJSON)
		}
		if proj.TrailingReturnsJSON != "" && proj.TrailingReturnsJSON != "{}" {
			detail.TrailingReturns = json.RawMessage(proj.TrailingReturnsJSON)
		}
	}
	return detail, nil
}

// resolveHistoryDimension picks the history dimension: explicit params win,
// then the asset's existing history state, then type defaults.
func (s *MarketAssetService) resolveHistoryDimension(
	ctx context.Context, asset repository.MarketAsset, adjustPolicy, pointType string,
) (string, string, error) {
	adjustPolicy = strings.TrimSpace(adjustPolicy)
	pointType = strings.TrimSpace(pointType)
	if adjustPolicy != "" && pointType != "" {
		return adjustPolicy, pointType, nil
	}
	states, err := s.assets.ListHistoryStatesByAsset(ctx, asset.AssetKey)
	if err != nil {
		return "", "", wrapRepo("list history states", err)
	}
	if len(states) > 0 {
		st := states[0]
		if adjustPolicy == "" {
			adjustPolicy = st.AdjustPolicy
		}
		if pointType == "" {
			pointType = st.PointType
		}
		return adjustPolicy, pointType, nil
	}
	if adjustPolicy == "" {
		adjustPolicy = "none"
	}
	if pointType == "" {
		pointType = DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
	}
	return adjustPolicy, pointType, nil
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
	req.AssetKey = strings.TrimSpace(req.AssetKey)
	req.AdjustPolicy = strings.TrimSpace(req.AdjustPolicy)
	req.PointType = strings.TrimSpace(req.PointType)
	req.Mode = strings.TrimSpace(req.Mode)
	if req.AssetKey == "" {
		return TaskCreateResult{}, newErr("invalid_request", "asset_key is required", nil)
	}
	if req.Mode != historyModeDefaultRefresh && req.Mode != historyModeSwitchSourceFull {
		return TaskCreateResult{}, newErr("invalid_request",
			"mode must be default_refresh or switch_source_full", nil)
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
		req.AdjustPolicy = "none"
	}
	if req.PointType == "" {
		req.PointType = DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
	}

	payload, err := s.buildHistoryPayload(ctx, asset, req)
	if err != nil {
		return TaskCreateResult{}, err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return TaskCreateResult{}, fmt.Errorf("marshal history payload: %w", err)
	}
	return s.createTask(ctx, repository.WorkerTaskTypeAssetHistorySync,
		historyDedupeKey(payload), string(payloadJSON),
		func(ctx context.Context, tx *sql.Tx, taskID string) error {
			return s.assets.SetHistoryLastTaskTx(ctx, tx,
				req.AssetKey, req.AdjustPolicy, req.PointType, taskID)
		})
}

func (s *MarketAssetService) buildHistoryPayload(
	ctx context.Context, asset repository.MarketAsset, req HistorySyncRequest,
) (AssetHistorySyncPayload, error) {
	base := AssetHistorySyncPayload{
		AssetKey:       asset.AssetKey,
		Market:         asset.Market,
		InstrumentType: asset.InstrumentType,
		RegionCode:     asset.RegionCode,
		Symbol:         asset.Symbol,
		InstrumentKind: asset.InstrumentKind,
		AdjustPolicy:   req.AdjustPolicy,
		PointType:      req.PointType,
	}

	st, hasState, err := s.assets.GetHistoryState(ctx, req.AssetKey, req.AdjustPolicy, req.PointType)
	if err != nil {
		return base, wrapRepo("load history state", err)
	}
	hasHistory := hasState && st.SourceName != "" && st.PointCount > 0 && st.DataAsOf != ""

	if req.Mode == historyModeSwitchSourceFull {
		if !hasState || st.LastTaskID == "" {
			return base, newErr("invalid_request",
				"switch_source_full requires a prior failed same-source refresh", nil)
		}
		lastTask, err := s.tasks.GetByID(ctx, st.LastTaskID)
		if err != nil {
			if errors.Is(err, repository.ErrWorkerTaskNotFound) {
				return base, newErr("invalid_request",
					"switch_source_full requires a prior failed same-source refresh", nil)
			}
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

func (s *MarketAssetService) hasMixedSources(
	ctx context.Context, assetKey, adjustPolicy, pointType string,
) (bool, error) {
	var mixed bool
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		summary, err := s.assets.PointsSummaryTx(ctx, tx, assetKey, adjustPolicy, pointType)
		if err != nil {
			return err
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
	return s.createTask(ctx, repository.WorkerTaskTypeFXRateSync,
		fxDedupeKey(payload), string(payloadJSON), nil)
}
