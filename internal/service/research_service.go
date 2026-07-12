package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// ResearchService orchestrates the portfolio research module (td/099):
// screener, collections, readiness, batch history sync, backtest runs and
// plan interop. Market data always flows through MarketAssetService tasks;
// backtests run on the local jobs queue.
type ResearchService struct {
	sql                     *sql.DB
	research                *repository.ResearchRepo
	assets                  *repository.MarketAssetRepo
	tasks                   *repository.WorkerTaskRepo
	jobs                    *repository.JobRepo
	instruments             *repository.InstrumentRepo
	marketData              *repository.MarketDataRepo
	plans                   *repository.PlanRepo
	holdings                *repository.HoldingsRepo
	params                  *repository.ParametersRepo
	alloc                   *repository.AllocationRepo
	holdingSvc              *HoldingsService
	portfolio               *repository.PortfolioSnapshotRepo
	marketSvc               *MarketAssetService
	optimizationConcurrency int
	optimizationBacktest    func(BacktestInput) (*BacktestResult, error)
	now                     func() time.Time
}

func NewResearchService(
	sqlDB *sql.DB,
	research *repository.ResearchRepo,
	assets *repository.MarketAssetRepo,
	tasks *repository.WorkerTaskRepo,
	jobs *repository.JobRepo,
	instruments *repository.InstrumentRepo,
	marketData *repository.MarketDataRepo,
	plans *repository.PlanRepo,
	holdings *repository.HoldingsRepo,
	marketSvc *MarketAssetService,
) *ResearchService {
	snapshotSvc := marketdata.NewSnapshotService(repository.NewSnapshotRepo(sqlDB), assets)
	return &ResearchService{
		sql:                     sqlDB,
		research:                research,
		assets:                  assets,
		tasks:                   tasks,
		jobs:                    jobs,
		instruments:             instruments,
		marketData:              marketData,
		plans:                   plans,
		holdings:                holdings,
		params:                  repository.NewParametersRepo(sqlDB),
		alloc:                   repository.NewAllocationRepo(sqlDB),
		holdingSvc:              NewHoldingsService(sqlDB, plans, holdings, snapshotSvc, assets),
		portfolio:               repository.NewPortfolioSnapshotRepo(sqlDB),
		marketSvc:               marketSvc,
		optimizationConcurrency: DefaultResearchOptimizationConcurrency,
		optimizationBacktest:    RunResearchBacktest,
		now:                     time.Now,
	}
}

// SetOptimizationConcurrency configures candidate-level parallelism. It is
// called during application startup before the service is used.
func (s *ResearchService) SetOptimizationConcurrency(concurrency int) {
	if concurrency < 1 {
		concurrency = DefaultResearchOptimizationConcurrency
	}
	s.optimizationConcurrency = concurrency
}

// Allowed collection enum values.
var researchRebalancePolicies = map[string]bool{
	ResearchRebalanceMonthly:   true,
	ResearchRebalanceQuarterly: true,
	ResearchRebalanceYearly:    true,
	ResearchRebalanceBuyHold:   true,
	ResearchRebalanceFixed:     true,
	ResearchRebalanceThreshold: true,
}

var researchStartPolicies = map[string]bool{
	ResearchStartPolicyCommon: true,
	ResearchStartPolicyCustom: true,
}

var researchBaseCurrencies = map[string]bool{"CNY": true, "USD": true, "HKD": true}

// --- screener ---

// ResearchAssetView is one screener row.
type ResearchAssetView struct {
	repository.MarketAsset
	InstrumentTypeLabel string                           `json:"instrument_type_label"`
	IsCash              bool                             `json:"is_cash"`
	HasHistory          bool                             `json:"has_history"`
	AdjustPolicy        string                           `json:"adjust_policy,omitempty"`
	PointType           string                           `json:"point_type,omitempty"`
	DataAsOf            string                           `json:"data_as_of,omitempty"`
	PointCount          int                              `json:"point_count"`
	HistorySource       string                           `json:"history_source,omitempty"`
	Stale               bool                             `json:"stale"`
	SyncStatus          string                           `json:"sync_status,omitempty"`
	SyncError           string                           `json:"sync_error,omitempty"`
	FXRequired          []string                         `json:"fx_required,omitempty"`
	FXAvailable         bool                             `json:"fx_available"`
	BacktestReady       bool                             `json:"backtest_ready"`
	QualityBadges       []string                         `json:"quality_badges"`
	Metrics             *repository.ResearchAssetMetrics `json:"metrics,omitempty"`
}

// ResearchAssetListResult is the GET /research/assets response.
type ResearchAssetListResult struct {
	Assets []ResearchAssetView `json:"assets"`
	Total  int                 `json:"total"`
}

// ResearchAssetListParams mirrors the screener query string.
type ResearchAssetListParams struct {
	Market                 string
	InstrumentTypes        []string
	Query                  string
	Currencies             []string
	IncludeInactive        bool
	HistoryStatus          string
	DataAsOfMin            string
	MinHistoryYears        float64
	MinCAGR                *float64
	MinReturn1Y            *float64
	MinReturn3Y            *float64
	MinReturn5Y            *float64
	MaxVolatility          *float64
	MinMaxDrawdown         *float64
	MinSharpe              *float64
	MinCalmar              *float64
	MaxDownsideVolatility  *float64
	MinReturnDrawdownRatio *float64
	BacktestReady          bool
	SortBy                 string
	SortDesc               bool
	Limit                  int
	Offset                 int
}

// researchMetricsBackfillLimit bounds one lazy backfill pass per screener
// query.
const researchMetricsBackfillLimit = 200

// ListResearchAssets runs the screener query. Before querying it lazily
// backfills metrics for dimensions synced before the research module existed.
func (s *ResearchService) ListResearchAssets(
	ctx context.Context, params ResearchAssetListParams,
) (ResearchAssetListResult, error) {
	if params.HistoryStatus != "" {
		switch params.HistoryStatus {
		case "synced", "missing", "stale", "syncing", "failed":
		default:
			return ResearchAssetListResult{}, newErr("invalid_request",
				"history_status must be one of synced, missing, stale, syncing, failed", nil)
		}
	}
	if params.SortBy != "" && !repository.IsResearchSortKey(params.SortBy) {
		return ResearchAssetListResult{}, newErr("invalid_request",
			"unsupported sort key "+params.SortBy, nil)
	}
	limit, offset := normalizePage(params.Limit, params.Offset)

	now := s.now()
	BackfillResearchAssetMetrics(ctx, s.assets, s.research, researchMetricsBackfillLimit, now.UnixMilli())

	rows, total, err := s.research.SearchResearchAssets(ctx, repository.ResearchAssetSearchFilter{
		Market:                 params.Market,
		InstrumentTypes:        params.InstrumentTypes,
		Query:                  strings.TrimSpace(params.Query),
		Currencies:             params.Currencies,
		IncludeInactive:        params.IncludeInactive,
		HistoryStatus:          params.HistoryStatus,
		DataAsOfMin:            params.DataAsOfMin,
		MinHistoryYears:        params.MinHistoryYears,
		MinCAGR:                params.MinCAGR,
		MinReturn1Y:            params.MinReturn1Y,
		MinReturn3Y:            params.MinReturn3Y,
		MinReturn5Y:            params.MinReturn5Y,
		MaxVolatility:          params.MaxVolatility,
		MinMaxDrawdown:         params.MinMaxDrawdown,
		MinSharpe:              params.MinSharpe,
		MinCalmar:              params.MinCalmar,
		MaxDownsideVolatility:  params.MaxDownsideVolatility,
		MinReturnDrawdownRatio: params.MinReturnDrawdownRatio,
		BacktestReady:          params.BacktestReady,
		NowDate:                now.UTC().Format("2006-01-02"),
		SortBy:                 params.SortBy,
		SortDesc:               params.SortDesc,
		Limit:                  limit,
		Offset:                 offset,
	})
	if err != nil {
		return ResearchAssetListResult{}, wrapRepo("search research assets", err)
	}

	fxAvailable, err := s.availableFXPairs(ctx)
	if err != nil {
		return ResearchAssetListResult{}, err
	}

	out := ResearchAssetListResult{Assets: make([]ResearchAssetView, 0, len(rows)), Total: total}
	for _, row := range rows {
		out.Assets = append(out.Assets, s.buildAssetView(row, fxAvailable))
	}
	return out, nil
}

// availableFXPairs lists FX pairs with stored history.
func (s *ResearchService) availableFXPairs(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for _, pair := range FXPairs {
		fx, err := s.loadFXData(ctx, pair)
		if err != nil {
			return nil, err
		}
		if fx.Found {
			out[pair] = true
		}
	}
	return out, nil
}

func (s *ResearchService) buildAssetView(
	row repository.ResearchAssetRow, fxAvailable map[string]bool,
) ResearchAssetView {
	view := ResearchAssetView{
		MarketAsset:         row.Asset,
		InstrumentTypeLabel: instrumentTypeLabelZH(row.Asset.InstrumentType),
		IsCash:              isSystemCashAsset(row.Asset),
		HasHistory:          row.HasHistory,
		AdjustPolicy:        row.AdjustPolicy,
		PointType:           row.PointType,
		DataAsOf:            row.DataAsOf,
		PointCount:          row.PointCount,
		HistorySource:       row.SourceName,
		Stale:               row.Stale,
		SyncStatus:          row.SyncStatus,
		SyncError:           row.SyncError,
		Metrics:             row.Metrics,
		QualityBadges:       []string{},
	}
	view.Metrics.FillReturnDrawdownRatio()
	view.FXRequired = ResearchFXPairsFor(row.Asset.Currency, "CNY")
	view.FXAvailable = true
	for _, pair := range view.FXRequired {
		if !fxAvailable[pair] {
			view.FXAvailable = false
		}
	}
	view.BacktestReady = view.IsCash || (view.HasHistory && view.FXAvailable)
	view.QualityBadges = researchAssetQualityBadges(view, row)
	return view
}

func researchAssetQualityBadges(
	view ResearchAssetView, row repository.ResearchAssetRow,
) []string {
	if view.IsCash {
		return []string{"normal"}
	}
	badges := make([]string, 0)
	if !view.HasHistory {
		badges = append(badges, "missing_history")
	}
	if view.HasHistory && row.Metrics != nil && row.Metrics.HistoryYears < 3 {
		badges = append(badges, "short_history")
	}
	if view.Stale {
		badges = append(badges, "stale")
	}
	if !view.FXAvailable {
		badges = append(badges, "fx_missing")
	}
	if row.Metrics != nil && row.Metrics.AnnualVolatility != nil && *row.Metrics.AnnualVolatility > 1 {
		badges = append(badges, "abnormal_volatility")
	}
	if row.SyncStatus == repository.WorkerTaskStatusFailed {
		badges = append(badges, "sync_failed")
	}
	if len(badges) == 0 {
		return []string{"normal"}
	}
	return badges
}

// --- collections ---

// ResearchCollectionItemInput is one item payload inside create/add
// requests.
type ResearchCollectionItemInput struct {
	AssetKey     string   `json:"asset_key"`
	Weight       *float64 `json:"weight,omitempty"`
	Enabled      *bool    `json:"enabled,omitempty"`
	WeightLocked bool     `json:"weight_locked"`
	AdjustPolicy string   `json:"adjust_policy"`
	PointType    string   `json:"point_type"`
	AssetClass   string   `json:"asset_class"`
	Region       string   `json:"region"`
	Note         string   `json:"note"`
}

// ResearchCollectionInput is the POST /collections payload.
type ResearchCollectionInput struct {
	Name                string                        `json:"name"`
	Description         string                        `json:"description"`
	BaseCurrency        string                        `json:"base_currency"`
	InitialAmountMinor  *int64                        `json:"initial_amount_minor,omitempty"`
	RebalancePolicy     string                        `json:"rebalance_policy"`
	RebalanceThreshold  float64                       `json:"rebalance_threshold"`
	StartPolicy         string                        `json:"start_policy"`
	WindowStart         string                        `json:"window_start"`
	WindowEnd           string                        `json:"window_end"`
	BenchmarkAssetKey   string                        `json:"benchmark_asset_key"`
	RiskFreeRate        float64                       `json:"risk_free_rate"`
	TransactionCostRate float64                       `json:"transaction_cost_rate"`
	TailRiskConfidence  *float64                      `json:"tail_risk_confidence,omitempty"`
	TailRiskHorizonDays *int                          `json:"tail_risk_horizon_days,omitempty"`
	Tags                []string                      `json:"tags"`
	Items               []ResearchCollectionItemInput `json:"items"`
	FromPlanID          string                        `json:"from_plan_id"`
	FromCollectionID    string                        `json:"from_collection_id"`
}

