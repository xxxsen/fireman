import type {
  ImprovementConfig,
  ImprovementApplication,
  ImprovementPreview,
  ImprovementReadiness,
  ImprovementRun,
  ImprovementRunSummary,
  Plan,
  PlanParameters,
} from "@/types/api";
import { apiGet, apiPost } from "./client";

export function getImprovementReadiness(planId: string, simulationRunId?: string) {
  const query = simulationRunId
    ? `?simulation_run_id=${encodeURIComponent(simulationRunId)}`
    : "";
  return apiGet<ImprovementReadiness>(
    `/api/v1/plans/${planId}/improvement-readiness${query}`,
  );
}

export function createImprovementRun(
  planId: string,
  body: ImprovementConfig & { simulation_run_id: string },
): Promise<{ run_id: string; task_id: string; status: string; reused: boolean }> {
  return apiPost(`/api/v1/plans/${planId}/improvement-runs`, body, {
    "Idempotency-Key": crypto.randomUUID(),
  });
}

export function listImprovementRuns(
  planId: string,
  limit = 20,
  offset = 0,
): Promise<{ runs: ImprovementRunSummary[]; total: number; limit: number; offset: number }> {
  return apiGet(
    `/api/v1/plans/${planId}/improvement-runs?limit=${limit}&offset=${offset}`,
  );
}

export function getImprovementRun(runId: string): Promise<ImprovementRun> {
  return apiGet(`/api/v1/improvement-runs/${runId}`);
}

export function previewImprovementProposal(
  runId: string,
  proposalId: string,
  expectedPlanConfigVersion: number,
): Promise<ImprovementPreview> {
  return apiPost(
    `/api/v1/improvement-runs/${runId}/proposals/${proposalId}/preview`,
    { expected_plan_config_version: expectedPlanConfigVersion },
  );
}

export function applyImprovementProposal(
  preview: ImprovementPreview,
): Promise<{ application: ImprovementApplication; plan: Plan; parameters: PlanParameters }> {
  return apiPost(
    `/api/v1/improvement-runs/${preview.run_id}/proposals/${preview.proposal_id}/apply`,
    {
      expected_plan_config_version: preview.expected_plan_config_version,
      preview_hash: preview.preview_hash,
      preview_expires_at: preview.preview_expires_at,
    },
  );
}
