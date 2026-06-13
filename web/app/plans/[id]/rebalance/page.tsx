"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSyncExternalStore, useEffect, useMemo, useState } from "react";
import { CurrentWeightCell, TargetWeightCell } from "@/components/plans/TargetWeightCell";
import { InlineTooltip } from "@/components/ui/InlineTooltip";
import { MetricHelp } from "@/components/ui/MetricHelp";
import type { RebalanceWorkspaceRow } from "@/lib/allocation-summary";
import { buildRebalanceWorkspaceRows } from "@/lib/allocation-summary";
import { downloadCsv } from "@/lib/csv";
import { getRebalance, getTargets } from "@/lib/api/holdings";
import {
  createRebalanceDraft,
  getActiveRebalanceDraft,
} from "@/lib/api/rebalance-drafts";
import {
  createPortfolioSnapshot,
  getParameters,
  getPlan,
  syncPlanTotalAssets,
} from "@/lib/api/plans";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
  rebalanceActionLabel,
  regionLabel,
} from "@/lib/format";
import { isSignificantScaleGap } from "@/lib/scale-gap";
import { ApiError } from "@/lib/api/client";
const SCALE_DISMISS_KEY = "fireman_scale_gap_dismissed";

class SyncScaleCancelled extends Error {
  constructor() {
    super("已取消");
    this.name = "SyncScaleCancelled";
  }
}

