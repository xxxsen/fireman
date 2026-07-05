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

// Coverage thresholds for worker task post-processing.
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
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	RegionCode     string `json:"region_code"`
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	Exchange       string `json:"exchange"`
	InstrumentKind string `json:"instrument_kind"`
	Currency       string `json:"currency"`
	SourceName     string `json:"source_name"`
	SourceAsOf     string `json:"source_as_of"`
}

type directoryResult struct {
	Type   string                 `json:"type"`
	Scope  string                 `json:"scope"`
	Assets []directoryResultAsset `json:"assets"`
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

func (s *PostProcessService) processDirectory(
	ctx context.Context, task repository.WorkerTask, raw []byte,
) error {
	var result directoryResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return permanentErr("invalid_result_payload", "directory result payload is invalid: "+err.Error())
	}
	if result.Type != repository.WorkerTaskTypeAssetDirectorySync {
		return permanentErr("result_task_mismatch",
			fmt.Sprintf("result type %q does not match task type %q", result.Type, task.Type))
	}
	var payload AssetDirectorySyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return permanentErr("invalid_task_payload", "directory task payload is invalid: "+err.Error())
	}
	if result.Scope != payload.Scope {
		return permanentErr("result_task_mismatch",
			fmt.Sprintf("result scope %q does not match task scope %q", result.Scope, payload.Scope))
	}

	type category struct{ market, instrumentType string }
	required := map[category]bool{}
	for _, market := range payload.Markets {
		for _, it := range payload.InstrumentTypes {
			required[category{market, it}] = true
		}
	}
	counts := map[category]int{}
	catSources := map[category]map[string]bool{}
	accepted := make([]directoryResultAsset, 0, len(result.Assets))
	for _, a := range result.Assets {
		if a.Market == "" || a.InstrumentType == "" || strings.TrimSpace(a.Symbol) == "" {
			return permanentErr("invalid_result_payload",
				"directory asset entry is missing market/instrument_type/symbol")
		}
		cat := category{a.Market, a.InstrumentType}
		if !required[cat] {
			// Entries outside the requested scope are ignored, never written.
			continue
		}
		if strings.TrimSpace(a.Name) == "" {
			a.Name = a.Symbol
		}
		counts[cat]++
		if catSources[cat] == nil {
			catSources[cat] = map[string]bool{}
		}
		catSources[cat][a.SourceName] = true
		accepted = append(accepted, a)
	}

	versionKey := "asset_directory|" + payload.Scope
	now := time.Now().UnixMilli()

	return s.withPostProcessTx(ctx, func(tx *sql.Tx) error {
		stored, err := s.assets.GetDataVersionTx(ctx, tx, versionKey)
		if err != nil {
			return err
		}
		if task.VersionNo <= stored {
			// Already processed at this or a higher version; success without
			// touching business tables or sync state.
			return nil
		}

		// Minimum coverage validation before any write: every required
		// category must be non-empty and not fall below 90% of the previous
		// successful count. First sync only requires non-empty. The previous
		// count is restricted to rows from the same listing sources as this
		// sync, so a listing-source/taxonomy migration behaves like a first
		// sync instead of failing the 90% gate forever.
		for cat := range required {
			incoming := counts[cat]
			if incoming == 0 {
				return permanentErr("directory_data_incomplete",
					fmt.Sprintf("required category %s/%s returned no assets", cat.market, cat.instrumentType))
			}
			sources := make([]string, 0, len(catSources[cat]))
			for s := range catSources[cat] {
				sources = append(sources, s)
			}
			prev, err := s.assets.CountActiveByTypeSourcesTx(ctx, tx, cat.market, cat.instrumentType, sources)
			if err != nil {
				return err
			}
			if prev > 0 && float64(incoming) < float64(prev)*directoryCoverageRatio {
				return permanentErr("directory_data_incomplete",
					fmt.Sprintf("category %s/%s returned %d assets, below 90%% of previous %d",
						cat.market, cat.instrumentType, incoming, prev))
			}
		}

		for _, a := range accepted {
			asset := repository.MarketAsset{
				AssetKey:       repository.BuildMarketAssetKey(a.Market, a.InstrumentType, a.RegionCode, a.Symbol),
				Market:         a.Market,
				InstrumentType: a.InstrumentType,
				RegionCode:     a.RegionCode,
				Symbol:         a.Symbol,
				Name:           a.Name,
				Exchange:       a.Exchange,
				InstrumentKind: a.InstrumentKind,
				Currency:       a.Currency,
				SourceName:     a.SourceName,
				SourceAsOf:     a.SourceAsOf,
			}
			if err := s.assets.UpsertAssetTx(ctx, tx, asset, now); err != nil {
				return err
			}
		}
		for cat := range required {
			if err := s.assets.MarkUnseenInactiveTx(ctx, tx, cat.market, cat.instrumentType, now, now); err != nil {
				return err
			}
		}
		if err := s.assets.SetSyncSuccessTx(ctx, tx, payload.Scope, task.ID, now); err != nil {
			return err
		}
		return s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID)
	})
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

