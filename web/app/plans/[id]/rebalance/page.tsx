"use client";

import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { CurrentWeightCell, TargetWeightCell } from "@/components/plans/TargetWeightCell";
import { InlineTooltip } from "@/components/ui/InlineTooltip";
import type { RebalanceWorkspaceRow } from "@/lib/allocation-summary";
import { buildRebalanceWorkspaceRows } from "@/lib/allocation-summary";
import { getRebalance, getTargets } from "@/lib/api/holdings";
import { assetClassLabel, formatMoney, regionLabel } from "@/lib/format";

export default function RebalancePage() {
  const planId = useParams().id as string;
  const searchParams = useSearchParams();
  const assetRefreshed = searchParams.get("asset_refreshed") === "1";

  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });
  const rebalance = useQuery({
    queryKey: ["rebalance", planId],
    queryFn: () => getRebalance(planId, "full"),
  });

  const summary = rebalance.data?.summary;
  const hasEnabledHoldings = (summary?.holdings_total_minor ?? 0) > 0;

  const workspaceRows = useMemo(() => {
    if (!targets.data || !rebalance.data) return [];
    return buildRebalanceWorkspaceRows(targets.data, rebalance.data.lines);
  }, [targets.data, rebalance.data]);

  if (targets.isLoading || rebalance.isLoading || !targets.data || !rebalance.data) {
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
        </div>
        {hasEnabledHoldings && (
          <Link
            href={`/plans/${planId}/asset-refresh`}
            className="inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm font-medium text-white"
            data-testid="asset-refresh-primary"
          >
            资产变更
          </Link>
        )}
      </div>

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
                {workspaceRows.map((row) => (
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
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  );
}