// ResearchCollectionListItem is one row of the collections listing.
type ResearchCollectionListItem struct {
	repository.ResearchCollection
	Tags            []string                `json:"tags"`
	EnabledAssets   int                     `json:"enabled_assets"`
	TotalAssets     int                     `json:"total_assets"`
	WeightSum       float64                 `json:"weight_sum"`
	WeightValid     bool                    `json:"weight_valid"`
	LatestRun       *ResearchRunView        `json:"latest_run,omitempty"`
	LatestRunResult *ResearchRunSummaryView `json:"latest_run_summary,omitempty"`
}

// ResearchCollectionDetail is the GET /collections/{id} response.
type ResearchCollectionDetail struct {
	repository.ResearchCollection
	Tags  []string                     `json:"tags"`
	Items []ResearchCollectionItemView `json:"items"`
}

// ResearchCollectionItemView is an item with directory display facts.
type ResearchCollectionItemView struct {
	repository.ResearchCollectionItem
	Name                string `json:"name"`
	Symbol              string `json:"symbol"`
	Market              string `json:"market"`
	InstrumentType      string `json:"instrument_type"`
	InstrumentTypeLabel string `json:"instrument_type_label"`
	Currency            string `json:"currency"`
	CanonicalSymbol     string `json:"canonical_symbol"`
	FeeMode             string `json:"fee_mode"`
	ListingStatus       string `json:"listing_status"`
	IsCash              bool   `json:"is_cash"`
}

func parseTags(tagsJSON string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
}

func validateCollectionEnums(baseCurrency, rebalancePolicy, startPolicy string) error {
	if !researchBaseCurrencies[baseCurrency] {
		return newErr("invalid_request", "base_currency must be one of CNY, USD, HKD", nil)
	}
	if !researchRebalancePolicies[rebalancePolicy] {
		return newErr("invalid_request",
			"rebalance_policy must be one of monthly, quarterly, yearly, buy_hold, fixed, threshold", nil)
	}
	if !researchStartPolicies[startPolicy] {
		return newErr("invalid_request",
			"start_policy must be common_intersection or custom_range", nil)
	}
	return nil
}

func validateWindowDates(startPolicy, windowStart, windowEnd string) error {
	if startPolicy != ResearchStartPolicyCustom {
		return nil
	}
	for _, d := range []string{windowStart, windowEnd} {
		if d == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			return newErr("invalid_request", "window dates must use YYYY-MM-DD", nil)
		}
	}
	if windowStart != "" && windowEnd != "" && windowEnd <= windowStart {
		return newErr("invalid_request", "window_end must be after window_start", nil)
	}
	return nil
}

// CreateCollection creates a collection, optionally seeded from items, an
// existing collection or a FIRE plan.
func (s *ResearchService) CreateCollection(
	ctx context.Context, in ResearchCollectionInput,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	name, err := normalizeResearchCollectionInput(&in)
	if err != nil {
		return zero, err
	}
	itemInputs, err := s.resolveResearchCollectionItems(ctx, &in)
	if err != nil {
		return zero, err
	}
	now := s.now().UnixMilli()
	collection, err := buildResearchCollectionRecord(in, name, now)
	if err != nil {
		return zero, err
	}
	if err := s.validateResearchBenchmarkAsset(ctx, in.BenchmarkAssetKey); err != nil {
		return zero, err
	}
	items, err := s.buildItems(ctx, collection.ID, itemInputs, now)
	if err != nil {
		return zero, err
	}

	if err := s.persistResearchCollection(ctx, collection, items); err != nil {
		return zero, err
	}
	return s.GetCollection(ctx, collection.ID)
}

func normalizeResearchCollectionInput(in *ResearchCollectionInput) (string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", newErr("invalid_request", "name is required", nil)
	}
	if in.BaseCurrency == "" {
		in.BaseCurrency = "CNY"
	}
	if in.RebalancePolicy == "" {
		in.RebalancePolicy = ResearchRebalanceMonthly
	}
	if in.StartPolicy == "" {
		in.StartPolicy = ResearchStartPolicyCommon
	}
	confidence := DefaultTailRiskConfidence
	if in.TailRiskConfidence != nil {
		confidence = *in.TailRiskConfidence
	}
	horizon := DefaultTailRiskHorizon
	if in.TailRiskHorizonDays != nil {
		horizon = *in.TailRiskHorizonDays
	}
	tailRisk, err := CanonicalTailRiskSpec(TailRiskSpec{Confidence: confidence, HorizonDays: horizon})
	if err != nil {
		return "", tailRiskAppError(err)
	}
	in.TailRiskConfidence = &tailRisk.Confidence
	in.TailRiskHorizonDays = &tailRisk.HorizonDays
	if err := validateCollectionEnums(in.BaseCurrency, in.RebalancePolicy, in.StartPolicy); err != nil {
		return "", err
	}
	if err := validateWindowDates(in.StartPolicy, in.WindowStart, in.WindowEnd); err != nil {
		return "", err
	}
	if in.RebalancePolicy == ResearchRebalanceThreshold && in.RebalanceThreshold <= 0 {
		return "", newErr(
			"invalid_request", "rebalance_threshold must be positive for the threshold policy", nil,
		)
	}
	return name, nil
}

func (s *ResearchService) resolveResearchCollectionItems(
	ctx context.Context, in *ResearchCollectionInput,
) ([]ResearchCollectionItemInput, error) {
	if in.FromPlanID != "" {
		items, currency, err := s.itemsFromPlan(ctx, in.FromPlanID)
		if err != nil {
			return nil, err
		}
		if in.BaseCurrency == "CNY" && currency != "" {
			in.BaseCurrency = currency
		}
		return items, nil
	}
	if in.FromCollectionID != "" {
		return s.itemsFromCollection(ctx, in.FromCollectionID)
	}
	return in.Items, nil
}

func buildResearchCollectionRecord(
	in ResearchCollectionInput, name string, now int64,
) (repository.ResearchCollection, error) {
	initialAmount := int64(100000000)
	if in.InitialAmountMinor != nil && *in.InitialAmountMinor > 0 {
		initialAmount = *in.InitialAmountMinor
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return repository.ResearchCollection{}, fmt.Errorf("marshal tags: %w", err)
	}
	return repository.ResearchCollection{
		ID: "rc_" + uuid.New().String(), Name: name, Description: strings.TrimSpace(in.Description),
		BaseCurrency: in.BaseCurrency, InitialAmountMinor: initialAmount,
		RebalancePolicy: in.RebalancePolicy, RebalanceThreshold: in.RebalanceThreshold,
		StartPolicy: in.StartPolicy, WindowStart: in.WindowStart, WindowEnd: in.WindowEnd,
		BenchmarkAssetKey: in.BenchmarkAssetKey, RiskFreeRate: in.RiskFreeRate,
		TransactionCostRate: in.TransactionCostRate, Status: repository.ResearchCollectionStatusActive,
		TailRiskConfidence: *in.TailRiskConfidence, TailRiskHorizonDays: *in.TailRiskHorizonDays,
		TagsJSON: string(tagsJSON), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *ResearchService) validateResearchBenchmarkAsset(ctx context.Context, assetKey string) error {
	if assetKey == "" {
		return nil
	}
	if _, err := s.assets.GetByKey(ctx, assetKey); err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return newErr("market_asset_not_found", "benchmark asset not found", nil)
		}
		return wrapRepo("load benchmark asset", err)
	}
	return nil
}

func (s *ResearchService) persistResearchCollection(
	ctx context.Context,
	collection repository.ResearchCollection,
	items []repository.ResearchCollectionItem,
) error {
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.research.CreateCollectionTx(ctx, tx, collection); err != nil {
			return fmt.Errorf("create research collection: %w", err)
		}
		for _, item := range items {
			if err := s.research.CreateItemTx(ctx, tx, item); err != nil {
				return fmt.Errorf("create research collection item: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return wrapRepo("create research collection", err)
	}
	return nil
}

// buildItems validates and materializes item inputs. Weights default to an
// equal split when every weight is omitted.
func (s *ResearchService) buildItems(
	ctx context.Context, collectionID string, inputs []ResearchCollectionItemInput, now int64,
) ([]repository.ResearchCollectionItem, error) {
	items := make([]repository.ResearchCollectionItem, 0, len(inputs))
	allOmitted := true
	for _, in := range inputs {
		if in.Weight != nil {
			allOmitted = false
		}
	}
	seen := map[string]bool{}
	seenCanonicalFunds := map[string]string{}
	for i, in := range inputs {
		item, asset, dimKey, err := s.buildResearchItem(
			ctx, collectionID, in, i, len(inputs), allOmitted, now,
		)
		if err != nil {
			return nil, err
		}
		if canonicalKey := canonicalFundIdentity(asset); canonicalKey != "" {
			if existingAssetKey, exists := seenCanonicalFunds[canonicalKey]; exists {
				return nil, duplicateCanonicalFundError(asset, existingAssetKey)
			}
			seenCanonicalFunds[canonicalKey] = asset.AssetKey
		}
		if seen[dimKey] {
			return nil, newErr("invalid_request",
				"duplicate item dimension "+dimKey, nil)
		}
		seen[dimKey] = true
		items = append(items, item)
	}
	return items, nil
}

func (s *ResearchService) buildResearchItem(
	ctx context.Context,
	collectionID string,
	in ResearchCollectionItemInput,
	index, total int,
	allWeightsOmitted bool,
	now int64,
) (repository.ResearchCollectionItem, repository.MarketAsset, string, error) {
	var zero repository.ResearchCollectionItem
	var zeroAsset repository.MarketAsset
	assetKey := strings.TrimSpace(in.AssetKey)
	if assetKey == "" {
		return zero, zeroAsset, "", newErr("invalid_request", "items require asset_key", nil)
	}
	asset, err := s.assets.GetByKey(ctx, assetKey)
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return zero, zeroAsset, "", newErr("market_asset_not_found", "market asset not found: "+assetKey, nil)
		}
		return zero, zeroAsset, "", wrapRepo("load market asset", err)
	}
	adjustPolicy := strings.TrimSpace(in.AdjustPolicy)
	pointType := strings.TrimSpace(in.PointType)
	if err := validateHistoryDimensionPair(adjustPolicy, pointType); err != nil {
		return zero, zeroAsset, "", err
	}
	if adjustPolicy == "" {
		adjustPolicy = DefaultAdjustPolicy(asset.InstrumentType)
	}
	if pointType == "" {
		if adjustPolicy == "none" && asset.InstrumentType != "cn_mutual_fund" {
			pointType = "close"
		} else {
			pointType = DefaultPointType(asset.InstrumentType, asset.InstrumentKind)
		}
	}
	if err := ValidateHistoryDimension(asset, adjustPolicy, pointType); err != nil {
		return zero, zeroAsset, "", err
	}
	weight := researchInputWeight(in.Weight, allWeightsOmitted, total)
	if weight < 0 || weight > 1+ResearchWeightTolerance {
		return zero, zeroAsset, "", newErr("invalid_request",
			fmt.Sprintf("weight for %s must be within [0, 1]", assetKey), nil)
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	item := repository.ResearchCollectionItem{
		ID: "rci_" + uuid.New().String(), CollectionID: collectionID, AssetKey: assetKey,
		Enabled: enabled, Weight: weight, WeightLocked: in.WeightLocked,
		AdjustPolicy: adjustPolicy, PointType: pointType,
		AssetClass: strings.TrimSpace(in.AssetClass), Region: strings.TrimSpace(in.Region),
		Note: strings.TrimSpace(in.Note), SortOrder: index, CreatedAt: now, UpdatedAt: now,
	}
	return item, asset, assetKey + "|" + adjustPolicy + "|" + pointType, nil
}

func canonicalFundIdentity(asset repository.MarketAsset) string {
	if asset.InstrumentType != "cn_mutual_fund" {
		return ""
	}
	canonical := strings.TrimSpace(asset.CanonicalSymbol)
	if canonical == "" {
		canonical = strings.TrimSpace(asset.Symbol)
	}
	if canonical == "" {
		return ""
	}
	return strings.ToUpper(asset.Market) + "|" + asset.InstrumentType + "|" + canonical
}

func duplicateCanonicalFundError(asset repository.MarketAsset, existingAssetKey string) error {
	canonical := strings.TrimSpace(asset.CanonicalSymbol)
	if canonical == "" {
		canonical = asset.Symbol
	}
	return newErr(
		"duplicate_fund_exposure",
		fmt.Sprintf("交易代码 %s 与已加入资产对应同一主基金 %s，不能重复加入组合", asset.Symbol, canonical),
		map[string]any{
			"asset_key": asset.AssetKey, "existing_asset_key": existingAssetKey,
			"canonical_symbol": canonical,
		},
	)
}

func (s *ResearchService) validateCanonicalFundConflict(
	ctx context.Context,
	assetKey string,
	existing []repository.ResearchCollectionItem,
) error {
	asset, err := s.assets.GetByKey(ctx, strings.TrimSpace(assetKey))
	if err != nil {
		if errors.Is(err, repository.ErrMarketAssetNotFound) {
			return newErr("market_asset_not_found", "market asset not found: "+assetKey, nil)
		}
		return wrapRepo("load candidate research item asset", err)
	}
	canonicalKey := canonicalFundIdentity(asset)
	if canonicalKey == "" {
		return nil
	}
	for _, item := range existing {
		existingAsset, loadErr := s.assets.GetByKey(ctx, item.AssetKey)
		if loadErr != nil {
			return wrapRepo("load existing research item asset", loadErr)
		}
		if canonicalFundIdentity(existingAsset) == canonicalKey {
			return duplicateCanonicalFundError(asset, existingAsset.AssetKey)
		}
	}
	return nil
}

func researchInputWeight(weight *float64, allOmitted bool, total int) float64 {
	if weight != nil {
		return *weight
	}
	if allOmitted && total > 0 {
		return 1 / float64(total)
	}
	return 0
}

// itemsFromPlan converts plan holdings into item inputs: current amounts
// become weights (td/099 §9.2).
func (s *ResearchService) itemsFromPlan(
	ctx context.Context, planID string,
) ([]ResearchCollectionItemInput, string, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return nil, "", newErr("plan_not_found", "plan not found", nil)
		}
		return nil, "", wrapRepo("load plan", err)
	}
	holdings, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return nil, "", wrapRepo("list plan holdings", err)
	}
	var total int64
	for _, h := range holdings {
		if h.Enabled && h.CurrentAmountMinor > 0 {
			total += h.CurrentAmountMinor
		}
	}
	if total == 0 {
		return nil, "", newErr("plan_holdings_empty",
			"计划没有可复制的持仓金额", nil)
	}
	// Merge duplicate asset keys (a plan can hold the same asset under
	// several asset_class/region groups).
	type agg struct {
		amount     int64
		assetClass string
		region     string
	}
	byKey := map[string]*agg{}
	var order []string
	for _, h := range holdings {
		if !h.Enabled || h.CurrentAmountMinor <= 0 {
			continue
		}
		if cur, ok := byKey[h.AssetKey]; ok {
			cur.amount += h.CurrentAmountMinor
			continue
		}
		byKey[h.AssetKey] = &agg{
			amount: h.CurrentAmountMinor, assetClass: h.AssetClass, region: h.Region,
		}
		order = append(order, h.AssetKey)
	}
	items := make([]ResearchCollectionItemInput, 0, len(order))
	for _, key := range order {
		a := byKey[key]
		weight := float64(a.amount) / float64(total)
		items = append(items, ResearchCollectionItemInput{
			AssetKey:   key,
			Weight:     &weight,
			AssetClass: a.assetClass,
			Region:     a.region,
		})
	}
	return items, plan.BaseCurrency, nil
}