func (s *PostProcessService) processHistory(
	ctx context.Context, task repository.WorkerTask, raw []byte,
) error {
	var result historyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return permanentErr("invalid_result_payload", "history result payload is invalid: "+err.Error())
	}
	if result.Type != repository.WorkerTaskTypeAssetHistorySync {
		return permanentErr("result_task_mismatch",
			fmt.Sprintf("result type %q does not match task type %q", result.Type, task.Type))
	}
	var payload AssetHistorySyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return permanentErr("invalid_task_payload", "history task payload is invalid: "+err.Error())
	}
	if result.AssetKey != payload.AssetKey ||
		result.AdjustPolicy != payload.AdjustPolicy ||
		result.PointType != payload.PointType {
		return permanentErr("result_task_mismatch",
			"result history dimension does not match task payload")
	}
	if payload.ReplacementMode != "merge" && payload.ReplacementMode != "full" {
		return permanentErr("invalid_task_payload",
			fmt.Sprintf("replacement_mode %q is not supported", payload.ReplacementMode))
	}

	points, err := normalizeHistoryPoints(result.Points)
	if err != nil {
		return err
	}

	versionKey := historyVersionKey(payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	now := time.Now()

	return s.withPostProcessTx(ctx, func(tx *sql.Tx) error {
		stored, err := s.assets.GetDataVersionTx(ctx, tx, versionKey)
		if err != nil {
			return err
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
			return err
		}

		if payload.ReplacementMode == "merge" {
			done, err := s.applyMergeHistory(ctx, tx, task, payload, result, points, existing, now)
			if err != nil || done {
				return err
			}
		} else {
			if err := s.applyFullHistory(ctx, tx, payload, result, points, existing, asset, now); err != nil {
				return err
			}
		}

		return s.finishHistoryCommit(ctx, tx, task, payload, result, now)
	})
}

// applyMergeHistory validates and applies a same-source incremental merge.
// done=true means the no_new_data fast path already finished the commit.
func (s *PostProcessService) applyMergeHistory(
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
		// Same-source refresh confirmed there is nothing new: update task
		// success and sync time only, keep data facts unchanged.
		st, ok, err := s.assets.GetHistoryStateTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
		if err != nil {
			return false, err
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
			return false, err
		}
		versionKey := historyVersionKey(payload.AssetKey, payload.AdjustPolicy, payload.PointType)
		if err := s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID); err != nil {
			return false, err
		}
		return true, nil
	}

	if len(points) == 0 {
		return false, permanentErr("provider_data_incomplete",
			"incremental result carried no points and no no_new_data marker")
	}
	if existing.Count > 0 {
		incomingMin := points[0].Date
		incomingMax := points[len(points)-1].Date
		// The incremental window must overlap existing data so no gap forms
		// between the stored series and the merged points.
		if incomingMin > existing.MaxDate {
			return false, permanentErr("provider_data_incomplete",
				fmt.Sprintf("incremental data starts %s after existing data_as_of %s (gap)",
					incomingMin, existing.MaxDate))
		}
		if incomingMax < existing.MaxDate {
			return false, permanentErr("provider_data_incomplete",
				fmt.Sprintf("incremental data ends %s before existing data_as_of %s",
					incomingMax, existing.MaxDate))
		}
	}
	repoPoints := make([]repository.MarketAssetPoint, len(points))
	for i, p := range points {
		repoPoints[i] = repository.MarketAssetPoint{
			AssetKey:     payload.AssetKey,
			AdjustPolicy: payload.AdjustPolicy,
			PointType:    payload.PointType,
			TradeDate:    p.Date,
			Value:        p.Value,
			SourceName:   result.SourceName,
			FetchedAt:    now.UnixMilli(),
		}
	}
	return false, s.assets.UpsertPointsTx(ctx, tx, repoPoints)
}

