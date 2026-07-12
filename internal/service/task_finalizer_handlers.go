package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// Coverage thresholds for worker task finalizationing.
const (
	// directoryCoverageRatio: each required category must return at least 90%
	// of the previous successful count.
	directoryCoverageRatio = 0.9
	// historyCoverageRatio: a full replacement must carry at least 95% of the
	// existing point count.
	historyCoverageRatio = 0.95
	// historyMaxDateSlackDays: a full replacement's newest date may lag the
	// existing newest date by at most 10 calendar days (unless delisted).
	historyMaxDateSlackDays = 10
)

// --- result payload schemas (written by the sidecar into resource_db) ---

type directoryResultAsset struct {
	Market          string `json:"market"`
	InstrumentType  string `json:"instrument_type"`
	RegionCode      string `json:"region_code"`
	Symbol          string `json:"symbol"`
	Name            string `json:"name"`
	Exchange        string `json:"exchange"`
	InstrumentKind  string `json:"instrument_kind"`
	CanonicalSymbol string `json:"canonical_symbol"`
	FeeMode         string `json:"fee_mode"`
	Currency        string `json:"currency"`
	SourceName      string `json:"source_name"`
	SourceAsOf      string `json:"source_as_of"`
}

type directoryResult struct {
	Type    string                 `json:"type"`
	SyncKey string                 `json:"sync_key"`
	Scope   string                 `json:"scope"`
	Assets  []directoryResultAsset `json:"assets"`
}

type historyResultPoint struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type historyResult struct {
	Type         string               `json:"type"`
	AssetKey     string               `json:"asset_key"`
	AdjustPolicy string               `json:"adjust_policy"`
	PointType    string               `json:"point_type"`
	SourceName   string               `json:"source_name"`
	NoNewData    bool                 `json:"no_new_data,omitempty"`
	Points       []historyResultPoint `json:"points"`
}

type fxResultRate struct {
	Date  string  `json:"date"`
	Pair  string  `json:"pair"`
	Value float64 `json:"value"`
}

type fxResult struct {
	Type       string         `json:"type"`
	Pairs      []string       `json:"pairs"`
	SourceName string         `json:"source_name"`
	Rates      []fxResultRate `json:"rates"`
}

// annualReturnView is the JSON shape stored in the detail projection.
type annualReturnView struct {
	Year         int     `json:"year"`
	AnnualReturn float64 `json:"annual_return"`
	StartDate    string  `json:"start_date"`
	EndDate      string  `json:"end_date"`
	StartValue   float64 `json:"start_value"`
	EndValue     float64 `json:"end_value"`
	Observations int     `json:"observations"`
	IsPartial    bool    `json:"is_partial"`
}

// --- asset_directory_sync ---

type directoryCategory struct{ market, instrumentType string }

func (s *TaskFinalizer) processDirectory(
	ctx context.Context, task repository.WorkerTask, raw []byte,
) error {
	result, payload, err := parseDirectoryResult(task, raw)
	if err != nil {
		return err
	}
	required := requiredDirectoryCategories(payload)
	accepted, counts, catSources, err := collectDirectoryResultAssets(result, required)
	if err != nil {
		return err
	}

	// The unit is the idempotency and commit boundary: sibling units of the
	// same scope never share a version key or block each other.
	versionKey := "asset_directory|" + payload.SyncKey
	now := time.Now().UnixMilli()

	return s.withTaskFinalizeTx(ctx, func(tx *sql.Tx) error {
		stored, err := s.assets.GetDataVersionTx(ctx, tx, versionKey)
		if err != nil {
			return wrapRepo("load directory data version", err)
		}
		if task.VersionNo <= stored {
			return nil
		}
		if err := s.validateDirectoryCoverageTx(ctx, tx, required, counts, catSources); err != nil {
			return err
		}
		return s.commitDirectoryTx(ctx, tx, task, payload, accepted, required, versionKey, now)
	})
}

