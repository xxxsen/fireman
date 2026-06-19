import type { Plan, PlanParameters, RegionTarget } from "@/types/api";
import { apiDelete, apiGet, apiPost, apiPut } from "./client";

export function listPlans(): Promise<Plan[]> {
  return apiGet<Plan[]>("/api/v1/plans");
}

export function getPlan(planId: string): Promise<Plan> {
  return apiGet<Plan>(`/api/v1/plans/${planId}`);
}

export function createPlanWizard(body: {
  name: string;
  base_currency?: string;
  valuation_date: string;
  selected_scenario_id: string;
  parameters: PlanParameters;
  holdings: {
    instrument_id: string;
    enabled: boolean;
    weight_within_group: number;
    current_amount_minor: number;
    sort_order: number;
  }[];
  apply_unallocated_to_cash?: boolean;
  region_targets: RegionTarget[];
}): Promise<Plan> {
  return apiPost<Plan>("/api/v1/plans/wizard", body);
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
}> {
  return apiGet(`/api/v1/plans/${planId}/parameters`);
}

export function updateParameters(
  planId: string,
  body: {
    config_version: number;
    parameters: PlanParameters;
    apply_unallocated_to_cash?: boolean;
  },
): Promise<{ parameters: PlanParameters }> {
  return apiPut(`/api/v1/plans/${planId}/parameters`, body);
}

export function createPortfolioSnapshot(
  planId: string,
  body: { config_version: number; note?: string },
): Promise<unknown> {
  return apiPost(`/api/v1/plans/${planId}/portfolio-snapshots`, body);
}

/** Sync plan benchmark total_assets_minor to current holdings sum. */
export function syncPlanTotalAssets(
  planId: string,
  body: {
    config_version: number;
    parameters: PlanParameters;
  },
): Promise<{ parameters: PlanParameters }> {
  return updateParameters(planId, body);
}
