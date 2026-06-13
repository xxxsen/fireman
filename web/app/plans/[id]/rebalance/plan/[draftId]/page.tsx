"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { RebalanceFundPoolBar } from "@/components/plans/RebalanceFundPoolBar";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { getHoldings } from "@/lib/api/holdings";
import {
  cancelRebalanceDraft,
  commitRebalanceDraft,
  getRebalanceDraft,
  patchRebalanceDraftLines,
  undoRebalanceDraft,
} from "@/lib/api/rebalance-drafts";
import { getPlan } from "@/lib/api/plans";
import {
  formatMoney,
  formatPercent,
  rebalanceActionLabel,
} from "@/lib/format";
import {
  applyRecommendedOneLine,
  buildReferencePackageItems,
  computeFundPool,
  countStagedChanges,
  findCashSweepHolding,
  formatPackageDeltaLabel,
  hasReferencePackage,
  isFundPoolBalanced,
  recommendedPlannedMinor,
} from "@/lib/rebalance-plan";
import { ApiError } from "@/lib/api/client";
import type { RebalanceDraftEvent } from "@/types/api";

function parseEventSummary(event: RebalanceDraftEvent): string {
  try {
    const payload = JSON.parse(event.payload_json) as { summary?: string };
    if (payload.summary) return payload.summary;
  } catch {
    /* ignore */
  }
  return event.event_type === "undo" ? "撤销上一步" : event.event_type;
}