func parseDirectoryResult(task repository.WorkerTask, raw []byte) (directoryResult, AssetDirectorySyncPayload, error) {
	var result directoryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return directoryResult{}, AssetDirectorySyncPayload{},
			permanentErr("invalid_result_payload", "directory result payload is invalid: "+err.Error())
	}
	if result.Type != repository.WorkerTaskTypeAssetDirectorySync {
		return directoryResult{}, AssetDirectorySyncPayload{}, permanentErr("result_task_mismatch",
			fmt.Sprintf("result type %q does not match task type %q", result.Type, task.Type))
	}
	var payload AssetDirectorySyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return directoryResult{}, AssetDirectorySyncPayload{},
			permanentErr("invalid_task_payload", "directory task payload is invalid: "+err.Error())
	}
	if payload.SyncKey == "" {
		return directoryResult{}, AssetDirectorySyncPayload{},
			permanentErr("invalid_task_payload", "directory task payload has no sync_key")
	}
	if result.SyncKey != payload.SyncKey {
		return directoryResult{}, AssetDirectorySyncPayload{}, permanentErr("result_task_mismatch",
			fmt.Sprintf("result sync_key %q does not match task sync_key %q",
				result.SyncKey, payload.SyncKey))
	}
	if result.Scope != payload.Scope {
		return directoryResult{}, AssetDirectorySyncPayload{}, permanentErr("result_task_mismatch",
			fmt.Sprintf("result scope %q does not match task scope %q", result.Scope, payload.Scope))
	}
	return result, payload, nil
}

func requiredDirectoryCategories(payload AssetDirectorySyncPayload) map[directoryCategory]bool {
	required := map[directoryCategory]bool{}
	for _, market := range payload.Markets {
		for _, it := range payload.InstrumentTypes {
			required[directoryCategory{market, it}] = true
		}
	}
	return required
}

func collectDirectoryResultAssets(
	result directoryResult,
	required map[directoryCategory]bool,
) ([]directoryResultAsset, map[directoryCategory]int, map[directoryCategory]map[string]bool, error) {
	counts := map[directoryCategory]int{}
	catSources := map[directoryCategory]map[string]bool{}
	accepted := make([]directoryResultAsset, 0, len(result.Assets))
	for _, a := range result.Assets {
		a, err := normalizeDirectoryResultAsset(a)
		if err != nil {
			return nil, nil, nil, err
		}
		cat := directoryCategory{a.Market, a.InstrumentType}
		if !required[cat] {
			// Entries outside the requested scope are ignored, never written.
			continue
		}
		counts[cat]++
		if catSources[cat] == nil {
			catSources[cat] = map[string]bool{}
		}
		catSources[cat][a.SourceName] = true
		accepted = append(accepted, a)
	}
	return accepted, counts, catSources, nil
}

func normalizeDirectoryResultAsset(a directoryResultAsset) (directoryResultAsset, error) {
	if a.Market == "" || a.InstrumentType == "" || strings.TrimSpace(a.Symbol) == "" {
		return a, permanentErr("invalid_result_payload",
			"directory asset entry is missing market/instrument_type/symbol")
	}
	if strings.TrimSpace(a.Name) == "" {
		a.Name = a.Symbol
	}
	if strings.TrimSpace(a.CanonicalSymbol) == "" {
		a.CanonicalSymbol = a.Symbol
	}
	if a.InstrumentType != "cn_mutual_fund" {
		a.FeeMode = ""
		return a, nil
	}
	return normalizeMutualFundFeeIdentity(a)
}

func normalizeMutualFundFeeIdentity(a directoryResultAsset) (directoryResultAsset, error) {
	if a.FeeMode == "" {
		if strings.HasSuffix(a.Name, "(后端)") || strings.HasSuffix(a.Name, "（后端）") {
			return a, permanentErr("invalid_result_payload",
				fmt.Sprintf("back-end mutual fund %s has no canonical fee identity", a.Symbol))
		}
		a.FeeMode = "standard"
	}
	if a.FeeMode != "standard" && a.FeeMode != "front_end" && a.FeeMode != "back_end" {
		return a, permanentErr("invalid_result_payload",
			fmt.Sprintf("mutual fund %s has unsupported fee_mode %q", a.Symbol, a.FeeMode))
	}
	if len(a.CanonicalSymbol) != 6 || strings.Trim(a.CanonicalSymbol, "0123456789") != "" {
		return a, permanentErr("invalid_result_payload",
			fmt.Sprintf("mutual fund %s has invalid canonical_symbol %q", a.Symbol, a.CanonicalSymbol))
	}
	if a.FeeMode == "back_end" && a.CanonicalSymbol == a.Symbol {
		return a, permanentErr("invalid_result_payload",
			fmt.Sprintf("back-end mutual fund %s is not linked to a distinct canonical symbol", a.Symbol))
	}
	if a.FeeMode != "back_end" && a.CanonicalSymbol != a.Symbol {
		return a, permanentErr("invalid_result_payload",
			fmt.Sprintf("mutual fund %s fee_mode %s cannot alias canonical symbol %s",
				a.Symbol, a.FeeMode, a.CanonicalSymbol))
	}
	return a, nil
}

