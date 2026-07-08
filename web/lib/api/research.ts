import { apiDelete, apiGet, apiPatch, apiPost } from "./client";
import type { WorkerTask } from "./market-assets";

// --- screener ---

/** Precomputed screener metrics for one asset history dimension. */
export interface ResearchAssetMetrics {
  asset_key: string;
  adjust_policy: string;
  point_type: string;
  start_date: string;
  end_date: string;
  point_count: number;
  history_years: number;
  cagr?: number | null;
  annual_volatility?: number | null;
  max_drawdown?: number | null;
  downside_volatility?: number | null;
  sharpe?: number | null;
  calmar?: number | null;
  return_1y?: number | null;
  return_3y?: number | null;
  return_5y?: number | null;
  return_drawdown_ratio?: number | null;
  computed_at: number;
}

/** One row of the research screener. */
export interface ResearchAssetView {
  asset_key: string;
  market: string;
  instrument_type: string;
  instrument_type_label: string;
  region_code: string;
  symbol: string;
  name: string;
  exchange: string;
  instrument_kind: string;
  currency: string;
  active: boolean;
  listing_status: string;
  is_cash: boolean;
  has_history: boolean;
  adjust_policy?: string;
  point_type?: string;
  data_as_of?: string;
  point_count: number;
  history_source?: string;
  stale: boolean;
  sync_status?: string;
  sync_error?: string;
  fx_required?: string[];
  fx_available: boolean;
  backtest_ready: boolean;
  quality_badges: string[];
  metrics?: ResearchAssetMetrics | null;
}

export interface ResearchAssetListResult {
  assets: ResearchAssetView[];
  total: number;
}

export interface ResearchAssetListParams {
  market?: string;
  instrumentTypes?: string[];
  q?: string;
  currencies?: string[];
  includeInactive?: boolean;
  historyStatus?: "synced" | "missing" | "stale" | "syncing" | "failed";
  dataAsOfMin?: string;
  minHistoryYears?: number;
  minCagr?: number;
  minReturn1y?: number;
  minReturn3y?: number;
  minReturn5y?: number;
  maxVolatility?: number;
  minMaxDrawdown?: number;
  minSharpe?: number;
  minCalmar?: number;
  maxDownsideVolatility?: number;
  minReturnDrawdown?: number;
  backtestReady?: boolean;
  sortBy?: string;
  sortDesc?: boolean;
  limit?: number;
  offset?: number;
}

export function listResearchAssets(
  params?: ResearchAssetListParams,
): Promise<ResearchAssetListResult> {
  const qs = new URLSearchParams();
  if (params?.market) qs.set("market", params.market);
  if (params?.instrumentTypes?.length) qs.set("instrument_types", params.instrumentTypes.join(","));
  if (params?.q) qs.set("q", params.q);
  if (params?.currencies?.length) qs.set("currencies", params.currencies.join(","));
  if (params?.includeInactive) qs.set("include_inactive", "true");
  if (params?.historyStatus) qs.set("history_status", params.historyStatus);
  if (params?.dataAsOfMin) qs.set("data_as_of_min", params.dataAsOfMin);
  if (params?.minHistoryYears !== undefined) qs.set("min_history_years", String(params.minHistoryYears));
  if (params?.minCagr !== undefined) qs.set("min_cagr", String(params.minCagr));
  if (params?.minReturn1y !== undefined) qs.set("min_return_1y", String(params.minReturn1y));
  if (params?.minReturn3y !== undefined) qs.set("min_return_3y", String(params.minReturn3y));
  if (params?.minReturn5y !== undefined) qs.set("min_return_5y", String(params.minReturn5y));
  if (params?.maxVolatility !== undefined) qs.set("max_volatility", String(params.maxVolatility));
  if (params?.minMaxDrawdown !== undefined) qs.set("min_max_drawdown", String(params.minMaxDrawdown));
  if (params?.minSharpe !== undefined) qs.set("min_sharpe", String(params.minSharpe));
  if (params?.minCalmar !== undefined) qs.set("min_calmar", String(params.minCalmar));
  if (params?.maxDownsideVolatility !== undefined)
    qs.set("max_downside_volatility", String(params.maxDownsideVolatility));
  if (params?.minReturnDrawdown !== undefined)
    qs.set("min_return_drawdown", String(params.minReturnDrawdown));
  if (params?.backtestReady) qs.set("backtest_ready", "true");
  if (params?.sortBy) qs.set("sort_by", params.sortBy);
  if (params?.sortDesc) qs.set("sort_desc", "true");
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/research/assets${query ? `?${query}` : ""}`);
}

// --- saved filters ---

export interface ResearchSavedFilter {
  id: string;
  name: string;
  filters_json: string;
  sort_order: number;
  created_at: number;
  updated_at: number;
}

export interface ResearchSavedFilterInput {
  name?: string;
  filters?: Record<string, unknown>;
  sort_order?: number;
}

export function listSavedFilters(): Promise<{ filters: ResearchSavedFilter[] }> {
  return apiGet("/api/v1/research/saved-filters");
}

export function createSavedFilter(input: ResearchSavedFilterInput): Promise<ResearchSavedFilter> {
  return apiPost("/api/v1/research/saved-filters", input);
}

export function updateSavedFilter(
  id: string,
  input: ResearchSavedFilterInput,
): Promise<ResearchSavedFilter> {
  return apiPatch(`/api/v1/research/saved-filters/${encodeURIComponent(id)}`, input);
}

export function deleteSavedFilter(id: string): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/research/saved-filters/${encodeURIComponent(id)}`);
}

