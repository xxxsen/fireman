import { apiPost } from "./client";

export function submitAssetRefresh(
  planId: string,
  body: {
    config_version: number;
    scenario_id?: string;
    holdings: {
      instrument_id: string;
      current_amount_minor: number;
      weight_within_group?: number;
      sort_order?: number;
    }[];
    total_assets_minor: number;
    sync_total_assets_minor?: boolean;
    config_changed?: boolean;
  },
) {
  return apiPost<{
    holdings: unknown[];
    before_total_minor: number;
    after_total_minor: number;
    synced_scale: boolean;
  }>(`/api/v1/plans/${planId}/asset-refresh`, body);
}