func directorySources(sources map[string]bool) []string {
	out := make([]string, 0, len(sources))
	for s := range sources {
		out = append(out, s)
	}
	return out
}

func (s *TaskFinalizer) validateDirectoryCoverageTx(
	ctx context.Context,
	tx *sql.Tx,
	required map[directoryCategory]bool,
	counts map[directoryCategory]int,
	catSources map[directoryCategory]map[string]bool,
) error {
	// Minimum coverage validation before any write: every required category
	// must be non-empty and not fall below 90% of the previous successful
	// count. Previous count is restricted to this sync's listing sources.
	for cat := range required {
		incoming := counts[cat]
		if incoming == 0 {
			return permanentErr("directory_data_incomplete",
				fmt.Sprintf("required category %s/%s returned no assets", cat.market, cat.instrumentType))
		}
		sources := directorySources(catSources[cat])
		prev, err := s.assets.CountActiveByTypeSourcesTx(ctx, tx, cat.market, cat.instrumentType, sources)
		if err != nil {
			return wrapRepo("count active directory assets", err)
		}
		if prev > 0 && float64(incoming) < float64(prev)*directoryCoverageRatio {
			return permanentErr("directory_data_incomplete",
				fmt.Sprintf("category %s/%s returned %d assets, below 90%% of previous %d",
					cat.market, cat.instrumentType, incoming, prev))
		}
	}
	return nil
}

func directoryAssetFromResult(a directoryResultAsset) repository.MarketAsset {
	return repository.MarketAsset{
		AssetKey:        repository.BuildMarketAssetKey(a.Market, a.InstrumentType, a.RegionCode, a.Symbol),
		Market:          a.Market,
		InstrumentType:  a.InstrumentType,
		RegionCode:      a.RegionCode,
		Symbol:          a.Symbol,
		Name:            a.Name,
		Exchange:        a.Exchange,
		InstrumentKind:  a.InstrumentKind,
		CanonicalSymbol: a.CanonicalSymbol,
		FeeMode:         a.FeeMode,
		Currency:        a.Currency,
		SourceName:      a.SourceName,
		SourceAsOf:      a.SourceAsOf,
	}
}

func (s *TaskFinalizer) commitDirectoryTx(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	payload AssetDirectorySyncPayload,
	accepted []directoryResultAsset,
	required map[directoryCategory]bool,
	versionKey string,
	now int64,
) error {
	for _, a := range accepted {
		if err := s.assets.UpsertAssetTx(ctx, tx, directoryAssetFromResult(a), now); err != nil {
			return wrapRepo("upsert directory asset", err)
		}
	}
	for cat := range required {
		if err := s.assets.MarkUnseenInactiveTx(ctx, tx, cat.market, cat.instrumentType, now, now); err != nil {
			return wrapRepo("mark unseen directory assets inactive", err)
		}
	}
	if err := s.assets.SetSyncSuccessTx(ctx, tx, payload.SyncKey, payload.Scope, task.ID, now); err != nil {
		return wrapRepo("set directory sync success", err)
	}
	if err := s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID); err != nil {
		return wrapRepo("set directory data version", err)
	}
	return nil
}

// --- asset_history_sync ---

func historyVersionKey(assetKey, adjustPolicy, pointType string) string {
	return "asset_history|" + assetKey + "|" + adjustPolicy + "|" + pointType
}

