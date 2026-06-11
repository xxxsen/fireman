import type { RebalanceDraftDetail } from "@/types/api";
import { apiDelete, apiGet, apiPatch, apiPost } from "./client";

export function createRebalanceDraft(
  planId: string,
  body: { force_new?: boolean } = {},
): Promise<RebalanceDraftDetail> {
  return apiPost<RebalanceDraftDetail>(`/api/v1/plans/${planId}/rebalance-drafts`, body);
}

export function getActiveRebalanceDraft(
  planId: string,
): Promise<RebalanceDraftDetail | null> {
  return apiGet<RebalanceDraftDetail | null>(
    `/api/v1/plans/${planId}/rebalance-drafts/active`,
  );
}

export function getRebalanceDraft(
  planId: string,
  draftId: string,
): Promise<RebalanceDraftDetail> {
  return apiGet<RebalanceDraftDetail>(
    `/api/v1/plans/${planId}/rebalance-drafts/${draftId}`,
  );
}

export function patchRebalanceDraftLines(
  planId: string,
  draftId: string,
  body: {
    stage?: boolean;
    lines: { line_id: string; planned_current_minor: number }[];
  },
): Promise<RebalanceDraftDetail> {
  return apiPatch<RebalanceDraftDetail>(
    `/api/v1/plans/${planId}/rebalance-drafts/${draftId}/lines`,
    body,
  );
}

export function undoRebalanceDraft(
  planId: string,
  draftId: string,
): Promise<RebalanceDraftDetail> {
  return apiPost<RebalanceDraftDetail>(
    `/api/v1/plans/${planId}/rebalance-drafts/${draftId}/undo`,
  );
}

export function commitRebalanceDraft(
  planId: string,
  draftId: string,
  body: {
    config_version: number;
    confirm_imbalanced?: boolean;
    sweep_unallocated_to_cash?: boolean;
    accept_scale_shrink?: boolean;
    record_snapshot?: boolean;
    snapshot_note?: string;
  },
): Promise<RebalanceDraftDetail> {
  return apiPost<RebalanceDraftDetail>(
    `/api/v1/plans/${planId}/rebalance-drafts/${draftId}/commit`,
    body,
  );
}

export function cancelRebalanceDraft(
  planId: string,
  draftId: string,
): Promise<{ cancelled: boolean }> {
  return apiDelete(`/api/v1/plans/${planId}/rebalance-drafts/${draftId}`);
}
