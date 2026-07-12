package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

const (
	AutoUpdateTargetDirectory = "directory_unit"
	AutoUpdateTargetHistory   = "asset_history"
	autoUpdateBatchSize       = 100
	autoUpdateScanTimeout     = 10 * time.Minute
	autoUpdateScanMinute      = 10
)

type AutoUpdateRuleView struct {
	repository.MarketDataAutoUpdateRule
	TargetLabel string          `json:"target_label"`
	Task        *WorkerTaskView `json:"task,omitempty"`
}

type AutoUpdateListResult struct {
	Items  []AutoUpdateRuleView `json:"items"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

type AutoUpdateListParams struct {
	TargetType string
	Enabled    string
	Query      string
	Limit      int
	Offset     int
}

type AutoUpdateDirectoryUnitView struct {
	SyncKey string `json:"sync_key"`
	Scope   string `json:"scope"`
	Label   string `json:"label"`
}

type AutoUpdateService struct {
	repo   *repository.MarketDataAutoUpdateRepo
	assets *repository.MarketAssetRepo
	market *MarketAssetService
	loc    *time.Location
	now    func() time.Time
}

func NewAutoUpdateService(
	repo *repository.MarketDataAutoUpdateRepo,
	assets *repository.MarketAssetRepo,
	market *MarketAssetService,
	loc *time.Location,
) *AutoUpdateService {
	if loc == nil {
		loc = time.UTC
	}
	return &AutoUpdateService{
		repo: repo, assets: assets, market: market, loc: loc, now: time.Now,
	}
}

// nextAlignedSlot returns the next crontab-aligned execution time after `after`
// for the given interval. Slots are aligned to wall-clock boundaries:
//   - <24h: fires at multiples of intervalHours within each day, at :10
//   - 24h: daily at 00:10
//   - >24h: every N days at 00:10, day-aligned via epoch modulo
func nextAlignedSlot(after time.Time, intervalHours int, loc *time.Location) time.Time {
	local := after.In(loc)

	if intervalHours < 24 {
		dayStart := time.Date(local.Year(), local.Month(), local.Day(),
			0, autoUpdateScanMinute, 0, 0, loc)
		interval := time.Duration(intervalHours) * time.Hour
		slot := dayStart
		for !slot.After(after) {
			slot = slot.Add(interval)
		}
		return slot
	}

	days := intervalHours / 24
	todaySlot := time.Date(local.Year(), local.Month(), local.Day(),
		0, autoUpdateScanMinute, 0, 0, loc)

	if days == 1 {
		if todaySlot.After(after) {
			return todaySlot
		}
		return todaySlot.AddDate(0, 0, 1)
	}

	ref := time.Date(2000, 1, 1, 0, 0, 0, 0, loc)
	for d := 0; d <= days; d++ {
		candidate := todaySlot.AddDate(0, 0, d)
		dayNum := int(candidate.Sub(ref).Hours()/24 + 0.5)
		if candidate.After(after) && dayNum%days == 0 {
			return candidate
		}
	}
	return todaySlot.AddDate(0, 0, days)
}

// nextScanTime returns the next local wall-clock slot, strictly after now.
// Every local day is anchored at 00:10; rebuilding candidates with time.Date
// avoids elapsed-time drift and preserves the configured wall-clock schedule
// across daylight-saving transitions.
func nextScanTime(now time.Time, intervalMinutes int, loc *time.Location) time.Time {
	local := now.In(loc)
	for dayOffset := 0; dayOffset <= 2; dayOffset++ {
		date := time.Date(local.Year(), local.Month(), local.Day()+dayOffset, 0, 0, 0, 0, loc)
		for minuteOffset := autoUpdateScanMinute; minuteOffset < 24*60; minuteOffset += intervalMinutes {
			candidate := time.Date(
				date.Year(), date.Month(), date.Day(), minuteOffset/60, minuteOffset%60, 0, 0, loc,
			)
			if candidate.After(now) {
				return candidate
			}
		}
	}
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, autoUpdateScanMinute, 0, 0, loc)
}

func (s *AutoUpdateService) DirectoryUnits() []AutoUpdateDirectoryUnitView {
	items := make([]AutoUpdateDirectoryUnitView, 0, len(directorySyncUnits))
	for _, unit := range directorySyncUnits {
		items = append(items, AutoUpdateDirectoryUnitView{
			SyncKey: unit.SyncKey,
			Scope:   unit.Scope,
			Label:   unit.Label,
		})
	}
	return items
}

func validInterval(hours int) error {
	if hours < 1 || hours > 168 {
		return newErr("invalid_request", "interval_hours must be between 1 and 168", nil)
	}
	return nil
}

func (s *AutoUpdateService) List(
	ctx context.Context,
	params AutoUpdateListParams,
) (AutoUpdateListResult, error) {
	if err := validateAutoUpdateListParams(&params); err != nil {
		return AutoUpdateListResult{}, err
	}
	items, total, err := s.repo.List(ctx, repository.MarketDataAutoUpdateFilter{
		TargetType: params.TargetType,
		Enabled:    params.Enabled,
		Query:      params.Query,
		Limit:      params.Limit,
		Offset:     params.Offset,
	})
	if err != nil {
		return AutoUpdateListResult{}, wrapRepo("list automatic update rules", err)
	}

	views := make([]AutoUpdateRuleView, 0, len(items))
	for _, rule := range items {
		view, viewErr := s.ruleView(ctx, rule)
		if viewErr != nil {
			return AutoUpdateListResult{}, viewErr
		}
		views = append(views, view)
	}
	return AutoUpdateListResult{
		Items: views, Total: total, Limit: params.Limit, Offset: params.Offset,
	}, nil
}

func validateAutoUpdateListParams(params *AutoUpdateListParams) error {
	if params.TargetType != "" &&
		params.TargetType != AutoUpdateTargetDirectory &&
		params.TargetType != AutoUpdateTargetHistory {
		return newErr("invalid_request", "invalid target_type", nil)
	}
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}
	if params.Offset < 0 {
		return newErr("invalid_request", "offset must be >= 0", nil)
	}
	switch params.Enabled {
	case "", "true", "false", "failed":
		return nil
	default:
		return newErr("invalid_request", "enabled must be true, false or failed", nil)
	}
}

func (s *AutoUpdateService) ruleView(
	ctx context.Context,
	rule repository.MarketDataAutoUpdateRule,
) (AutoUpdateRuleView, error) {
	label, err := s.ruleTargetLabel(ctx, rule)
	if err != nil {
		return AutoUpdateRuleView{}, err
	}
	task, hasTask, err := s.ruleTask(ctx, rule.LastTaskID)
	if err != nil {
		return AutoUpdateRuleView{}, err
	}
	view := AutoUpdateRuleView{
		MarketDataAutoUpdateRule: rule,
		TargetLabel:              label,
	}
	if hasTask {
		view.Task = &task
	}
	return view, nil
}

func (s *AutoUpdateService) ruleTargetLabel(
	ctx context.Context,
	rule repository.MarketDataAutoUpdateRule,
) (string, error) {
	if rule.TargetType == AutoUpdateTargetDirectory {
		if unit, ok := DirectoryUnitBySyncKey(rule.SyncKey); ok {
			return unit.Label, nil
		}
		return rule.SyncKey, nil
	}
	asset, err := s.assets.GetByKey(ctx, rule.AssetKey)
	if errors.Is(err, repository.ErrMarketAssetNotFound) {
		return rule.AssetKey, nil
	}
	if err != nil {
		return "", wrapRepo("load automatic update asset", err)
	}
	if strings.TrimSpace(asset.Name) == "" {
		return rule.AssetKey, nil
	}
	return asset.Name + " (" + rule.AssetKey + ")", nil
}

func (s *AutoUpdateService) ruleTask(
	ctx context.Context,
	taskID string,
) (WorkerTaskView, bool, error) {
	if taskID == "" {
		return WorkerTaskView{}, false, nil
	}
	task, err := s.market.GetTask(ctx, taskID)
	if err == nil {
		return task, true, nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) && appErr.Code == "task_not_found" {
		return WorkerTaskView{}, false, nil
	}
	return WorkerTaskView{}, false, err
}

func (s *AutoUpdateService) CreateDirectory(
	ctx context.Context,
	syncKey string,
	intervalHours int,
) (AutoUpdateRuleView, error) {
	syncKey = strings.ToLower(strings.TrimSpace(syncKey))
	if _, ok := DirectoryUnitBySyncKey(syncKey); !ok {
		return AutoUpdateRuleView{}, newErr(
			"invalid_request", "unknown sync_key "+syncKey, nil,
		)
	}
	if err := validInterval(intervalHours); err != nil {
		return AutoUpdateRuleView{}, err
	}
	now := s.now()
	nextRunAt := nextAlignedSlot(now, intervalHours, s.loc).UnixMilli()
	rule, err := s.repo.UpsertDirectory(
		ctx, syncKey, intervalHours, now.UnixMilli(), nextRunAt,
	)
	if err != nil {
		return AutoUpdateRuleView{}, wrapRepo("upsert directory automatic update", err)
	}
	return s.ruleView(ctx, rule)
}

func (s *AutoUpdateService) SetHistory(
	ctx context.Context,
	assetKey string,
	adjustPolicy string,
	pointType string,
	enabled bool,
) (AutoUpdateRuleView, error) {
	assetKey = strings.TrimSpace(assetKey)
	adjustPolicy = strings.TrimSpace(adjustPolicy)
	pointType = strings.TrimSpace(pointType)
	if assetKey == "" || adjustPolicy == "" || pointType == "" {
		return AutoUpdateRuleView{}, newErr(
			"invalid_request",
			"asset_key, adjust_policy and point_type are required",
			nil,
		)
	}
	asset, err := s.assets.GetByKey(ctx, assetKey)
	if errors.Is(err, repository.ErrMarketAssetNotFound) {
		return AutoUpdateRuleView{}, newErr(
			"market_asset_not_found", "market asset not found", nil,
		)
	}
	if err != nil {
		return AutoUpdateRuleView{}, wrapRepo("load market asset", err)
	}
	if !asset.Active {
		return AutoUpdateRuleView{}, newErr(
			"invalid_request",
			"inactive market asset cannot be configured for automatic update",
			nil,
		)
	}
	if err := validateAutoUpdateHistoryDimension(asset, adjustPolicy, pointType); err != nil {
		return AutoUpdateRuleView{}, err
	}
	if enabled {
		return s.enableHistory(ctx, assetKey, adjustPolicy, pointType)
	}
	return s.disableHistory(ctx, assetKey, adjustPolicy, pointType)
}

func (s *AutoUpdateService) enableHistory(
	ctx context.Context,
	assetKey string,
	adjustPolicy string,
	pointType string,
) (AutoUpdateRuleView, error) {
	now := s.now()
	nextRunAt := nextAlignedSlot(now, 24, s.loc).UnixMilli()
	rule, err := s.repo.EnableHistory(
		ctx, assetKey, adjustPolicy, pointType, now.UnixMilli(), nextRunAt,
	)
	if err != nil {
		return AutoUpdateRuleView{}, wrapRepo("enable history automatic update", err)
	}
	return s.ruleView(ctx, rule)
}

func (s *AutoUpdateService) disableHistory(
	ctx context.Context,
	assetKey string,
	adjustPolicy string,
	pointType string,
) (AutoUpdateRuleView, error) {
	rule, err := s.repo.GetHistory(ctx, assetKey, adjustPolicy, pointType)
	if errors.Is(err, repository.ErrAutoUpdateRuleNotFound) {
		return AutoUpdateRuleView{}, newErr(
			"auto_update_rule_not_found", "auto update rule not found", nil,
		)
	}
	if err != nil {
		return AutoUpdateRuleView{}, wrapRepo("load history automatic update", err)
	}
	updated, err := s.repo.Update(
		ctx, rule.ID, rule.Version, false, rule.IntervalHours, s.now().UnixMilli(), nil,
	)
	if err != nil {
		return AutoUpdateRuleView{}, wrapRepo("disable history automatic update", err)
	}
	return s.ruleView(ctx, updated)
}

func (s *AutoUpdateService) Update(
	ctx context.Context,
	id string,
	version int64,
	enabled bool,
	intervalHours int,
) (AutoUpdateRuleView, error) {
	if err := validInterval(intervalHours); err != nil {
		return AutoUpdateRuleView{}, err
	}
	if err := s.requireRule(ctx, id); err != nil {
		return AutoUpdateRuleView{}, err
	}
	now := s.now()
	var nextRunAt *int64
	if enabled {
		aligned := nextAlignedSlot(now, intervalHours, s.loc).UnixMilli()
		nextRunAt = &aligned
	}
	rule, err := s.repo.Update(
		ctx, id, version, enabled, intervalHours, now.UnixMilli(), nextRunAt,
	)
	if errors.Is(err, repository.ErrAutoUpdateRuleNotFound) {
		return AutoUpdateRuleView{}, newErr(
			"rule_version_conflict",
			"auto update rule was changed or removed; reload and retry",
			nil,
		)
	}
	if err != nil {
		return AutoUpdateRuleView{}, wrapRepo("update automatic update rule", err)
	}
	return s.ruleView(ctx, rule)
}

func (s *AutoUpdateService) requireRule(ctx context.Context, id string) error {
	_, err := s.repo.Get(ctx, id)
	if errors.Is(err, repository.ErrAutoUpdateRuleNotFound) {
		return newErr("auto_update_rule_not_found", "auto update rule not found", nil)
	}
	if err != nil {
		return wrapRepo("load automatic update rule", err)
	}
	return nil
}

func (s *AutoUpdateService) HistoryRule(
	ctx context.Context,
	assetKey string,
	adjustPolicy string,
	pointType string,
) (AutoUpdateRuleView, bool, error) {
	rule, err := s.repo.GetHistory(ctx, assetKey, adjustPolicy, pointType)
	if errors.Is(err, repository.ErrAutoUpdateRuleNotFound) {
		return AutoUpdateRuleView{}, false, nil
	}
	if err != nil {
		return AutoUpdateRuleView{}, false, wrapRepo(
			"load history automatic update rule", err,
		)
	}
	view, err := s.ruleView(ctx, rule)
	if err != nil {
		return AutoUpdateRuleView{}, false, err
	}
	return view, true, nil
}

type autoUpdateScanCounts struct {
	candidates int
	created    int
	reused     int
	failed     int
}

func (s *AutoUpdateService) RunOnce(ctx context.Context) error {
	startedAt := s.now()
	now := startedAt.UnixMilli()
	if err := s.repo.Reconcile(ctx, now); err != nil {
		return wrapRepo("reconcile automatic update tasks", err)
	}
	counts := autoUpdateScanCounts{}
	for {
		done, err := s.runBatch(ctx, now, &counts)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}
	slog.InfoContext(
		ctx,
		"auto update scan complete",
		"candidates", counts.candidates,
		"created", counts.created,
		"reused", counts.reused,
		"failed", counts.failed,
		"duration_ms", s.now().Sub(startedAt).Milliseconds(),
	)
	return nil
}

func (s *AutoUpdateService) runBatch(
	ctx context.Context,
	now int64,
	counts *autoUpdateScanCounts,
) (bool, error) {
	rules, err := s.repo.Due(ctx, now, autoUpdateBatchSize)
	if err != nil {
		return false, wrapRepo("list due automatic update rules", err)
	}
	if len(rules) == 0 {
		return true, nil
	}
	counts.candidates += len(rules)
	for _, rule := range rules {
		s.scheduleRule(ctx, rule, now, counts)
	}
	return len(rules) < autoUpdateBatchSize, nil
}

func (s *AutoUpdateService) scheduleRule(
	ctx context.Context,
	rule repository.MarketDataAutoUpdateRule,
	now int64,
	counts *autoUpdateScanCounts,
) {
	task, err := s.enqueueRule(ctx, rule, now)
	if errors.Is(err, repository.ErrAutoUpdateRuleNotFound) {
		return
	}
	if err != nil {
		s.markScanFailure(ctx, rule, err)
		counts.failed++
		return
	}
	if task.Existed {
		counts.reused++
		return
	}
	counts.created++
}

func (s *AutoUpdateService) enqueueRule(
	ctx context.Context,
	rule repository.MarketDataAutoUpdateRule,
	now int64,
) (TaskCreateResult, error) {
	nowTime := time.UnixMilli(now)
	next := nextAlignedSlot(nowTime, rule.IntervalHours, s.loc).UnixMilli()
	bind := func(ctx context.Context, tx *sql.Tx, taskID string) error {
		return s.repo.BindTaskTx(
			ctx, tx, rule.ID, rule.Version, taskID, now, next,
		)
	}
	switch rule.TargetType {
	case AutoUpdateTargetDirectory:
		return s.market.SyncDirectoryWithTaskHook(ctx, rule.SyncKey, bind)
	case AutoUpdateTargetHistory:
		return s.market.SyncHistoryWithTaskHook(ctx, HistorySyncRequest{
			AssetKey:     rule.AssetKey,
			AdjustPolicy: rule.AdjustPolicy,
			PointType:    rule.PointType,
			Mode:         historyModeDefaultRefresh,
		}, bind)
	default:
		return TaskCreateResult{}, newErr(
			"invalid_request", "unsupported automatic update target type", nil,
		)
	}
}

func validateAutoUpdateHistoryDimension(
	asset repository.MarketAsset,
	adjustPolicy string,
	pointType string,
) error {
	return ValidateHistoryDimension(asset, adjustPolicy, pointType)
}

func (s *AutoUpdateService) markScanFailure(
	ctx context.Context,
	rule repository.MarketDataAutoUpdateRule,
	cause error,
) {
	code := autoUpdateFailureCode(cause)
	now := s.now()
	next := nextAlignedSlot(now, rule.IntervalHours, s.loc).UnixMilli()
	err := s.repo.MarkScheduleFailure(
		ctx, rule.ID, rule.Version, code, cause.Error(), now.UnixMilli(), next,
	)
	if err != nil && !errors.Is(err, repository.ErrAutoUpdateRuleNotFound) {
		slog.WarnContext(
			ctx,
			"record auto update scheduling failure failed",
			"rule_id", rule.ID,
			"error", err,
		)
	}
}

func autoUpdateFailureCode(err error) string {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		return "auto_update_task_create_failed"
	}
	switch appErr.Code {
	case "invalid_request", "market_asset_not_found", "asset_identity_incomplete":
		return "auto_update_target_invalid"
	default:
		return "auto_update_task_create_failed"
	}
}

type AutoUpdateScheduler struct {
	svc             *AutoUpdateService
	intervalMinutes int
	loc             *time.Location
	now             func() time.Time
	after           func(time.Duration) <-chan time.Time
	scan            func(context.Context)
	cancel          context.CancelFunc
	done            chan struct{}
	once            sync.Once
}

func NewAutoUpdateScheduler(service *AutoUpdateService, intervalMinutes int) *AutoUpdateScheduler {
	scheduler := &AutoUpdateScheduler{
		svc: service, intervalMinutes: intervalMinutes, loc: service.loc,
		now: service.now, after: time.After, done: make(chan struct{}),
	}
	scheduler.scan = scheduler.runOnce
	return scheduler
}

func (s *AutoUpdateScheduler) Start(ctx context.Context) {
	s.once.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		s.cancel = cancel
		go func() {
			defer close(s.done)
			s.run(runCtx)
		}()
	})
}

func (s *AutoUpdateScheduler) run(ctx context.Context) {
	s.scan(ctx)
	for {
		now := s.now()
		next := nextScanTime(now, s.intervalMinutes, s.loc)
		wait := next.Sub(now)
		select {
		case <-ctx.Done():
			return
		case <-s.after(wait):
			s.scan(ctx)
		}
	}
}

func (s *AutoUpdateScheduler) runOnce(ctx context.Context) {
	scanCtx, cancel := context.WithTimeout(ctx, autoUpdateScanTimeout)
	defer cancel()
	slog.InfoContext(scanCtx, "auto update scan starting")
	if err := s.svc.RunOnce(scanCtx); err != nil {
		slog.ErrorContext(scanCtx, "auto update scan failed", "error", err)
	}
}

func (s *AutoUpdateScheduler) Stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
}
