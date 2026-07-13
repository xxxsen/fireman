import { apiGet, apiPost, apiPut } from "./client";
import type { Task, WorkerTaskStatus } from "@/types/api";
export { isTaskActive, isTaskTerminal } from "./tasks";
export type { WorkerTaskStatus } from "@/types/api";

export type WorkerTask = Task;

export interface MarketAsset {
  asset_key: string;
  market: string;
  instrument_type: string;
  region_code: string;
  symbol: string;
  name: string;
  exchange: string;
  instrument_kind: string;
  canonical_symbol?: string;
  fee_mode?: "" | "standard" | "front_end" | "back_end";
  currency: string;
  active: boolean;
  listing_status: string;
  last_seen_at: number;
  source_name: string;
  source_as_of: string;
  refreshed_at: number;
  created_at: number;
  updated_at: number;
  /**
   * Backend-owned instrument-type presentation (Chinese label and identity
   * ordering priority); pickers must use these instead of local mappings.
   */
  instrument_type_label?: string;
  instrument_type_priority?: number;
  /** Local history readiness attached by the listing API. */
  has_history?: boolean;
  history_data_as_of?: string;
  history_point_count?: number;
  history_source_name?: string;
  /** Latest history sync task status when the asset has no local history. */
  history_sync_status?: WorkerTaskStatus;
  history_sync_error?: string;
}

/** Single-unit sync status block (FX rates). */
export interface MarketAssetSyncView {
  scope: string;
  task?: WorkerTask | null;
  last_success_at?: number | null;
  last_success_task_id: string;
}

/** One directory sync unit's status inside a scope aggregation. */
export interface DirectorySyncUnitView {
  sync_key: string;
  label: string;
  task?: WorkerTask | null;
  last_success_at?: number | null;
  last_success_task_id: string;
}

/** Scope aggregate status computed by the backend from directory sync units. */
export type DirectoryScopeStatus =
  "running" | "complete" | "partial" | "failed" | "never";

/** Aggregated sync view of one directory scope. */
export interface DirectoryScopeSyncView {
  scope: string;
  label: string;
  status: DirectoryScopeStatus;
  /** Oldest full-success time; absent while any unit has never succeeded. */
  last_success_at?: number | null;
  units: DirectorySyncUnitView[];
}

export interface MarketAssetListResult {
  assets: MarketAsset[];
  syncs: DirectoryScopeSyncView[];
  fx_sync?: MarketAssetSyncView | null;
  total: number;
}

export interface MarketAssetListParams {
  market?: string;
  instrumentTypes?: string[];
  symbolQ?: string;
  nameQ?: string;
  includeInactive?: boolean;
  limit?: number;
  offset?: number;
}

export function listMarketAssets(
  params?: MarketAssetListParams,
): Promise<MarketAssetListResult> {
  const qs = new URLSearchParams();
  if (params?.market) qs.set("market", params.market);
  if (params?.instrumentTypes?.length)
    qs.set("instrument_types", params.instrumentTypes.join(","));
  if (params?.symbolQ) qs.set("symbol_q", params.symbolQ);
  if (params?.nameQ) qs.set("name_q", params.nameQ);
  if (params?.includeInactive) qs.set("include_inactive", "true");
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/market-assets${query ? `?${query}` : ""}`);
}

export interface TaskCreateResult {
  task: WorkerTask;
  existed: boolean;
}

export type DirectorySyncScope = "cn_all" | "hk_all" | "us_all";

/** One unit's created-or-existing task in the sync response. */
export interface DirectorySyncTaskItem {
  sync_key: string;
  label: string;
  task: WorkerTask;
  existed: boolean;
}

export interface DirectorySyncResult {
  scope: string;
  tasks: DirectorySyncTaskItem[];
}

/**
 * Creates directory sync tasks: `{scope}` syncs every unit of the scope,
 * `{sync_key}` syncs a single unit. Units with an active task come back with
 * `existed: true`.
 */
export function syncMarketAssets(
  body:
    | { scope: DirectorySyncScope; force?: boolean }
    | { sync_key: string; force?: boolean },
): Promise<DirectorySyncResult> {
  return apiPost("/api/v1/market-assets/sync", body);
}

export interface MarketAssetHistoryView {
  adjust_policy: string;
  point_type: string;
  data_as_of: string;
  point_count: number;
  source_name: string;
  last_success_at?: number | null;
  last_success_task_id: string;
  task?: WorkerTask | null;
  can_switch_source: boolean;
  auto_update?: AutoUpdateRule | null;
}

export interface AutoUpdateRule {
  id: string;
  target_type: "directory_unit" | "asset_history";
  sync_key: string;
  asset_key: string;
  adjust_policy: string;
  point_type: string;
  enabled: boolean;
  interval_hours: number;
  next_run_at?: number | null;
  last_enqueued_at?: number | null;
  last_task_id: string;
  last_success_at?: number | null;
  last_failed_at?: number | null;
  last_error_code: string;
  last_error_message: string;
  version: number;
  created_at: number;
  updated_at: number;
}

export interface MarketAssetPoint {
  date: string;
  value: number;
}

export interface MarketAssetAnnualReturn {
  year: number;
  annual_return: number;
  start_date: string;
  end_date: string;
  start_value: number;
  end_value: number;
  observations: number;
  is_partial: boolean;
}

export interface MarketAssetTrailingReturns {
  as_of_date: string;
  point_type: string;
  source_name: string;
  periods: Record<
    string,
    {
      status: string;
      target_start_date: string;
      start_date: string | null;
      end_date: string;
      actual_days: number | null;
      cumulative_return: number | null;
      annualized_return: number | null;
    }
  >;
}

export interface MarketAssetDetail {
  asset: MarketAsset;
  history: MarketAssetHistoryView;
  points: MarketAssetPoint[];
  annual_returns: MarketAssetAnnualReturn[];
  trailing_returns?: MarketAssetTrailingReturns;
}

export function getMarketAssetDetail(
  assetKey: string,
  options?: { adjustPolicy?: string; pointType?: string },
): Promise<MarketAssetDetail> {
  const qs = new URLSearchParams({ asset_key: assetKey });
  if (options?.adjustPolicy) qs.set("adjust_policy", options.adjustPolicy);
  if (options?.pointType) qs.set("point_type", options.pointType);
  return apiGet(`/api/v1/market-assets/by-key?${qs.toString()}`);
}

export type HistorySyncMode = "default_refresh" | "switch_source_full";

export function syncMarketAssetHistory(body: {
  asset_key: string;
  adjust_policy?: string;
  point_type?: string;
  mode: HistorySyncMode;
}): Promise<TaskCreateResult> {
  return apiPost("/api/v1/market-assets/history-sync", body);
}

export function setMarketAssetHistoryAutoUpdate(body: {
  asset_key: string;
  adjust_policy: string;
  point_type: string;
  enabled: boolean;
}): Promise<AutoUpdateRule> {
  return apiPut("/api/v1/market-assets/history-auto-update", body);
}

export function syncFXRates(): Promise<TaskCreateResult> {
  return apiPost("/api/v1/market-assets/fx-sync", {});
}
