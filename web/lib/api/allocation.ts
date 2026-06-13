import type {
  AllocationScenario,
  AssetClassTarget,
  RegionTarget,
} from "@/types/api";
import { apiDelete, apiGet, apiPost, apiPut } from "./client";

export function listScenarios(): Promise<{ scenarios: AllocationScenario[] }> {
  return apiGet("/api/v1/allocation-scenarios");
}

export function createScenario(body: {
  name: string;
  description?: string;
  weights: AssetClassTarget[];
  region_targets?: RegionTarget[];
  copy_from_id?: string;
}): Promise<AllocationScenario> {
  return apiPost("/api/v1/allocation-scenarios", body);
}

export function updateScenario(
  scenarioId: string,
  body: {
    name: string;
    description?: string;
    weights: AssetClassTarget[];
    region_targets: RegionTarget[];
  },
): Promise<AllocationScenario> {
  return apiPut(`/api/v1/allocation-scenarios/${scenarioId}`, body);
}

export function deleteScenario(scenarioId: string): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/allocation-scenarios/${scenarioId}`);
}

export function getAllocation(planId: string): Promise<{
  asset_class_targets: AssetClassTarget[];
  region_targets: RegionTarget[];
}> {
  return apiGet(`/api/v1/plans/${planId}/allocation`);
}

export function updateAllocation(
  planId: string,
  body: {
    config_version: number;
    asset_class_targets: AssetClassTarget[];
    region_targets: RegionTarget[];
  },
): Promise<{
  asset_class_targets: AssetClassTarget[];
  region_targets: RegionTarget[];
}> {
  return apiPut(`/api/v1/plans/${planId}/allocation`, body);
}

export function applyScenario(
  planId: string,
  body: {
    scenario_id: string;
    config_version: number;
    dry_run: boolean;
  },
): Promise<{
  scenario_id: string;
  before: AssetClassTarget[];
  after: AssetClassTarget[];
  applied: boolean;
  config_version?: number;
}> {
  return apiPost(`/api/v1/plans/${planId}/apply-scenario`, body);
}