// normalizeHistoryPoints validates, dedupes (last wins) and sorts result
// points by date.
func normalizeHistoryPoints(in []historyResultPoint) ([]historyResultPoint, error) {
	byDate := make(map[string]historyResultPoint, len(in))
	for _, p := range in {
		if _, err := time.Parse("2006-01-02", p.Date); err != nil {
			return nil, permanentErr("invalid_result_payload",
				fmt.Sprintf("point date %q is not YYYY-MM-DD", p.Date))
		}
		if p.Value <= 0 || math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			return nil, permanentErr("invalid_result_payload",
				fmt.Sprintf("point value for %s is not a positive finite number", p.Date))
		}
		byDate[p.Date] = p
	}
	out := make([]historyResultPoint, 0, len(byDate))
	for _, p := range byDate {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (s *TaskFinalizer) processHistory(
	ctx context.Context, task repository.WorkerTask, raw []byte,
) error {
	result, payload, err := parseHistoryResult(task, raw)
	if err != nil {
		return err
	}
	points, err := normalizeHistoryPoints(result.Points)
	if err != nil {
		return err
	}

	versionKey := historyVersionKey(payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	now := time.Now()

	return s.withTaskFinalizeTx(ctx, func(tx *sql.Tx) error {
		stored, err := s.assets.GetDataVersionTx(ctx, tx, versionKey)
		if err != nil {
			return wrapRepo("load history data version", err)
		}
		if task.VersionNo <= stored {
			return nil
		}

		asset, err := s.assets.GetByKeyTx(ctx, tx, payload.AssetKey)
		if err != nil {
			return permanentErr("market_asset_not_found",
				"market asset for history task not found: "+payload.AssetKey)
		}

		existing, err := s.assets.PointsSummaryTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
		if err != nil {
			return wrapRepo("summarize history points", err)
		}

		if err := s.applyHistoryChange(ctx, tx, task, payload, result, points, existing, asset, now); err != nil {
			return err
		}
		return s.finishHistoryCommit(ctx, tx, task, payload, result, now)
	})
}

func parseHistoryResult(task repository.WorkerTask, raw []byte) (historyResult, AssetHistorySyncPayload, error) {
	var result historyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return historyResult{}, AssetHistorySyncPayload{},
			permanentErr("invalid_result_payload", "history result payload is invalid: "+err.Error())
	}
	if result.Type != repository.WorkerTaskTypeAssetHistorySync {
		return historyResult{}, AssetHistorySyncPayload{}, permanentErr("result_task_mismatch",
			fmt.Sprintf("result type %q does not match task type %q", result.Type, task.Type))
	}
	var payload AssetHistorySyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return historyResult{}, AssetHistorySyncPayload{},
			permanentErr("invalid_task_payload", "history task payload is invalid: "+err.Error())
	}
	if result.AssetKey != payload.AssetKey ||
		result.AdjustPolicy != payload.AdjustPolicy ||
		result.PointType != payload.PointType {
		return historyResult{}, AssetHistorySyncPayload{}, permanentErr("result_task_mismatch",
			"result history dimension does not match task payload")
	}
	if payload.ReplacementMode != "merge" && payload.ReplacementMode != "full" {
		return historyResult{}, AssetHistorySyncPayload{}, permanentErr("invalid_task_payload",
			fmt.Sprintf("replacement_mode %q is not supported", payload.ReplacementMode))
	}
	return result, payload, nil
}

func (s *TaskFinalizer) applyHistoryChange(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	payload AssetHistorySyncPayload,
	result historyResult,
	points []historyResultPoint,
	existing repository.MarketAssetPointsSummary,
	asset repository.MarketAsset,
	now time.Time,
) error {
	if payload.ReplacementMode == "full" {
		return s.applyFullHistory(ctx, tx, payload, result, points, existing, asset, now)
	}
	done, err := s.applyMergeHistory(ctx, tx, task, payload, result, points, existing, now)
	if err != nil || done {
		return err
	}
	return nil
}

// applyMergeHistory validates and applies a same-source incremental merge.
// done=true means the no_new_data fast path already finished the commit.
func (s *TaskFinalizer) applyMergeHistory(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	payload AssetHistorySyncPayload,
	result historyResult,
	points []historyResultPoint,
	existing repository.MarketAssetPointsSummary,
	now time.Time,
) (bool, error) {
	if payload.RequiredSourceName == "" {
		return false, permanentErr("invalid_task_payload", "merge task has no required_source_name")
	}
	if result.SourceName != payload.RequiredSourceName {
		return false, permanentErr("source_mismatch",
			fmt.Sprintf("merge result source %q does not match required source %q",
				result.SourceName, payload.RequiredSourceName))
	}

	if result.NoNewData {
		return s.finishNoNewDataHistory(ctx, tx, task, payload, existing, now)
	}

	if len(points) == 0 {
		return false, permanentErr("provider_data_incomplete",
			"incremental result carried no points and no no_new_data marker")
	}
	if err := validateIncrementalOverlap(points, existing); err != nil {
		return false, err
	}
	repoPoints := historyRepoPoints(payload, result.SourceName, points, now)
	if err := s.assets.UpsertPointsTx(ctx, tx, repoPoints); err != nil {
		return false, wrapRepo("upsert incremental history points", err)
	}
	return false, nil
}