// --- collections ---

export type ResearchRebalancePolicy =
  | "monthly"
  | "quarterly"
  | "yearly"
  | "buy_hold"
  | "fixed"
  | "threshold";

export type ResearchStartPolicy = "common_intersection" | "custom_range";

export interface ResearchCollection {
  id: string;
  name: string;
  description: string;
  base_currency: string;
  initial_amount_minor: number;
  rebalance_policy: ResearchRebalancePolicy;
  rebalance_threshold: number;
  start_policy: ResearchStartPolicy;
  window_start: string;
  window_end: string;
  benchmark_asset_key?: string;
  risk_free_rate: number;
  transaction_cost_rate: number;
  status: "active" | "archived";
  created_at: number;
  updated_at: number;
}

export interface ResearchCollectionItem {
  id: string;
  collection_id: string;
  asset_key: string;
  enabled: boolean;
  weight: number;
  weight_locked: boolean;
  adjust_policy: string;
  point_type: string;
  asset_class: string;
  region: string;
  note: string;
  sort_order: number;
  created_at: number;
  updated_at: number;
}

export interface ResearchCollectionItemView extends ResearchCollectionItem {
  name: string;
  symbol: string;
  market: string;
  instrument_type: string;
  instrument_type_label: string;
  currency: string;
  listing_status: string;
  is_cash: boolean;
}

export interface ResearchCollectionDetail extends ResearchCollection {
  tags: string[];
  items: ResearchCollectionItemView[];
}

export interface ResearchRunSummary {
  cumulative_return: number;
  cagr: number;
  annual_volatility?: number | null;
  max_drawdown: number;
  sharpe?: number | null;
  calmar?: number | null;
  best_year?: { year: number; return: number } | null;
  worst_year?: { year: number; return: number } | null;
  best_month?: { year: number; month: number; return: number } | null;
  worst_month?: { year: number; month: number; return: number } | null;
  positive_month_ratio?: number | null;
  current_drawdown_days: number;
  max_drawdown_duration_days: number;
  max_drawdown_start?: string;
  max_drawdown_trough?: string;
  max_drawdown_recovery?: string;
  effective_return_days: number;
  risk_free_rate: number;
  contributions: ResearchAssetContribution[];
  correlations?: ResearchCorrelations | null;
  benchmark?: {
    asset_key: string;
    name: string;
    cumulative_return: number;
    cagr: number;
    max_drawdown: number;
  } | null;
}

export interface ResearchAssetContribution {
  asset_key: string;
  name: string;
  target_weight: number;
  end_weight: number;
  cumulative_contribution: number;
  risk_contribution?: number | null;
  drawdown_contribution: number;
}

export interface ResearchCorrelations {
  asset_keys: string[];
  names: string[];
  matrix: (number | null)[][];
}

export interface ResearchSeriesQuality {
  asset_key?: string;
  name?: string;
  pair?: string;
  currency?: string;
  fx_pair?: string;
  is_cash?: boolean;
  raw_start?: string;
  raw_end?: string;
  raw_point_count: number;
  usable_start?: string;
  usable_end?: string;
  fill_count: number;
  max_fill_gap_days: number;
  fill_tolerance_days: number;
  fill_gap_exceeded: boolean;
  limits_common_start?: boolean;
  limits_common_end?: boolean;
}

