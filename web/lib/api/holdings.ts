import type { Plan, PlanHolding, RegionTarget } from "@/types/api";
import { apiGet, apiPost, apiPut } from "./client";

export function getHoldings(planId: string): Promise<{ holdings: PlanHolding[] }> {
  return apiGet(`/api/v1/plans/${planId}/holdings`);
}

export function updateHoldings(
  planId: string,
  body: {
    config_version: number;
    holdings: {
      asset_key: string;
      asset_class: string;
      region: string;
      enabled: boolean;
      weight_within_group: number;
      current_amount_minor: number;
      sort_order: number;
    }[];
  },
): Promise<{ holdings: PlanHolding[] }> {
  return apiPut(`/api/v1/plans/${planId}/holdings`, body);
}

export interface HoldingRegionWeightView {
  holding_id: string;
  asset_key: string;
  region: string;
  portfolio_target_weight: number;
}

export interface HoldingRegionChangePreview {
  preview_hash: string;
  plan_config_version: number;
  holding_id: string;
  asset_key: string;
  asset_class: string;
  from_region: string;
  target_region: string;
  before_region_targets: RegionTarget[];
  after_region_targets: RegionTarget[];
  before_weights: HoldingRegionWeightView[];
  after_weights: HoldingRegionWeightView[];
}

export function previewHoldingRegionChange(
  planId: string,
  holdingId: string,
  targetRegion: "domestic" | "foreign",
): Promise<HoldingRegionChangePreview> {
  return apiPost(`/api/v1/plans/${planId}/holding-region-changes/preview`, {
    holding_id: holdingId,
    target_region: targetRegion,
  });
}

export function applyHoldingRegionChange(
  planId: string,
  preview: HoldingRegionChangePreview,
): Promise<{
  preview: HoldingRegionChangePreview;
  plan: Plan;
  allocation: {
    asset_class_targets: { asset_class: string; weight: number }[];
    region_targets: RegionTarget[];
  };
  holdings: PlanHolding[];
}> {
  return apiPost(`/api/v1/plans/${planId}/holding-region-changes/apply`, {
    holding_id: preview.holding_id,
    target_region: preview.target_region,
    preview_hash: preview.preview_hash,
  });
}

export interface HoldingSimulationSnapshot {
  id: string;
  asset_key: string;
  inclusion_date: string;
  as_of_date: string;
  complete_year_count: number;
  historical_cagr: number;
  modeled_annual_return: number;
  annual_volatility: number;
  max_drawdown: number;
  quality_status: string;
  source_hash: string;
}

export function getHoldingSnapshot(planId: string, holdingId: string) {
  return apiGet<HoldingSimulationSnapshot>(
    `/api/v1/plans/${planId}/holdings/${holdingId}/simulation-snapshot`,
  );
}

export function syncHoldingSnapshot(
  planId: string,
  holdingId: string,
  configVersion: number,
) {
  return apiPost<HoldingSimulationSnapshot>(
    `/api/v1/plans/${planId}/holdings/${holdingId}/sync-simulation-snapshot`,
    { config_version: configVersion },
  );
}

export function getTargets(planId: string) {
  return apiGet<import("@/types/api").TargetView>(`/api/v1/plans/${planId}/targets`);
}

export function getRebalance(
  planId: string,
  mode: "full" | "new_cash" = "full",
  newCashMinor?: number,
) {
  const params = new URLSearchParams({ mode });
  if (mode === "new_cash" && newCashMinor !== undefined) {
    params.set("new_cash_minor", String(newCashMinor));
  }
  return apiGet<import("@/types/api").RebalanceResult>(
    `/api/v1/plans/${planId}/rebalance?${params}`,
  );
}