func (s *TaskFinalizer) finishNoNewDataHistory(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	payload AssetHistorySyncPayload,
	existing repository.MarketAssetPointsSummary,
	now time.Time,
) (bool, error) {
	// Same-source refresh confirmed there is nothing new: update task success
	// and sync time only, keep data facts unchanged.
	st, ok, err := s.assets.GetHistoryStateTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err != nil {
		return false, wrapRepo("load history state", err)
	}
	if !ok {
		st = repository.MarketAssetHistoryState{
			AssetKey:     payload.AssetKey,
			AdjustPolicy: payload.AdjustPolicy,
			PointType:    payload.PointType,
			DataAsOf:     existing.MaxDate,
			PointCount:   existing.Count,
			SourceName:   payload.RequiredSourceName,
		}
	}
	successAt := now.UnixMilli()
	st.LastTaskID = task.ID
	st.LastSuccessTaskID = task.ID
	st.LastSuccessAt = &successAt
	st.UpdatedAt = successAt
	if err := s.assets.SetHistorySuccessTx(ctx, tx, st); err != nil {
		return false, wrapRepo("set no-new-data history success", err)
	}
	versionKey := historyVersionKey(payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err := s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID); err != nil {
		return false, wrapRepo("set no-new-data history version", err)
	}
	return true, nil
}

func validateIncrementalOverlap(
	points []historyResultPoint,
	existing repository.MarketAssetPointsSummary,
) error {
	if existing.Count == 0 {
		return nil
	}
	incomingMin := points[0].Date
	incomingMax := points[len(points)-1].Date
	if incomingMin > existing.MaxDate {
		return permanentErr("provider_data_incomplete",
			fmt.Sprintf("incremental data starts %s after existing data_as_of %s (gap)",
				incomingMin, existing.MaxDate))
	}
	if incomingMax < existing.MaxDate {
		return permanentErr("provider_data_incomplete",
			fmt.Sprintf("incremental data ends %s before existing data_as_of %s",
				incomingMax, existing.MaxDate))
	}
	return nil
}

func historyRepoPoints(
	payload AssetHistorySyncPayload,
	sourceName string,
	points []historyResultPoint,
	now time.Time,
) []repository.MarketAssetPoint {
	out := make([]repository.MarketAssetPoint, len(points))
	for i, p := range points {
		out[i] = repository.MarketAssetPoint{
			AssetKey:     payload.AssetKey,
			AdjustPolicy: payload.AdjustPolicy,
			PointType:    payload.PointType,
			TradeDate:    p.Date,
			Value:        p.Value,
			SourceName:   sourceName,
			FetchedAt:    now.UnixMilli(),
		}
	}
	return out
}

// applyFullHistory validates full replacement coverage and swaps the series.
func (s *TaskFinalizer) applyFullHistory(
	ctx context.Context,
	tx *sql.Tx,
	payload AssetHistorySyncPayload,
	result historyResult,
	points []historyResultPoint,
	existing repository.MarketAssetPointsSummary,
	asset repository.MarketAsset,
	now time.Time,
) error {
	if len(points) == 0 {
		return permanentErr("provider_data_incomplete", "full replacement result is empty")
	}
	if strings.TrimSpace(result.SourceName) == "" {
		return permanentErr("invalid_result_payload", "full replacement result has no source_name")
	}
	if payload.RequiredSourceName != "" && result.SourceName != payload.RequiredSourceName {
		return permanentErr("source_mismatch",
			fmt.Sprintf("full result source %q does not match required source %q",
				result.SourceName, payload.RequiredSourceName))
	}

	if err := validateFullHistoryCoverage(points, existing, asset); err != nil {
		return err
	}

	if err := s.assets.DeletePointsTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType); err != nil {
		return wrapRepo("delete existing history points", err)
	}
	repoPoints := historyRepoPoints(payload, result.SourceName, points, now)
	if err := s.assets.UpsertPointsTx(ctx, tx, repoPoints); err != nil {
		return wrapRepo("upsert full history points", err)
	}
	return nil
}

