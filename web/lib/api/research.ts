import { apiDelete, apiGet, apiPatch, apiPost } from "./client";
import type { WorkerTask } from "./market-assets";
import type { AssetClassTarget, RegionTarget } from "@/types/api";

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
  q?: string;
  limit?: number;
}

export function listResearchAssets(
  params?: ResearchAssetListParams,
): Promise<ResearchAssetListResult> {
  const qs = new URLSearchParams();
  if (params?.q) qs.set("q", params.q);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return apiGet(`/api/v1/research/assets${query ? `?${query}` : ""}`);
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
  tail_risk_confidence: 0.9 | 0.95 | 0.99;
  tail_risk_horizon_days: 1 | 20;
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
  total_turnover?: number;
  total_transaction_cost_minor?: number;
  transaction_cost_drag?: number;
  tail_risk?: ResearchTailRisk | null;
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

export interface ResearchTailRiskSpec {
  confidence: 0.9 | 0.95 | 0.99;
  horizon_days: 1 | 20;
}

export interface ResearchTailRisk extends ResearchTailRiskSpec {
  algorithm_version: string;
  scenario_count: number;
  tail_count: number;
  var_loss: number;
  cvar_loss: number;
  worst_loss: number;
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

/** Build an add-item request without echoing a screener history dimension. */
export function researchItemInputFromAsset(
  asset: Pick<ResearchAssetView, "asset_key">,
): ResearchCollectionItemInput {
  return {
    asset_key: asset.asset_key,
    weight: 0,
    enabled: true,
  };
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
  tail_risk_confidence?: ResearchTailRiskSpec["confidence"];
  tail_risk_horizon_days?: ResearchTailRiskSpec["horizon_days"];
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
  tail_risk_confidence?: ResearchTailRiskSpec["confidence"];
  tail_risk_horizon_days?: ResearchTailRiskSpec["horizon_days"];
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

export interface ResearchTailRiskReadiness {
  confidence: number;
  horizon_days: number;
  effective_return_count: number;
  scenario_count: number;
  minimum_scenario_count: number;
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
  tail_risk?: ResearchTailRiskReadiness | null;
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

export interface ResearchPlanReplacementHolding {
  asset_key: string;
  name: string;
  symbol: string;
  weight: number;
  asset_class: string;
  region: string;
  weight_within_group: number;
  current_amount_minor: number;
}

export interface ResearchPlanReplacementPreview {
  plan_id: string;
  plan_name: string;
  collection_id: string;
  base_currency: string;
  target_total_assets_minor: number;
  expected_config_version: number;
  replacement_hash: string;
  before_holding_count: number;
  after_holding_count: number;
  existing_holdings_will_change: boolean;
  rounding_adjustment_minor: number;
  allocation: {
    asset_class_targets: AssetClassTarget[];
    region_targets: RegionTarget[];
  };
  holdings: ResearchPlanReplacementHolding[];
  removed_holdings: Array<{ asset_key: string; name: string; symbol: string }>;
  warnings?: string[];
}

export interface ResearchPlanApplyResult {
  plan_id: string;
  collection_id: string;
  config_version: number;
  holding_count: number;
  portfolio_snapshot_id: string;
}

export function previewPlanReplacement(
  collectionId: string,
  planId: string,
): Promise<ResearchPlanReplacementPreview> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/plan-preview`,
    { plan_id: planId },
  );
}

export function applyPlanReplacement(
  collectionId: string,
  request: {
    plan_id: string;
    expected_config_version: number;
    expected_replacement_hash: string;
    mode: "replace_all";
  },
): Promise<ResearchPlanApplyResult> {
  return apiPost(
    `/api/v1/research/collections/${encodeURIComponent(collectionId)}/apply-to-plan`,
    request,
  );
}

// --- optimization (td/103) ---

export interface ResearchOptimizationConfig {
  weight_step: number;
  max_candidate_count?: number;
  top_k?: number;
  tail_risk: ResearchTailRiskSpec;
  minimum_cagr?: number | null;
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
  objective: "max_cagr" | "min_drawdown" | "max_calmar" | "min_cvar";
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
  best_by_cvar: ResearchOptimizationResultItem[];
  cvar_eligible_count: number;
  warnings?: { code: string; message: string }[];
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
  tail_risk?: ResearchTailRiskReadiness | null;
}

export function getOptimizationReadiness(
  collectionId: string,
  params?: { weightStep?: number; confidence?: number; horizonDays?: number },
): Promise<ResearchOptimizationReadiness> {
  const qs = new URLSearchParams();
  if (params?.weightStep !== undefined) qs.set("weight_step", String(params.weightStep));
  if (params?.confidence !== undefined) qs.set("cvar_confidence", String(params.confidence));
  if (params?.horizonDays !== undefined) qs.set("cvar_horizon_days", String(params.horizonDays));
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

export interface ResearchOptimizationApplyRequest {
  objective: "max_cagr" | "min_drawdown" | "max_calmar" | "min_cvar";
  rank: number;
  expected_collection_updated_at: number;
}

export function applyOptimization(
  optimizationId: string,
  request: ResearchOptimizationApplyRequest,
): Promise<ResearchCollectionDetail> {
  return apiPost(
    `/api/v1/research/optimizations/${encodeURIComponent(optimizationId)}/apply`,
    request,
  );
}
