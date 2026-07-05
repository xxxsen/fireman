import { apiGet, apiPost } from "./client";

/** Worker task status enum for market data sync tasks. */
export type WorkerTaskStatus =
  | "pending"
  | "running"
  | "pre_complete"
  | "complete"
  | "failed"
  | "canceled";

export interface WorkerTask {
  id: string;
  type: string;
  status: WorkerTaskStatus;
  error_code?: string;
  error_message?: string;
  created_at: number;
  started_at?: number | null;
  finished_at?: number | null;
  heartbeat_at?: number | null;
}

/** Active statuses keep polling; terminal statuses stop it. */
export function isTaskActive(status: WorkerTaskStatus | undefined): boolean {
  return status === "pending" || status === "running" || status === "pre_complete";
}

export interface MarketAsset {
  asset_key: string;
  market: string;
  instrument_type: string;
  region_code: string;
  symbol: string;
  name: string;
  exchange: string;
  instrument_kind: string;
  currency: string;
  active: boolean;
  listing_status: string;
  last_seen_at: number;
  source_name: string;
  source_as_of: string;
  refreshed_at: number;
  created_at: number;
  updated_at: number;
  /** Local history readiness attached by the listing API. */
  has_history?: boolean;
  history_data_as_of?: string;
  history_point_count?: number;
  history_source_name?: string;
  /** Latest history sync task status when the asset has no local history. */
  history_sync_status?: WorkerTaskStatus;
  history_sync_error?: string;
}

export interface MarketAssetSyncView {
  scope: string;
  task?: WorkerTask | null;
  last_success_at?: number | null;
  last_success_task_id: string;
}

export interface MarketAssetListResult {
  assets: MarketAsset[];
  sync?: MarketAssetSyncView | null;
  syncs: MarketAssetSyncView[];
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

export function listMarketAssets(params?: MarketAssetListParams): Promise<MarketAssetListResult> {
  const qs = new URLSearchParams();
  if (params?.market) qs.set("market", params.market);
  if (params?.instrumentTypes?.length) qs.set("instrument_types", params.instrumentTypes.join(","));
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

export function syncMarketAssets(body: {
  scope: DirectorySyncScope;
  force?: boolean;
}): Promise<TaskCreateResult> {
  return apiPost("/api/v1/market-assets/sync", body);
}

export function getTask(id: string): Promise<WorkerTask> {
  return apiGet(`/api/v1/tasks/${encodeURIComponent(id)}`);
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

export function syncFXRates(): Promise<TaskCreateResult> {
  return apiPost("/api/v1/market-assets/fx-sync", {});
}
