"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { listPlans } from "@/lib/api/plans";
import {
  applyPlanReplacement,
  previewPlanReplacement,
  updateCollectionItem,
  type ResearchCollectionDetail,
  type ResearchPlanApplyResult,
  type ResearchPlanReplacementPreview,
} from "@/lib/api/research";
import { ApiError } from "@/lib/api/client";
import { queryErrorMessage } from "@/lib/query-error";
import { assetClassLabel, formatMoneyScaled, formatPercent, regionLabel } from "@/lib/format";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { LoadingState } from "@/components/ui/LoadingState";

interface IncompleteItem {
  item_id: string;
  asset_key: string;
  missing_fields: string[];
}

function parseIncompleteItems(error: unknown): IncompleteItem[] | null {
  if (!(error instanceof ApiError) || error.code !== "research_item_classification_incomplete") {
    return null;
  }
  const items = error.details?.items;
  return Array.isArray(items) ? (items as IncompleteItem[]) : [];
}

export interface CopyToPlanDialogProps {
  open: boolean;
  onClose: () => void;
  detail: ResearchCollectionDetail;
}

/** Preview and atomically replace a FIRE plan with the research allocation. */
export function CopyToPlanDialog({ open, onClose, detail }: CopyToPlanDialogProps) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [incomplete, setIncomplete] = useState<IncompleteItem[] | null>(null);
  const [fixes, setFixes] = useState<Record<string, { asset_class: string; region: string }>>({});
  const [preview, setPreview] = useState<ResearchPlanReplacementPreview | null>(null);
  const [applied, setApplied] = useState<ResearchPlanApplyResult | null>(null);
  const [conflictMessage, setConflictMessage] = useState<string | null>(null);

  const plansQuery = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
    enabled: open,
  });

  const itemsByID = useMemo(
    () => new Map(detail.items.map((item) => [item.id, item])),
    [detail.items],
  );

  const previewMutation = useMutation({
    mutationFn: () => previewPlanReplacement(detail.id, selectedPlanId),
    onSuccess: (result) => {
      setIncomplete(null);
      setConflictMessage(null);
      setPreview(result);
    },
    onError: (error) => {
      const items = parseIncompleteItems(error);
      if (!items) return;
      setIncomplete(items);
      const seed: Record<string, { asset_class: string; region: string }> = {};
      for (const incompleteItem of items) {
        const item = itemsByID.get(incompleteItem.item_id);
        seed[incompleteItem.item_id] = {
          asset_class: item?.asset_class ?? "",
          region: item?.region ?? "",
        };
      }
      setFixes(seed);
    },
  });

  const applyMutation = useMutation({
    mutationFn: () => {
      if (!preview) throw new Error("缺少替换预览");
      return applyPlanReplacement(detail.id, {
        plan_id: preview.plan_id,
        expected_config_version: preview.expected_config_version,
        expected_replacement_hash: preview.replacement_hash,
        mode: "replace_all",
      });
    },
    onSuccess: (result) => {
      setApplied(result);
      for (const key of [
        ["plans"],
        ["plan", result.plan_id],
        ["parameters", result.plan_id],
        ["allocation", result.plan_id],
        ["holdings", result.plan_id],
        ["simulations", result.plan_id],
      ]) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
    },
    onError: (error) => {
      if (
        error instanceof ApiError &&
        (error.code === "plan_config_conflict" || error.code === "research_collection_changed")
      ) {
        setPreview(null);
        setConflictMessage("计划或研究组合已发生变化，请重新生成预览后再确认替换。");
      }
    },
  });

  const fixMutation = useMutation({
    mutationFn: async () => {
      for (const [itemId, fix] of Object.entries(fixes)) {
        await updateCollectionItem(detail.id, itemId, fix);
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["research", "collection", detail.id] });
      setIncomplete(null);
      previewMutation.mutate();
    },
  });

  const allFixed = useMemo(
    () =>
      (incomplete ?? []).every((item) => {
        const fix = fixes[item.item_id];
        return Boolean(fix?.asset_class && fix?.region);
      }),
    [incomplete, fixes],
  );

  function handleClose() {
    setIncomplete(null);
    setPreview(null);
    setApplied(null);
    setConflictMessage(null);
    previewMutation.reset();
    applyMutation.reset();
    onClose();
  }

  const plans = plansQuery.data ?? [];
  const previewError =
    previewMutation.isError && !incomplete ? queryErrorMessage(previewMutation.error) : null;
  const applyError =
    applyMutation.isError && !conflictMessage ? queryErrorMessage(applyMutation.error) : null;

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      title="应用到 FIRE 计划"
      className="max-w-3xl"
      footer={
        applied && preview ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={handleClose}>关闭</Button>
            <Button
              onClick={() => router.push(`/plans/${applied.plan_id}/overview?source=research`)}
              data-testid="goto-plan-overview"
            >
              查看「{preview.plan_name}」
            </Button>
          </div>
        ) : preview ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={() => setPreview(null)}>返回</Button>
            <Button
              variant="danger"
              pending={applyMutation.isPending}
              onClick={() => applyMutation.mutate()}
              data-testid="apply-plan-replacement"
            >
              确认完整替换
            </Button>
          </div>
        ) : incomplete ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={handleClose}>取消</Button>
            <Button
              disabled={!allFixed}
              pending={fixMutation.isPending || previewMutation.isPending}
              onClick={() => fixMutation.mutate()}
              data-testid="fix-and-retry"
            >
              保存并重新预览
            </Button>
          </div>
        ) : (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={handleClose}>取消</Button>
            <Button
              disabled={!selectedPlanId}
              pending={previewMutation.isPending}
              onClick={() => previewMutation.mutate()}
              data-testid="preview-plan-replacement"
            >
              查看替换预览
            </Button>
          </div>
        )
      }
    >
      {applied && preview ? (
        <div className="space-y-2" data-testid="apply-result">
          <p className="text-sm font-medium text-ink">研究组合已完整应用到「{preview.plan_name}」。</p>
          <p className="text-sm text-ink-muted">
            已写入 {applied.holding_count} 项持仓、目标配置和组合快照；计划配置版本为 {applied.config_version}。
          </p>
        </div>
      ) : preview ? (
        <div className="space-y-4" data-testid="replacement-preview">
          <div className="border-b border-line pb-3">
            <p className="text-sm font-medium text-danger">此操作会完整替换计划当前的目标配置和全部持仓。</p>
            <p className="mt-1 text-sm text-ink-muted">
              「{preview.plan_name}」持仓将从 {preview.before_holding_count} 项变为 {preview.after_holding_count} 项，
              目标资产总额为 {formatMoneyScaled(preview.target_total_assets_minor, preview.base_currency)}。
            </p>
          </div>

          <div className="flex flex-wrap gap-x-5 gap-y-1 text-sm">
            {preview.allocation.asset_class_targets
              .filter((target) => target.weight > 0)
              .map((target) => (
                <span key={target.asset_class}>
                  {assetClassLabel(target.asset_class)} {formatPercent(target.weight)}
                </span>
              ))}
          </div>

          {preview.removed_holdings.length > 0 && (
            <div>
              <h3 className="mb-1 text-sm font-medium text-ink">将移除的持仓</h3>
              <p className="text-sm text-ink-muted">
                {preview.removed_holdings.map((item) => item.name || item.symbol || item.asset_key).join("、")}
              </p>
            </div>
          )}

          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-line text-left text-xs text-ink-muted">
                  <th className="px-2 py-1.5 font-medium">替换后资产</th>
                  <th className="px-2 py-1.5 font-medium">组合权重</th>
                  <th className="px-2 py-1.5 font-medium">大类 / 区域</th>
                  <th className="px-2 py-1.5 text-right font-medium">目标金额</th>
                </tr>
              </thead>
              <tbody>
                {preview.holdings.map((holding) => (
                  <tr key={holding.asset_key} className="border-b border-line/60 last:border-0">
                    <td className="px-2 py-1.5">
                      <span className="block max-w-52 truncate font-medium text-ink">
                        {holding.name || holding.asset_key}
                      </span>
                      <span className="block text-xs text-ink-muted">{holding.symbol}</span>
                    </td>
                    <td className="px-2 py-1.5 font-mono-numeric text-xs">{formatPercent(holding.weight)}</td>
                    <td className="px-2 py-1.5 text-xs">
                      {assetClassLabel(holding.asset_class)} / {regionLabel(holding.region)}
                    </td>
                    <td className="px-2 py-1.5 text-right font-mono-numeric text-xs">
                      {formatMoneyScaled(holding.current_amount_minor, preview.base_currency)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {applyError && <p className="text-sm text-danger" role="alert">替换失败：{applyError}</p>}
        </div>
      ) : incomplete ? (
        <div className="space-y-3" data-testid="copy-incomplete">
          <p className="text-sm text-warning">以下资产缺少 FIRE 资产大类或区域，补齐后才能生成替换预览。</p>
          <ul className="space-y-2">
            {incomplete.map((item) => {
              const source = itemsByID.get(item.item_id);
              const fix = fixes[item.item_id] ?? { asset_class: "", region: "" };
              return (
                <li key={item.item_id} className="flex flex-wrap items-center gap-2 border-b border-line px-1 py-2 text-sm">
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium text-ink">{source?.name ?? item.asset_key}</span>
                    <span className="block text-xs text-ink-muted">{item.asset_key}</span>
                  </span>
                  <select
                    value={fix.asset_class}
                    onChange={(event) => setFixes({ ...fixes, [item.item_id]: { ...fix, asset_class: event.target.value } })}
                    className="rounded-md border border-line bg-surface px-2 py-1 text-xs"
                    aria-label="资产大类"
                    data-testid={`fix-class-${item.item_id}`}
                  >
                    <option value="">大类...</option>
                    <option value="equity">权益</option>
                    <option value="bond">债券</option>
                    <option value="cash">现金/其他</option>
                  </select>
                  <select
                    value={fix.region}
                    onChange={(event) => setFixes({ ...fixes, [item.item_id]: { ...fix, region: event.target.value } })}
                    className="rounded-md border border-line bg-surface px-2 py-1 text-xs"
                    aria-label="区域"
                    data-testid={`fix-region-${item.item_id}`}
                  >
                    <option value="">区域...</option>
                    <option value="domestic">国内</option>
                    <option value="foreign">国外</option>
                  </select>
                </li>
              );
            })}
          </ul>
          {fixMutation.isError && <p className="text-sm text-danger" role="alert">保存失败：{queryErrorMessage(fixMutation.error)}</p>}
        </div>
      ) : (
        <div className="space-y-3">
          <p className="text-sm text-ink-muted">
            选择目标计划后核对完整替换内容。目标金额按计划当前总资产计算，不使用研究组合初始资金。
          </p>
          {conflictMessage && <p className="text-sm text-warning" role="alert">{conflictMessage}</p>}
          {plansQuery.isLoading && <LoadingState label="加载计划..." />}
          {plansQuery.isError && <p className="text-sm text-danger" role="alert">加载计划失败：{queryErrorMessage(plansQuery.error)}</p>}
          {!plansQuery.isLoading && plans.length === 0 && <p className="text-sm text-ink-muted">暂无计划，请先创建 FIRE 计划。</p>}
          <div className="space-y-2" role="radiogroup" aria-label="选择目标计划">
            {plans.map((plan) => (
              <label key={plan.id} className="flex cursor-pointer items-center gap-3 border-b border-line px-1 py-2 text-sm hover:bg-surface-muted">
                <input
                  type="radio"
                  name="copy-to-plan"
                  value={plan.id}
                  checked={selectedPlanId === plan.id}
                  onChange={() => setSelectedPlanId(plan.id)}
                />
                <span className="flex-1">
                  <span className="font-medium text-ink">{plan.name}</span>
                  <span className="ml-2 text-xs text-ink-muted">{plan.base_currency} · 估值日 {plan.valuation_date}</span>
                </span>
              </label>
            ))}
          </div>
          {previewError && <p className="text-sm text-danger" role="alert">预览失败：{previewError}</p>}
        </div>
      )}
    </Dialog>
  );
}