// applyFullHistory validates full replacement coverage and swaps the series.
func (s *PostProcessService) applyFullHistory(
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

	if existing.Count > 0 {
		incomingMin := points[0].Date
		incomingMax := points[len(points)-1].Date
		if incomingMin > existing.MinDate {
			return permanentErr("provider_data_incomplete",
				fmt.Sprintf("full replacement starts %s later than existing earliest %s",
					incomingMin, existing.MinDate))
		}
		delisted := asset.ListingStatus == "inactive" || asset.ListingStatus == "delisted"
		if !delisted {
			cutoff, err := time.Parse("2006-01-02", existing.MaxDate)
			if err == nil {
				minAllowed := cutoff.AddDate(0, 0, -historyMaxDateSlackDays).Format("2006-01-02")
				if incomingMax < minAllowed {
					return permanentErr("provider_data_incomplete",
						fmt.Sprintf("full replacement ends %s, more than %d days before existing latest %s",
							incomingMax, historyMaxDateSlackDays, existing.MaxDate))
				}
			}
		}
		if float64(len(points)) < float64(existing.Count)*historyCoverageRatio {
			return permanentErr("provider_data_incomplete",
				fmt.Sprintf("full replacement carries %d points, below 95%% of existing %d",
					len(points), existing.Count))
		}
	}

	if err := s.assets.DeletePointsTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType); err != nil {
		return err
	}
	repoPoints := make([]repository.MarketAssetPoint, len(points))
	for i, p := range points {
		repoPoints[i] = repository.MarketAssetPoint{
			AssetKey:     payload.AssetKey,
			AdjustPolicy: payload.AdjustPolicy,
			PointType:    payload.PointType,
			TradeDate:    p.Date,
			Value:        p.Value,
			SourceName:   result.SourceName,
			FetchedAt:    now.UnixMilli(),
		}
	}
	return s.assets.UpsertPointsTx(ctx, tx, repoPoints)
}

