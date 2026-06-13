import type { PlanCashFlow, PlanParameters } from "@/types/api";

export interface ParametersFormSnapshot {
  planName: string;
  parameters: ReturnType<typeof normalizeParameters>;
  cashFlows: ReturnType<typeof normalizeCashFlows>;
  gapAction: "" | "cash";
}

function normalizeParameters(params: PlanParameters) {
  const { updated_at, plan_id, ...rest } = params;
  void updated_at;
  void plan_id;
  return rest;
}

function normalizeCashFlows(flows: PlanCashFlow[]) {
  return flows.map(({ plan_id, ...rest }) => {
    void plan_id;
    return rest;
  });
}

export function buildParametersFormSnapshot(
  planName: string,
  parameters: PlanParameters,
  cashFlows: PlanCashFlow[],
  gapAction: "" | "cash",
): ParametersFormSnapshot {
  return {
    planName,
    parameters: normalizeParameters(parameters),
    cashFlows: normalizeCashFlows(cashFlows),
    gapAction,
  };
}

export function isParametersFormDirty(
  initial: ParametersFormSnapshot | null,
  current: ParametersFormSnapshot,
): boolean {
  if (!initial) return false;
  return JSON.stringify(initial) !== JSON.stringify(current);
}
