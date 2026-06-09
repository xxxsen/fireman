import type { Plan, PlanCashFlow, PlanParameters } from "@/types/api";
import { apiDelete, apiGet, apiPost, apiPut } from "./client";

export function listPlans(): Promise<Plan[]> {
  return apiGet<Plan[]>("/api/v1/plans");
}

export function getPlan(planId: string): Promise<Plan> {
  return apiGet<Plan>(`/api/v1/plans/${planId}`);
}

export function createPlan(body: {
  name: string;
  base_currency?: string;
  valuation_date: string;
  selected_scenario_id?: string;
}): Promise<Plan> {
  return apiPost<Plan>("/api/v1/plans", body);
}

export function updatePlan(
  planId: string,
  body: {
    config_version: number;
    name: string;
    base_currency: string;
    valuation_date: string;
    status: string;
  },
): Promise<Plan> {
  return apiPut<Plan>(`/api/v1/plans/${planId}`, body);
}

export function deletePlan(planId: string): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/plans/${planId}`);
}

export function getParameters(planId: string): Promise<{
  parameters: PlanParameters;
  cash_flows: PlanCashFlow[];
}> {
  return apiGet(`/api/v1/plans/${planId}/parameters`);
}

export function updateParameters(
  planId: string,
  body: {
    config_version: number;
    parameters: PlanParameters;
    cash_flows?: PlanCashFlow[];
    apply_unallocated_to_cash?: boolean;
  },
): Promise<{ parameters: PlanParameters; cash_flows: PlanCashFlow[] }> {
  return apiPut(`/api/v1/plans/${planId}/parameters`, body);
}

export function createPortfolioSnapshot(
  planId: string,
  body: { config_version: number; note?: string },
): Promise<unknown> {
  return apiPost(`/api/v1/plans/${planId}/portfolio-snapshots`, body);
}
