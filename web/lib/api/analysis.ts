import { apiGet, apiPost } from "./client";

export interface AnalysisJobView {
  job_id: string;
  plan_id: string;
  status: string;
  input_hash: string;
  current_config_hash: string;
  result_stale: boolean;
  result_json?: Record<string, unknown>;
  created_at: number;
}

export function createStressTest(planId: string, body?: { runs?: number; seed?: string }) {
  return apiPost<{ job_id: string; status: string }>(
    `/api/v1/plans/${planId}/stress-tests`,
    body ?? {},
  );
}

export function listStressTests(planId: string) {
  return apiGet<{ stress_tests: AnalysisJobView[] }>(`/api/v1/plans/${planId}/stress-tests`);
}

export function getStressTest(jobId: string) {
  return apiGet<AnalysisJobView>(`/api/v1/stress-tests/${jobId}`);
}

export function createSensitivityTest(planId: string, body?: { runs?: number; seed?: string }) {
  return apiPost<{ job_id: string; status: string }>(
    `/api/v1/plans/${planId}/sensitivity-tests`,
    body ?? {},
  );
}

export function listSensitivityTests(planId: string) {
  return apiGet<{ sensitivity_tests: AnalysisJobView[] }>(
    `/api/v1/plans/${planId}/sensitivity-tests`,
  );
}

export function getSensitivityTest(jobId: string) {
  return apiGet<AnalysisJobView>(`/api/v1/sensitivity-tests/${jobId}`);
}
