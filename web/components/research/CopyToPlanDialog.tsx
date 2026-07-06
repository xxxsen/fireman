"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { listPlans } from "@/lib/api/plans";
import {
  copyToPlan,
  updateCollectionItem,
  type ResearchCollectionDetail,
  type ResearchCopyToPlanResult,
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
  if (!(error instanceof ApiError) || error.code !== "research_items_incomplete") return null;
  const items = error.details?.items;
  if (!Array.isArray(items)) return [];
  return items as IncompleteItem[];
}

export interface CopyToPlanDialogProps {
  open: boolean;
  onClose: () => void;
  detail: ResearchCollectionDetail;
}

/**
 * Copy the collection into a FIRE plan holdings draft: pick a plan, backfill
 * missing asset_class/region on items, preview the draft and jump to the
 * plan's holdings correction flow.
 */
export function CopyToPlanDialog({ open, onClose, detail }: CopyToPlanDialogProps) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [incomplete, setIncomplete] = useState<IncompleteItem[] | null>(null);
  const [fixes, setFixes] = useState<Record<string, { asset_class: string; region: string }>>({});
  const [result, setResult] = useState<ResearchCopyToPlanResult | null>(null);

  const plansQuery = useQuery({
    queryKey: ["plans"],
    queryFn: listPlans,
    enabled: open,
  });

  const itemsByID = useMemo(() => {
    const map = new Map(detail.items.map((it) => [it.id, it]));
    return map;
  }, [detail.items]);

  const copyMutation = useMutation({
    mutationFn: () => copyToPlan(detail.id, selectedPlanId),
    onSuccess: (res) => {
      setIncomplete(null);
      setResult(res);
    },
    onError: (err) => {
      const items = parseIncompleteItems(err);
      if (items) {
        setIncomplete(items);
        const seed: Record<string, { asset_class: string; region: string }> = {};
        for (const it of items) {
          const item = itemsByID.get(it.item_id);
          seed[it.item_id] = {
            asset_class: item?.asset_class ?? "",
            region: item?.region ?? "",
          };
        }
        setFixes(seed);
      }
    },
  });

  const fixMutation = useMutation({
    mutationFn: async () => {
      for (const [itemId, fix] of Object.entries(fixes)) {
        await updateCollectionItem(detail.id, itemId, {
          asset_class: fix.asset_class,
          region: fix.region,
        });
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["research", "collection", detail.id] });
      setIncomplete(null);
      copyMutation.mutate();
    },
  });

  const allFixed = useMemo(
    () =>
      (incomplete ?? []).every((it) => {
        const fix = fixes[it.item_id];
        return fix && fix.asset_class && fix.region;
      }),
    [incomplete, fixes],
  );

  function handleClose() {
    setIncomplete(null);
    setResult(null);
    copyMutation.reset();
    onClose();
  }

  const plans = plansQuery.data ?? [];
  const copyError =
    copyMutation.isError && !incomplete ? queryErrorMessage(copyMutation.error) : null;

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      title="复制到 FIRE 计划"
      className="max-w-2xl"
      footer={
        result ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={handleClose}>
              关闭
            </Button>
            <Button
              onClick={() => router.push(`/plans/${result.plan_id}/holdings`)}
              data-testid="goto-plan-holdings"
            >
              前往「{result.plan_name}」持仓校正
            </Button>
          </div>
        ) : incomplete ? (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={handleClose}>
              取消
            </Button>
            <Button
              disabled={!allFixed}
              pending={fixMutation.isPending || copyMutation.isPending}
              onClick={() => fixMutation.mutate()}
              data-testid="fix-and-retry"
            >
              保存并重试
            </Button>
          </div>
        ) : (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="secondary" onClick={handleClose}>
              取消
            </Button>
            <Button
              disabled={!selectedPlanId}
              pending={copyMutation.isPending}
              onClick={() => copyMutation.mutate()}
              data-testid="copy-to-plan-confirm"
            >
              生成持仓草稿
            </Button>
          </div>
        )
      }
    >
      {result ? (
        <div className="space-y-3" data-testid="copy-result">
          <p className="text-sm text-ink">
            已根据集合权重生成持仓草稿（目标计划「{result.plan_name}」）。研究集合不会直接改写计划持仓，
            请在计划持仓页按草稿校正金额。
          </p>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-line text-left text-xs text-ink-muted">
                  <th className="px-2 py-1.5 font-medium">资产</th>
                  <th className="px-2 py-1.5 font-medium">权重</th>
                  <th className="px-2 py-1.5 font-medium">大类 / 区域</th>
                  <th className="px-2 py-1.5 text-right font-medium">目标金额</th>
                </tr>
              </thead>
              <tbody>
                {result.holdings.map((h) => (
                  <tr key={h.asset_key} className="border-b border-line/60 last:border-0">
                    <td className="px-2 py-1.5">
                      <span className="block max-w-52 truncate font-medium text-ink">
                        {h.name || h.asset_key}
                      </span>
                      <span className="block text-xs text-ink-muted">{h.symbol}</span>
                    </td>
                    <td className="px-2 py-1.5 font-mono-numeric text-xs">
                      {formatPercent(h.weight)}
                    </td>
                    <td className="px-2 py-1.5 text-xs">
                      {assetClassLabel(h.asset_class)} / {regionLabel(h.region)}
                    </td>
                    <td className="px-2 py-1.5 text-right font-mono-numeric text-xs">
                      {formatMoneyScaled(h.current_amount_minor, detail.base_currency)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : incomplete ? (
        <div className="space-y-3" data-testid="copy-incomplete">
          <p className="text-sm text-warning">
            以下资产缺少 FIRE 资产大类或区域，必须补齐后才能复制到计划。
          </p>
          <ul className="space-y-2">
            {incomplete.map((it) => {
              const item = itemsByID.get(it.item_id);
              const fix = fixes[it.item_id] ?? { asset_class: "", region: "" };
              return (
                <li
                  key={it.item_id}
                  className="flex flex-wrap items-center gap-2 rounded-md border border-line px-3 py-2 text-sm"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium text-ink">
                      {item?.name ?? it.asset_key}
                    </span>
                    <span className="block text-xs text-ink-muted">{it.asset_key}</span>
                  </span>
                  <select
                    value={fix.asset_class}
                    onChange={(e) =>
                      setFixes({ ...fixes, [it.item_id]: { ...fix, asset_class: e.target.value } })
                    }
                    className="rounded-md border border-line bg-surface px-2 py-1 text-xs"
                    aria-label="资产大类"
                    data-testid={`fix-class-${it.item_id}`}
                  >
                    <option value="">大类…</option>
                    <option value="equity">权益</option>
                    <option value="bond">债券</option>
                    <option value="cash">现金/其他</option>
                  </select>
                  <select
                    value={fix.region}
                    onChange={(e) =>
                      setFixes({ ...fixes, [it.item_id]: { ...fix, region: e.target.value } })
                    }
                    className="rounded-md border border-line bg-surface px-2 py-1 text-xs"
                    aria-label="区域"
                    data-testid={`fix-region-${it.item_id}`}
                  >
                    <option value="">区域…</option>
                    <option value="domestic">国内</option>
                    <option value="foreign">国外</option>
                  </select>
                </li>
              );
            })}
          </ul>
          {fixMutation.isError && (
            <p className="text-sm text-danger" role="alert">
              保存失败：{queryErrorMessage(fixMutation.error)}
            </p>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <p className="text-sm text-ink-muted">
            选择目标计划。集合权重将按初始资金折算为目标金额，进入计划持仓草稿。
          </p>
          {plansQuery.isLoading && <LoadingState label="加载计划…" />}
          {plansQuery.isError && (
            <p className="text-sm text-danger" role="alert">
              加载计划失败：{queryErrorMessage(plansQuery.error)}
            </p>
          )}
          {!plansQuery.isLoading && plans.length === 0 && (
            <p className="text-sm text-ink-muted">暂无计划，请先创建 FIRE 计划。</p>
          )}
          <div className="space-y-2" role="radiogroup" aria-label="选择目标计划">
            {plans.map((plan) => (
              <label
                key={plan.id}
                className="flex cursor-pointer items-center gap-3 rounded-md border border-line px-3 py-2 text-sm hover:bg-surface-muted has-[:checked]:border-brand has-[:checked]:bg-brand/5"
              >
                <input
                  type="radio"
                  name="copy-to-plan"
                  value={plan.id}
                  checked={selectedPlanId === plan.id}
                  onChange={() => setSelectedPlanId(plan.id)}
                />
                <span className="flex-1">
                  <span className="font-medium text-ink">{plan.name}</span>
                  <span className="ml-2 text-xs text-ink-muted">
                    {plan.base_currency} · 估值日 {plan.valuation_date}
                  </span>
                </span>
              </label>
            ))}
          </div>
          {copyError && (
            <p className="text-sm text-danger" role="alert">
              复制失败：{copyError}
            </p>
          )}
        </div>
      )}
    </Dialog>
  );
}