export interface ResearchDataQuality {
  common_start_policy: string;
  common_end_policy: string;
  forward_fill_days_max: number;
  common_start: string;
  common_end: string;
  window_start: string;
  window_end: string;
  assets?: ResearchSeriesQuality[] | null;
  fx?: ResearchSeriesQuality[] | null;
  benchmark?: ResearchSeriesQuality | null;
}

export type ResearchRunStatus = "queued" | "running" | "succeeded" | "failed" | "canceled";

export interface ResearchJobView {
  status: string;
  phase: string;
  progress_current: number;
  progress_total: number;
  error_code?: string;
  error_message?: string;
}

export interface ResearchRunView {
  id: string;
  collection_id: string;
  job_id: string;
  input_hash: string;
  source_hash: string;
  engine_version: string;
  base_currency: string;
  rebalance_policy: string;
  window_start: string;
  window_end: string;
  status: ResearchRunStatus;
  summary?: ResearchRunSummary;
  data_quality?: ResearchDataQuality;
  created_at: number;
  completed_at?: number | null;
  job?: ResearchJobView | null;
}

export interface ResearchCollectionListItem extends ResearchCollection {
  tags: string[];
  enabled_assets: number;
  total_assets: number;
  weight_sum: number;
  weight_valid: boolean;
  latest_run?: ResearchRunView | null;
  latest_run_summary?: ResearchRunSummary | null;
}

export interface ResearchCollectionItemInput {
  asset_key: string;
  weight?: number;
  enabled?: boolean;
  weight_locked?: boolean;
  adjust_policy?: string;
  point_type?: string;
  asset_class?: string;
  region?: string;
  note?: string;
}

export interface ResearchCollectionInput {
  name: string;
  description?: string;
  base_currency?: string;
  initial_amount_minor?: number;
  rebalance_policy?: ResearchRebalancePolicy;
  rebalance_threshold?: number;
  start_policy?: ResearchStartPolicy;
  window_start?: string;
  window_end?: string;
  benchmark_asset_key?: string;
  risk_free_rate?: number;
  transaction_cost_rate?: number;
  tags?: string[];
  items?: ResearchCollectionItemInput[];
  from_plan_id?: string;
  from_collection_id?: string;
}

export interface ResearchCollectionUpdate {
  name?: string;
  description?: string;
  base_currency?: string;
  initial_amount_minor?: number;
  rebalance_policy?: ResearchRebalancePolicy;
  rebalance_threshold?: number;
  start_policy?: ResearchStartPolicy;
  window_start?: string;
  window_end?: string;
  benchmark_asset_key?: string;
  risk_free_rate?: number;
  transaction_cost_rate?: number;
  status?: "active" | "archived";
  tags?: string[];
}

export function listCollections(
  status?: "active" | "archived",
): Promise<{ collections: ResearchCollectionListItem[] }> {
  const qs = status ? `?status=${status}` : "";
  return apiGet(`/api/v1/research/collections${qs}`);
}

export function createCollection(input: ResearchCollectionInput): Promise<ResearchCollectionDetail> {
  return apiPost("/api/v1/research/collections", input);
}

export function getCollection(id: string): Promise<ResearchCollectionDetail> {
  return apiGet(`/api/v1/research/collections/${encodeURIComponent(id)}`);
}

export function updateCollection(
  id: string,
  input: ResearchCollectionUpdate,
): Promise<ResearchCollectionDetail> {
  return apiPatch(`/api/v1/research/collections/${encodeURIComponent(id)}`, input);
}

export function deleteCollection(
  id: string,
  hard?: boolean,
): Promise<{ deleted?: boolean; archived?: boolean }> {
  return apiDelete(
    `/api/v1/research/collections/${encodeURIComponent(id)}${hard ? "?hard=true" : ""}`,
  );
}

// --- items ---

export interface ResearchItemUpdate {
  enabled?: boolean;
  weight?: number;
  weight_locked?: boolean;
  adjust_policy?: string;
  point_type?: string;
  asset_class?: string;
  region?: string;
  note?: string;
  sort_order?: number;
}