export default function RebalancePage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [actionFilter, setActionFilter] = useState("all");
  const [scaleDismissOverride, setScaleDismissOverride] = useState<boolean | null>(null);
  const [syncSuccessMsg, setSyncSuccessMsg] = useState<string | null>(null);

  const scaleDismissKey = `${SCALE_DISMISS_KEY}:${planId}`;
  const storedScaleDismissed = useSyncExternalStore(
    () => () => {},
    () =>
      typeof window !== "undefined" &&
      sessionStorage.getItem(scaleDismissKey) === "1",
    () => false,
  );
  const scaleDismissed = scaleDismissOverride ?? storedScaleDismissed;

  useEffect(() => {
    if (!syncSuccessMsg) return;
    const timer = window.setTimeout(() => setSyncSuccessMsg(null), 3000);
    return () => window.clearTimeout(timer);
  }, [syncSuccessMsg]);

  const plan = useQuery({
    queryKey: ["plan", planId],
    queryFn: () => getPlan(planId),
  });
  const rebalance = useQuery({
    queryKey: ["rebalance", planId],
    queryFn: () => getRebalance(planId, "full"),
  });
  const activeDraft = useQuery({
    queryKey: ["rebalance-draft-active", planId],
    queryFn: () => getActiveRebalanceDraft(planId),
  });
  const parameters = useQuery({
    queryKey: ["parameters", planId],
    queryFn: () => getParameters(planId),
  });

  const summary = rebalance.data?.summary;
  const threshold = parameters.data?.parameters.rebalance_threshold ?? 0.03;
  const maxStructuralGap = useMemo(() => {
    if (!rebalance.data) return 0;
    return rebalance.data.lines.reduce((max, line) => {
      if (!line.enabled) return max;
      return Math.max(max, Math.abs(line.structural_gap_weight));
    }, 0);
  }, [rebalance.data]);
  const scaleGap = summary?.scale_gap_minor ?? 0;
  const showScaleBar = !scaleDismissed && isSignificantScaleGap(scaleGap);
  const hasEnabledHoldings = (summary?.holdings_total_minor ?? 0) > 0;
  const structuralActionable = summary?.structural_actionable_count ?? summary?.actionable_count ?? 0;
  const canCreatePlan =
    hasEnabledHoldings && structuralActionable > 0 && maxStructuralGap > threshold;
  const draftDetail = activeDraft.data;
  const draftStagedCount = draftDetail
    ? draftDetail.lines.filter(
        (line) => line.planned_current_minor !== line.baseline_current_minor,
      ).length
    : 0;

  const createDraft = useMutation({
    mutationFn: (forceNew?: boolean) =>
      createRebalanceDraft(planId, forceNew ? { force_new: true } : {}),
    onSuccess: (data) => {
      void queryClient.invalidateQueries({ queryKey: ["rebalance-draft-active", planId] });
      router.push(`/plans/${planId}/rebalance/plan/${data.draft.id}`);
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "active_draft_exists") {
        const draftId =
          typeof err.details?.draft_id === "string" ? err.details.draft_id : draftDetail?.draft.id;
        const createdAtMs =
          typeof err.details?.created_at === "number"
            ? err.details.created_at
            : draftDetail?.draft.created_at;
        if (!draftId) return;
        const created = createdAtMs
          ? new Date(createdAtMs).toLocaleDateString("zh-CN")
          : "未知日期";
        const choice = window.confirm(
          `您有一个进行中的调仓计划（创建于 ${created}）。\n\n确定放弃并新建？取消则继续编辑现有计划。`,
        );
        if (choice) createDraft.mutate(true);
        else router.push(`/plans/${planId}/rebalance/plan/${draftId}`);
      }
    },
  });

  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });

  const workspaceRows = useMemo(() => {
    if (!targets.data || !rebalance.data) return [];
    return buildRebalanceWorkspaceRows(
      targets.data,
      rebalance.data.lines,
      actionFilter,
    );
  }, [targets.data, rebalance.data, actionFilter]);

  const syncScale = useMutation({
    mutationFn: async () => {
      if (!plan.data || !parameters.data || !summary) {
        throw new Error("数据尚未加载");
      }
      const nextTotal = summary.holdings_total_minor;
      const from = formatMoney(summary.configured_total_minor);
      const to = formatMoney(nextTotal);
      const confirmed = window.confirm(
        `将计划基准规模从 ${from} 同步为 ${to}？\n\n同步后 FIRE 模拟将基于新基准；若持仓未反映全部资产，请勿下调。`,
      );
      if (!confirmed) throw new SyncScaleCancelled();
      return syncPlanTotalAssets(planId, {
        config_version: plan.data.config_version,
        parameters: {
          ...parameters.data.parameters,
          total_assets_minor: nextTotal,
        },
      });
    },
    onSuccess: () => {
      for (const key of ["plan", "parameters", "targets", "rebalance", "dashboard", "holdings"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
      setSyncSuccessMsg("计划基准规模已同步至持仓合计");
    },
  });

  const snapshot = useMutation({
    mutationFn: () => {
      if (!plan.data) throw new Error("计划尚未加载");
      return createPortfolioSnapshot(planId, {
        config_version: plan.data.config_version,
        note: "调仓后记录新持仓",
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["holdings", planId] });
      void queryClient.invalidateQueries({ queryKey: ["plan", planId] });
    },
  });

  const dismissScaleBar = () => {
    sessionStorage.setItem(scaleDismissKey, "1");
    setScaleDismissOverride(true);
  };

  if (targets.isLoading || rebalance.isLoading || !targets.data || !rebalance.data) {
    return <p className="text-slate-600">加载调仓工作台…</p>;
  }

  const exportCsv = () => {
    downloadCsv(
      "rebalance-structural.csv",
      ["标的", "结构还差金额", "结构建议"],
      rebalance.data.lines
        .filter((line) => line.enabled)
        .filter((line) => actionFilter === "all" || line.action === actionFilter)
        .map((line) => [
          line.instrument_name ?? line.instrument_code ?? line.instrument_id,
          (line.structural_gap_amount_minor / 100).toFixed(2),
          rebalanceActionLabel(line.action),
        ]),
    );
  };

  const dimensionLabel = (row: (typeof workspaceRows)[number]) => {
    if (row.level === "asset_class") return assetClassLabel(row.asset_class);
    if (row.level === "region") return regionLabel(row.region ?? "");
    return row.label;
  };

  const dimensionClass = (row: RebalanceWorkspaceRow) => {
    if (row.level === "asset_class") return "font-medium text-slate-900";
    if (row.level === "region") return "pl-8 text-slate-700";
    return "pl-14 text-slate-800";
  };

  const summaryAmountPlaceholder = (
    row: RebalanceWorkspaceRow,
    kind: "target" | "current",
  ) => {
    const amount =
      kind === "target" ? row.target_amount_minor : row.current_amount_minor;
    const label = kind === "target" ? "合计目标金额" : "合计当前金额";
    return (
      <InlineTooltip content={`${label}：${formatMoney(amount)}`}>
        <span className="text-slate-400">—</span>
      </InlineTooltip>
    );
  };

  const planScaleRows = rebalance.data.lines.filter((line) => line.enabled);

  const syncScaleFailed =
    syncScale.isError && !(syncScale.error instanceof SyncScaleCancelled);

  return (
    <div className="space-y-6">
      {syncSuccessMsg && (
        <div
          role="status"
          className="rounded-lg border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800"
        >
          {syncSuccessMsg}
        </div>
      )}
      {showScaleBar && summary && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
          {scaleGap > 0 ? (
            <p>
              当前持仓 <strong>{formatMoney(summary.holdings_total_minor)}</strong>
              ，高于计划基准规模{" "}
              <strong>{formatMoney(summary.configured_total_minor)}</strong>
              （<strong>规模超出 {formatMoney(scaleGap)}</strong>
              ）。调仓建议仅看结构偏差；若需让计划基准与市值一致，可同步更新。
              <MetricHelp termKey="scale_gap_over" />
            </p>
          ) : (
            <p>
              计划基准 <strong>{formatMoney(summary.configured_total_minor)}</strong>
              ，高于持仓合计{" "}
              <strong>{formatMoney(summary.holdings_total_minor)}</strong>
              （<strong>规模缺口 {formatMoney(Math.abs(scaleGap))}</strong>
              ）。调仓建议仅看结构偏差。若持仓已反映真实市值（如市场缩水），可同步下调计划基准；若仍有未录入资产，请补录持仓或将差额计入现金。
              <MetricHelp termKey="scale_gap_under" />
            </p>
          )}
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <Link
              href={`/plans/${planId}/asset-refresh?reason=scale`}
              className="min-h-11 rounded-md bg-amber-800 px-4 text-sm font-medium text-white"
            >
              更新账户资产
            </Link>
            <button
              type="button"
              className="min-h-11 rounded-md bg-amber-800 px-4 text-sm font-medium text-white disabled:opacity-50"
              disabled={syncScale.isPending}
              onClick={() => syncScale.mutate()}
            >
              同步计划基准至持仓合计
            </button>
            {scaleGap < 0 && (
              <Link
                href={`/plans/${planId}/holdings`}
                className="text-sm font-medium underline"
              >
                去补录持仓
              </Link>
            )}
            <button
              type="button"
              className="text-sm text-amber-800 underline"
              onClick={dismissScaleBar}
            >
              暂不处理
            </button>
          </div>
          {syncScaleFailed && (
            <p className="mt-2 text-red-700">
              {syncScale.error instanceof Error ? syncScale.error.message : "同步失败"}
            </p>
          )}
        </div>
      )}

      {!targets.data.weight_checks.passed && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
          {targets.data.weight_checks.checks
            .filter((check) => !check.passed)
            .map((check) => check.message)
            .join("；")}
          <Link
            href={`/plans/${planId}/settings?section=scenarios`}
            className="ml-2 font-medium underline"
          >
            检查场景与权重
          </Link>
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">调仓工作台</h1>
          <p className="mt-1 text-sm text-slate-600">
            只是更新账户市值？→{" "}
            <Link href={`/plans/${planId}/asset-refresh`} className="underline">
              资产变更
            </Link>
            ；要按建议调整持仓？→ 调仓计划
            <MetricHelp termKey="asset_refresh_vs_rebalance_plan" />
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {draftDetail ? (
            <Link
              href={`/plans/${planId}/rebalance/plan/${draftDetail.draft.id}`}
              className="relative inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm font-medium text-white"
            >
              继续调仓计划
              {draftStagedCount > 0 && (
                <span className="ml-2 rounded-full bg-amber-400 px-2 py-0.5 text-xs text-slate-900">
                  {draftStagedCount}
                </span>
              )}
            </Link>
          ) : canCreatePlan ? (
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm font-medium text-white disabled:opacity-50"
              disabled={createDraft.isPending}
              onClick={() => createDraft.mutate(undefined)}
            >
              创建调仓计划
            </button>
          ) : null}
          <label className="text-sm">
            动作筛选
            <select
              className="ml-2 min-h-11 rounded-md border px-3"
              value={actionFilter}
              onChange={(event) => setActionFilter(event.target.value)}
            >
              <option value="all">全部</option>
              <option value="increase">增配</option>
              <option value="decrease">减配</option>
              <option value="hold">不动</option>
            </select>
          </label>
          <button type="button" className="text-sm font-medium underline" onClick={exportCsv}>
            导出 CSV
          </button>
        </div>
      </div>

      {!hasEnabledHoldings ? (
        <section className="rounded-lg border border-dashed border-slate-300 p-8 text-center">
          <h2 className="font-medium">尚未录入持仓</h2>
          <p className="mt-2 text-sm text-slate-600">请先录入持仓后再查看结构偏差与调仓建议。</p>
          <Link
            href={`/plans/${planId}/holdings`}
            className="mt-4 inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm text-white"
          >
            去持仓管理
          </Link>
        </section>
      ) : (
        <section>
          <h2 className="flex items-center font-medium">
            结构偏差汇总
            <MetricHelp termKey="structural_gap_row" />
          </h2>
          <div className="mt-3 overflow-x-auto rounded-lg border border-slate-200">
            <table className="min-w-full text-sm">
              <thead className="bg-slate-50">
                <tr>
                  <th className="px-3 py-2 text-left">维度</th>
                  <th className="px-3 py-2 text-right">
                    <span className="inline-flex items-center justify-end">
                      目标占比
                      <MetricHelp termKey="target_weight_portfolio" />
                    </span>
                  </th>
                  <th className="px-3 py-2 text-right">
                    <span className="inline-flex items-center justify-end">
                      现状占比
                      <MetricHelp termKey="current_weight_portfolio" />
                    </span>
                  </th>
                  <th className="px-3 py-2 text-right">目标金额</th>
                  <th className="px-3 py-2 text-right">当前金额</th>
                  <th className="px-3 py-2 text-right">结构还差金额</th>
                  <th className="px-3 py-2 text-right">结构还差占比</th>
                  <th className="px-3 py-2 text-left">建议</th>
                  <th className="px-3 py-2" />
                </tr>
              </thead>
              <tbody>
                {workspaceRows.map((row) => (
                  <tr
                    key={row.key}
                    className={`border-t ${
                      row.level === "holding" ? "bg-white hover:bg-slate-50" : "bg-slate-50/60"
                    }`}
                  >
                    <td className={`px-3 py-2 ${dimensionClass(row)}`}>
                      {dimensionLabel(row)}
                      {row.level === "holding" && row.instrument_code && (
                        <span className="block text-xs font-normal text-slate-500">
                          {row.instrument_code}
                        </span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right">
                      <TargetWeightCell row={row} />
                    </td>
                    <td className="px-3 py-2 text-right">
                      <CurrentWeightCell row={row} />
                    </td>
                    <td className="px-3 py-2 text-right">
                      {row.level === "holding"
                        ? formatMoney(row.target_amount_minor)
                        : summaryAmountPlaceholder(row, "target")}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {row.level === "holding"
                        ? formatMoney(row.current_amount_minor)
                        : summaryAmountPlaceholder(row, "current")}
                    </td>
                    <td
                      className={`px-3 py-2 text-right font-medium ${
                        row.gap_amount_minor >= 0 ? "text-emerald-700" : "text-red-700"
                      }`}
                    >
                      {row.level === "holding"
                        ? formatMoney(row.gap_amount_minor)
                        : (
                          <>
                            {row.gap_amount_minor >= 0 ? "待投入 " : "待减配 "}
                            {formatMoney(Math.abs(row.gap_amount_minor))}
                          </>
                        )}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {formatPercent(row.gap_weight)}
                    </td>
                    <td className="px-3 py-2">
                      {row.level === "holding" && row.action ? (
                        <>
                          {rebalanceActionLabel(row.action)}
                          {row.suggested_trade_minor !== 0 && (
                            <span className="block text-xs text-slate-500">
                              {formatMoney(Math.abs(row.suggested_trade_minor ?? 0))}
                            </span>
                          )}
                        </>
                      ) : (
                        <span className="text-slate-400">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {row.level === "holding" && row.holding_id ? (
                        <div className="flex flex-col items-end gap-1">
                          {draftDetail ? (
                            <Link
                              href={`/plans/${planId}/rebalance/plan/${draftDetail.draft.id}`}
                              className="whitespace-nowrap text-sm font-medium underline"
                            >
                              调仓计划
                            </Link>
                          ) : canCreatePlan ? (
                            <button
                              type="button"
                              className="whitespace-nowrap text-sm font-medium underline"
                              onClick={() => createDraft.mutate(undefined)}
                            >
                              创建调仓计划
                            </button>
                          ) : null}
                          <Link
                            href={`/plans/${planId}/holdings?highlight=${row.holding_id}`}
                            className="whitespace-nowrap text-xs text-slate-500 underline"
                          >
                            在持仓中编辑
                          </Link>
                        </div>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {structuralActionable === 0 && !isSignificantScaleGap(scaleGap) && (
            <p className="mt-3 text-sm text-emerald-700">
              结构与规模均与目标一致。
            </p>
          )}
          {structuralActionable === 0 && isSignificantScaleGap(scaleGap) && (
            <p className="mt-3 text-sm text-amber-800">
              结构无调整建议；请处理规模偏差。
            </p>
          )}
        </section>
      )}

      {hasEnabledHoldings && (
        <details className="rounded-lg border border-slate-200 p-4">
          <summary className="cursor-pointer font-medium text-slate-800">
            按计划规模对齐（高级）
            <span className="ml-2 text-xs font-normal text-slate-500">
              基于计划总资产 {formatMoney(summary?.configured_total_minor ?? 0)}，可能与增值后市值不一致
            </span>
          </summary>
          <p className="mt-3 text-sm text-slate-600">
            仅当你有意将总市值对齐到计划快照时参考；日常调仓请以上方结构偏差为准。
            <MetricHelp termKey="plan_scale_gap_row" />
          </p>
          <div className="mt-3 overflow-x-auto">
            <table className="min-w-full text-sm text-slate-500">
              <thead>
                <tr className="text-left">
                  <th className="px-3 py-2">维度</th>
                  <th className="px-3 py-2 text-right">计划目标金额</th>
                  <th className="px-3 py-2 text-right">当前金额</th>
                  <th className="px-3 py-2 text-right">计划规模还差</th>
                  <th className="px-3 py-2 text-left">计划规模建议</th>
                </tr>
              </thead>
              <tbody>
                {planScaleRows.map((line) => (
                  <tr key={line.holding_id} className="border-t">
                    <td className="px-3 py-2">
                      {line.instrument_name ?? line.instrument_code ?? line.instrument_id}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {formatMoney(line.target_amount_minor)}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {formatMoney(line.current_amount_minor)}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {formatMoney(line.plan_gap_amount_minor)}
                    </td>
                    <td className="px-3 py-2">
                      {rebalanceActionLabel(line.plan_scale_action)}
                      {line.plan_scale_suggested_trade_minor !== 0 && (
                        <span className="ml-1">
                          {formatMoney(Math.abs(line.plan_scale_suggested_trade_minor))}
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}

      <div className="rounded-lg border border-slate-200 p-4">
        <p className="text-sm text-slate-600">
          系统只生成建议，不修改当前金额。实际交易后请记录新持仓快照。
        </p>
        <button
          type="button"
          className="mt-3 min-h-11 rounded-md bg-slate-900 px-4 text-sm font-medium text-white disabled:opacity-50"
          onClick={() => snapshot.mutate()}
          disabled={snapshot.isPending}
        >
          记录调仓后快照
        </button>
      </div>
    </div>
  );
}