func (s *ResearchService) itemsFromCollection(
	ctx context.Context, collectionID string,
) ([]ResearchCollectionItemInput, error) {
	if _, err := s.research.GetCollection(ctx, collectionID); err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return nil, newErr("research_collection_not_found", "source collection not found", nil)
		}
		return nil, wrapRepo("load source collection", err)
	}
	items, err := s.research.ListItems(ctx, collectionID)
	if err != nil {
		return nil, wrapRepo("list source items", err)
	}
	out := make([]ResearchCollectionItemInput, 0, len(items))
	for _, item := range items {
		w := item.Weight
		enabled := item.Enabled
		out = append(out, ResearchCollectionItemInput{
			AssetKey:     item.AssetKey,
			Weight:       &w,
			Enabled:      &enabled,
			WeightLocked: item.WeightLocked,
			AdjustPolicy: item.AdjustPolicy,
			PointType:    item.PointType,
			AssetClass:   item.AssetClass,
			Region:       item.Region,
			Note:         item.Note,
		})
	}
	return out, nil
}

// ListCollections returns collections with weight/data status and latest run
// annotations.
func (s *ResearchService) ListCollections(
	ctx context.Context, status string,
) ([]ResearchCollectionListItem, error) {
	if status != "" && status != repository.ResearchCollectionStatusActive &&
		status != repository.ResearchCollectionStatusArchived {
		return nil, newErr("invalid_request", "status must be active or archived", nil)
	}
	collections, err := s.research.ListCollections(ctx, status)
	if err != nil {
		return nil, wrapRepo("list research collections", err)
	}
	ids := make([]string, len(collections))
	for i, c := range collections {
		ids[i] = c.ID
	}
	counts, err := s.research.CountItemsByCollections(ctx, ids)
	if err != nil {
		return nil, wrapRepo("count research items", err)
	}
	weightSums, err := s.research.SumEnabledWeightsByCollections(ctx, ids)
	if err != nil {
		return nil, wrapRepo("sum research weights", err)
	}
	latest, err := s.research.LatestRunsByCollections(ctx, ids)
	if err != nil {
		return nil, wrapRepo("load latest runs", err)
	}

	out := make([]ResearchCollectionListItem, 0, len(collections))
	for _, c := range collections {
		item := ResearchCollectionListItem{
			ResearchCollection: c,
			Tags:               parseTags(c.TagsJSON),
			EnabledAssets:      counts[c.ID][0],
			TotalAssets:        counts[c.ID][1],
			WeightSum:          weightSums[c.ID],
		}
		item.WeightValid = item.EnabledAssets > 0 &&
			math.Abs(item.WeightSum-1) <= ResearchWeightTolerance
		if run, ok := latest[c.ID]; ok {
			view := buildRunView(run)
			item.LatestRun = &view
			if run.Status == repository.ResearchRunStatusSucceeded {
				if summary := parseRunSummary(run.SummaryJSON); summary != nil {
					item.LatestRunResult = summary
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *ResearchService) GetCollection(
	ctx context.Context, id string,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	collection, err := s.research.GetCollection(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	items, err := s.research.ListItems(ctx, id)
	if err != nil {
		return zero, wrapRepo("list research items", err)
	}
	detail := ResearchCollectionDetail{
		ResearchCollection: collection,
		Tags:               parseTags(collection.TagsJSON),
		Items:              make([]ResearchCollectionItemView, 0, len(items)),
	}
	for _, item := range items {
		view := ResearchCollectionItemView{ResearchCollectionItem: item}
		if asset, err := s.assets.GetByKey(ctx, item.AssetKey); err == nil {
			view.Name = asset.Name
			view.Symbol = asset.Symbol
			view.Market = asset.Market
			view.InstrumentType = asset.InstrumentType
			view.InstrumentTypeLabel = instrumentTypeLabelZH(asset.InstrumentType)
			view.Currency = asset.Currency
			view.CanonicalSymbol = asset.CanonicalSymbol
			view.FeeMode = asset.FeeMode
			view.ListingStatus = asset.ListingStatus
			view.IsCash = isSystemCashAsset(asset)
		}
		detail.Items = append(detail.Items, view)
	}
	return detail, nil
}

// ResearchCollectionUpdate is the PATCH /collections/{id} payload; nil
// pointers leave fields unchanged.
type ResearchCollectionUpdate struct {
	Name                *string  `json:"name,omitempty"`
	Description         *string  `json:"description,omitempty"`
	BaseCurrency        *string  `json:"base_currency,omitempty"`
	InitialAmountMinor  *int64   `json:"initial_amount_minor,omitempty"`
	RebalancePolicy     *string  `json:"rebalance_policy,omitempty"`
	RebalanceThreshold  *float64 `json:"rebalance_threshold,omitempty"`
	StartPolicy         *string  `json:"start_policy,omitempty"`
	WindowStart         *string  `json:"window_start,omitempty"`
	WindowEnd           *string  `json:"window_end,omitempty"`
	BenchmarkAssetKey   *string  `json:"benchmark_asset_key,omitempty"`
	RiskFreeRate        *float64 `json:"risk_free_rate,omitempty"`
	TransactionCostRate *float64 `json:"transaction_cost_rate,omitempty"`
	TailRiskConfidence  *float64 `json:"tail_risk_confidence,omitempty"`
	TailRiskHorizonDays *int     `json:"tail_risk_horizon_days,omitempty"`
	Status              *string  `json:"status,omitempty"`
	Tags                []string `json:"tags,omitempty"`
}

func (s *ResearchService) UpdateCollection(
	ctx context.Context, id string, in ResearchCollectionUpdate,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	collection, err := s.research.GetCollection(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	currentCollectionUpdatedAt := collection.UpdatedAt
	if err := applyResearchCollectionUpdate(&collection, in); err != nil {
		return zero, err
	}
	if err := s.applyResearchBenchmarkUpdate(ctx, &collection, in.BenchmarkAssetKey); err != nil {
		return zero, err
	}
	if err := validateUpdatedResearchCollection(&collection); err != nil {
		return zero, err
	}
	collection.UpdatedAt = maxInt64(s.now().UnixMilli(), currentCollectionUpdatedAt+1)
	if err := s.research.UpdateCollectionTx(ctx, nil, collection); err != nil {
		return zero, wrapRepo("update research collection", err)
	}
	return s.GetCollection(ctx, id)
}

func applyResearchCollectionUpdate(
	collection *repository.ResearchCollection, in ResearchCollectionUpdate,
) error {
	if err := applyResearchCollectionIdentity(collection, in); err != nil {
		return err
	}
	applyResearchCollectionWindow(collection, in)
	return applyResearchCollectionMetadata(collection, in)
}

func applyResearchCollectionIdentity(
	collection *repository.ResearchCollection, in ResearchCollectionUpdate,
) error {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return newErr("invalid_request", "name cannot be empty", nil)
		}
		collection.Name = name
	}
	if in.Description != nil {
		collection.Description = strings.TrimSpace(*in.Description)
	}
	if in.BaseCurrency != nil {
		collection.BaseCurrency = *in.BaseCurrency
	}
	if in.InitialAmountMinor != nil {
		if *in.InitialAmountMinor <= 0 {
			return newErr("invalid_request", "initial_amount_minor must be positive", nil)
		}
		collection.InitialAmountMinor = *in.InitialAmountMinor
	}
	return nil
}

func applyResearchCollectionWindow(
	collection *repository.ResearchCollection, in ResearchCollectionUpdate,
) {
	if in.RebalancePolicy != nil {
		collection.RebalancePolicy = *in.RebalancePolicy
	}
	if in.RebalanceThreshold != nil {
		collection.RebalanceThreshold = *in.RebalanceThreshold
	}
	if in.StartPolicy != nil {
		collection.StartPolicy = *in.StartPolicy
	}
	if in.WindowStart != nil {
		collection.WindowStart = *in.WindowStart
	}
	if in.WindowEnd != nil {
		collection.WindowEnd = *in.WindowEnd
	}
}

func applyResearchCollectionMetadata(
	collection *repository.ResearchCollection, in ResearchCollectionUpdate,
) error {
	if in.RiskFreeRate != nil {
		collection.RiskFreeRate = *in.RiskFreeRate
	}
	if in.TransactionCostRate != nil {
		collection.TransactionCostRate = *in.TransactionCostRate
	}
	if in.TailRiskConfidence != nil {
		collection.TailRiskConfidence = *in.TailRiskConfidence
	}
	if in.TailRiskHorizonDays != nil {
		collection.TailRiskHorizonDays = *in.TailRiskHorizonDays
	}
	if in.Status != nil {
		if *in.Status != repository.ResearchCollectionStatusActive &&
			*in.Status != repository.ResearchCollectionStatusArchived {
			return newErr("invalid_request", "status must be active or archived", nil)
		}
		collection.Status = *in.Status
	}
	if in.Tags != nil {
		tagsJSON, err := json.Marshal(in.Tags)
		if err != nil {
			return fmt.Errorf("marshal tags: %w", err)
		}
		collection.TagsJSON = string(tagsJSON)
	}
	return nil
}

func (s *ResearchService) applyResearchBenchmarkUpdate(
	ctx context.Context,
	collection *repository.ResearchCollection,
	benchmarkAssetKey *string,
) error {
	if benchmarkAssetKey == nil {
		return nil
	}
	key := strings.TrimSpace(*benchmarkAssetKey)
	if err := s.validateResearchBenchmarkAsset(ctx, key); err != nil {
		return err
	}
	collection.BenchmarkAssetKey = key
	return nil
}

func validateUpdatedResearchCollection(collection *repository.ResearchCollection) error {
	if err := validateCollectionEnums(
		collection.BaseCurrency, collection.RebalancePolicy, collection.StartPolicy,
	); err != nil {
		return err
	}
	if err := validateWindowDates(
		collection.StartPolicy, collection.WindowStart, collection.WindowEnd,
	); err != nil {
		return err
	}
	if collection.RebalancePolicy == ResearchRebalanceThreshold && collection.RebalanceThreshold <= 0 {
		return newErr("invalid_request",
			"rebalance_threshold must be positive for the threshold policy", nil)
	}
	tailRisk, err := CanonicalTailRiskSpec(TailRiskSpec{
		Confidence: collection.TailRiskConfidence, HorizonDays: collection.TailRiskHorizonDays,
	})
	if err != nil {
		return tailRiskAppError(err)
	}
	collection.TailRiskConfidence = tailRisk.Confidence
	collection.TailRiskHorizonDays = tailRisk.HorizonDays
	return nil
}

// DeleteCollection archives by default; hard=true removes the collection and
// cascades items and runs.
func (s *ResearchService) DeleteCollection(ctx context.Context, id string, hard bool) error {
	if hard {
		if err := s.research.DeleteCollection(ctx, id); err != nil {
			if errors.Is(err, repository.ErrResearchCollectionNotFound) {
				return newErr("research_collection_not_found", "research collection not found", nil)
			}
			return wrapRepo("delete research collection", err)
		}
		return nil
	}
	if err := s.research.SetCollectionStatus(ctx, id,
		repository.ResearchCollectionStatusArchived, s.now().UnixMilli()); err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return newErr("research_collection_not_found", "research collection not found", nil)
		}
		return wrapRepo("archive research collection", err)
	}
	return nil
}

// --- items ---

func (s *ResearchService) AddItem(
	ctx context.Context, collectionID string, in ResearchCollectionItemInput,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	collection, err := s.research.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	existing, err := s.research.ListItems(ctx, collectionID)
	if err != nil {
		return zero, wrapRepo("list research items", err)
	}
	if err := s.validateCanonicalFundConflict(ctx, in.AssetKey, existing); err != nil {
		return zero, err
	}
	now := s.now().UnixMilli()
	items, err := s.buildItems(ctx, collectionID, []ResearchCollectionItemInput{in}, now)
	if err != nil {
		return zero, err
	}
	item := items[0]
	item.SortOrder = len(existing)
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.research.CreateItemTx(ctx, tx, item); err != nil {
			if isUniqueConstraintErr(err) {
				return newErr("research_item_duplicate",
					"该资产维度已在集合中", map[string]any{"asset_key": item.AssetKey})
			}
			return fmt.Errorf("create research item: %w", err)
		}
		if err := s.research.TouchCollectionTx(ctx, tx, collection.ID, now); err != nil {
			return fmt.Errorf("touch research collection: %w", err)
		}
		return nil
	})
	if err != nil {
		return zero, wrapRepo("add research item", err)
	}
	return s.GetCollection(ctx, collectionID)
}

