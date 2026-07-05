import type { PlanHolding } from "@/types/api";
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
