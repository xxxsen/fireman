import type { PlanParameters } from "@/types/api";

export interface ParametersFormSnapshot {
  planName: string;
  parameters: ReturnType<typeof normalizeParameters>;
  gapAction: "" | "cash";
}

function normalizeParameters(params: PlanParameters) {
  const { updated_at, plan_id, ...rest } = params;
  void updated_at;
  void plan_id;
  return rest;
}

export function buildParametersFormSnapshot(
  planName: string,
  parameters: PlanParameters,
  gapAction: "" | "cash",
): ParametersFormSnapshot {
  return {
    planName,
    parameters: normalizeParameters(parameters),
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