export function addCollectionItem(
  collectionId: string,
  input: ResearchCollectionItemInput,
): Promise<ResearchCollectionDetail> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/items`,
    input,
  );
}

export function updateCollectionItem(
  collectionId: string,
  itemId: string,
  input: ResearchItemUpdate,
): Promise<ResearchCollectionDetail> {
  return apiPatch(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/items/${encodeURIComponent(itemId)}`,
    input,
  );
}

export function deleteCollectionItem(
  collectionId: string,
  itemId: string,
): Promise<ResearchCollectionDetail> {
  return apiDelete(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/items/${encodeURIComponent(itemId)}`,
  );
}

export function normalizeWeights(collectionId: string): Promise<ResearchCollectionDetail> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/normalize-weights`,
    {},
  );
}

// --- readiness & sync ---

export interface ResearchReadinessIssue {
  asset_key?: string;
  pair?: string;
  reason: string;
  message: string;
}

export interface ResearchReadinessAssetView {
  item_id: string;
  asset_key: string;
  name: string;
  currency: string;
  is_cash: boolean;
  enabled: boolean;
  weight: number;
  adjust_policy: string;
  point_type: string;
  listing_status: string;
  has_history: boolean;
  history_start?: string;
  history_end?: string;
  point_count: number;
  data_as_of?: string;
  stale: boolean;
  sync_status?: string;
  sync_error?: string;
  fx_pairs?: string[];
  limits_common_start?: boolean;
  limits_common_end?: boolean;
}

export interface ResearchReadiness {
  ready: boolean;
  weight_sum: number;
  common_start?: string;
  common_end?: string;
  window_start?: string;
  window_end?: string;
  blocking_reasons: ResearchReadinessIssue[];
  warnings: ResearchReadinessIssue[];
  assets: ResearchReadinessAssetView[];
  data_dependencies: {
    asset_count: number;
    fx_pairs: string[];
    stale_asset_count: number;
    missing_history_count: number;
  };
}

export function getReadiness(collectionId: string): Promise<ResearchReadiness> {
  return apiGet(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/readiness`,
  );
}

export interface ResearchSyncAssetResult {
  asset_key: string;
  status: "created" | "existed" | "skipped";
  reason?: string;
  task?: WorkerTask | null;
}

export interface ResearchSyncFXResult {
  pair: string;
  status: "created" | "existed" | "skipped";
  task?: WorkerTask | null;
}

export interface ResearchSyncBlocked {
  asset_key: string;
  code: string;
  message: string;
}

export interface ResearchSyncResult {
  assets: ResearchSyncAssetResult[];
  fx: ResearchSyncFXResult[];
  blocked: ResearchSyncBlocked[];
}

export function syncCollectionHistory(
  collectionId: string,
  body?: { asset_keys?: string[]; force?: boolean },
): Promise<ResearchSyncResult> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/sync-history`,
    body ?? {},
  );
}

// --- backtests & runs ---

export interface ResearchBacktestResult {
  run: ResearchRunView;
  reused: boolean;
}

export function createBacktest(collectionId: string): Promise<ResearchBacktestResult> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/backtests`,
    {},
  );
}

export interface ResearchBacktestYear {
  run_id: string;
  year: number;
  annual_return: number;
  volatility: number;
  max_drawdown: number;
  start_nav: number;
  end_nav: number;
  is_partial: boolean;
}

export interface ResearchBacktestMonth {
  run_id: string;
  year: number;
  month: number;
  monthly_return: number;
}

export interface ResearchRunDetail extends ResearchRunView {
  years: ResearchBacktestYear[];
  months: ResearchBacktestMonth[];
  input_snapshot?: Record<string, unknown>;
}

export function listRuns(
  collectionId: string,
  limit?: number,
): Promise<{ runs: ResearchRunView[] }> {
  const qs = limit ? `?limit=${limit}` : "";
  return apiGet(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/runs${qs}`,
  );
}

export function listRecentRuns(limit?: number): Promise<{ runs: ResearchRunView[] }> {
  const qs = limit ? `?limit=${limit}` : "";
  return apiGet(`/api/v1/research/runs${qs}`);
}

export function getRun(runId: string): Promise<ResearchRunDetail> {
  return apiGet(`/api/v1/research/runs/${encodeURIComponent(runId)}`);
}

