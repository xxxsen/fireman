package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// Readiness reasons for a holding that blocks simulation.
const (
	ReadinessReasonHistoryMissing      = "history_missing"
	ReadinessReasonInsufficientHistory = "insufficient_history"
)

// MissingHistoryItem is one plan holding whose market asset blocks simulation.
type MissingHistoryItem struct {
	HoldingID string `json:"holding_id"`
	AssetKey  string `json:"asset_key"`
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}

// SimulationReadinessView is the GET /plans/{id}/simulation-readiness response.
type SimulationReadinessView struct {
	Ready          bool                 `json:"ready"`
	MissingHistory []MissingHistoryItem `json:"missing_history"`
	ActiveTasks    []WorkerTaskView     `json:"active_tasks"`
}

// SyncMissingAssetEntry pairs an asset with its history sync task.
type SyncMissingAssetEntry struct {
	AssetKey string          `json:"asset_key"`
	Task     *WorkerTaskView `json:"task,omitempty"`
}

// SyncMissingHistoryResult is the POST /plans/{id}/sync-missing-asset-history
// response: tasks created now, active tasks reused, and assets already ready.
type SyncMissingHistoryResult struct {
	Created  []SyncMissingAssetEntry `json:"created"`
	Existing []SyncMissingAssetEntry `json:"existing"`
	Ready    []SyncMissingAssetEntry `json:"ready"`
}

// SimulationReadinessService checks whether every enabled plan holding has
// enough local market asset history to build its simulation snapshot, and
// batch-creates history sync tasks for the ones that do not.
type SimulationReadinessService struct {
	sql       *sql.DB
	plans     *repository.PlanRepo
	holdings  *repository.HoldingsRepo
	assetRepo *repository.MarketAssetRepo
	tasks     *repository.WorkerTaskRepo
	snapSvc   *marketdata.SnapshotService
	assetSvc  *MarketAssetService
}

func NewSimulationReadinessService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	holdings *repository.HoldingsRepo,
	assetRepo *repository.MarketAssetRepo,
	tasks *repository.WorkerTaskRepo,
	snapSvc *marketdata.SnapshotService,
	assetSvc *MarketAssetService,
) *SimulationReadinessService {
	return &SimulationReadinessService{
		sql: sqlDB, plans: plans, holdings: holdings, assetRepo: assetRepo,
		tasks: tasks, snapSvc: snapSvc, assetSvc: assetSvc,
	}
}

// Check reports simulation readiness without mutating anything: holdings with
// a frozen snapshot are ready; holdings saved lazily are probed by building
// (not persisting) their snapshot from current local history.
func (s *SimulationReadinessService) Check(
	ctx context.Context, planID string,
) (SimulationReadinessView, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return SimulationReadinessView{}, newErr("plan_not_found", "plan not found", nil)
		}
		return SimulationReadinessView{}, wrapRepo("load plan", err)
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return SimulationReadinessView{}, wrapRepo("list holdings", err)
	}

	view := SimulationReadinessView{
		Ready:          true,
		MissingHistory: []MissingHistoryItem{},
		ActiveTasks:    []WorkerTaskView{},
	}
	checkedAssets := make(map[string]string) // asset_key -> reason ("" = ready)
	seenTask := make(map[string]struct{})
	for _, h := range holds {
		if !h.Enabled || h.SimulationSnapshotID != "" {
			continue
		}
		reason, ok := checkedAssets[h.AssetKey]
		if !ok {
			reason, err = s.probeAsset(ctx, plan, h.AssetKey)
			if err != nil {
				return SimulationReadinessView{}, err
			}
			checkedAssets[h.AssetKey] = reason
		}
		if reason == "" {
			continue
		}
		view.Ready = false
		view.MissingHistory = append(view.MissingHistory, MissingHistoryItem{
			HoldingID: h.ID, AssetKey: h.AssetKey,
			Symbol: h.InstrumentCode, Name: h.InstrumentName,
			Reason: reason,
		})
		if task, ok := s.activeHistoryTask(ctx, h.AssetKey); ok {
			if _, dup := seenTask[task.ID]; !dup {
				seenTask[task.ID] = struct{}{}
				view.ActiveTasks = append(view.ActiveTasks, task)
			}
		}
	}
	return view, nil
}

// probeAsset builds (without persisting) the snapshot for one asset and maps
// the failure to a readiness reason. Empty reason means the asset is ready.
func (s *SimulationReadinessService) probeAsset(
	ctx context.Context, plan repository.Plan, assetKey string,
) (string, error) {
	_, err := s.snapSvc.BuildSnapshotForHolding(ctx, plan.ID, assetKey, plan.ValuationDate)
	if err == nil {
		return "", nil
	}
	var snapErr *marketdata.SnapshotError
	if errors.As(err, &snapErr) {
		if snapErr.Code == "asset_history_missing" {
			return ReadinessReasonHistoryMissing, nil
		}
		return ReadinessReasonInsufficientHistory, nil
	}
	return "", wrapRepo("probe asset readiness", err)
}

// activeHistoryTask returns the pending/running history sync task recorded on
// any history dimension of the asset.
func (s *SimulationReadinessService) activeHistoryTask(
	ctx context.Context, assetKey string,
) (WorkerTaskView, bool) {
	states, err := s.assetRepo.ListHistoryStatesByAsset(ctx, assetKey)
	if err != nil {
		return WorkerTaskView{}, false
	}
	for _, st := range states {
		if st.LastTaskID == "" {
			continue
		}
		task, err := s.tasks.GetByID(ctx, st.LastTaskID)
		if err != nil {
			continue
		}
		if task.Status == repository.WorkerTaskStatusPending ||
			task.Status == repository.WorkerTaskStatusRunning {
			return taskToView(task), true
		}
	}
	return WorkerTaskView{}, false
}

