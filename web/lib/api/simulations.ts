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

export function getScenarioComparison(planId: string): Promise<ScenarioComparison> {
  return apiGet(`/api/v1/plans/${planId}/scenario-comparison`);
}

export function getReturnOverrides(
  planId: string,
): Promise<{ overrides: ReturnOverride[] }> {
  return apiGet(`/api/v1/plans/${planId}/return-overrides`);
}

export function setReturnOverride(
  planId: string,
  instrumentId: string,
  body: {
    forward_return: number | null;
    annual_volatility: number | null;
    reason: string;
    expires_at: string;
  },
): Promise<ReturnOverride> {
  return apiPut(`/api/v1/plans/${planId}/return-overrides/${instrumentId}`, body);
}

export function deleteReturnOverride(
  planId: string,
  instrumentId: string,
): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/plans/${planId}/return-overrides/${instrumentId}`);
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