export interface ResearchRunPoint {
  date: string;
  nav: number;
  cumulative_return: number;
  period_return: number;
  drawdown: number;
  benchmark_nav?: number | null;
  benchmark_return?: number | null;
  weights?: Record<string, number>;
  contributions?: Record<string, number>;
}

export interface ResearchRunPointsResult {
  points: ResearchRunPoint[];
  total: number;
}

export function getRunPoints(
  runId: string,
  params?: { from?: string; to?: string; limit?: number; offset?: number; includeWeights?: boolean },
): Promise<ResearchRunPointsResult> {
  const qs = new URLSearchParams();
  if (params?.from) qs.set("from", params.from);
  if (params?.to) qs.set("to", params.to);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  if (params?.includeWeights) qs.set("include_weights", "true");
  const query = qs.toString();
  return apiGet(`/api/v1/research/runs/${encodeURIComponent(runId)}/points${query ? `?${query}` : ""}`);
}

/** Absolute URL of the run CSV export used by download links. */
export function runExportCSVUrl(runId: string): string {
  const base = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
  return `${base}/api/v1/research/runs/${encodeURIComponent(runId)}/export.csv`;
}

// --- plan interop ---

export interface ResearchPlanDraftHolding {
  asset_key: string;
  name: string;
  symbol: string;
  weight: number;
  asset_class: string;
  region: string;
  current_amount_minor: number;
}

export interface ResearchCopyToPlanResult {
  plan_id: string;
  plan_name: string;
  collection_id: string;
  holdings: ResearchPlanDraftHolding[];
}

export function copyToPlan(
  collectionId: string,
  planId: string,
): Promise<ResearchCopyToPlanResult> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/copy-to-plan`,
    { plan_id: planId },
  );
}

// --- optimization (td/103) ---

export interface ResearchOptimizationConfig {
  weight_step: number;
  max_candidate_count?: number;
  top_k?: number;
}

export interface ResearchOptimizationWeightEntry {
  item_id: string;
  asset_key: string;
  name: string;
  weight: number;
  locked: boolean;
}

export interface ResearchOptimizationResultItem {
  rank: number;
  objective: "max_cagr" | "min_drawdown" | "max_calmar";
  score: number;
  weights: ResearchOptimizationWeightEntry[];
  summary: ResearchRunSummary;
}

export interface ResearchOptimizationResult {
  candidate_count: number;
  evaluated_count: number;
  skipped_count: number;
  best_by_cagr: ResearchOptimizationResultItem[];
  best_by_drawdown: ResearchOptimizationResultItem[];
  best_by_calmar: ResearchOptimizationResultItem[];
}

export interface ResearchOptimizationRun {
  id: string;
  collection_id: string;
  job_id: string;
  status: ResearchRunStatus;
  config: ResearchOptimizationConfig;
  candidate_count: number;
  evaluated_count: number;
  result?: ResearchOptimizationResult | null;
  error_code?: string;
  error_message?: string;
  base_currency: string;
  rebalance_policy: string;
  window_start: string;
  window_end: string;
  engine_version: string;
  created_at: number;
  completed_at?: number | null;
  job?: ResearchJobView | null;
}

export interface ResearchOptimizationReadiness {
  ready: boolean;
  candidate_count: number;
  enabled_count: number;
  locked_count: number;
  tunable_count: number;
  locked_weight_sum: number;
  blocking_reasons: ResearchReadinessIssue[];
  warnings: ResearchReadinessIssue[];
}

export function getOptimizationReadiness(
  collectionId: string,
  weightStep?: number,
): Promise<ResearchOptimizationReadiness> {
  const qs = new URLSearchParams();
  if (weightStep !== undefined) qs.set("weight_step", String(weightStep));
  const query = qs.toString();
  return apiGet(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/optimization-readiness${query ? `?${query}` : ""}`,
  );
}

export function createOptimization(
  collectionId: string,
  config?: Partial<ResearchOptimizationConfig>,
): Promise<{ optimization: ResearchOptimizationRun; reused: boolean }> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/optimizations`,
    config ?? {},
  );
}

export function getOptimization(
  optimizationId: string,
): Promise<ResearchOptimizationRun> {
  return apiGet(
    `/api/v1/research/optimizations/${encodeURIComponent(optimizationId)}`,
  );
}

export function getLatestOptimization(
  collectionId: string,
): Promise<ResearchOptimizationRun | null> {
  return apiGet(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/optimizations/latest`,
  );
}
