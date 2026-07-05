package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// Readiness reasons for a holding that blocks simulation.
const (
	ReadinessReasonHistoryMissing                = "history_missing"
	ReadinessReasonHistorySyncRunning            = "history_sync_running"
	ReadinessReasonSimulationInsufficientHistory = "simulation_insufficient_history"
	ReadinessReasonProviderDataAnomaly           = "provider_data_anomaly"
	ReadinessReasonAssetIdentityConflict         = "asset_identity_conflict"
)

// BlockingAsset is one plan holding whose market asset blocks simulation.
// Not every blocked asset is missing history: an asset can be fully synced
// yet fail snapshot admission (data anomaly, wrong asset identity, or not
// enough complete years).
type BlockingAsset struct {
	HoldingID string `json:"holding_id"`
	AssetKey  string `json:"asset_key"`
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	Message   string `json:"message,omitempty"`
	// CandidateAssetKeys lists better-matching directory identities when the
	// reason is asset_identity_conflict.
	CandidateAssetKeys []string `json:"candidate_asset_keys,omitempty"`
}

// SimulationReadinessView is the GET /plans/{id}/simulation-readiness response.
type SimulationReadinessView struct {
	Ready          bool             `json:"ready"`
	BlockingAssets []BlockingAsset  `json:"blocking_assets"`
	ActiveTasks    []WorkerTaskView `json:"active_tasks"`
}

// SyncMissingAssetEntry pairs an asset with its history sync task.
type SyncMissingAssetEntry struct {
	AssetKey string          `json:"asset_key"`
	Task     *WorkerTaskView `json:"task,omitempty"`
}

// SyncMissingBlockedEntry is an asset for which no sync task was created
// because syncing again would not make it simulatable.
type SyncMissingBlockedEntry struct {
	AssetKey           string   `json:"asset_key"`
	Reason             string   `json:"reason"`
	Message            string   `json:"message,omitempty"`
	CandidateAssetKeys []string `json:"candidate_asset_keys,omitempty"`
}

// SyncMissingHistoryResult is the POST /plans/{id}/sync-missing-asset-history
// response: tasks created now, active tasks reused, assets already ready and
// assets blocked for reasons a new sync cannot fix.
type SyncMissingHistoryResult struct {
	Created  []SyncMissingAssetEntry   `json:"created"`
	Existing []SyncMissingAssetEntry   `json:"existing"`
	Ready    []SyncMissingAssetEntry   `json:"ready"`
	Blocked  []SyncMissingBlockedEntry `json:"blocked"`
}

// assetProbe is the readiness verdict for one market asset. Empty Reason
// means the asset can build its simulation snapshot right now.
type assetProbe struct {
	Reason             string
	Message            string
	CandidateAssetKeys []string
}