// ResearchItemUpdate is the PATCH items payload.
type ResearchItemUpdate struct {
	Enabled      *bool    `json:"enabled,omitempty"`
	Weight       *float64 `json:"weight,omitempty"`
	WeightLocked *bool    `json:"weight_locked,omitempty"`
	AdjustPolicy *string  `json:"adjust_policy,omitempty"`
	PointType    *string  `json:"point_type,omitempty"`
	AssetClass   *string  `json:"asset_class,omitempty"`
	Region       *string  `json:"region,omitempty"`
	Note         *string  `json:"note,omitempty"`
	SortOrder    *int     `json:"sort_order,omitempty"`
}

func (s *ResearchService) UpdateItem(
	ctx context.Context, collectionID, itemID string, in ResearchItemUpdate,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	item, err := s.research.GetItem(ctx, collectionID, itemID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchItemNotFound) {
			return zero, newErr("research_item_not_found", "collection item not found", nil)
		}
		return zero, wrapRepo("load research item", err)
	}
	if err := applyResearchItemUpdate(&item, in); err != nil {
		return zero, err
	}
	asset, err := s.assets.GetByKey(ctx, item.AssetKey)
	if err != nil {
		return zero, wrapRepo("load research item asset", err)
	}
	if err := ValidateHistoryDimension(asset, item.AdjustPolicy, item.PointType); err != nil {
		return zero, err
	}
	now := s.now().UnixMilli()
	item.UpdatedAt = now
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.research.UpdateItemTx(ctx, tx, item); err != nil {
			if isUniqueConstraintErr(err) {
				return newErr("research_item_duplicate",
					"该资产维度已在集合中", map[string]any{"asset_key": item.AssetKey})
			}
			return fmt.Errorf("update research item: %w", err)
		}
		if err := s.research.TouchCollectionTx(ctx, tx, collectionID, now); err != nil {
			return fmt.Errorf("touch research collection: %w", err)
		}
		return nil
	})
	if err != nil {
		return zero, wrapRepo("update research item", err)
	}
	return s.GetCollection(ctx, collectionID)
}

func applyResearchItemUpdate(item *repository.ResearchCollectionItem, in ResearchItemUpdate) error {
	if in.Enabled != nil {
		item.Enabled = *in.Enabled
	}
	if in.Weight != nil {
		if *in.Weight < 0 || *in.Weight > 1+ResearchWeightTolerance {
			return newErr("invalid_request", "weight must be within [0, 1]", nil)
		}
		item.Weight = *in.Weight
	}
	if in.WeightLocked != nil {
		item.WeightLocked = *in.WeightLocked
	}
	if in.AdjustPolicy != nil && strings.TrimSpace(*in.AdjustPolicy) != "" {
		item.AdjustPolicy = strings.TrimSpace(*in.AdjustPolicy)
	}
	if in.PointType != nil && strings.TrimSpace(*in.PointType) != "" {
		item.PointType = strings.TrimSpace(*in.PointType)
	}
	if in.AssetClass != nil {
		item.AssetClass = strings.TrimSpace(*in.AssetClass)
	}
	if in.Region != nil {
		item.Region = strings.TrimSpace(*in.Region)
	}
	if in.Note != nil {
		item.Note = strings.TrimSpace(*in.Note)
	}
	if in.SortOrder != nil {
		item.SortOrder = *in.SortOrder
	}
	return nil
}

func (s *ResearchService) DeleteItem(
	ctx context.Context, collectionID, itemID string,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	now := s.now().UnixMilli()
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.research.DeleteItemTx(ctx, tx, collectionID, itemID); err != nil {
			return fmt.Errorf("delete research item: %w", err)
		}
		if err := s.research.TouchCollectionTx(ctx, tx, collectionID, now); err != nil {
			return fmt.Errorf("touch research collection: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrResearchItemNotFound) {
			return zero, newErr("research_item_not_found", "collection item not found", nil)
		}
		return zero, wrapRepo("delete research item", err)
	}
	return s.GetCollection(ctx, collectionID)
}

// NormalizeWeights rescales enabled unlocked items so enabled weights sum to
// exactly 1, honoring locked weights (td/099 §3.4). When every unlocked
// weight is 0, the remainder is split equally.
func (s *ResearchService) NormalizeWeights(
	ctx context.Context, collectionID string,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	if _, err := s.research.GetCollection(ctx, collectionID); err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	items, err := s.research.ListItems(ctx, collectionID)
	if err != nil {
		return zero, wrapRepo("list research items", err)
	}

	unlocked, err := normalizeResearchItemWeights(items)
	if err != nil {
		return zero, err
	}

	now := s.now().UnixMilli()
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		for _, idx := range unlocked {
			items[idx].UpdatedAt = now
			if err := s.research.UpdateItemTx(ctx, tx, items[idx]); err != nil {
				return fmt.Errorf("normalize research item weight: %w", err)
			}
		}
		if err := s.research.TouchCollectionTx(ctx, tx, collectionID, now); err != nil {
			return fmt.Errorf("touch research collection: %w", err)
		}
		return nil
	})
	if err != nil {
		return zero, wrapRepo("normalize research weights", err)
	}
	return s.GetCollection(ctx, collectionID)
}

func normalizeResearchItemWeights(items []repository.ResearchCollectionItem) ([]int, error) {
	lockedSum, unlockedSum := 0.0, 0.0
	unlocked := make([]int, 0, len(items))
	for i, item := range items {
		if !item.Enabled {
			continue
		}
		if item.WeightLocked {
			lockedSum += item.Weight
		} else {
			unlocked = append(unlocked, i)
			unlockedSum += item.Weight
		}
	}
	if len(unlocked) == 0 {
		return nil, newErr("research_normalize_impossible", "没有可归一化的未锁定资产", nil)
	}
	remainder := 1 - lockedSum
	if remainder < -ResearchWeightTolerance {
		return nil, newErr("research_normalize_impossible",
			"锁定权重合计已超过 100%", map[string]any{"locked_sum": lockedSum})
	}
	remainder = math.Max(0, remainder)
	for _, index := range unlocked {
		if unlockedSum > 0 {
			items[index].Weight = items[index].Weight / unlockedSum * remainder
		} else {
			items[index].Weight = remainder / float64(len(unlocked))
		}
	}
	sum := lockedSum
	for _, index := range unlocked {
		sum += items[index].Weight
	}
	items[unlocked[len(unlocked)-1]].Weight += 1 - sum
	return unlocked, nil
}

// --- readiness ---

func (s *ResearchService) GetReadiness(
	ctx context.Context, collectionID string,
) (ResearchReadiness, error) {
	collection, err := s.research.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return ResearchReadiness{}, newErr("research_collection_not_found",
				"research collection not found", nil)
		}
		return ResearchReadiness{}, wrapRepo("load research collection", err)
	}
	ds, err := s.loadResearchDataset(ctx, collection)
	if err != nil {
		return ResearchReadiness{}, err
	}
	return evaluateResearchReadiness(ds, s.now()), nil
}

// --- sync-history ---

// ResearchSyncRequest optionally narrows the batch sync to specific assets
// (single-asset retry) or forces refresh of up-to-date assets.
type ResearchSyncRequest struct {
	AssetKeys []string `json:"asset_keys,omitempty"`
	Force     bool     `json:"force"`
}

// ResearchSyncAssetResult is one asset's outcome in the sync response.
type ResearchSyncAssetResult struct {
	AssetKey string          `json:"asset_key"`
	Status   string          `json:"status"` // created | existed | skipped
	Reason   string          `json:"reason,omitempty"`
	Task     *WorkerTaskView `json:"task,omitempty"`
}

// ResearchSyncFXResult is one FX pair's outcome.
type ResearchSyncFXResult struct {
	Pair   string          `json:"pair"`
	Status string          `json:"status"`
	Task   *WorkerTaskView `json:"task,omitempty"`
}

