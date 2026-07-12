import type {
  JobEvent,
  PathDetail,
  PathIndexRow,
  ReturnOverride,
  ScenarioComparison,
  SimulationRun,
} from "@/types/api";
import { apiDelete, apiGet, apiPost, apiPut } from "./client";

export function createSimulation(
  planId: string,
  body?: { runs?: number; seed?: string },
): Promise<{ job_id: string; run_id: string; status: string }> {
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
  | "provider_data_anomaly"
  | "asset_identity_conflict";

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
  candidate_asset_keys?: string[];
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
  candidate_asset_keys?: string[];
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

export function getJob(jobId: string) {
  return apiGet<import("@/types/api").Job>(`/api/v1/jobs/${jobId}`);
}

export function cancelJob(jobId: string) {
  return apiPost(`/api/v1/jobs/${jobId}/cancel`);
}

export interface JobEventHandlers {
  onEvent?: (data: JobEvent) => void;
  onTerminal?: (data: JobEvent) => void;
  onError?: () => void;
}

/** Subscribe to job progress through generic SSE data frames. */
export function subscribeJobEvents(jobId: string, handlers: JobEventHandlers): EventSource | null {
  if (typeof EventSource === "undefined") return null;
  const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";
  const es = new EventSource(`${API_BASE}/api/v1/jobs/${jobId}/events`);

  es.onmessage = (e) => {
    try {
      const data = JSON.parse(e.data) as JobEvent;
      handlers.onEvent?.(data);
      if (
        data.status === "succeeded" ||
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
