"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { CurrentWeightCell, TargetWeightCell } from "@/components/plans/TargetWeightCell";
import { InlineTooltip } from "@/components/ui/InlineTooltip";
import type { RebalanceWorkspaceRow } from "@/lib/allocation-summary";
import { buildRebalanceWorkspaceRows } from "@/lib/allocation-summary";
import { getRebalance, getTargets } from "@/lib/api/holdings";
import {
  createRebalanceExecution,
  getActiveRebalanceExecution,
} from "@/lib/api/rebalance-executions";
import { assetClassLabel, formatMoney, regionLabel } from "@/lib/format";
import { ApiError } from "@/lib/api/client";

function lineStatusHint(status: string, remainingMinor: number): string | null {
  switch (status) {
    case "partial":
      return `执行中 · 剩余 ${formatMoney(Math.abs(remainingMinor))}`;
    case "done":
      return "已完成";
    case "not_started":
      return remainingMinor !== 0 ? `剩余 ${formatMoney(Math.abs(remainingMinor))}` : null;
    default:
      return null;
  }
}

export default function RebalancePage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const searchParams = useSearchParams();
  const assetRefreshed = searchParams.get("asset_refreshed") === "1";
  const executionCompleted = searchParams.get("execution_completed") === "1";

  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });
  const rebalance = useQuery({
    queryKey: ["rebalance", planId],
    queryFn: () => getRebalance(planId, "full"),
  });
  const activeExecution = useQuery({
    queryKey: ["rebalance-execution-active", planId],
    queryFn: () => getActiveRebalanceExecution(planId),
  });

  const createExecution = useMutation({
    mutationFn: () => createRebalanceExecution(planId),
    onSuccess: (detail) => {
      void queryClient.invalidateQueries({ queryKey: ["rebalance-execution-active", planId] });
      router.push(`/plans/${planId}/rebalance/executions/${detail.execution.id}`);
    },
  });

  const summary = rebalance.data?.summary;
  const hasEnabledHoldings = (summary?.holdings_total_minor ?? 0) > 0;
  const active = activeExecution.data;
  const executionInProgress = !!active?.execution;

  const executionLineByInstrument = useMemo(() => {
    const map = new Map<string, { status: string; remaining_delta_minor: number }>();
    for (const line of active?.lines ?? []) {
      map.set(line.instrument_id, {
        status: line.execution_status,
        remaining_delta_minor: line.remaining_delta_minor,
      });
    }
    return map;
  }, [active?.lines]);

  const workspaceRows = useMemo(() => {
    if (!targets.data || !rebalance.data) return [];
    return buildRebalanceWorkspaceRows(targets.data, rebalance.data.lines);
  }, [targets.data, rebalance.data]);

  if (
    targets.isLoading ||
    rebalance.isLoading ||
    activeExecution.isLoading ||
    !targets.data ||
    !rebalance.data
  ) {
    return <p className="text-slate-600">加载持仓预览…</p>;
  }

  const dimensionLabel = (row: RebalanceWorkspaceRow) => {
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

  const gapAmountCell = (row: RebalanceWorkspaceRow) => {
    if (row.gap_amount_minor === 0) {
      return <span className="text-slate-400">—</span>;
    }

    const formatted =
      row.gap_amount_minor >= 0
        ? `待投入 ${formatMoney(row.gap_amount_minor)}`
        : `待减配 ${formatMoney(Math.abs(row.gap_amount_minor))}`;
    const content = (
      <span
        className={`font-medium ${
          row.gap_amount_minor >= 0 ? "text-emerald-700" : "text-red-700"
        }`}
      >
        {formatted}
      </span>
    );

    if (row.level === "holding") return content;

    return (
      <InlineTooltip content={formatted}>
        <span className="text-slate-400" aria-label={formatted}>
          —
        </span>
      </InlineTooltip>
    );
  };

  const executionHref = executionInProgress
    ? `/plans/${planId}/rebalance/executions/${active!.execution.id}`
    : `/plans/${planId}/rebalance/executions`;

  return (
    <div className="space-y-6">
      {assetRefreshed && (
        <div
          role="status"
          className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800"
        >
          资产变更已提交，持仓预览已更新。
        </div>
      )}
      {executionCompleted && (
        <div
          role="status"
          className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800"
        >
          调仓执行已完成，持仓已同步更新。
        </div>
      )}

      {!targets.data.weight_checks.passed && (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
          {targets.data.weight_checks.checks
            .filter((check) => !check.passed)
            .map((check) => check.message)
            .join("；")}
          <Link
            href={`/plans/${planId}/settings?section=plan-targets`}
            className="ml-2 font-medium underline"
          >
            检查计划目标配置
          </Link>
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">持仓预览</h1>
          <p className="mt-1 text-sm text-slate-600">
            对比当前持仓与目标结构；本页仅展示差异，不直接编辑持仓。
          </p>
          {executionInProgress && (
            <p className="mt-2 text-sm text-amber-800" data-testid="execution-blocking-hint">
              当前有进行中的调仓执行。请先完成或放弃调仓，再进行资产变更。
            </p>
          )}
          {executionInProgress && active && (
            <p className="mt-1 text-sm text-slate-600">
              进行中 · 已完成 {active.stats.done_line_count}/{active.stats.line_count} 个资产
              {active.stats.skipped_line_count ? ` · 跳过 ${active.stats.skipped_line_count} 个` : ""} · 现金池{" "}
              {formatMoney(active.execution.cash_pool_minor)}
            </p>
          )}
        </div>
        {hasEnabledHoldings && (
          <div className="flex flex-wrap gap-2">
            {executionInProgress ? (
              <span
                className="inline-flex min-h-11 cursor-not-allowed items-center rounded-md border border-slate-200 bg-slate-100 px-4 text-sm font-medium text-slate-400"
                data-testid="asset-refresh-primary-disabled"
                aria-disabled="true"
              >
                资产变更
              </span>
            ) : (
              <Link
                href={`/plans/${planId}/asset-refresh`}
                className="inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm font-medium"
                data-testid="asset-refresh-primary"
              >
                资产变更
              </Link>
            )}
            {executionInProgress ? (
              <Link
                href={executionHref}
                className="inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm font-medium text-white"
                data-testid="continue-rebalance-execution"
              >
                继续调仓执行
              </Link>
            ) : (
              <button
                type="button"
                className="inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm font-medium text-white disabled:opacity-50"
                data-testid="start-rebalance-execution"
                disabled={createExecution.isPending}
                onClick={() => createExecution.mutate()}
              >
                调仓执行
              </button>
            )}
          </div>
        )}
      </div>

      {createExecution.error && (
        <p className="text-sm text-red-600" role="alert">
          {createExecution.error instanceof ApiError
            ? createExecution.error.message
            : "创建调仓执行失败"}
        </p>
      )}

      {!hasEnabledHoldings ? (
        <section className="rounded-lg border border-dashed border-slate-300 p-8 text-center">
          <h2 className="font-medium">尚未录入持仓</h2>
          <p className="mt-2 text-sm text-slate-600">请先通过资产变更录入当前真实持仓。</p>
          <Link
            href={`/plans/${planId}/asset-refresh`}
            className="mt-4 inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm text-white"
            data-testid="asset-refresh-primary"
          >
            资产变更
          </Link>
        </section>
      ) : (
        <section>
          <h2 className="font-medium">结构偏差汇总</h2>
          <div className="mt-3 overflow-x-auto rounded-lg border border-slate-200">
            <table className="min-w-full text-sm">
              <thead className="bg-slate-50">
                <tr>
                  <th className="px-3 py-2 text-left">维度</th>
                  <th className="px-3 py-2 text-right">目标占比</th>
                  <th className="px-3 py-2 text-right">现状占比</th>
                  <th className="px-3 py-2 text-right">目标金额</th>
                  <th className="px-3 py-2 text-right">当前金额</th>
                  <th className="px-3 py-2 text-right">待投入 / 偏差</th>
                </tr>
              </thead>
              <tbody>
                {workspaceRows.map((row) => {
                  const execLine =
                    row.level === "holding" && row.instrument_id
                      ? executionLineByInstrument.get(row.instrument_id)
                      : undefined;
                  const execHint = execLine
                    ? lineStatusHint(execLine.status, execLine.remaining_delta_minor)
                    : null;
                  return (
                    <tr
                      key={row.key}
                      className={`border-t ${
                        row.level === "holding" ? "bg-white hover:bg-slate-50" : "bg-slate-50/60"
                      }`}
                    >
                      <td className={`px-3 py-2 ${dimensionClass(row)}`}>
                        {row.level === "holding" && row.instrument_id ? (
                          <Link
                            href={`/assets/${row.instrument_id}`}
                            className="font-medium underline-offset-2 hover:underline"
                          >
                            {dimensionLabel(row)}
                          </Link>
                        ) : (
                          dimensionLabel(row)
                        )}
                        {row.level === "holding" && row.instrument_code && (
                          <span className="block text-xs font-normal text-slate-500">
                            {row.instrument_code}
                          </span>
                        )}
                        {execHint && (
                          <span
                            className="mt-1 block text-xs text-sky-700"
                            data-testid="execution-line-hint"
                          >
                            {execHint}
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
                      <td className="px-3 py-2 text-right">{gapAmountCell(row)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  );
}