// SimulationReadinessService checks whether every enabled plan holding can
// build its simulation snapshot from local market asset history, and
// batch-creates history sync tasks for the ones that are actually missing
// history.
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
		BlockingAssets: []BlockingAsset{},
		ActiveTasks:    []WorkerTaskView{},
	}
	checkedAssets := make(map[string]assetProbe)
	seenTask := make(map[string]struct{})
	for _, h := range holds {
		if !h.Enabled || h.SimulationSnapshotID != "" {
			continue
		}
		probe, ok := checkedAssets[h.AssetKey]
		if !ok {
			probe, err = s.probeAsset(ctx, plan, h.AssetKey)
			if err != nil {
				return SimulationReadinessView{}, err
			}
			checkedAssets[h.AssetKey] = probe
		}
		if probe.Reason == "" {
			continue
		}
		view.Ready = false
		view.BlockingAssets = append(view.BlockingAssets, BlockingAsset{
			HoldingID: h.ID, AssetKey: h.AssetKey,
			Symbol: h.InstrumentCode, Name: h.InstrumentName,
			Reason: probe.Reason, Message: probe.Message,
			CandidateAssetKeys: probe.CandidateAssetKeys,
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
// the failure to a readiness reason. Snapshot admission is the single source
// of truth: having history points is not enough to be ready.
func (s *SimulationReadinessService) probeAsset(
	ctx context.Context, plan repository.Plan, assetKey string,
) (assetProbe, error) {
	_, err := s.snapSvc.BuildSnapshotForHolding(ctx, plan.ID, assetKey, plan.ValuationDate)
	if err == nil {
		return assetProbe{}, nil
	}
	var snapErr *marketdata.SnapshotError
	if !errors.As(err, &snapErr) {
		return assetProbe{}, wrapRepo("probe asset readiness", err)
	}
	// A pending/running history sync may change the verdict, so report it as
	// in-flight instead of a terminal blocked state.
	if _, running := s.activeHistoryTask(ctx, assetKey); running {
		return assetProbe{
			Reason:  ReadinessReasonHistorySyncRunning,
			Message: "历史同步任务进行中，完成后将自动重新检查",
		}, nil
	}
	if snapErr.Code == "asset_history_missing" {
		return assetProbe{
			Reason:  ReadinessReasonHistoryMissing,
			Message: "该标的尚未同步历史数据",
		}, nil
	}

	// Local history exists but the snapshot cannot be built. Distinguish
	// wrong-identity and data-anomaly cases from plain short history.
	anomaly := snapshotFailureIsAnomaly(snapErr)
	current, candidates := s.identityCandidates(ctx, assetKey, anomaly)
	if len(candidates) > 0 {
		return assetProbe{
			Reason:             ReadinessReasonAssetIdentityConflict,
			Message:            identityConflictMessage(current, candidates),
			CandidateAssetKeys: candidateKeys(candidates),
		}, nil
	}
	if anomaly {
		return assetProbe{
			Reason:  ReadinessReasonProviderDataAnomaly,
			Message: "历史已同步，但数据质量异常，暂不可用于模拟",
		}, nil
	}
	return assetProbe{
		Reason:  ReadinessReasonSimulationInsufficientHistory,
		Message: snapErr.Message,
	}, nil
}

// snapshotFailureIsAnomaly reports whether the snapshot failure stems from a
// data-quality/metric anomaly (as opposed to plainly short history).
func snapshotFailureIsAnomaly(e *marketdata.SnapshotError) bool {
	if e == nil || e.Details == nil {
		return false
	}
	if q, ok := e.Details["quality_status"].(string); ok &&
		q == marketdata.QualityStatusProviderDataAnomaly {
		return true
	}
	for _, key := range []string{"cagr_status", "volatility_status", "drawdown_status"} {
		st, ok := e.Details[key].(string)
		if !ok {
			continue
		}
		if st == marketdata.MetricStatusInvalidMetricValue ||
			st == marketdata.MetricStatusProviderDataAnomaly {
			return true
		}
	}
	return false
}

// identityCandidates finds other active directory rows with the same
// market+symbol that likely represent the intended asset. Name equality is
// the primary signal; the mutual-vs-exchange fund pattern additionally
// qualifies when the current asset failed with a data anomaly (a money-market
// fund fetched under an exchange-traded identity produces anomalous series).
func (s *SimulationReadinessService) identityCandidates(
	ctx context.Context, assetKey string, anomaly bool,
) (repository.MarketAsset, []repository.MarketAsset) {
	current, err := s.assetRepo.GetByKey(ctx, assetKey)
	if err != nil {
		return repository.MarketAsset{}, nil
	}
	siblings, err := s.assetRepo.ListAssetsByMarketSymbol(ctx, current.Market, current.Symbol)
	if err != nil {
		return current, nil
	}
	var out []repository.MarketAsset
	for _, cand := range siblings {
		if cand.AssetKey == current.AssetKey {
			continue
		}
		nameMatch := namesRoughlyEqual(current.Name, cand.Name)
		typePattern := current.InstrumentType == "cn_exchange_fund" &&
			cand.InstrumentType == "cn_mutual_fund"
		if nameMatch || (typePattern && anomaly) {
			out = append(out, cand)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return instrumentTypePriority(out[i].InstrumentType) <
			instrumentTypePriority(out[j].InstrumentType)
	})
	return current, out
}

// instrumentTypePriority orders identity-conflict candidates: mutual funds
// first, then exchange funds and stocks, then everything else.
func instrumentTypePriority(t string) int {
	switch t {
	case "cn_mutual_fund":
		return 0
	case "cn_exchange_fund":
		return 1
	case "cn_exchange_stock":
		return 2
	default:
		return 3
	}
}

func candidateKeys(assets []repository.MarketAsset) []string {
	keys := make([]string, len(assets))
	for i, a := range assets {
		keys[i] = a.AssetKey
	}
	return keys
}

func identityConflictMessage(current repository.MarketAsset, candidates []repository.MarketAsset) string {
	curLabel := instrumentTypeLabelZH(current.InstrumentType)
	best := candidates[0]
	candDesc := instrumentTypeLabelZH(best.InstrumentType)
	if best.Name != "" {
		candDesc = best.Name + " · " + candDesc
	}
	return fmt.Sprintf(
		"该代码存在多个资产身份，当前「%s」历史已同步但无法用于模拟，建议在持仓校正中切换为：%s",
		curLabel, candDesc,
	)
}

// instrumentTypeLabelZH renders a user-facing label for a directory
// instrument type inside backend messages. Labels match the web UI's
// instrumentTypeLabel so advice and pickers use the same wording.
func instrumentTypeLabelZH(t string) string {
	switch t {
	case "cn_mutual_fund":
		return "公募基金"
	case "cn_exchange_fund":
		return "场内 ETF / LOF"
	case "cn_exchange_stock":
		return "A 股"
	case "hk_stock":
		return "港股"
	case "hk_etf":
		return "香港 ETF"
	case "us_stock":
		return "美国股票"
	case "us_etf":
		return "美国 ETF"
	default:
		return t
	}
}

// normalizeAssetName strips whitespace and upper-cases for a tolerant
// name comparison across directory sources.
func normalizeAssetName(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}

// namesRoughlyEqual reports whether two directory names refer to the same
// underlying asset: equal after normalization, or one contains the other
// (sources differ in suffixes like share-class markers).
func namesRoughlyEqual(a, b string) bool {
	na, nb := normalizeAssetName(a), normalizeAssetName(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || strings.Contains(na, nb) || strings.Contains(nb, na)
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
// for enabled holding assets that are actually missing history. Snapshot
// admission is the ready criterion; assets whose history is synced but not
// simulatable come back as blocked instead of getting useless new tasks.
func (s *SimulationReadinessService) SyncMissingHistory(
	ctx context.Context, planID string,
) (SyncMissingHistoryResult, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
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
		Blocked:  []SyncMissingBlockedEntry{},
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
		if err := s.syncOneAsset(ctx, plan, h.AssetKey, &out); err != nil {
			return SyncMissingHistoryResult{}, err
		}
	}
	return out, nil
}

func (s *SimulationReadinessService) syncOneAsset(
	ctx context.Context, plan repository.Plan, assetKey string,
	out *SyncMissingHistoryResult,
) error {
	asset, err := s.assetRepo.GetByKey(ctx, assetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return newErr("market_asset_not_found",
				fmt.Sprintf("market asset %s not found", assetKey), nil)
		}
		return wrapRepo("load market asset", err)
	}
	probe, err := s.probeAsset(ctx, plan, assetKey)
	if err != nil {
		return err
	}
	switch probe.Reason {
	case "":
		out.Ready = append(out.Ready, SyncMissingAssetEntry{AssetKey: assetKey})
		return nil
	case ReadinessReasonHistorySyncRunning:
		entry := SyncMissingAssetEntry{AssetKey: assetKey}
		if task, ok := s.activeHistoryTask(ctx, assetKey); ok {
			entry.Task = &task
		}
		out.Existing = append(out.Existing, entry)
		return nil
	case ReadinessReasonHistoryMissing:
		res, err := s.assetSvc.SyncHistory(ctx, HistorySyncRequest{
			AssetKey:     assetKey,
			AdjustPolicy: "none",
			PointType:    DefaultPointType(asset.InstrumentType, asset.InstrumentKind),
			Mode:         historyModeDefaultRefresh,
		})
		if err != nil {
			return err
		}
		task := res.Task
		entry := SyncMissingAssetEntry{AssetKey: assetKey, Task: &task}
		if res.Existed {
			out.Existing = append(out.Existing, entry)
			return nil
		}
		out.Created = append(out.Created, entry)
		return nil
	default:
		// History is synced but a new sync cannot fix the admission failure.
		out.Blocked = append(out.Blocked, SyncMissingBlockedEntry{
			AssetKey:           assetKey,
			Reason:             probe.Reason,
			Message:            probe.Message,
			CandidateAssetKeys: probe.CandidateAssetKeys,
		})
		return nil
	}
}
