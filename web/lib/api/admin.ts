import { apiGet } from "./client";
import type { WorkerTaskStatus } from "./market-assets";

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

export interface AdminJobStats {
  queued: number;
  running: number;
  failed_last_24h: number;
  succeeded_last_24h: number;
}

export interface AdminCallbackStats {
  total_last_24h: number;
  failed_last_24h: number;
}

export interface AdminDirectoryScopeHealth {
  scope: string;
  last_success_at: number | null;
  active_task_status: string;
  stale: boolean;
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
  jobs: AdminJobStats;
  callbacks: AdminCallbackStats;
  sync_health: AdminSyncHealth;
  storage: AdminStorageStats;
}

export function getAdminOverview(): Promise<AdminOverview> {
  return apiGet("/api/v1/admin/overview");
}

// --- worker tasks ---

export type WorkerTaskType =
  | "asset_directory_sync"
  | "asset_history_sync"
  | "fx_rate_sync";

export const WORKER_TASK_TYPE_LABELS: Record<WorkerTaskType, string> = {
  asset_directory_sync: "目录同步",
  asset_history_sync: "历史同步",
  fx_rate_sync: "汇率同步",
};

export function workerTaskTypeLabel(type: string): string {
  return WORKER_TASK_TYPE_LABELS[type as WorkerTaskType] ?? type;
}

export interface AdminWorkerTaskItem {
  id: string;
  type: string;
  status: WorkerTaskStatus;
  dedupe_key: string;
  error_code: string;
  error_message: string;
  post_process_attempts: number;
  created_at: number;
  started_at: number | null;
  finished_at: number | null;
  duration_ms: number | null;
}

export interface AdminWorkerTaskListParams {
  type?: string;
  status?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export function listAdminWorkerTasks(
  params?: AdminWorkerTaskListParams,
): Promise<AdminPage<AdminWorkerTaskItem>> {
  const qs = new URLSearchParams();
  if (params?.type) qs.set("type", params.type);
  if (params?.status) qs.set("status", params.status);
  if (params?.q) qs.set("q", params.q);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/admin/worker-tasks${query ? `?${query}` : ""}`);
}

export interface AdminWorkerTaskFull {
  id: string;
  version_no: number;
  type: string;
  status: WorkerTaskStatus;
  dedupe_key: string;
  payload_json: string;
  result_data: string;
  heartbeat_at?: number | null;
  error_code: string;
  error_message: string;
  post_process_attempts: number;
  next_post_process_at?: number | null;
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

export type CallbackResult = "success" | "retryable_error" | "permanent_error";

export interface AdminPostProcessRecord {
  id: number;
  task_id: string;
  task_type: string;
  attempt_no: number;
  result: CallbackResult;
  error_code: string;
  error_message: string;
  duration_ms: number;
  created_at: number;
}

export interface AdminWorkerTaskDetail {
  task: AdminWorkerTaskFull;
  timeline: AdminTaskTimelinePhase[];
  heartbeat?: AdminTaskHeartbeat | null;
  post_process_records: AdminPostProcessRecord[];
}

export function getAdminWorkerTask(taskId: string): Promise<AdminWorkerTaskDetail> {
  return apiGet(`/api/v1/admin/worker-tasks/${encodeURIComponent(taskId)}`);
}

// --- jobs ---

export type AdminJobStatus = "queued" | "running" | "succeeded" | "failed" | "canceled";

export const JOB_TYPE_LABELS: Record<string, string> = {
  simulation: "模拟",
  stress: "压力测试",
  sensitivity: "敏感性分析",
};

export function jobTypeLabel(type: string): string {
  return JOB_TYPE_LABELS[type] ?? type;
}

export interface AdminJobItem {
  id: string;
  plan_id: string;
  plan_name: string;
  type: string;
  status: AdminJobStatus;
  phase: string;
  progress_current: number;
  progress_total: number;
  error_code: string;
  error_message: string;
  created_at: number;
  started_at: number | null;
  finished_at: number | null;
  duration_ms: number | null;
}

export interface AdminJobListParams {
  type?: string;
  status?: string;
  planId?: string;
  limit?: number;
  offset?: number;
}

export function listAdminJobs(params?: AdminJobListParams): Promise<AdminPage<AdminJobItem>> {
  const qs = new URLSearchParams();
  if (params?.type) qs.set("type", params.type);
  if (params?.status) qs.set("status", params.status);
  if (params?.planId) qs.set("plan_id", params.planId);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/admin/jobs${query ? `?${query}` : ""}`);
}

// --- post process records ---

export interface AdminCallbackListParams {
  taskId?: string;
  result?: string;
  taskType?: string;
  limit?: number;
  offset?: number;
}

export function listAdminPostProcessRecords(
  params?: AdminCallbackListParams,
): Promise<AdminPage<AdminPostProcessRecord>> {
  const qs = new URLSearchParams();
  if (params?.taskId) qs.set("task_id", params.taskId);
  if (params?.result) qs.set("result", params.result);
  if (params?.taskType) qs.set("task_type", params.taskType);
  if (params?.limit !== undefined) qs.set("limit", String(params.limit));
  if (params?.offset !== undefined) qs.set("offset", String(params.offset));
  const query = qs.toString();
  return apiGet(`/api/v1/admin/post-process-records${query ? `?${query}` : ""}`);
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
  hk_all: "香港市场目录",
  us_all: "美国市场目录",
};

export function directoryScopeLabel(scope: string): string {
  return DIRECTORY_SCOPE_LABELS[scope] ?? scope;
}

export const CALLBACK_RESULT_LABELS: Record<CallbackResult, string> = {
  success: "成功",
  retryable_error: "可重试失败",
  permanent_error: "永久失败",
};
