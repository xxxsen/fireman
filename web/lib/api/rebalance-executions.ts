import type { RebalanceExecutionDetail, RebalanceExecutionSummary } from "@/types/api";
import { apiGet, apiPost } from "./client";

export function createRebalanceExecution(
  planId: string,
  body: { asset_keys?: string[]; force_new?: boolean } = {},
): Promise<RebalanceExecutionDetail> {
  return apiPost<RebalanceExecutionDetail>(`/api/v1/plans/${planId}/rebalance-executions`, body);
}

export function listRebalanceExecutions(planId: string): Promise<RebalanceExecutionSummary[]> {
  return apiGet<RebalanceExecutionSummary[]>(`/api/v1/plans/${planId}/rebalance-executions`);
}

export function getActiveRebalanceExecution(
  planId: string,
): Promise<RebalanceExecutionDetail | null> {
  return apiGet<RebalanceExecutionDetail | null>(
    `/api/v1/plans/${planId}/rebalance-executions/active`,
  );
}

export function getRebalanceExecution(
  planId: string,
  executionId: string,
): Promise<RebalanceExecutionDetail> {
  return apiGet<RebalanceExecutionDetail>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}`,
  );
}

export function sellRebalanceExecution(
  planId: string,
  executionId: string,
  body: { line_id: string; amount_minor: number; note?: string },
): Promise<RebalanceExecutionDetail> {
  return apiPost<RebalanceExecutionDetail>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}/sell`,
    body,
  );
}

export function buyRebalanceExecution(
  planId: string,
  executionId: string,
  body: { line_id: string; amount_minor: number; note?: string },
): Promise<RebalanceExecutionDetail> {
  return apiPost<RebalanceExecutionDetail>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}/buy`,
    body,
  );
}

export function skipRebalanceExecutionLine(
  planId: string,
  executionId: string,
  body: { line_id: string },
): Promise<RebalanceExecutionDetail> {
  return apiPost<RebalanceExecutionDetail>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}/skip`,
    body,
  );
}

export function noteRebalanceExecution(
  planId: string,
  executionId: string,
  body: { note: string },
): Promise<RebalanceExecutionDetail> {
  return apiPost<RebalanceExecutionDetail>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}/notes`,
    body,
  );
}

export function completeRebalanceExecution(
  planId: string,
  executionId: string,
  body: { config_version: number },
): Promise<RebalanceExecutionDetail> {
  return apiPost<RebalanceExecutionDetail>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}/complete`,
    body,
  );
}

export function cancelRebalanceExecution(
  planId: string,
  executionId: string,
): Promise<{ canceled: boolean }> {
  return apiPost<{ canceled: boolean }>(
    `/api/v1/plans/${planId}/rebalance-executions/${executionId}/cancel`,
  );
}