// ResearchSyncBlocked reports an asset that could not get a task.
type ResearchSyncBlocked struct {
	AssetKey string `json:"asset_key"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// ResearchSyncResult is the POST /collections/{id}/sync-history response
// (td/099 §5.4).
type ResearchSyncResult struct {
	Assets  []ResearchSyncAssetResult `json:"assets"`
	FX      []ResearchSyncFXResult    `json:"fx"`
	Blocked []ResearchSyncBlocked     `json:"blocked"`
}

// SyncCollectionHistory batch-creates (or reuses) asset_history_sync tasks
// for enabled assets that need data, plus fx_rate_sync for cross-currency
// collections (td/099 §3.5).
func (s *ResearchService) SyncCollectionHistory(
	ctx context.Context, collectionID string, req ResearchSyncRequest,
) (ResearchSyncResult, error) {
	out := ResearchSyncResult{
		Assets:  []ResearchSyncAssetResult{},
		FX:      []ResearchSyncFXResult{},
		Blocked: []ResearchSyncBlocked{},
	}
	collection, err := s.research.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return out, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return out, wrapRepo("load research collection", err)
	}
	ds, err := s.loadResearchDataset(ctx, collection)
	if err != nil {
		return out, err
	}

	only := map[string]bool{}
	for _, key := range req.AssetKeys {
		only[key] = true
	}
	nowDay := int(s.now().Unix() / 86400)
	if err := s.syncResearchAssets(ctx, ds, req, only, nowDay, &out); err != nil {
		return out, err
	}
	if err := s.syncResearchFX(ctx, ds, req.Force, nowDay, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *ResearchService) syncResearchAssets(
	ctx context.Context,
	ds *researchDataset,
	req ResearchSyncRequest,
	only map[string]bool,
	nowDay int,
	out *ResearchSyncResult,
) error {
	for _, asset := range ds.Enabled {
		if asset.IsCash || (len(only) > 0 && !only[asset.Item.AssetKey]) {
			continue
		}
		result, blocked, err := s.syncResearchAsset(ctx, asset, req.Force || len(only) > 0, nowDay)
		if err != nil {
			return err
		}
		if blocked != nil {
			out.Blocked = append(out.Blocked, *blocked)
		} else {
			out.Assets = append(out.Assets, result)
		}
	}
	return nil
}

func (s *ResearchService) syncResearchAsset(
	ctx context.Context, asset researchAssetData, force bool, nowDay int,
) (ResearchSyncAssetResult, *ResearchSyncBlocked, error) {
	if asset.Task != nil && repository.IsActiveWorkerTaskStatus(asset.Task.Status) {
		view := taskToView(*asset.Task)
		return ResearchSyncAssetResult{
			AssetKey: asset.Item.AssetKey, Status: "existed", Task: &view,
		}, nil, nil
	}
	need, reason := researchAssetNeedsSync(asset, nowDay)
	if force {
		need = true
		if reason == "" {
			reason = "forced"
		}
	}
	if !need {
		return ResearchSyncAssetResult{
			AssetKey: asset.Item.AssetKey, Status: "skipped", Reason: "up_to_date",
		}, nil, nil
	}
	result, err := s.marketSvc.SyncHistory(ctx, HistorySyncRequest{
		AssetKey: asset.Item.AssetKey, AdjustPolicy: asset.Item.AdjustPolicy,
		PointType: asset.Item.PointType, Mode: historyModeDefaultRefresh,
	})
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			return ResearchSyncAssetResult{}, &ResearchSyncBlocked{
				AssetKey: asset.Item.AssetKey, Code: appErr.Code, Message: appErr.Message,
			}, nil
		}
		return ResearchSyncAssetResult{}, nil, err
	}
	status := "created"
	if result.Existed {
		status = "existed"
	}
	task := result.Task
	return ResearchSyncAssetResult{
		AssetKey: asset.Item.AssetKey, Status: status, Reason: reason, Task: &task,
	}, nil, nil
}

func (s *ResearchService) syncResearchFX(
	ctx context.Context,
	ds *researchDataset,
	force bool,
	nowDay int,
	out *ResearchSyncResult,
) error {
	if len(ds.FXPairs) == 0 {
		return nil
	}
	if !force && !researchFXNeedsSync(ds, nowDay) {
		for _, pair := range ds.FXPairs {
			out.FX = append(out.FX, ResearchSyncFXResult{Pair: pair, Status: "skipped"})
		}
		return nil
	}
	result, err := s.marketSvc.SyncFXRates(ctx)
	if err != nil {
		return err
	}
	status := "created"
	if result.Existed {
		status = "existed"
	}
	task := result.Task
	for _, pair := range ds.FXPairs {
		out.FX = append(out.FX, ResearchSyncFXResult{Pair: pair, Status: status, Task: &task})
	}
	return nil
}

func researchFXNeedsSync(ds *researchDataset, nowDay int) bool {
	if ds.FXSyncActive {
		return true
	}
	for _, pair := range ds.FXPairs {
		fx := ds.FX[pair]
		if fx == nil || !fx.Found {
			return true
		}
		last := fx.Points[len(fx.Points)-1].TradeDate
		if day, err := parseResearchDate(last); err == nil && nowDay-day > ResearchStaleToleranceDays("") {
			return true
		}
	}
	return false
}

// researchAssetNeedsSync decides whether one asset needs a history refresh:
// missing history, stale data, or a failed last sync (td/099 §3.5).
func researchAssetNeedsSync(a researchAssetData, nowDay int) (bool, string) {
	if len(a.Points) == 0 {
		return true, "missing_history"
	}
	if a.Task != nil && a.Task.Status == repository.WorkerTaskStatusFailed {
		return true, "retry_failed"
	}
	if a.Asset.ListingStatus != "" && a.Asset.ListingStatus != "active" {
		return false, ""
	}
	asOf := a.State.DataAsOf
	if asOf == "" {
		asOf = a.Points[len(a.Points)-1].TradeDate
	}
	if d, err := parseResearchDate(asOf); err == nil {
		if nowDay-d > ResearchStaleToleranceDays(a.Asset.InstrumentType) {
			return true, "stale"
		}
	}
	return false, ""
}

// GetTask proxies single worker-task polling for the research task panel.
func (s *ResearchService) GetTask(ctx context.Context, taskID string) (WorkerTaskView, error) {
	return s.marketSvc.GetTask(ctx, taskID)
}

// --- backtest creation ---

// ResearchBacktestResult is the POST /collections/{id}/backtests response.
type ResearchBacktestResult struct {
	Run    ResearchRunView `json:"run"`
	Reused bool            `json:"reused"`
}

// researchInputSnapshot is the frozen run input stored on the run row. It
// carries parameters and per-series summaries/hashes — not raw points; the
// runner reloads points and verifies the source hash.
type researchInputSnapshot struct {
	EngineVersion string                   `json:"engine_version"`
	SourceHash    string                   `json:"source_hash"`
	CommonStart   string                   `json:"common_start"`
	CommonEnd     string                   `json:"common_end"`
	WindowStart   string                   `json:"window_start"`
	WindowEnd     string                   `json:"window_end"`
	Collection    researchSnapshotParams   `json:"collection"`
	Assets        []researchSnapshotAsset  `json:"assets"`
	FX            []researchSnapshotSeries `json:"fx"`
	Benchmark     *researchSnapshotAsset   `json:"benchmark,omitempty"`
}

type researchSnapshotParams struct {
	CollectionID        string  `json:"collection_id"`
	BaseCurrency        string  `json:"base_currency"`
	InitialAmountMinor  int64   `json:"initial_amount_minor"`
	RebalancePolicy     string  `json:"rebalance_policy"`
	RebalanceThreshold  float64 `json:"rebalance_threshold"`
	StartPolicy         string  `json:"start_policy"`
	RiskFreeRate        float64 `json:"risk_free_rate"`
	TransactionCostRate float64 `json:"transaction_cost_rate"`
	TailRiskConfidence  float64 `json:"tail_risk_confidence"`
	TailRiskHorizonDays int     `json:"tail_risk_horizon_days"`
	BenchmarkAssetKey   string  `json:"benchmark_asset_key,omitempty"`
}

type researchSnapshotAsset struct {
	ItemID       string  `json:"item_id"`
	AssetKey     string  `json:"asset_key"`
	Name         string  `json:"name"`
	Currency     string  `json:"currency"`
	Weight       float64 `json:"weight"`
	WeightLocked bool    `json:"weight_locked"`
	IsCash       bool    `json:"is_cash"`
	AdjustPolicy string  `json:"adjust_policy"`
	PointType    string  `json:"point_type"`
	researchSnapshotSeries
}

type researchSnapshotSeries struct {
	Pair       string `json:"pair,omitempty"`
	SourceName string `json:"source_name,omitempty"`
	// AnchorDate is the pre-window forward-fill anchor (last observation
	// strictly before the window start) when the series has no observation
	// on the window start day itself (td/100 Finding 1). When set it equals
	// FirstDate.
	AnchorDate string `json:"anchor_date,omitempty"`
	FirstDate  string `json:"first_date,omitempty"`
	LastDate   string `json:"last_date,omitempty"`
	PointCount int    `json:"point_count"`
	PointsHash string `json:"points_hash,omitempty"`
}

// CreateBacktest gates on readiness, freezes the input snapshot, computes
// source/input hashes and either reuses an existing run or creates job+run
// in one transaction (td/099 §5.5).
func (s *ResearchService) CreateBacktest(
	ctx context.Context, collectionID string,
) (ResearchBacktestResult, error) {
	var zero ResearchBacktestResult
	collection, err := s.research.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	if collection.Status != repository.ResearchCollectionStatusActive {
		return zero, newErr("research_collection_archived",
			"归档的集合不能运行回测", nil)
	}
	ds, err := s.loadResearchDataset(ctx, collection)
	if err != nil {
		return zero, err
	}
	readiness := evaluateResearchReadiness(ds, s.now())
	if !readiness.Ready {
		return zero, newErr("research_collection_not_ready",
			"集合未通过回测准入检查", map[string]any{"readiness": readiness})
	}

	snapshot := buildResearchSnapshot(ds, readiness)
	inputHash := computeResearchInputHash(snapshot, ds)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return zero, fmt.Errorf("marshal input snapshot: %w", err)
	}

	if reused, found, err := s.findReusableResearchRun(ctx, collectionID, inputHash); err != nil {
		return zero, err
	} else if found {
		return reused, nil
	}

	now := s.now().UnixMilli()
	runID := "rbr_" + uuid.New().String()
	jobID := "job_" + uuid.New().String()
	payloadJSON, err := json.Marshal(map[string]string{
		"run_id": runID, "collection_id": collectionID,
	})
	if err != nil {
		return zero, fmt.Errorf("marshal job payload: %w", err)
	}
	run := repository.ResearchBacktestRun{
		ID:                runID,
		CollectionID:      collectionID,
		JobID:             jobID,
		InputHash:         inputHash,
		InputSnapshotJSON: string(snapshotJSON),
		SourceHash:        snapshot.SourceHash,
		EngineVersion:     ResearchEngineVersion,
		BaseCurrency:      collection.BaseCurrency,
		RebalancePolicy:   collection.RebalancePolicy,
		WindowStart:       snapshot.WindowStart,
		WindowEnd:         snapshot.WindowEnd,
		Status:            repository.ResearchRunStatusQueued,
		SummaryJSON:       "{}",
		DataQualityJSON:   "{}",
		CreatedAt:         now,
	}
	if err := s.persistQueuedResearchRun(ctx, run, inputHash, string(payloadJSON), now); err != nil {
		return zero, err
	}
	return ResearchBacktestResult{Run: buildRunView(run)}, nil
}

func (s *ResearchService) findReusableResearchRun(
	ctx context.Context, collectionID, inputHash string,
) (ResearchBacktestResult, bool, error) {
	run, err := s.research.FindSucceededRunByInputHash(ctx, collectionID, inputHash)
	if err == nil {
		return ResearchBacktestResult{Run: buildRunView(run), Reused: true}, true, nil
	}
	if !errors.Is(err, repository.ErrResearchRunNotFound) {
		return ResearchBacktestResult{}, false, wrapRepo("find succeeded run", err)
	}
	run, err = s.research.FindActiveRunByInputHash(ctx, collectionID, inputHash)
	if errors.Is(err, repository.ErrResearchRunNotFound) {
		return ResearchBacktestResult{}, false, nil
	}
	if err != nil {
		return ResearchBacktestResult{}, false, wrapRepo("find active run", err)
	}
	return ResearchBacktestResult{Run: buildRunView(run), Reused: true}, true, nil
}

func (s *ResearchService) persistQueuedResearchRun(
	ctx context.Context,
	run repository.ResearchBacktestRun,
	inputHash, payloadJSON string,
	now int64,
) error {
	job := repository.Job{
		ID: run.JobID, Type: repository.JobTypeResearchBacktest,
		Status: repository.JobStatusQueued, InputHash: inputHash,
		PayloadJSON: payloadJSON, CreatedAt: now,
	}
	return s.persistQueuedResearchJob(ctx, job, "create research backtest", func(tx *sql.Tx) error {
		if err := s.research.CreateRunTx(ctx, tx, run); err != nil {
			return fmt.Errorf("create research backtest run: %w", err)
		}
		return nil
	})
}

func (s *ResearchService) persistQueuedResearchJob(
	ctx context.Context,
	job repository.Job,
	operation string,
	createRun func(*sql.Tx) error,
) error {
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.jobs.Create(ctx, tx, job); err != nil {
			return fmt.Errorf("create research job: %w", err)
		}
		return createRun(tx)
	})
	if err != nil {
		return wrapRepo(operation, err)
	}
	return nil
}

// buildResearchSnapshot freezes the dataset into the auditable input
// snapshot (td/099 §5.6).
func buildResearchSnapshot(ds *researchDataset, readiness ResearchReadiness) researchInputSnapshot {
	snapshot := researchInputSnapshot{
		EngineVersion: ResearchEngineVersion,
		CommonStart:   readiness.CommonStart,
		CommonEnd:     readiness.CommonEnd,
		WindowStart:   readiness.WindowStart,
		WindowEnd:     readiness.WindowEnd,
		Collection:    snapshotResearchCollectionParams(ds.Collection),
		FX:            []researchSnapshotSeries{},
	}
	winLo, winHi := readiness.WindowStart, readiness.WindowEnd

	assets := make([]researchSnapshotAsset, 0, len(ds.Enabled))
	for _, a := range ds.Enabled {
		entry := researchSnapshotAsset{
			ItemID:       a.Item.ID,
			AssetKey:     a.Item.AssetKey,
			Name:         a.Asset.Name,
			Currency:     a.Asset.Currency,
			Weight:       a.Item.Weight,
			WeightLocked: a.Item.WeightLocked,
			IsCash:       a.IsCash,
			AdjustPolicy: a.Item.AdjustPolicy,
			PointType:    a.Item.PointType,
		}
		if !a.IsCash {
			entry.researchSnapshotSeries = summarizeAssetSeries(a.Points, winLo, winHi)
		}
		assets = append(assets, entry)
	}
	sort.Slice(assets, func(i, j int) bool {
		if assets[i].AssetKey != assets[j].AssetKey {
			return assets[i].AssetKey < assets[j].AssetKey
		}
		if assets[i].AdjustPolicy != assets[j].AdjustPolicy {
			return assets[i].AdjustPolicy < assets[j].AdjustPolicy
		}
		return assets[i].PointType < assets[j].PointType
	})
	snapshot.Assets = assets

	for _, pair := range ds.FXPairs {
		fx := ds.FX[pair]
		if fx == nil {
			continue
		}
		series := summarizeFXSeries(fx.Points, winLo, winHi)
		series.Pair = pair
		series.SourceName = fx.SourceName
		snapshot.FX = append(snapshot.FX, series)
	}

	if ds.Benchmark != nil {
		b := ds.Benchmark
		entry := researchSnapshotAsset{
			AssetKey:     b.Item.AssetKey,
			Name:         b.Asset.Name,
			Currency:     b.Asset.Currency,
			IsCash:       b.IsCash,
			AdjustPolicy: b.Item.AdjustPolicy,
			PointType:    b.Item.PointType,
		}
		if !b.IsCash {
			entry.researchSnapshotSeries = summarizeAssetSeries(b.Points, winLo, winHi)
		}
		snapshot.Benchmark = &entry
	}

	snapshot.SourceHash = computeResearchSourceHash(snapshot)
	return snapshot
}

func snapshotResearchCollectionParams(collection repository.ResearchCollection) researchSnapshotParams {
	return researchSnapshotParams{
		CollectionID: collection.ID, BaseCurrency: collection.BaseCurrency,
		InitialAmountMinor: collection.InitialAmountMinor, RebalancePolicy: collection.RebalancePolicy,
		RebalanceThreshold: collection.RebalanceThreshold, StartPolicy: collection.StartPolicy,
		RiskFreeRate: collection.RiskFreeRate, TransactionCostRate: collection.TransactionCostRate,
		TailRiskConfidence: collection.TailRiskConfidence, TailRiskHorizonDays: collection.TailRiskHorizonDays,
		BenchmarkAssetKey: collection.BenchmarkAssetKey,
	}
}

// researchSeriesObs is the source-agnostic observation shape shared by the
// asset and FX series summaries.
type researchSeriesObs struct {
	date   string
	value  float64
	source string
}

// summarizeResearchSeries hashes the minimal closure of observations the
// valuation actually uses (td/100 Finding 1): the in-window slice plus, when
// the series has no observation on the window start day, the last pre-window
// observation that forward-fill anchors on. Changing that anchor point must
// change the source hash.
func summarizeResearchSeries(obs []researchSeriesObs, winLo, winHi string) researchSnapshotSeries {
	anchor := researchSeriesAnchor(obs, winLo)
	out := researchSnapshotSeries{AnchorDate: anchor}
	includeFrom := winLo
	if anchor != "" {
		includeFrom = anchor
	}

	h := sha256.New()
	if out.AnchorDate != "" {
		fmt.Fprintf(h, "anchor:%s\n", out.AnchorDate)
	}
	for _, p := range obs {
		if includeFrom != "" && p.date < includeFrom {
			continue
		}
		if winHi != "" && p.date > winHi {
			continue
		}
		if out.FirstDate == "" || p.date < out.FirstDate {
			out.FirstDate = p.date
		}
		if p.date > out.LastDate {
			out.LastDate = p.date
		}
		out.PointCount++
		fmt.Fprintf(h, "%s:%s\n", p.date, strconv.FormatFloat(p.value, 'g', 17, 64))
		if out.SourceName == "" {
			out.SourceName = p.source
		}
	}
	out.PointsHash = hex.EncodeToString(h.Sum(nil))
	return out
}

func researchSeriesAnchor(obs []researchSeriesObs, windowStart string) string {
	if windowStart == "" {
		return ""
	}
	anchor := ""
	for _, point := range obs {
		if point.date == windowStart {
			return ""
		}
		if point.date < windowStart && point.date > anchor {
			anchor = point.date
		}
	}
	return anchor
}

// summarizeAssetSeries hashes the usable slice of one asset series
// (in-window points plus the pre-window forward-fill anchor).
func summarizeAssetSeries(points []repository.MarketAssetPoint, winLo, winHi string) researchSnapshotSeries {
	obs := make([]researchSeriesObs, len(points))
	for i, p := range points {
		obs[i] = researchSeriesObs{date: p.TradeDate, value: p.Value, source: p.SourceName}
	}
	return summarizeResearchSeries(obs, winLo, winHi)
}

func summarizeFXSeries(points []repository.MarketDataPoint, winLo, winHi string) researchSnapshotSeries {
	obs := make([]researchSeriesObs, len(points))
	for i, p := range points {
		obs[i] = researchSeriesObs{date: p.TradeDate, value: p.Value, source: p.SourceName}
	}
	return summarizeResearchSeries(obs, winLo, winHi)
}

// computeResearchSourceHash hashes the market-data facts of one snapshot
// (td/099 §5.6): per-series identity, coverage, the pre-window forward-fill
// anchor, per-point hash and the common window. Anchor values are already
// part of PointsHash; the anchor date is written explicitly so the hash
// distinguishes anchored from non-anchored coverage (td/100 Finding 1).
func computeResearchSourceHash(snapshot researchInputSnapshot) string {
	h := sha256.New()
	fmt.Fprintf(h, "common:%s..%s\n", snapshot.CommonStart, snapshot.CommonEnd)
	for _, a := range snapshot.Assets {
		fmt.Fprintf(h, "asset:%s|%s|%s|%s|%s|%s|%s|%d|%s\n",
			a.AssetKey, a.AdjustPolicy, a.PointType, a.SourceName,
			a.AnchorDate, a.FirstDate, a.LastDate, a.PointCount, a.PointsHash)
	}
	for _, fx := range snapshot.FX {
		fmt.Fprintf(h, "fx:%s|%s|%s|%s|%s|%d|%s\n",
			fx.Pair, fx.SourceName, fx.AnchorDate, fx.FirstDate, fx.LastDate,
			fx.PointCount, fx.PointsHash)
	}
	if b := snapshot.Benchmark; b != nil {
		fmt.Fprintf(h, "benchmark:%s|%s|%s|%s|%s|%s|%s|%d|%s\n",
			b.AssetKey, b.AdjustPolicy, b.PointType, b.SourceName,
			b.AnchorDate, b.FirstDate, b.LastDate, b.PointCount, b.PointsHash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// computeResearchInputHash hashes everything that decides run reuse
// (td/099 §5.6): source hash, collection parameters, enabled items, engine
// version, rebalance rule and window policy.
func computeResearchInputHash(snapshot researchInputSnapshot, ds *researchDataset) string {
	h := sha256.New()
	fmt.Fprintf(h, "source:%s\n", snapshot.SourceHash)
	fmt.Fprintf(h, "engine:%s\n", snapshot.EngineVersion)
	c := snapshot.Collection
	fmt.Fprintf(h, "params:%s|%d|%s|%s|%s|%s|%s|%s|%s|%d|%s\n",
		c.BaseCurrency, c.InitialAmountMinor, c.RebalancePolicy,
		strconv.FormatFloat(c.RebalanceThreshold, 'g', 17, 64),
		c.StartPolicy,
		strconv.FormatFloat(c.RiskFreeRate, 'g', 17, 64),
		strconv.FormatFloat(c.TransactionCostRate, 'g', 17, 64),
		c.BenchmarkAssetKey,
		strconv.FormatFloat(c.TailRiskConfidence, 'g', 17, 64), c.TailRiskHorizonDays,
		TailRiskAlgorithmVersion)
	fmt.Fprintf(h, "window:%s..%s\n", snapshot.WindowStart, snapshot.WindowEnd)
	items := make([]repository.ResearchCollectionItem, 0, len(ds.Enabled))
	for _, a := range ds.Enabled {
		items = append(items, a.Item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AssetKey != items[j].AssetKey {
			return items[i].AssetKey < items[j].AssetKey
		}
		if items[i].AdjustPolicy != items[j].AdjustPolicy {
			return items[i].AdjustPolicy < items[j].AdjustPolicy
		}
		return items[i].PointType < items[j].PointType
	})
	for _, item := range items {
		fmt.Fprintf(h, "item:%s|%s|%s|%s|%t\n",
			item.AssetKey, item.AdjustPolicy, item.PointType,
			strconv.FormatFloat(item.Weight, 'g', 17, 64), item.Enabled)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// --- run views & queries ---

// ResearchRunSummaryView is the parsed summary block of a succeeded run.
type ResearchRunSummaryView = BacktestSummary

// ResearchRunView is the API shape of one run.
type ResearchRunView struct {
	ID              string           `json:"id"`
	CollectionID    string           `json:"collection_id"`
	JobID           string           `json:"job_id"`
	InputHash       string           `json:"input_hash"`
	SourceHash      string           `json:"source_hash"`
	EngineVersion   string           `json:"engine_version"`
	BaseCurrency    string           `json:"base_currency"`
	RebalancePolicy string           `json:"rebalance_policy"`
	WindowStart     string           `json:"window_start"`
	WindowEnd       string           `json:"window_end"`
	Status          string           `json:"status"`
	Summary         json.RawMessage  `json:"summary,omitempty"`
	DataQuality     json.RawMessage  `json:"data_quality,omitempty"`
	CreatedAt       int64            `json:"created_at"`
	CompletedAt     *int64           `json:"completed_at,omitempty"`
	Job             *ResearchJobView `json:"job,omitempty"`
}

// ResearchJobView is the embedded job progress block.
type ResearchJobView struct {
	Status          string `json:"status"`
	Phase           string `json:"phase"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	RetryCount      int    `json:"retry_count"`
	HeartbeatAt     *int64 `json:"heartbeat_at,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

func buildResearchJobView(job repository.Job) *ResearchJobView {
	return &ResearchJobView{
		Status:          job.Status,
		Phase:           job.Phase,
		ProgressCurrent: job.ProgressCurrent,
		ProgressTotal:   job.ProgressTotal,
		RetryCount:      job.RetryCount,
		HeartbeatAt:     job.HeartbeatAt,
		ErrorCode:       job.ErrorCode,
		ErrorMessage:    job.ErrorMessage,
	}
}

func applyBacktestJobState(view *ResearchRunView, job repository.Job) {
	view.Job = buildResearchJobView(job)
	if view.Status != repository.ResearchRunStatusQueued &&
		view.Status != repository.ResearchRunStatusRunning {
		return
	}
	view.Status = job.Status
	if job.FinishedAt != nil {
		view.CompletedAt = job.FinishedAt
	}
}

func buildRunView(run repository.ResearchBacktestRun) ResearchRunView {
	view := ResearchRunView{
		ID:              run.ID,
		CollectionID:    run.CollectionID,
		JobID:           run.JobID,
		InputHash:       run.InputHash,
		SourceHash:      run.SourceHash,
		EngineVersion:   run.EngineVersion,
		BaseCurrency:    run.BaseCurrency,
		RebalancePolicy: run.RebalancePolicy,
		WindowStart:     run.WindowStart,
		WindowEnd:       run.WindowEnd,
		Status:          run.Status,
		CreatedAt:       run.CreatedAt,
		CompletedAt:     run.CompletedAt,
	}
	if run.SummaryJSON != "" && run.SummaryJSON != "{}" {
		view.Summary = json.RawMessage(run.SummaryJSON)
	}
	if run.DataQualityJSON != "" && run.DataQualityJSON != "{}" {
		view.DataQuality = json.RawMessage(run.DataQualityJSON)
	}
	return view
}

func parseRunSummary(summaryJSON string) *ResearchRunSummaryView {
	if summaryJSON == "" || summaryJSON == "{}" {
		return nil
	}
	var summary ResearchRunSummaryView
	if err := json.Unmarshal([]byte(summaryJSON), &summary); err != nil {
		return nil
	}
	return &summary
}

// ResearchRunDetail is the GET /runs/{id} response: run + years + months +
// input snapshot.
type ResearchRunDetail struct {
	ResearchRunView
	Years         []repository.ResearchBacktestYear  `json:"years"`
	Months        []repository.ResearchBacktestMonth `json:"months"`
	InputSnapshot json.RawMessage                    `json:"input_snapshot,omitempty"`
}

func (s *ResearchService) GetRun(ctx context.Context, runID string) (ResearchRunDetail, error) {
	var zero ResearchRunDetail
	run, err := s.research.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchRunNotFound) {
			return zero, newErr("research_run_not_found", "research run not found", nil)
		}
		return zero, wrapRepo("load research run", err)
	}
	detail := ResearchRunDetail{ResearchRunView: buildRunView(run)}
	if run.InputSnapshotJSON != "" {
		detail.InputSnapshot = json.RawMessage(run.InputSnapshotJSON)
	}
	if job, err := s.jobs.GetByID(ctx, run.JobID); err == nil {
		applyBacktestJobState(&detail.ResearchRunView, job)
	}
	years, err := s.research.ListYears(ctx, runID)
	if err != nil {
		return zero, wrapRepo("list run years", err)
	}
	if years == nil {
		years = []repository.ResearchBacktestYear{}
	}
	months, err := s.research.ListMonths(ctx, runID)
	if err != nil {
		return zero, wrapRepo("list run months", err)
	}
	if months == nil {
		months = []repository.ResearchBacktestMonth{}
	}
	detail.Years = years
	detail.Months = months
	return detail, nil
}

// ListRuns returns runs of one collection with embedded job progress for
// active runs.
func (s *ResearchService) ListRuns(
	ctx context.Context, collectionID string, limit int,
) ([]ResearchRunView, error) {
	if _, err := s.research.GetCollection(ctx, collectionID); err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return nil, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return nil, wrapRepo("load research collection", err)
	}
	runs, err := s.research.ListRunsByCollection(ctx, collectionID, limit)
	if err != nil {
		return nil, wrapRepo("list research runs", err)
	}
	out := make([]ResearchRunView, 0, len(runs))
	for _, run := range runs {
		view := buildRunView(run)
		if run.Status == repository.ResearchRunStatusQueued ||
			run.Status == repository.ResearchRunStatusRunning {
			if job, err := s.jobs.GetByID(ctx, run.JobID); err == nil {
				applyBacktestJobState(&view, job)
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// ListRecentRuns powers the /research home's recent runs panel.
func (s *ResearchService) ListRecentRuns(ctx context.Context, limit int) ([]ResearchRunView, error) {
	runs, err := s.research.ListRecentRuns(ctx, limit)
	if err != nil {
		return nil, wrapRepo("list recent research runs", err)
	}
	out := make([]ResearchRunView, 0, len(runs))
	for _, run := range runs {
		out = append(out, buildRunView(run))
	}
	return out, nil
}

// ResearchRunPointView is one curve point with parsed weights/contributions.
type ResearchRunPointView struct {
	Date             string             `json:"date"`
	NAV              float64            `json:"nav"`
	CumulativeReturn float64            `json:"cumulative_return"`
	PeriodReturn     float64            `json:"period_return"`
	Drawdown         float64            `json:"drawdown"`
	BenchmarkNAV     *float64           `json:"benchmark_nav,omitempty"`
	BenchmarkReturn  *float64           `json:"benchmark_return,omitempty"`
	Weights          map[string]float64 `json:"weights,omitempty"`
	Contributions    map[string]float64 `json:"contributions,omitempty"`
}

// ResearchRunPointsResult is the GET /runs/{id}/points response.
type ResearchRunPointsResult struct {
	Points []ResearchRunPointView `json:"points"`
	Total  int                    `json:"total"`
}

// ResearchPointsParams narrows the points query.
type ResearchPointsParams struct {
	From           string
	To             string
	Limit          int
	Offset         int
	IncludeWeights bool
}

func (s *ResearchService) GetRunPoints(
	ctx context.Context, runID string, params ResearchPointsParams,
) (ResearchRunPointsResult, error) {
	var zero ResearchRunPointsResult
	if _, err := s.research.GetRun(ctx, runID); err != nil {
		if errors.Is(err, repository.ErrResearchRunNotFound) {
			return zero, newErr("research_run_not_found", "research run not found", nil)
		}
		return zero, wrapRepo("load research run", err)
	}
	points, total, err := s.research.ListPoints(ctx, runID, repository.ResearchPointsQuery{
		From: params.From, To: params.To, Limit: params.Limit, Offset: params.Offset,
	})
	if err != nil {
		return zero, wrapRepo("list run points", err)
	}
	out := ResearchRunPointsResult{Points: make([]ResearchRunPointView, 0, len(points)), Total: total}
	for _, p := range points {
		view := ResearchRunPointView{
			Date:             p.TradeDate,
			NAV:              p.NAV,
			CumulativeReturn: p.CumulativeReturn,
			PeriodReturn:     p.PeriodReturn,
			Drawdown:         p.Drawdown,
			BenchmarkNAV:     p.BenchmarkNAV,
			BenchmarkReturn:  p.BenchmarkReturn,
		}
		if params.IncludeWeights {
			_ = json.Unmarshal([]byte(p.WeightsJSON), &view.Weights)
			_ = json.Unmarshal([]byte(p.ContributionsJSON), &view.Contributions)
		}
		out.Points = append(out.Points, view)
	}
	return out, nil
}

// ExportRunCSV renders the run's daily curve as CSV (td/099 §5.3).
func (s *ResearchService) ExportRunCSV(ctx context.Context, runID string) (string, string, error) {
	run, err := s.research.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchRunNotFound) {
			return "", "", newErr("research_run_not_found", "research run not found", nil)
		}
		return "", "", wrapRepo("load research run", err)
	}
	points, _, err := s.research.ListPoints(ctx, runID, repository.ResearchPointsQuery{})
	if err != nil {
		return "", "", wrapRepo("list run points", err)
	}
	var b strings.Builder
	b.WriteString("date,nav,cumulative_return,period_return,drawdown,benchmark_nav,benchmark_return\n")
	for _, p := range points {
		benchNAV, benchRet := "", ""
		if p.BenchmarkNAV != nil {
			benchNAV = strconv.FormatFloat(*p.BenchmarkNAV, 'g', 12, 64)
		}
		if p.BenchmarkReturn != nil {
			benchRet = strconv.FormatFloat(*p.BenchmarkReturn, 'g', 12, 64)
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s\n",
			p.TradeDate,
			strconv.FormatFloat(p.NAV, 'g', 12, 64),
			strconv.FormatFloat(p.CumulativeReturn, 'g', 12, 64),
			strconv.FormatFloat(p.PeriodReturn, 'g', 12, 64),
			strconv.FormatFloat(p.Drawdown, 'g', 12, 64),
			benchNAV, benchRet)
	}
	filename := fmt.Sprintf("research_run_%s_%s_%s.csv", run.ID, run.WindowStart, run.WindowEnd)
	return b.String(), filename, nil
}

// --- copy to plan ---

const researchPlanWeightTolerance = 1e-9

type ResearchPlanPreviewRequest struct {
	PlanID string `json:"plan_id"`
}

type ResearchPlanApplyRequest struct {
	PlanID                  string `json:"plan_id"`
	ExpectedConfigVersion   int    `json:"expected_config_version"`
	ExpectedReplacementHash string `json:"expected_replacement_hash"`
	Mode                    string `json:"mode"`
}

type ResearchPlanReplacementHolding struct {
	AssetKey           string  `json:"asset_key"`
	Name               string  `json:"name"`
	Symbol             string  `json:"symbol"`
	Weight             float64 `json:"weight"`
	AssetClass         string  `json:"asset_class"`
	Region             string  `json:"region"`
	WeightWithinGroup  float64 `json:"weight_within_group"`
	CurrentAmountMinor int64   `json:"current_amount_minor"`
}

type ResearchPlanRemovedHolding struct {
	AssetKey string `json:"asset_key"`
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
}

type ResearchPlanReplacementPreview struct {
	PlanID                     string                           `json:"plan_id"`
	PlanName                   string                           `json:"plan_name"`
	CollectionID               string                           `json:"collection_id"`
	BaseCurrency               string                           `json:"base_currency"`
	TargetTotalAssetsMinor     int64                            `json:"target_total_assets_minor"`
	ExpectedConfigVersion      int                              `json:"expected_config_version"`
	ReplacementHash            string                           `json:"replacement_hash"`
	BeforeHoldingCount         int                              `json:"before_holding_count"`
	AfterHoldingCount          int                              `json:"after_holding_count"`
	ExistingHoldingsWillChange bool                             `json:"existing_holdings_will_change"`
	RoundingAdjustmentMinor    int64                            `json:"rounding_adjustment_minor"`
	Allocation                 repository.PlanAllocation        `json:"allocation"`
	Holdings                   []ResearchPlanReplacementHolding `json:"holdings"`
	RemovedHoldings            []ResearchPlanRemovedHolding     `json:"removed_holdings"`
	Warnings                   []string                         `json:"warnings,omitempty"`
}

type ResearchPlanApplyResult struct {
	PlanID              string `json:"plan_id"`
	CollectionID        string `json:"collection_id"`
	ConfigVersion       int    `json:"config_version"`
	HoldingCount        int    `json:"holding_count"`
	PortfolioSnapshotID string `json:"portfolio_snapshot_id"`
}

type researchPlanReplacement struct {
	preview       ResearchPlanReplacementPreview
	writes        []HoldingWriteItem
	valuationDate string
}

func (s *ResearchService) PreviewPlanReplacement(
	ctx context.Context, collectionID string, req ResearchPlanPreviewRequest,
) (ResearchPlanReplacementPreview, error) {
	if strings.TrimSpace(req.PlanID) == "" {
		return ResearchPlanReplacementPreview{}, newErr("invalid_request", "plan_id is required", nil)
	}
	replacement, err := s.buildResearchPlanReplacement(ctx, nil, collectionID, req.PlanID)
	if err != nil {
		return ResearchPlanReplacementPreview{}, err
	}
	return replacement.preview, nil
}

//nolint:gocognit,gocyclo,funlen // Atomic replacement keeps validation and writes in one transaction.
func (s *ResearchService) ApplyPlanReplacement(
	ctx context.Context, collectionID string, req ResearchPlanApplyRequest,
) (ResearchPlanApplyResult, error) {
	if strings.TrimSpace(req.PlanID) == "" || req.Mode != "replace_all" ||
		strings.TrimSpace(req.ExpectedReplacementHash) == "" {
		return ResearchPlanApplyResult{}, newErr(
			"invalid_request", "plan_id, expected_replacement_hash and mode=replace_all are required", nil,
		)
	}
	portfolioSnapshotID := "psnap_" + uuid.New().String()
	var result ResearchPlanApplyResult
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		replacement, err := s.buildResearchPlanReplacement(ctx, tx, collectionID, req.PlanID)
		if err != nil {
			return err
		}
		if replacement.preview.ExpectedConfigVersion != req.ExpectedConfigVersion {
			return newErr("plan_config_conflict", "plan configuration version mismatch", map[string]any{
				"expected": replacement.preview.ExpectedConfigVersion, "provided": req.ExpectedConfigVersion,
			})
		}
		if replacement.preview.ReplacementHash != req.ExpectedReplacementHash {
			return newErr("research_collection_changed", "research collection changed after preview", nil)
		}
		prep, err := s.holdingSvc.prepareHoldingsUpdateWithPendingBumps(
			ctx, tx, req.PlanID, HoldingsUpdateRequest{
				ConfigVersion: req.ExpectedConfigVersion, Holdings: replacement.writes,
			}, 0, replacement.preview.Allocation,
		)
		if err != nil {
			return err
		}
		if err := s.alloc.Replace(ctx, tx, req.PlanID, replacement.preview.Allocation); err != nil {
			return fmt.Errorf("replace plan allocation: %w", err)
		}
		for _, pending := range prep.pendingSnaps {
			if !pending.skip {
				if err := s.holdingSvc.snapSvc.CreatePlanSnapshotTx(ctx, tx, pending.snap); err != nil {
					return fmt.Errorf("create holding simulation snapshot: %w", err)
				}
			}
		}
		if err := s.holdings.Replace(ctx, tx, req.PlanID, prep.built); err != nil {
			return fmt.Errorf("replace plan holdings: %w", err)
		}
		items := make([]repository.PortfolioSnapshotItem, 0, len(prep.built))
		for _, holding := range prep.built {
			items = append(items, repository.PortfolioSnapshotItem{
				AssetKey: holding.AssetKey, AmountMinor: holding.CurrentAmountMinor,
			})
		}
		if err := s.portfolio.CreateTx(ctx, tx, repository.PortfolioSnapshot{
			ID: portfolioSnapshotID, PlanID: req.PlanID,
			SnapshotDate:     replacement.valuationDate,
			TotalAmountMinor: replacement.preview.TargetTotalAssetsMinor,
			Note:             "研究组合完整替换", Items: items,
		}); err != nil {
			return fmt.Errorf("create research replacement portfolio snapshot: %w", err)
		}
		newVersion, err := s.plans.BumpVersionTx(ctx, tx, req.PlanID, req.ExpectedConfigVersion)
		if err != nil {
			return fmt.Errorf("bump plan version after research replacement: %w", err)
		}
		result = ResearchPlanApplyResult{
			PlanID: req.PlanID, CollectionID: collectionID, ConfigVersion: newVersion,
			HoldingCount: len(prep.built), PortfolioSnapshotID: portfolioSnapshotID,
		}
		return nil
	})
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			return ResearchPlanApplyResult{}, appErr
		}
		if errors.Is(err, repository.ErrVersionConflict) {
			return ResearchPlanApplyResult{}, newErr("plan_config_conflict", "plan configuration version mismatch", nil)
		}
		return ResearchPlanApplyResult{}, newErr(
			"research_plan_apply_failed", "failed to apply research portfolio to plan", nil,
		)
	}
	return result, nil
}

//nolint:gocognit,gocyclo,funlen // Preview validates the complete allocation and holdings write-set in one pass.
func (s *ResearchService) buildResearchPlanReplacement(
	ctx context.Context, tx *sql.Tx, collectionID, planID string,
) (researchPlanReplacement, error) {
	var collection repository.ResearchCollection
	var plan repository.Plan
	var params repository.PlanParameters
	var items []repository.ResearchCollectionItem
	var existing []repository.PlanHolding
	var err error
	if tx == nil {
		collection, err = s.research.GetCollection(ctx, collectionID)
	} else {
		collection, err = s.research.GetCollectionTx(ctx, tx, collectionID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return researchPlanReplacement{}, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return researchPlanReplacement{}, wrapRepo("load research collection", err)
	}
	if tx == nil {
		plan, err = s.plans.GetByID(ctx, planID)
	} else {
		plan, err = s.plans.GetByIDTx(ctx, tx, planID)
	}
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return researchPlanReplacement{}, newErr("plan_not_found", "plan not found", nil)
		}
		return researchPlanReplacement{}, wrapRepo("load plan", err)
	}
	if collection.BaseCurrency != plan.BaseCurrency {
		return researchPlanReplacement{}, newErr(
			"research_plan_currency_mismatch", "research and FIRE plan base currencies must match",
			map[string]any{"research_currency": collection.BaseCurrency, "plan_currency": plan.BaseCurrency},
		)
	}
	if tx == nil {
		params, err = s.params.Get(ctx, planID)
	} else {
		params, err = s.params.GetTx(ctx, tx, planID)
	}
	if err != nil {
		return researchPlanReplacement{}, wrapRepo("load plan parameters", err)
	}
	if params.TotalAssetsMinor <= 0 {
		return researchPlanReplacement{}, newErr("invalid_request", "target plan total assets must be positive", nil)
	}
	if tx == nil {
		items, err = s.research.ListItems(ctx, collectionID)
	} else {
		items, err = s.research.ListItemsTx(ctx, tx, collectionID)
	}
	if err != nil {
		return researchPlanReplacement{}, wrapRepo("list research items", err)
	}
	if tx == nil {
		existing, err = s.holdings.ListByPlan(ctx, planID)
	} else {
		existing, err = s.holdings.ListByPlanTx(ctx, tx, planID)
	}
	if err != nil {
		return researchPlanReplacement{}, wrapRepo("list plan holdings", err)
	}

	enabled, err := enabledResearchItemsForReplacement(items)
	if err != nil {
		return researchPlanReplacement{}, err
	}
	sort.Slice(enabled, func(i, j int) bool {
		a, b := enabled[i], enabled[j]
		if a.AssetClass != b.AssetClass {
			return a.AssetClass < b.AssetClass
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		if a.AssetKey != b.AssetKey {
			return a.AssetKey < b.AssetKey
		}
		return a.ID < b.ID
	})

	type groupKey struct{ assetClass, region string }
	classWeight := make(map[string]float64)
	groupWeight := make(map[groupKey]float64)
	seenAssets := make(map[string]struct{}, len(enabled))
	positive := make([]repository.ResearchCollectionItem, 0, len(enabled))
	for _, item := range enabled {
		if _, duplicate := seenAssets[item.AssetKey]; duplicate {
			return researchPlanReplacement{}, newErr(
				"holding_duplicate", "research collection contains duplicate asset", map[string]any{
					"asset_key": item.AssetKey,
				})
		}
		seenAssets[item.AssetKey] = struct{}{}
		if item.Weight <= researchPlanWeightTolerance {
			continue
		}
		positive = append(positive, item)
		classWeight[item.AssetClass] += item.Weight
		groupWeight[groupKey{item.AssetClass, item.Region}] += item.Weight
	}
	if len(positive) == 0 {
		return researchPlanReplacement{}, newErr("research_collection_empty", "集合没有正权重资产", nil)
	}

	allocation := repository.PlanAllocation{}
	for _, assetClass := range domain.AssetClasses {
		allocation.AssetClassTargets = append(allocation.AssetClassTargets, repository.AssetClassTarget{
			AssetClass: assetClass, Weight: classWeight[assetClass],
		})
		for _, region := range domain.Regions {
			weight := 0.0
			if classWeight[assetClass] > researchPlanWeightTolerance {
				weight = groupWeight[groupKey{assetClass, region}] / classWeight[assetClass]
			} else if region == domain.RegionDomestic {
				weight = 1
			}
			allocation.RegionTargets = append(allocation.RegionTargets, repository.RegionTarget{
				AssetClass: assetClass, Region: region, WeightWithinClass: weight,
			})
		}
	}

	writes := make([]HoldingWriteItem, 0, len(positive))
	previewHoldings := make([]ResearchPlanReplacementHolding, 0, len(positive))
	roundedTotal := int64(0)
	for i, item := range positive {
		var asset repository.MarketAsset
		if tx == nil {
			asset, err = s.assets.GetByKey(ctx, item.AssetKey)
		} else {
			asset, err = s.assets.GetByKeyTx(ctx, tx, item.AssetKey)
		}
		if err != nil {
			return researchPlanReplacement{}, newErr(
				"market_asset_not_found", "research asset is not in the market directory", map[string]any{
					"asset_key": item.AssetKey,
				})
		}
		if !asset.Active {
			return researchPlanReplacement{}, newErr("market_asset_inactive", "research asset is inactive", map[string]any{
				"asset_key": item.AssetKey,
			})
		}
		amount := int64(math.Round(float64(params.TotalAssetsMinor) * item.Weight))
		roundedTotal += amount
		withinGroup := item.Weight / groupWeight[groupKey{item.AssetClass, item.Region}]
		writes = append(writes, HoldingWriteItem{
			AssetKey: item.AssetKey, AssetClass: item.AssetClass, Region: item.Region,
			Enabled: true, WeightWithinGroup: withinGroup, CurrentAmountMinor: amount,
			SortOrder: i * 10,
		})
		previewHoldings = append(previewHoldings, ResearchPlanReplacementHolding{
			AssetKey: item.AssetKey, Name: asset.Name, Symbol: asset.Symbol, Weight: item.Weight,
			AssetClass: item.AssetClass, Region: item.Region, WeightWithinGroup: withinGroup,
			CurrentAmountMinor: amount,
		})
	}
	roundingAdjustment := params.TotalAssetsMinor - roundedTotal
	last := len(writes) - 1
	if writes[last].CurrentAmountMinor+roundingAdjustment < 0 {
		return researchPlanReplacement{}, newErr(
			"invalid_request", "target plan total assets are too small for deterministic amount rounding", nil,
		)
	}
	writes[last].CurrentAmountMinor += roundingAdjustment
	previewHoldings[last].CurrentAmountMinor += roundingAdjustment

	domainHoldings := make([]domain.HoldingWeightInput, 0, len(writes))
	for _, item := range writes {
		domainHoldings = append(domainHoldings, domain.HoldingWeightInput{
			AssetClass: item.AssetClass, Region: item.Region, Enabled: true,
			WeightWithinGroup: item.WeightWithinGroup, CurrentAmountMinor: item.CurrentAmountMinor,
		})
	}
	if checks := domain.ValidateAllWeights(toDomainAllocation(allocation), domainHoldings); !checks.Passed {
		return researchPlanReplacement{}, newErr("plan_weights_invalid", "derived plan weights are invalid", map[string]any{
			"checks": checks.Checks,
		})
	}

	identityJSON, err := json.Marshal(struct {
		CollectionID string                    `json:"collection_id"`
		PlanID       string                    `json:"plan_id"`
		Total        int64                     `json:"total_assets_minor"`
		Allocation   repository.PlanAllocation `json:"allocation"`
		Holdings     []HoldingWriteItem        `json:"holdings"`
	}{collectionID, planID, params.TotalAssetsMinor, allocation, writes})
	if err != nil {
		return researchPlanReplacement{}, wrapRepo("encode research plan replacement", err)
	}
	replacementHashBytes := sha256.Sum256(identityJSON)
	replacementHash := hex.EncodeToString(replacementHashBytes[:])

	newKeys := make(map[string]struct{}, len(writes))
	for _, item := range writes {
		newKeys[item.AssetKey] = struct{}{}
	}
	removed := make([]ResearchPlanRemovedHolding, 0)
	for _, holding := range existing {
		if _, kept := newKeys[holding.AssetKey]; !kept {
			removed = append(removed, ResearchPlanRemovedHolding{
				AssetKey: holding.AssetKey, Name: holding.InstrumentName, Symbol: holding.InstrumentCode,
			})
		}
	}
	warnings := []string(nil)
	if len(existing) > 0 {
		warnings = append(warnings, "现有目标配置和全部持仓将被完整替换")
	}
	preview := ResearchPlanReplacementPreview{
		PlanID: planID, PlanName: plan.Name, CollectionID: collectionID,
		BaseCurrency: plan.BaseCurrency, TargetTotalAssetsMinor: params.TotalAssetsMinor,
		ExpectedConfigVersion: plan.ConfigVersion, ReplacementHash: replacementHash,
		BeforeHoldingCount: len(existing), AfterHoldingCount: len(writes),
		ExistingHoldingsWillChange: len(existing) > 0, RoundingAdjustmentMinor: roundingAdjustment,
		Allocation: allocation, Holdings: previewHoldings, RemovedHoldings: removed, Warnings: warnings,
	}
	return researchPlanReplacement{preview: preview, writes: writes, valuationDate: plan.ValuationDate}, nil
}

func enabledResearchItemsForReplacement(
	items []repository.ResearchCollectionItem,
) ([]repository.ResearchCollectionItem, error) {
	enabled := make([]repository.ResearchCollectionItem, 0, len(items))
	incomplete := make([]map[string]any, 0)
	weightSum := 0.0
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		enabled = append(enabled, item)
		weightSum += item.Weight
		invalid := make([]string, 0, 2)
		if !isValidHoldingAssetClass(strings.TrimSpace(item.AssetClass)) {
			invalid = append(invalid, "asset_class")
		}
		if !isValidHoldingRegion(strings.TrimSpace(item.Region)) {
			invalid = append(invalid, "region")
		}
		if len(invalid) > 0 {
			incomplete = append(incomplete, map[string]any{
				"item_id": item.ID, "asset_key": item.AssetKey, "missing_fields": invalid,
			})
		}
		if math.IsNaN(item.Weight) || math.IsInf(item.Weight, 0) || item.Weight < 0 {
			return nil, newErr("research_weights_not_normalized", "research weights must be finite and non-negative", nil)
		}
	}
	if len(enabled) == 0 {
		return nil, newErr("research_collection_empty", "集合没有启用的资产", nil)
	}
	if len(incomplete) > 0 {
		return nil, newErr(
			"research_item_classification_incomplete", "部分资产缺少有效的 FIRE 资产大类或区域",
			map[string]any{"items": incomplete},
		)
	}
	if math.Abs(weightSum-1) > researchPlanWeightTolerance {
		return nil, newErr("research_weights_not_normalized", "enabled research weights must sum to 100%", map[string]any{
			"actual": weightSum, "target": 1,
		})
	}
	return enabled, nil
}
