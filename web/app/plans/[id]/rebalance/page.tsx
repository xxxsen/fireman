"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { CurrentWeightCell, TargetWeightCell } from "@/components/plans/TargetWeightCell";
import { InlineTooltip } from "@/components/ui/InlineTooltip";
import { MetricHelp } from "@/components/ui/MetricHelp";
import type { RebalanceWorkspaceRow } from "@/lib/allocation-summary";
import { buildRebalanceWorkspaceRows } from "@/lib/allocation-summary";
import { downloadCsv } from "@/lib/csv";
import { getRebalance, getTargets } from "@/lib/api/holdings";
import { createPortfolioSnapshot, getPlan } from "@/lib/api/plans";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
  rebalanceActionLabel,
  regionLabel,
} from "@/lib/format";

export default function RebalancePage() {
  const planId = useParams().id as string;
  const queryClient = useQueryClient();
  const [actionFilter, setActionFilter] = useState("all");

  const plan = useQuery({
    queryKey: ["plan", planId],
    queryFn: () => getPlan(planId),
  });
  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });
  const rebalance = useQuery({
    queryKey: ["rebalance", planId],
    queryFn: () => getRebalance(planId, "full"),
  });

  const workspaceRows = useMemo(() => {
    if (!targets.data || !rebalance.data) return [];
    return buildRebalanceWorkspaceRows(
      targets.data,
      rebalance.data.lines,
      actionFilter,
    );
  }, [targets.data, rebalance.data, actionFilter]);

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

  if (targets.isLoading || rebalance.isLoading || !targets.data || !rebalance.data) {
    return <p className="text-slate-600">加载调仓工作台…</p>;
  }

  const exportCsv = () => {
    downloadCsv(
      "rebalance.csv",
      ["维度", "目标金额", "当前金额", "还差金额", "建议"],
      rebalance.data.lines
        .filter((line) => line.enabled)
        .filter((line) => actionFilter === "all" || line.action === actionFilter)
        .map((line) => [
          line.instrument_name ?? line.instrument_code ?? line.instrument_id,
          (line.target_amount_minor / 100).toFixed(2),
          (line.current_amount_minor / 100).toFixed(2),
          (line.deviation_amount_minor / 100).toFixed(2),
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

  return (
    <div className="space-y-6">
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
        <h1 className="text-xl font-semibold">调仓工作台</h1>
        <div className="flex flex-wrap items-center gap-3">
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

      <section>
        <h2 className="flex items-center font-medium">
          配置缺口汇总
          <MetricHelp termKey="portfolio_gap_row" />
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
                <th className="px-3 py-2 text-right">还差金额</th>
                <th className="px-3 py-2 text-right">还差占比</th>
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
                      <Link
                        href={`/plans/${planId}/holdings?highlight=${row.holding_id}`}
                        className="whitespace-nowrap text-sm underline"
                      >
                        更新持仓
                      </Link>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

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
