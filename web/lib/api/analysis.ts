import { apiGet, apiPost } from "./client";
import type { WorkerTaskStatus } from "@/types/api";

export interface AnalysisTaskView {
  task_id: string;
  plan_id: string;
  status: string;
  input_hash: string;
  current_config_hash: string;
  result_stale: boolean;
  simulation_run_id?: string;
  result_json?: Record<string, unknown>;
  created_at: number;
}

export interface CreateAnalysisBody {
  runs?: number;
  seed?: string;
  simulation_run_id?: string;
}

function listQuery(simulationRunId?: string): string {
  return simulationRunId
    ? `?simulation_run_id=${encodeURIComponent(simulationRunId)}`
    : "";
}

export function createStressTest(planId: string, body?: CreateAnalysisBody) {
	return apiPost<{ task_id: string; status: WorkerTaskStatus; reused: boolean }>(
    `/api/v1/plans/${planId}/stress-tests`,
    body ?? {},
  );
}

export function listStressTests(planId: string, simulationRunId?: string) {
	return apiGet<{ stress_tests: AnalysisTaskView[] }>(
    `/api/v1/plans/${planId}/stress-tests${listQuery(simulationRunId)}`,
  );
}

export function getStressTest(taskId: string) {
	return apiGet<AnalysisTaskView>(`/api/v1/stress-tests/${taskId}`);
}

export function createSensitivityTest(planId: string, body?: CreateAnalysisBody) {
  return apiPost<{ task_id: string; status: WorkerTaskStatus; reused: boolean }>(
    `/api/v1/plans/${planId}/sensitivity-tests`,
    body ?? {},
  );
}

export function listSensitivityTests(planId: string, simulationRunId?: string) {
	return apiGet<{ sensitivity_tests: AnalysisTaskView[] }>(
    `/api/v1/plans/${planId}/sensitivity-tests${listQuery(simulationRunId)}`,
  );
}

export function getSensitivityTest(taskId: string) {
	return apiGet<AnalysisTaskView>(`/api/v1/sensitivity-tests/${taskId}`);
}