export default function RebalancePlanPage() {
  const planId = useParams().id as string;
  const draftId = useParams().draftId as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [edits, setEdits] = useState<Record<string, number>>({});
  const [previewOpen, setPreviewOpen] = useState(false);
  const [recordSnapshot, setRecordSnapshot] = useState(false);
  const [sweepToCash, setSweepToCash] = useState(true);
  const [acceptScaleShrink, setAcceptScaleShrink] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);

  const plan = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const holdings = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });
  const draft = useQuery({
    queryKey: ["rebalance-draft", planId, draftId],
    queryFn: () => getRebalanceDraft(planId, draftId),
  });

  const lines = useMemo(() => draft.data?.lines ?? [], [draft.data?.lines]);
  const events = draft.data?.events ?? [];
  const fundPool = useMemo(
    () =>
      computeFundPool(
        lines.map((line) => ({
          baseline_current_minor: line.baseline_current_minor,
          planned_current_minor: edits[line.id] ?? line.planned_current_minor,
        })),
      ),
    [lines, edits],
  );
  const stagedCount = countStagedChanges(
    lines.map((line) => ({
      baseline_current_minor: line.baseline_current_minor,
      planned_current_minor: edits[line.id] ?? line.planned_current_minor,
    })),
  );
  const packageItems = useMemo(
    () => buildReferencePackageItems(lines),
    [lines],
  );
  const cashHolding = useMemo(
    () => findCashSweepHolding(holdings.data?.holdings ?? []),
    [holdings.data?.holdings],
  );
  const needsSweepChoice = !isFundPoolBalanced(fundPool.netMinor) && fundPool.netMinor > 0;

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["rebalance-draft", planId, draftId] });
    void queryClient.invalidateQueries({ queryKey: ["rebalance-draft-active", planId] });
  };

  const stage = useMutation({
    mutationFn: async (lineIds: string[]) => {
      const payload = lineIds.map((lineId) => ({
        line_id: lineId,
        planned_current_minor: edits[lineId] ?? lines.find((l) => l.id === lineId)!.planned_current_minor,
      }));
      return patchRebalanceDraftLines(planId, draftId, { stage: true, lines: payload });
    },
    onSuccess: () => {
      setEdits({});
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "暂存失败"),
  });

  const applyRecommended = useMutation({
    mutationFn: async (lineId: string) => {
      const line = lines.find((l) => l.id === lineId);
      if (!line || line.recommended_package_delta_minor === 0) {
        throw new Error("本行无推荐变动");
      }
      const target = recommendedPlannedMinor(
        line.baseline_current_minor,
        line.recommended_package_delta_minor,
      );
      const label = line.instrument_name ?? line.instrument_code ?? "标的";
      const ok = window.confirm(
        `将 ${label} 计划金额 ${formatMoney(line.planned_current_minor)} → ${formatMoney(target)}（推荐 ${formatPackageDeltaLabel(line.recommended_package_delta_minor)}）？`,
      );
      if (!ok) throw new Error("已取消");
      return patchRebalanceDraftLines(planId, draftId, {
        stage: true,
        lines: [applyRecommendedOneLine(line)],
      });
    },
    onSuccess: (_data, lineId) => {
      const line = lines.find((l) => l.id === lineId);
      const label = line?.instrument_name ?? line?.instrument_code ?? "标的";
      setToast(`已应用 ${label} 的推荐金额`);
      setTimeout(() => setToast(null), 3000);
      setEdits({});
      invalidate();
    },
    onError: (err) => {
      if (err instanceof Error && err.message === "已取消") return;
      setError(err instanceof ApiError ? err.message : "应用推荐金额失败");
    },
  });

  const undo = useMutation({
    mutationFn: () => undoRebalanceDraft(planId, draftId),
    onSuccess: () => {
      setEdits({});
      invalidate();
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "撤销失败"),
  });

  const commit = useMutation({
    mutationFn: () => {
      if (!plan.data || !draft.data) throw new Error("数据尚未加载");
      const imbalanced = !isFundPoolBalanced(fundPool.netMinor);
      if (imbalanced && fundPool.netMinor > 0) {
        if (cashHolding && sweepToCash) {
          /* sweep on commit */
        } else if (!acceptScaleShrink) {
          throw new Error("请选择未分配资金处理方式");
        }
      } else if (imbalanced && fundPool.netMinor < 0) {
        const ok = window.confirm(
          "增配超出减配释放，仍要提交并更新持仓吗？",
        );
        if (!ok) throw new Error("已取消");
      }
      for (const line of lines) {
        const planned = edits[line.id] ?? line.planned_current_minor;
        if (planned < 0) throw new Error("计划金额不能为负");
      }
      return commitRebalanceDraft(planId, draftId, {
        config_version: plan.data.config_version,
        confirm_imbalanced: imbalanced,
        sweep_unallocated_to_cash: Boolean(cashHolding && sweepToCash && fundPool.netMinor > 0),
        accept_scale_shrink: acceptScaleShrink && fundPool.netMinor > 0,
        record_snapshot: recordSnapshot,
      });
    },
    onSuccess: () => {
      for (const key of ["holdings", "targets", "rebalance", "dashboard", "plan"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
      router.push(`/plans/${planId}/rebalance`);
    },
    onError: (err) =>
      setError(err instanceof Error && err.message !== "已取消" ? err.message : null),
  });

  const cancel = useMutation({
    mutationFn: () => cancelRebalanceDraft(planId, draftId),
    onSuccess: () => router.push(`/plans/${planId}/rebalance`),
  });

  if (plan.isLoading || draft.isLoading || !plan.data || !draft.data) {
    return <p className="text-slate-600">加载调仓计划…</p>;
  }

  if (draft.data.draft.status !== "draft") {
    return (
      <div className="space-y-4">
        <p className="text-slate-600">此调仓计划已{draft.data.draft.status === "committed" ? "提交" : "放弃"}。</p>
        <Link href={`/plans/${planId}/rebalance`} className="underline">
          返回持仓预览
        </Link>
      </div>
    );
  }

  const actionableLines = lines.filter(
    (line) => line.frozen_action === "increase" || line.frozen_action === "decrease",
  );

  return (
    <div className="space-y-6 pb-24">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">
            {plan.data.name} · 调仓计划
          </h1>
          <p className="mt-1 text-sm text-slate-600">
            状态：进行中 · 基准持仓合计{" "}
            {formatMoney(draft.data.draft.baseline_holdings_total_minor, plan.data.base_currency)}
            · {actionableLines.length} 个标的待调整
            <MetricHelp termKey="rebalance_plan_draft" />
          </p>
        </div>
        <button
          type="button"
          className="text-sm text-red-700 underline"
          onClick={() => {
            if (window.confirm("确定放弃此调仓计划？正式持仓不会变更。")) {
              cancel.mutate();
            }
          }}
        >
          放弃计划
        </button>
      </div>

      {error && (
        <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
          {error}
        </div>
      )}
      {toast && (
        <div className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800">
          {toast}
        </div>
      )}

      {hasReferencePackage(lines) && (
        <section className="rounded-lg border border-slate-200 bg-slate-50 px-4 py-3 text-sm">
          <p className="font-medium text-slate-800">
            参考调仓方案（结构对齐，含未达阈值的微调）
            <MetricHelp termKey="rebalance_reference_package" />
          </p>
          <p className="mt-1 text-slate-700">{packageItems.join("   ")}</p>
          <p className="mt-2 text-xs text-slate-600">
            行内「不动」表示未超调仓阈值；方案为完整对齐参考，请逐行应用或手工调整。
          </p>
        </section>
      )}

      <RebalanceFundPoolBar
        releasedMinor={fundPool.releasedMinor}
        usedMinor={fundPool.usedMinor}
        netMinor={fundPool.netMinor}
        currency={plan.data.base_currency}
      />

      <section className="overflow-x-auto rounded-lg border border-slate-200">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left text-slate-500">
            <tr>
              <th className="px-3 py-2">标的</th>
              <th className="px-3 py-2 text-right">基准当前</th>
              <th className="px-3 py-2 text-right">
                冻结结构目标
                <MetricHelp termKey="frozen_structural_gap" />
              </th>
              <th className="px-3 py-2 text-right">冻结结构还差</th>
              <th className="px-3 py-2">参考建议</th>
              <th className="px-3 py-2 text-right">
                方案变动
                <MetricHelp termKey="rebalance_reference_package" />
              </th>
              <th className="px-3 py-2 text-right">计划当前金额</th>
              <th className="px-3 py-2 text-right">相对基准变动</th>
              <th className="px-3 py-2">状态</th>
              <th className="px-3 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((line) => {
              const planned = edits[line.id] ?? line.planned_current_minor;
              const delta = planned - line.baseline_current_minor;
              const status =
                planned === line.baseline_current_minor
                  ? "未改"
                  : line.last_saved_at && planned === line.planned_current_minor
                    ? "已暂存"
                    : "编辑中";
              const hasPackageDelta = line.recommended_package_delta_minor !== 0;
              return (
                <tr key={line.id} className="border-t">
                  <td className="px-3 py-2">
                    <span className="font-medium">
                      {line.instrument_name ?? line.instrument_code}
                    </span>
                    <span className="block text-xs text-slate-500">{line.instrument_code}</span>
                  </td>
                  <td className="px-3 py-2 text-right">
                    {formatMoney(line.baseline_current_minor, plan.data.base_currency)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {formatMoney(line.frozen_target_minor, plan.data.base_currency)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    {formatMoney(line.frozen_gap_minor, plan.data.base_currency)}
                    <span className="block text-xs text-slate-500">
                      {formatPercent(line.frozen_gap_weight)}
                    </span>
                  </td>
                  <td className="px-3 py-2">
                    {rebalanceActionLabel(line.frozen_action)}
                    {line.frozen_suggested_trade_minor !== 0 && (
                      <span className="block text-xs text-slate-500">
                        {formatMoney(Math.abs(line.frozen_suggested_trade_minor), plan.data.base_currency)}
                      </span>
                    )}
                  </td>
                  <td
                    className={`px-3 py-2 text-right font-medium ${
                      line.recommended_package_delta_minor > 0
                        ? "text-emerald-700"
                        : line.recommended_package_delta_minor < 0
                          ? "text-red-700"
                          : "text-slate-500"
                    }`}
                  >
                    {formatPackageDeltaLabel(line.recommended_package_delta_minor)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <MoneyInput
                      valueMinor={planned}
                      onChange={(value) =>
                        setEdits((prev) => ({ ...prev, [line.id]: value }))
                      }
                    />
                  </td>
                  <td
                    className={`px-3 py-2 text-right font-medium ${
                      delta >= 0 ? "text-emerald-700" : "text-red-700"
                    }`}
                  >
                    {delta >= 0 ? "+" : ""}
                    {formatMoney(delta, plan.data.base_currency)}
                  </td>
                  <td className="px-3 py-2 text-xs text-slate-600">{status}</td>
                  <td className="px-3 py-2">
                    {hasPackageDelta ? (
                      <span className="inline-flex items-center gap-1">
                        <button
                          type="button"
                          className="text-xs underline disabled:opacity-50"
                          disabled={applyRecommended.isPending || Object.keys(edits).length > 0}
                          title={Object.keys(edits).length > 0 ? "请先暂存或放弃未保存编辑" : undefined}
                          onClick={() => applyRecommended.mutate(line.id)}
                        >
                          应用推荐金额
                        </button>
                        <MetricHelp termKey="apply_recommended_one_line" />
                      </span>
                    ) : (
                      <span className="text-xs text-slate-400">—</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="font-medium">变更时间线</h2>
        {events.length === 0 ? (
          <p className="mt-2 text-sm text-slate-600">暂无暂存记录。</p>
        ) : (
          <ol className="mt-2 space-y-2 text-sm">
            {events
              .filter((e) => e.event_type === "stage" || e.event_type === "undo")
              .map((event) => (
                <li key={event.id} className="flex justify-between gap-2">
                  <span>
                    {event.event_type === "undo" ? "↩ " : "• "}
                    {parseEventSummary(event)}
                  </span>
                </li>
              ))}
          </ol>
        )}
        <button
          type="button"
          className="mt-3 text-sm underline disabled:opacity-50"
          disabled={undo.isPending || !events.some((e) => e.event_type === "stage")}
          onClick={() => undo.mutate()}
        >
          撤销上一步
        </button>
      </section>

      <div className="fixed inset-x-0 bottom-0 border-t bg-white/95 px-4 py-3">
        <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-3">
          <button
            type="button"
            className="min-h-11 rounded-md border px-4 text-sm disabled:opacity-50"
            disabled={stage.isPending || Object.keys(edits).length === 0}
            onClick={() => stage.mutate(Object.keys(edits))}
          >
            暂存本步变更
          </button>
          <button
            type="button"
            className="min-h-11 rounded-md border px-4 text-sm"
            onClick={() => {
              setSweepToCash(Boolean(cashHolding));
              setAcceptScaleShrink(false);
              setPreviewOpen(true);
            }}
          >
            预览最终持仓
          </button>
          <button
            type="button"
            className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
            disabled={commit.isPending || stagedCount === 0}
            onClick={() => {
              if (Object.keys(edits).length > 0) {
                setError("请先暂存未保存的编辑");
                return;
              }
              setSweepToCash(Boolean(cashHolding));
              setAcceptScaleShrink(false);
              setPreviewOpen(true);
            }}
          >
            完成并更新持仓
          </button>
          {stagedCount > 0 && (
            <span className="text-xs text-slate-500">{stagedCount} 项待提交变更</span>
          )}
        </div>
      </div>

      {previewOpen && (
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/30 p-4 sm:items-center">
          <div className="max-h-[80vh] w-full max-w-lg overflow-y-auto rounded-lg bg-white p-4 shadow-xl">
            <h3 className="font-medium">预览最终持仓</h3>
            <ul className="mt-3 divide-y text-sm">
              {lines.map((line) => {
                const planned = edits[line.id] ?? line.planned_current_minor;
                const delta = planned - line.baseline_current_minor;
                if (delta === 0) return null;
                return (
                  <li key={line.id} className="flex justify-between py-2">
                    <span>{line.instrument_name ?? line.instrument_code}</span>
                    <span>
                      {formatMoney(line.baseline_current_minor)} → {formatMoney(planned)}
                    </span>
                  </li>
                );
              })}
              {cashHolding && sweepToCash && !acceptScaleShrink && fundPool.netMinor > 0 && (
                <li key="cash-sweep" className="flex justify-between py-2">
                  <span>{cashHolding.instrument_name ?? "现金"}</span>
                  <span>
                    {formatMoney(cashHolding.current_amount_minor)} →{" "}
                    {formatMoney(cashHolding.current_amount_minor + fundPool.netMinor)}
                  </span>
                </li>
              )}
            </ul>

            {needsSweepChoice && (
              <div className="mt-4 space-y-3 rounded-md border border-sky-200 bg-sky-50 p-3 text-sm">
                <p>
                  尚有 {formatMoney(fundPool.netMinor, plan.data.base_currency)} 未在标的间分配。
                </p>
                {cashHolding ? (
                  <label className="flex items-start gap-2">
                    <input
                      type="radio"
                      name="sweep_choice"
                      checked={sweepToCash && !acceptScaleShrink}
                      onChange={() => {
                        setSweepToCash(true);
                        setAcceptScaleShrink(false);
                      }}
                    />
                    <span>
                      未分配资金将计入「{cashHolding.instrument_name ?? "现金"}」持仓（
                      {formatMoney(cashHolding.current_amount_minor)} →{" "}
                      {formatMoney(cashHolding.current_amount_minor + fundPool.netMinor)}）
                      <MetricHelp termKey="unallocated_sweep_to_cash" />
                    </span>
                  </label>
                ) : (
                  <p className="text-amber-800">
                    计划中尚无现金持仓。请先到{" "}
                    <Link href={`/plans/${planId}/rebalance`} className="underline">
                      持仓预览
                    </Link>{" "}
                    通过资产变更添加 CNY 现金，或选择接受组合规模下降。
                  </p>
                )}
                <label className="flex items-start gap-2">
                  <input
                    type="radio"
                    name="sweep_choice"
                    checked={acceptScaleShrink}
                    onChange={() => {
                      setSweepToCash(false);
                      setAcceptScaleShrink(true);
                    }}
                  />
                  <span>
                    接受组合规模下降（不增加现金，总市值减少{" "}
                    {formatMoney(fundPool.netMinor, plan.data.base_currency)}）
                  </span>
                </label>
              </div>
            )}

            <label className="mt-4 flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={recordSnapshot}
                onChange={(e) => setRecordSnapshot(e.target.checked)}
              />
              记录调仓后快照
            </label>
            <div className="mt-4 flex gap-3">
              <button
                type="button"
                className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
                disabled={
                  commit.isPending ||
                  (needsSweepChoice && !cashHolding && !acceptScaleShrink) ||
                  (needsSweepChoice && Boolean(cashHolding) && !sweepToCash && !acceptScaleShrink)
                }
                onClick={() => commit.mutate()}
              >
                确认提交
              </button>
              <button
                type="button"
                className="min-h-11 rounded-md border px-4 text-sm"
                onClick={() => setPreviewOpen(false)}
              >
                返回分配
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
