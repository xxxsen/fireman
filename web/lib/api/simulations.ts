import type {
  TaskEvent,
  PathDetail,
  PathIndexRow,
  ReturnOverride,
  ScenarioComparison,
  SimulationRun,
  WorkerTaskStatus,
} from "@/types/api";
import { apiDelete, apiGet, apiPost, apiPut } from "./client";
export { listTasks } from "./tasks";

export function createSimulation(
  planId: string,
  body?: { runs?: number; seed?: string },
): Promise<{ task_id: string; run_id: string; status: WorkerTaskStatus; reused: boolean }> {
  return apiPost(
    `/api/v1/plans/${planId}/simulations`,
    body ?? {},
    { "Idempotency-Key": crypto.randomUUID() },
  );
}

export function listSimulations(planId: string): Promise<{ simulations: SimulationRun[] }> {
  return apiGet(`/api/v1/plans/${planId}/simulations`);
}

export function getSimulation(runId: string): Promise<SimulationRun> {
  return apiGet(`/api/v1/simulations/${runId}`);
}

export function listPaths(runId: string): Promise<{ paths: PathIndexRow[] }> {
  return apiGet(`/api/v1/simulations/${runId}/paths`);
}

export function getPathDetail(runId: string, pathNo: number): Promise<PathDetail> {
  return apiGet(`/api/v1/simulations/${runId}/paths/${pathNo}`);
}

export function getScenarioComparison(planId: string, runId: string): Promise<ScenarioComparison> {
  return apiGet(`/api/v1/plans/${planId}/simulations/${runId}/scenario-comparison`);
}

export function getReturnOverrides(
  planId: string,
): Promise<{ overrides: ReturnOverride[] }> {
  return apiGet(`/api/v1/plans/${planId}/return-overrides`);
}

export function setReturnOverride(
  planId: string,
  assetKey: string,
  body: {
    forward_return: number | null;
    annual_volatility: number | null;
    reason: string;
    expires_at: string;
  },
): Promise<ReturnOverride> {
  return apiPut(
    `/api/v1/plans/${planId}/return-overrides/${encodeURIComponent(assetKey)}`,
    body,
  );
}

export function deleteReturnOverride(
  planId: string,
  assetKey: string,
): Promise<{ deleted: boolean }> {
  return apiDelete(
    `/api/v1/plans/${planId}/return-overrides/${encodeURIComponent(assetKey)}`,
  );
}

// ---- Simulation readiness (snapshot admission gate) ----

export type ReadinessReason =
  | "history_missing"
  | "history_sync_running"
  | "simulation_insufficient_history"
  | "provider_data_anomaly";

/**
 * One plan holding whose market asset blocks simulation. Not every blocked
 * asset is missing history: it can be synced yet fail snapshot admission.
 */
export interface BlockingAsset {
  holding_id: string;
  asset_key: string;
  symbol: string;
  name: string;
  reason: ReadinessReason | string;
  message?: string;
}

export interface SimulationReadiness {
  ready: boolean;
  blocking_assets: BlockingAsset[];
  active_tasks: import("@/lib/api/market-assets").WorkerTask[];
}

export function getSimulationReadiness(planId: string): Promise<SimulationReadiness> {
  return apiGet(`/api/v1/plans/${planId}/simulation-readiness`);
}

export interface SyncMissingAssetEntry {
  asset_key: string;
  task?: import("@/lib/api/market-assets").WorkerTask | null;
}

/** An asset for which a new sync task would not make it simulatable. */
export interface SyncMissingBlockedEntry {
  asset_key: string;
  reason: ReadinessReason | string;
  message?: string;
}

export interface SyncMissingHistoryResult {
  created: SyncMissingAssetEntry[];
  existing: SyncMissingAssetEntry[];
  ready: SyncMissingAssetEntry[];
  blocked: SyncMissingBlockedEntry[];
}

export function syncMissingAssetHistory(planId: string): Promise<SyncMissingHistoryResult> {
  return apiPost(`/api/v1/plans/${planId}/sync-missing-asset-history`, {});
}

export function getTask(taskId: string) {
	return apiGet<import("@/types/api").Task>(`/api/v1/tasks/${taskId}`);
}

export function cancelTask(taskId: string) {
	return apiPost(`/api/v1/tasks/${taskId}/cancel`);
}

export interface TaskEventHandlers {
	onEvent?: (data: TaskEvent) => void;
	onTerminal?: (data: TaskEvent) => void;
  onError?: () => void;
}

/** Subscribe to job progress through generic SSE data frames. */
export function subscribeTaskEvents(taskId: string, handlers: TaskEventHandlers): EventSource | null {
  if (typeof EventSource === "undefined") return null;
  const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
	const es = new EventSource(`${API_BASE}/api/v1/tasks/${taskId}/events`);

  es.onmessage = (e) => {
    try {
		const data = JSON.parse(e.data) as TaskEvent;
      handlers.onEvent?.(data);
      if (
        data.status === "complete" ||
        data.status === "failed" ||
        data.status === "canceled"
      ) {
        handlers.onTerminal?.(data);
        es.close();
      }
    } catch {
      handlers.onError?.();
    }
  };

  es.onerror = () => {
    handlers.onError?.();
    es.close();
  };

  return es;
}
