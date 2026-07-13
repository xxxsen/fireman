import type {
  FrontierApplication,
  FrontierConfig,
  FrontierPreview,
  FrontierReadiness,
  FrontierRun,
  FrontierRunSummary,
  Plan,
  PlanParameters,
} from "@/types/api";
import { apiGet, apiPost } from "./client";

export type FrontierRequest = Pick<
  FrontierConfig,
  | "frontier_type"
  | "target_success_probability"
  | "evaluation_runs"
  | "retirement_age_range"
  | "search"
> & { source_simulation_run_id: string };

export function getFrontierReadiness(planId: string, body: FrontierRequest) {
  return apiPost<FrontierReadiness>(
    `/api/v1/plans/${planId}/fire-frontier-readiness`,
    body,
  );
}

export function createFrontierRun(planId: string, body: FrontierRequest): Promise<{
  run_id: string;
  task_id: string;
  status: string;
  reused: boolean;
}> {
  return apiPost(`/api/v1/plans/${planId}/fire-frontier-runs`, body, {
    "Idempotency-Key": crypto.randomUUID(),
  });
}

export function listFrontierRuns(planId: string, limit = 20, offset = 0): Promise<{
  runs: FrontierRunSummary[];
  total: number;
  limit: number;
  offset: number;
}> {
  return apiGet(
    `/api/v1/plans/${planId}/fire-frontier-runs?limit=${limit}&offset=${offset}`,
  );
}

export function getFrontierRun(runId: string): Promise<FrontierRun> {
  return apiGet(`/api/v1/fire-frontier-runs/${runId}`);
}

export function previewFrontierPoint(
  runId: string,
  pointId: string,
  expectedPlanConfigVersion: number,
): Promise<FrontierPreview> {
  return apiPost(
    `/api/v1/fire-frontier-runs/${runId}/points/${pointId}/preview`,
    { expected_plan_config_version: expectedPlanConfigVersion },
  );
}

export function applyFrontierPoint(preview: FrontierPreview): Promise<{
  application: FrontierApplication;
  plan: Plan;
  parameters: PlanParameters;
}> {
  return apiPost(
    `/api/v1/fire-frontier-runs/${preview.run_id}/points/${preview.point_id}/apply`,
    {
      expected_plan_config_version: preview.expected_plan_config_version,
      preview_hash: preview.preview_hash,
      preview_expires_at: preview.preview_expires_at,
    },
  );
}
