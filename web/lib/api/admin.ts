import { apiGet, apiPost, apiPut } from "./client";
import type {
  AutoUpdateRule,
  WorkerTask,
  WorkerTaskStatus,
} from "./market-assets";

export interface AdminAutoUpdateRule extends AutoUpdateRule {
  target_label: string;
  task?: WorkerTask | null;
}

export interface AdminAutoUpdateDirectoryUnit {
  sync_key: string;
  scope: string;
  label: string;
}

export interface AdminAutoUpdateListParams {
  targetType?: string;
  enabled?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export function listAdminAutoUpdates(
  params?: AdminAutoUpdateListParams,
): Promise<AdminPage<AdminAutoUpdateRule>> {
  const query = new URLSearchParams();
  if (params?.targetType) query.set("target_type", params.targetType);
  if (params?.enabled) query.set("enabled", params.enabled);
  if (params?.q) query.set("q", params.q);
  if (params?.limit) query.set("limit", String(params.limit));
  if (params?.offset) query.set("offset", String(params.offset));
  return apiGet(`/api/v1/admin/auto-updates${query.size ? `?${query}` : ""}`);
}

export function createAdminDirectoryAutoUpdate(body: {
  sync_key: string;
  interval_hours: number;
}): Promise<AdminAutoUpdateRule> {
  return apiPost("/api/v1/admin/auto-updates/directories", body);
}

export function listAdminAutoUpdateDirectoryUnits(): Promise<
  AdminAutoUpdateDirectoryUnit[]
> {
  return apiGet("/api/v1/admin/auto-updates/directories");
}

export function updateAdminAutoUpdate(
  id: string,
  body: { enabled: boolean; interval_hours: number; version: number },
): Promise<AdminAutoUpdateRule> {
  return apiPut(`/api/v1/admin/auto-updates/${encodeURIComponent(id)}`, body);
}

/** Shared pagination envelope for every /api/v1/admin/* listing. */
export interface AdminPage<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

// --- overview ---

export interface AdminWorkerTaskStats {
  active: number;
  by_status: Record<string, number>;
  failed_last_24h: number;
  completed_last_24h: number;
  stale_running: number;
}

export interface AdminFinalizeStats {
  total_last_24h: number;
  failed_last_24h: number;
}

export interface AdminDirectoryUnitHealth {
  sync_key: string;
  label: string;
  last_success_at: number | null;
  active_task_status: string;
  latest_task_failed: boolean;
  stale: boolean;
}

export interface AdminDirectoryScopeHealth {
  scope: string;
  label: string;
  /** Scope aggregate: running | complete | partial | failed | never. */
  status: string;
  /** Oldest full-success time; null while any unit has never succeeded. */
  last_success_at: number | null;
  active_task_status: string;
  stale: boolean;
  units: AdminDirectoryUnitHealth[];
}

export interface AdminFXPairHealth {
  pair: string;
  last_success_at: number | null;
}

export interface AdminHistoryDimensionStats {
  total: number;
  stale_over_7d: number;
  never_synced: number;
}

export interface AdminSyncHealth {
  directory_scopes: AdminDirectoryScopeHealth[];
  fx_pairs: AdminFXPairHealth[];
  history_dimensions: AdminHistoryDimensionStats;
}

export interface AdminStorageStats {
  main_db_bytes: number;
  resource_db_bytes: number;
  resource_count: number;
}

export interface AdminOverview {
  worker_tasks: AdminWorkerTaskStats;
  finalizations: AdminFinalizeStats;
  sync_health: AdminSyncHealth;
  storage: AdminStorageStats;
}

export function getAdminOverview(): Promise<AdminOverview> {
  return apiGet("/api/v1/admin/overview");
}

// --- worker tasks ---

export type WorkerTaskType =
  | "simulation"
  | "stress"
  | "sensitivity"
  | "research_backtest"
  | "research_optimization_backtest"
  | "market_data_auto_update_scan"
  | "asset_directory_sync"
  | "asset_history_sync"
  | "fx_rate_sync";

export const WORKER_TASK_TYPE_LABELS: Record<WorkerTaskType, string> = {
  simulation: "FIRE 模拟",
  stress: "压力测试",
  sensitivity: "敏感性分析",
  research_backtest: "组合回测",
  research_optimization_backtest: "组合寻优",
  market_data_auto_update_scan: "自动更新扫描",
  asset_directory_sync: "目录同步",
  asset_history_sync: "历史同步",
  fx_rate_sync: "汇率同步",
};

export function workerTaskTypeLabel(type: string): string {
  return WORKER_TASK_TYPE_LABELS[type as WorkerTaskType] ?? type;
}

export interface AdminWorkerTaskItem {
  id: string;
  worker_type: "go_worker" | "sidecar_worker";
  type: string;
  status: WorkerTaskStatus;
  scope_type: string;
  scope_id: string;
  dedupe_key: string;
  claimed_by?: string;
  attempt_count: number;
  max_attempts: number;
  progress_current: number;
  progress_total: number;
  phase: string;
  heartbeat_at?: number | null;
  lease_expires_at?: number | null;
  error_code: string;
  error_message: string;
  finalize_attempts: number;
  created_at: number;
  started_at: number | null;
  finished_at: number | null;
  duration_ms: number | null;
}

export interface AdminWorkerTaskListParams {
  workerType?: string;
  type?: string;
  status?: string;
  scopeType?: string;
  scopeId?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export function listAdminWorkerTasks(
  params?: AdminWorkerTaskListParams,
): Promise<AdminPage<AdminWorkerTaskItem>> {
  const qs = new URLSearchParams();
  if (params?.workerType) qs.set("worker_type", params.workerType);
  if (params?.type) qs.set("type", params.type);
  if (params?.status) qs.set("status", params.status);
  if (params?.scopeType) qs.set("scope_type", params.scopeType);
  if (params?.scopeId) qs.set("scope_id", params.scopeId);
  if (params?.q) qs.set("q", params.q);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/admin/worker-tasks${query ? `?${query}` : ""}`);
}

export interface AdminWorkerTaskFull {
  id: string;
  version_no: number;
  worker_type: "go_worker" | "sidecar_worker";
  type: string;
  status: WorkerTaskStatus;
  scope_type: string;
  scope_id: string;
  dedupe_key: string;
  payload_json: string;
  result_key?: string;
  result_meta_json: string;
  claimed_by?: string;
  attempt_count: number;
  max_attempts: number;
  heartbeat_at?: number | null;
  error_code: string;
  error_message: string;
  finalize_attempts: number;
  next_finalize_at?: number | null;
  created_at: number;
  started_at?: number | null;
  pre_completed_at?: number | null;
  finished_at?: number | null;
}

export interface AdminTaskTimelinePhase {
  phase: "created" | "started" | "pre_complete" | "finished";
  at: number;
  status?: string;
}

export interface AdminTaskHeartbeat {
  at: number;
  stale: boolean;
}

export type FinalizeResult = "success" | "retryable_error" | "permanent_error";

export interface AdminFinalizeRecord {
  id: number;
  task_id: string;
  task_type: string;
  attempt_no: number;
  result: FinalizeResult;
  error_code: string;
  error_message: string;
  duration_ms: number;
  created_at: number;
}

export interface AdminWorkerTaskDetail {
  task: AdminWorkerTaskFull;
  timeline: AdminTaskTimelinePhase[];
  heartbeat?: AdminTaskHeartbeat | null;
  finalize_records: AdminFinalizeRecord[];
  attempts: Array<{
    task_id: string;
    attempt_no: number;
    worker_type: string;
    worker_id: string;
    claimed_at: number;
    last_heartbeat_at?: number | null;
    released_at?: number | null;
    outcome: string;
    error_code: string;
    error_message: string;
  }>;
}

export function getAdminWorkerTask(
  taskId: string,
): Promise<AdminWorkerTaskDetail> {
  return apiGet(`/api/v1/admin/worker-tasks/${encodeURIComponent(taskId)}`);
}

// --- finalization records ---

export interface AdminFinalizeListParams {
  taskId?: string;
  result?: string;
  taskType?: string;
  limit?: number;
  offset?: number;
}

export function listAdminFinalizeRecords(
  params?: AdminFinalizeListParams,
): Promise<AdminPage<AdminFinalizeRecord>> {
  const qs = new URLSearchParams();
  if (params?.taskId) qs.set("task_id", params.taskId);
  if (params?.result) qs.set("result", params.result);
  if (params?.taskType) qs.set("task_type", params.taskType);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/admin/finalize-records${query ? `?${query}` : ""}`);
}

// --- data versions ---

export interface AdminDataVersion {
  version_key: string;
  version_no: number;
  task_id: string;
  updated_at: number;
}

export interface AdminDataVersionListParams {
  prefix?: string;
  limit?: number;
  offset?: number;
}

export function listAdminDataVersions(
  params?: AdminDataVersionListParams,
): Promise<AdminPage<AdminDataVersion>> {
  const qs = new URLSearchParams();
  if (params?.prefix) qs.set("prefix", params.prefix);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/admin/data-versions${query ? `?${query}` : ""}`);
}

// --- shared labels ---

export const DIRECTORY_SCOPE_LABELS: Record<string, string> = {
  cn_all: "中国市场目录",
  hk_all: "港股市场目录",
  us_all: "美股市场目录",
};

export function directoryScopeLabel(scope: string): string {
  return DIRECTORY_SCOPE_LABELS[scope] ?? scope;
}

export const DIRECTORY_SCOPE_STATUS_LABELS: Record<string, string> = {
  running: "同步中",
  complete: "已同步",
  partial: "部分未同步",
  failed: "同步失败",
  never: "从未同步",
};

export function directoryScopeStatusLabel(status: string): string {
  return DIRECTORY_SCOPE_STATUS_LABELS[status] ?? status;
}

export const FINALIZE_RESULT_LABELS: Record<FinalizeResult, string> = {
  success: "成功",
  retryable_error: "可重试失败",
  permanent_error: "永久失败",
};