func validateFullHistoryCoverage(
	points []historyResultPoint,
	existing repository.MarketAssetPointsSummary,
	asset repository.MarketAsset,
) error {
	if existing.Count == 0 {
		return nil
	}
	incomingMin := points[0].Date
	incomingMax := points[len(points)-1].Date
	if incomingMin > existing.MinDate {
		return permanentErr("provider_data_incomplete",
			fmt.Sprintf("full replacement starts %s later than existing earliest %s",
				incomingMin, existing.MinDate))
	}
	if err := validateFullHistoryMaxDate(incomingMax, existing, asset); err != nil {
		return err
	}
	if float64(len(points)) < float64(existing.Count)*historyCoverageRatio {
		return permanentErr("provider_data_incomplete",
			fmt.Sprintf("full replacement carries %d points, below 95%% of existing %d",
				len(points), existing.Count))
	}
	return nil
}

func validateFullHistoryMaxDate(
	incomingMax string,
	existing repository.MarketAssetPointsSummary,
	asset repository.MarketAsset,
) error {
	if asset.ListingStatus == "inactive" || asset.ListingStatus == "delisted" {
		return nil
	}
	cutoff, ok := parseHistoryDate(existing.MaxDate)
	if !ok {
		return nil
	}
	minAllowed := cutoff.AddDate(0, 0, -historyMaxDateSlackDays).Format("2006-01-02")
	if incomingMax >= minAllowed {
		return nil
	}
	return permanentErr("provider_data_incomplete",
		fmt.Sprintf("full replacement ends %s, more than %d days before existing latest %s",
			incomingMax, historyMaxDateSlackDays, existing.MaxDate))
}

func parseHistoryDate(value string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", value)
	return t, err == nil
}

// finishHistoryCommit recomputes projections and updates the history state
// and version table.
func (s *TaskFinalizer) finishHistoryCommit(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	payload AssetHistorySyncPayload,
	result historyResult,
	now time.Time,
) error {
	series, err := s.assets.ListPointsTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err != nil {
		return wrapRepo("list committed history points", err)
	}
	if len(series) == 0 {
		return permanentErr("provider_data_incomplete", "history series is empty after commit")
	}
	dataAsOf := series[len(series)-1].TradeDate
	projection, err := buildHistoryProjection(payload, result, series, dataAsOf, now)
	if err != nil {
		return err
	}
	if err := s.assets.SetDetailProjectionTx(ctx, tx, projection); err != nil {
		return wrapRepo("set history detail projection", err)
	}

	// Research screener metrics projection (td/099): keep it in the same
	// transaction so the screener never sees points without metrics.
	metrics := ComputeResearchAssetMetrics(
		payload.AssetKey, payload.AdjustPolicy, payload.PointType, series, now.UnixMilli())
	if err := s.research.UpsertMetricsTx(ctx, tx, metrics); err != nil {
		return wrapRepo("upsert research metrics", err)
	}

	successAt := now.UnixMilli()
	state := committedHistoryState(task, payload, result, dataAsOf, len(series), successAt)
	if err := s.assets.SetHistorySuccessTx(ctx, tx, state); err != nil {
		return wrapRepo("set history success", err)
	}

	// Single-source invariant check after commit: merge is source-pinned and
	// full replaces everything, so more than one distinct source signals a
	// mixed series that the next refresh must repair with a full replacement.
	summary, err := s.assets.PointsSummaryTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err != nil {
		return wrapRepo("summarize committed history points", err)
	}
	if len(summary.SourceNames) > 1 {
		slog.WarnContext(ctx, "market asset history is mixed-source after commit",
			"asset_key", payload.AssetKey,
			"sources", strings.Join(summary.SourceNames, ","))
	}

	versionKey := historyVersionKey(payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err := s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID); err != nil {
		return wrapRepo("set history data version", err)
	}
	return nil
}

func historyDataPoints(series []repository.MarketAssetPoint) []marketdata.DataPoint {
	out := make([]marketdata.DataPoint, len(series))
	for i, p := range series {
		out[i] = marketdata.DataPoint{
			TradeDate:  p.TradeDate,
			Value:      p.Value,
			PointType:  p.PointType,
			SourceName: p.SourceName,
			FetchedAt:  p.FetchedAt,
		}
	}
	return out
}