// finishHistoryCommit recomputes projections and updates the history state
// and version table.
func (s *PostProcessService) finishHistoryCommit(
	ctx context.Context,
	tx *sql.Tx,
	task repository.WorkerTask,
	payload AssetHistorySyncPayload,
	result historyResult,
	now time.Time,
) error {
	series, err := s.assets.ListPointsTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err != nil {
		return err
	}
	if len(series) == 0 {
		return permanentErr("provider_data_incomplete", "history series is empty after commit")
	}
	dataAsOf := series[len(series)-1].TradeDate

	dp := make([]marketdata.DataPoint, len(series))
	for i, p := range series {
		dp[i] = marketdata.DataPoint{
			TradeDate:  p.TradeDate,
			Value:      p.Value,
			PointType:  p.PointType,
			SourceName: p.SourceName,
			FetchedAt:  p.FetchedAt,
		}
	}

	annual := marketdata.ComputeAnnualReturns(dp)
	annualViews := make([]annualReturnView, len(annual))
	for i, a := range annual {
		annualViews[i] = annualReturnView{
			Year: a.Year, AnnualReturn: a.AnnualReturn,
			StartDate: a.StartDate, EndDate: a.EndDate,
			StartValue: a.StartValue, EndValue: a.EndValue,
			Observations: a.Observations, IsPartial: a.IsPartial,
		}
	}
	annualJSON, err := json.Marshal(annualViews)
	if err != nil {
		return fmt.Errorf("marshal annual returns: %w", err)
	}
	trailing := marketdata.ComputeTrailingReturns(dp, dataAsOf, payload.PointType, result.SourceName)
	trailingJSON, err := json.Marshal(trailingReturnsToMap(trailing))
	if err != nil {
		return fmt.Errorf("marshal trailing returns: %w", err)
	}
	if err := s.assets.SetDetailProjectionTx(ctx, tx, repository.MarketAssetDetailProjection{
		AssetKey:            payload.AssetKey,
		AdjustPolicy:        payload.AdjustPolicy,
		PointType:           payload.PointType,
		AnnualReturnsJSON:   string(annualJSON),
		TrailingReturnsJSON: string(trailingJSON),
		ComputedAt:          now.UnixMilli(),
	}); err != nil {
		return err
	}

	successAt := now.UnixMilli()
	sourceName := result.SourceName
	if payload.ReplacementMode == "merge" {
		sourceName = payload.RequiredSourceName
	}
	if err := s.assets.SetHistorySuccessTx(ctx, tx, repository.MarketAssetHistoryState{
		AssetKey:          payload.AssetKey,
		AdjustPolicy:      payload.AdjustPolicy,
		PointType:         payload.PointType,
		LastTaskID:        task.ID,
		LastSuccessTaskID: task.ID,
		LastSuccessAt:     &successAt,
		DataAsOf:          dataAsOf,
		PointCount:        len(series),
		SourceName:        sourceName,
		UpdatedAt:         successAt,
	}); err != nil {
		return err
	}

	// Single-source invariant check after commit: merge is source-pinned and
	// full replaces everything, so more than one distinct source signals a
	// mixed series that the next refresh must repair with a full replacement.
	summary, err := s.assets.PointsSummaryTx(ctx, tx, payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	if err != nil {
		return err
	}
	if len(summary.SourceNames) > 1 {
		slog.WarnContext(ctx, "market asset history is mixed-source after commit",
			"asset_key", payload.AssetKey,
			"sources", strings.Join(summary.SourceNames, ","))
	}

	versionKey := historyVersionKey(payload.AssetKey, payload.AdjustPolicy, payload.PointType)
	return s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID)
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

func (s *PostProcessService) processFXRates(
	ctx context.Context, task repository.WorkerTask, raw []byte,
) error {
	var result fxResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return permanentErr("invalid_result_payload", "fx result payload is invalid: "+err.Error())
	}
	if result.Type != repository.WorkerTaskTypeFXRateSync {
		return permanentErr("result_task_mismatch",
			fmt.Sprintf("result type %q does not match task type %q", result.Type, task.Type))
	}
	var payload FXRateSyncPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return permanentErr("invalid_task_payload", "fx task payload is invalid: "+err.Error())
	}

	sourceName := strings.TrimSpace(result.SourceName)
	if sourceName == "" {
		sourceName = "fx_rate_sync"
	}

	// Group and validate rates per pair.
	ratesByPair := make(map[string]map[string]float64)
	for _, r := range result.Rates {
		pair := strings.ToUpper(strings.TrimSpace(r.Pair))
		if _, err := time.Parse("2006-01-02", r.Date); err != nil {
			return permanentErr("invalid_result_payload",
				fmt.Sprintf("fx rate date %q is not YYYY-MM-DD", r.Date))
		}
		if r.Value <= 0 || math.IsNaN(r.Value) || math.IsInf(r.Value, 0) {
			return permanentErr("invalid_result_payload",
				fmt.Sprintf("fx rate for %s on %s is not a positive finite number", pair, r.Date))
		}
		if ratesByPair[pair] == nil {
			ratesByPair[pair] = make(map[string]float64)
		}
		ratesByPair[pair][r.Date] = r.Value
	}
	for _, pair := range payload.Pairs {
		if len(ratesByPair[strings.ToUpper(pair)]) == 0 {
			return permanentErr("provider_data_incomplete",
				"fx result carries no rates for requested pair "+pair)
		}
	}

	// Resolve the system FX instruments outside the write transaction; they
	// are seeded by migrations and immutable.
	instByPair := make(map[string]repository.InstrumentRecord, len(payload.Pairs))
	for _, pair := range payload.Pairs {
		inst, err := s.instRepo.FindByKey(ctx, "SYSTEM", "fx_rate", strings.ToUpper(pair), "none")
		if err != nil {
			return permanentErr("fx_instrument_not_found",
				"system fx instrument not found for pair "+pair)
		}
		instByPair[strings.ToUpper(pair)] = inst
	}

	now := time.Now().UnixMilli()
	return s.withPostProcessTx(ctx, func(tx *sql.Tx) error {
		processedAny := false
		for _, rawPair := range payload.Pairs {
			pair := strings.ToUpper(rawPair)
			versionKey := "fx_rate|" + pair
			stored, err := s.assets.GetDataVersionTx(ctx, tx, versionKey)
			if err != nil {
				return err
			}
			if task.VersionNo <= stored {
				continue
			}
			inst := instByPair[pair]
			dates := make([]string, 0, len(ratesByPair[pair]))
			for d := range ratesByPair[pair] {
				dates = append(dates, d)
			}
			sort.Strings(dates)
			points := make([]repository.MarketDataPoint, len(dates))
			for i, d := range dates {
				points[i] = repository.MarketDataPoint{
					InstrumentID: inst.ID, TradeDate: d, Value: ratesByPair[pair][d],
					PointType: "fx_rate", SourceName: sourceName, FetchedAt: now,
				}
			}
			if err := s.marketRepo.DeleteAllTx(ctx, tx, inst.ID); err != nil {
				return err
			}
			if err := s.marketRepo.UpsertBatch(ctx, tx, inst.ID, points); err != nil {
				return err
			}
			if err := s.instRepo.TouchUpdated(ctx, tx, inst.ID); err != nil {
				return err
			}
			if err := s.assets.SetDataVersionTx(ctx, tx, versionKey, task.VersionNo, task.ID); err != nil {
				return err
			}
			processedAny = true
		}
		if processedAny {
			return s.assets.SetSyncSuccessTx(ctx, tx, ScopeFXRates, task.ID, now)
		}
		return nil
	})
}