// EnsureHoldingSnapshots builds and persists snapshots for enabled holdings
// saved lazily (empty simulation_snapshot_id) whose history is now available.
// It runs before simulation creation so the frozen input uses fresh snapshots.
func (s *SimulationReadinessService) EnsureHoldingSnapshots(
	ctx context.Context, planID string,
) error {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return newErr("plan_not_found", "plan not found", nil)
		}
		return wrapRepo("load plan", err)
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return wrapRepo("list holdings", err)
	}
	builtByAsset := make(map[string]repository.SimulationSnapshot)
	persisted := make(map[string]bool)
	for _, h := range holds {
		if !h.Enabled || h.SimulationSnapshotID != "" {
			continue
		}
		snap, ok := builtByAsset[h.AssetKey]
		if !ok {
			built, err := s.snapSvc.BuildSnapshotForHolding(ctx, plan.ID, h.AssetKey, plan.ValuationDate)
			if err != nil {
				var snapErr *marketdata.SnapshotError
				if errors.As(err, &snapErr) {
					// Still not buildable; the readiness gate reports it.
					continue
				}
				return wrapRepo("build holding snapshot", err)
			}
			snap = built
			builtByAsset[h.AssetKey] = built
		}
		holdingID := h.ID
		_, isCash := repository.SystemCashSnapshotIDForAsset(h.AssetKey)
		needPersist := !isCash && !persisted[h.AssetKey]
		err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
			if needPersist {
				if err := s.snapSvc.CreatePlanSnapshotTx(ctx, tx, snap); err != nil {
					return err
				}
			}
			return s.holdings.UpdateSnapshotID(ctx, tx, holdingID, snap.ID)
		})
		if err != nil {
			return wrapRepo("persist holding snapshot", err)
		}
		persisted[h.AssetKey] = true
	}
	return nil
}

// SyncMissingHistory creates (or reuses) default-refresh history sync tasks
// for every enabled holding asset without local history points.
func (s *SimulationReadinessService) SyncMissingHistory(
	ctx context.Context, planID string,
) (SyncMissingHistoryResult, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return SyncMissingHistoryResult{}, newErr("plan_not_found", "plan not found", nil)
		}
		return SyncMissingHistoryResult{}, wrapRepo("load plan", err)
	}
	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return SyncMissingHistoryResult{}, wrapRepo("list holdings", err)
	}

	out := SyncMissingHistoryResult{
		Created:  []SyncMissingAssetEntry{},
		Existing: []SyncMissingAssetEntry{},
		Ready:    []SyncMissingAssetEntry{},
	}
	seen := make(map[string]struct{})
	for _, h := range holds {
		if !h.Enabled {
			continue
		}
		if _, ok := seen[h.AssetKey]; ok {
			continue
		}
		seen[h.AssetKey] = struct{}{}
		if repository.IsSystemCashAssetKey(h.AssetKey) {
			out.Ready = append(out.Ready, SyncMissingAssetEntry{AssetKey: h.AssetKey})
			continue
		}
		entry, bucket, err := s.syncOneAsset(ctx, h.AssetKey)
		if err != nil {
			return SyncMissingHistoryResult{}, err
		}
		switch bucket {
		case "ready":
			out.Ready = append(out.Ready, entry)
		case "existing":
			out.Existing = append(out.Existing, entry)
		default:
			out.Created = append(out.Created, entry)
		}
	}
	return out, nil
}

func (s *SimulationReadinessService) syncOneAsset(
	ctx context.Context, assetKey string,
) (SyncMissingAssetEntry, string, error) {
	asset, err := s.assetRepo.GetByKey(ctx, assetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return SyncMissingAssetEntry{}, "", newErr("market_asset_not_found",
				fmt.Sprintf("market asset %s not found", assetKey), nil)
		}
		return SyncMissingAssetEntry{}, "", wrapRepo("load market asset", err)
	}
	adjustPolicy := "none"
	pointType := DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
	st, ok, err := s.assetRepo.GetHistoryState(ctx, assetKey, adjustPolicy, pointType)
	if err != nil {
		return SyncMissingAssetEntry{}, "", wrapRepo("load history state", err)
	}
	if ok && st.PointCount > 0 {
		return SyncMissingAssetEntry{AssetKey: assetKey}, "ready", nil
	}
	// Another dimension may already hold history (e.g. adjusted series).
	states, err := s.assetRepo.ListHistoryStatesByAsset(ctx, assetKey)
	if err != nil {
		return SyncMissingAssetEntry{}, "", wrapRepo("list history states", err)
	}
	for _, dim := range states {
		if dim.PointCount > 0 {
			return SyncMissingAssetEntry{AssetKey: assetKey}, "ready", nil
		}
	}

	res, err := s.assetSvc.SyncHistory(ctx, HistorySyncRequest{
		AssetKey:     assetKey,
		AdjustPolicy: adjustPolicy,
		PointType:    pointType,
		Mode:         historyModeDefaultRefresh,
	})
	if err != nil {
		return SyncMissingAssetEntry{}, "", err
	}
	task := res.Task
	entry := SyncMissingAssetEntry{AssetKey: assetKey, Task: &task}
	if res.Existed {
		return entry, "existing", nil
	}
	return entry, "created", nil
}