func annualReturnViews(dp []marketdata.DataPoint) []annualReturnView {
	annual := marketdata.ComputeAnnualReturns(dp)
	out := make([]annualReturnView, len(annual))
	for i, a := range annual {
		out[i] = annualReturnView{
			Year: a.Year, AnnualReturn: a.AnnualReturn,
			StartDate: a.StartDate, EndDate: a.EndDate,
			StartValue: a.StartValue, EndValue: a.EndValue,
			Observations: a.Observations, IsPartial: a.IsPartial,
		}
	}
	return out
}

func buildHistoryProjection(
	payload AssetHistorySyncPayload,
	result historyResult,
	series []repository.MarketAssetPoint,
	dataAsOf string,
	now time.Time,
) (repository.MarketAssetDetailProjection, error) {
	dp := historyDataPoints(series)
	annualJSON, err := json.Marshal(annualReturnViews(dp))
	if err != nil {
		return repository.MarketAssetDetailProjection{}, fmt.Errorf("marshal annual returns: %w", err)
	}
	trailing := marketdata.ComputeTrailingReturns(dp, dataAsOf, payload.PointType, result.SourceName)
	trailingJSON, err := json.Marshal(trailingReturnsToMap(trailing))
	if err != nil {
		return repository.MarketAssetDetailProjection{}, fmt.Errorf("marshal trailing returns: %w", err)
	}
	return repository.MarketAssetDetailProjection{
		AssetKey:            payload.AssetKey,
		AdjustPolicy:        payload.AdjustPolicy,
		PointType:           payload.PointType,
		AnnualReturnsJSON:   string(annualJSON),
		TrailingReturnsJSON: string(trailingJSON),
		ComputedAt:          now.UnixMilli(),
	}, nil
}

func committedHistoryState(
	task repository.WorkerTask,
	payload AssetHistorySyncPayload,
	result historyResult,
	dataAsOf string,
	pointCount int,
	successAt int64,
) repository.MarketAssetHistoryState {
	sourceName := result.SourceName
	if payload.ReplacementMode == "merge" {
		sourceName = payload.RequiredSourceName
	}
	return repository.MarketAssetHistoryState{
		AssetKey:          payload.AssetKey,
		AdjustPolicy:      payload.AdjustPolicy,
		PointType:         payload.PointType,
		LastTaskID:        task.ID,
		LastSuccessTaskID: task.ID,
		LastSuccessAt:     &successAt,
		DataAsOf:          dataAsOf,
		PointCount:        pointCount,
		SourceName:        sourceName,
		UpdatedAt:         successAt,
	}
}

// trailingReturnsToMap serializes trailing returns into the detail projection
// JSON shape consumed by the asset detail API.
func trailingReturnsToMap(t marketdata.TrailingReturns) map[string]any {
	periods := map[string]any{}
	for key, p := range t.Periods {
		periods[key] = map[string]any{
			"status": p.Status, "target_start_date": p.TargetStartDate,
			"start_date": p.StartDate, "end_date": p.EndDate,
			"actual_days": p.ActualDays, "cumulative_return": p.CumulativeReturn,
			"annualized_return": p.AnnualizedReturn,
		}
	}
	return map[string]any{
		"as_of_date": t.AsOfDate, "point_type": t.PointType, "source_name": t.SourceName,
		"periods": periods,
	}
}

// --- fx_rate_sync ---

func (s *TaskFinalizer) processFXRates(
	ctx context.Context, task repository.WorkerTask, raw []byte,
) error {
	result, payload, err := parseFXResult(task, raw)
	if err != nil {
		return err
	}
	sourceName := strings.TrimSpace(result.SourceName)
	if sourceName == "" {
		sourceName = "fx_rate_sync"
	}
	ratesByPair, err := groupFXRates(result.Rates, payload.Pairs)
	if err != nil {
		return err
	}
	instByPair, err := s.fxInstruments(ctx, payload.Pairs)
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	return s.withTaskFinalizeTx(ctx, func(tx *sql.Tx) error {
		processedAny := false
		for _, rawPair := range payload.Pairs {
			processed, err := s.processFXPairTx(
				ctx, tx, task, rawPair, sourceName, ratesByPair, instByPair, now,
			)
			if err != nil {
				return err
			}
			processedAny = processedAny || processed
		}
		if !processedAny {
			return nil
		}
		if err := s.assets.SetSyncSuccessTx(ctx, tx, ScopeFXRates, ScopeFXRates, task.ID, now); err != nil {
			return wrapRepo("set fx sync success", err)
		}
		return nil
	})
}

func parseFXResult(task repository.WorkerTask, raw []byte) (fxResult, FXRateSyncPayload, error) {
	var result fxResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fxResult{}, FXRateSyncPayload{},
			permanentErr("invalid_result_payload", "fx result payload is invalid: "+err.Error())
	}
	if result.Type != repository.WorkerTaskTypeFXRateSync {
		return fxResult{}, FXRateSyncPayload{}, permanentErr("result_task_mismatch",
			fmt.Sprintf("result type %q does not match task type %q", result.Type, task.Type))
	}
	var payload FXRateSyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return fxResult{}, FXRateSyncPayload{},
			permanentErr("invalid_task_payload", "fx task payload is invalid: "+err.Error())
	}
	return result, payload, nil
}

func groupFXRates(rates []fxResultRate, pairs []string) (map[string]map[string]float64, error) {
	ratesByPair := make(map[string]map[string]float64)
	for _, r := range rates {
		pair := strings.ToUpper(strings.TrimSpace(r.Pair))
		if _, err := time.Parse("2006-01-02", r.Date); err != nil {
			return nil, permanentErr("invalid_result_payload",
				fmt.Sprintf("fx rate date %q is not YYYY-MM-DD", r.Date))
		}
		if r.Value <= 0 || math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
			return nil, permanentErr("invalid_result_payload",
				fmt.Sprintf("fx rate for %s on %s is not a positive finite number", pair, r.Date))
		}
		if ratesByPair[pair] == nil {
			ratesByPair[pair] = make(map[string]float64)
		}
		ratesByPair[pair][r.Date] = r.Value
	}
	for _, pair := range pairs {
		if len(ratesByPair[strings.ToUpper(pair)]) == 0 {
			return nil, permanentErr("provider_data_incomplete",
				"fx result carries no rates for requested pair "+pair)
		}
	}
	return ratesByPair, nil
}

func (s *TaskFinalizer) fxInstruments(
	ctx context.Context, pairs []string,
) (map[string]repository.InstrumentRecord, error) {
	instByPair := make(map[string]repository.InstrumentRecord, len(pairs))
	for _, pair := range pairs {
		inst, err := s.instRepo.FindByKey(ctx, "SYSTEM", "fx_rate", strings.ToUpper(pair), "none")
		if err != nil {
			return nil, permanentErr("fx_instrument_not_found",
				"system fx instrument not found for pair "+pair)
		}
		instByPair[strings.ToUpper(pair)] = inst
	}
	return instByPair, nil
}

func (s *TaskFinalizer) processFXPairTx(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	rawPair string,
	sourceName string,
	ratesByPair map[string]map[string]float64,
	instByPair map[string]repository.InstrumentRecord,
	now int64,
) (bool, error) {
	pair := strings.ToUpper(rawPair)
	versionKey := "fx_rate|" + pair
	stored, err := s.assets.GetDataVersionTx(ctx, tx, versionKey)
	if err != nil {
		return false, wrapRepo("load fx data version", err)
	}
	if task.VersionNo <= stored {
		return false, nil
	}
	inst := instByPair[pair]
	points := fxPoints(inst.ID, sourceName, ratesByPair[pair], now)
	if err := s.marketRepo.DeleteAllTx(ctx, tx, inst.ID); err != nil {
		return false, wrapRepo("delete fx points", err)
	}
	if err := s.marketRepo.UpsertBatch(ctx, tx, inst.ID, points); err != nil {
		return false, wrapRepo("upsert fx points", err)
	}
	if err := s.instRepo.TouchUpdated(ctx, tx, inst.ID); err != nil {
		return false, wrapRepo("touch fx instrument", err)
	}
	if err := s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID); err != nil {
		return false, wrapRepo("set fx data version", err)
	}
	return true, nil
}

func fxPoints(
	instrumentID string,
	sourceName string,
	rates map[string]float64,
	now int64,
) []repository.MarketDataPoint {
	dates := make([]string, 0, len(rates))
	for d := range rates {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	points := make([]repository.MarketDataPoint, len(dates))
	for i, d := range dates {
		points[i] = repository.MarketDataPoint{
			InstrumentID: instrumentID, TradeDate: d, Value: rates[d],
			PointType: "fx_rate", SourceName: sourceName, FetchedAt: now,
		}
	}
	return points
}
