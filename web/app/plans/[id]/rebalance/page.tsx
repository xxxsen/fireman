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
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { queryErrorMessage } from "@/lib/query-error";

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
    ((targets.isError || rebalance.isError) && (!targets.data || !rebalance.data)) ||
    (activeExecution.isError && active == null)
  ) {
    return (
      <ErrorState
        message="无法加载持仓预览。请确认后端服务可用后重试。"
        onRetry={() => {
          if (targets.isError) void targets.refetch();
          if (rebalance.isError) void rebalance.refetch();
          if (activeExecution.isError) void activeExecution.refetch();
        }}
        backHref={`/plans/${planId}/overview`}
        backLabel="返回总览"
        technicalDetail={queryErrorMessage(
          targets.error ?? rebalance.error ?? activeExecution.error,
        )}
      />
    );
  }

  if (
    targets.isLoading ||
    rebalance.isLoading ||
    activeExecution.isLoading ||
    !targets.data ||
    !rebalance.data
  ) {
    return <LoadingState label="加载持仓预览…" />;
  }

  const dimensionLabel = (row: RebalanceWorkspaceRow) => {
    if (row.level === "asset_class") return assetClassLabel(row.asset_class);
    if (row.level === "region") return regionLabel(row.region ?? "");
    return row.label;
  };

  const dimensionClass = (row: RebalanceWorkspaceRow) => {
    if (row.level === "asset_class") return "font-medium text-ink";
    if (row.level === "region") return "pl-8 text-ink-muted";
    return "pl-14 text-ink";
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
        <span className="text-ink-muted">—</span>
      </InlineTooltip>
    );
  };

  const gapAmountCell = (row: RebalanceWorkspaceRow) => {
    if (row.gap_amount_minor === 0) {
      return <span className="text-ink-muted">—</span>;
    }

    const formatted =
      row.gap_amount_minor >= 0
        ? `待投入 ${formatMoney(row.gap_amount_minor)}`
        : `待减配 ${formatMoney(Math.abs(row.gap_amount_minor))}`;
    const content = (
      <span
        className={`font-medium ${
          row.gap_amount_minor >= 0 ? "text-positive" : "text-danger"
        }`}
      >
        {formatted}
      </span>
    );

    if (row.level === "holding") return content;

    return (
      <InlineTooltip content={formatted}>
        <span className="text-ink-muted" aria-label={formatted}>
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
        <Alert variant="success">资产变更已提交，持仓预览已更新。</Alert>
      )}
      {executionCompleted && (
        <Alert variant="success">调仓执行已完成，持仓已同步更新。</Alert>
      )}

      {!targets.data.weight_checks.passed && (
        <Alert variant="danger">
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
        </Alert>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-ink">持仓预览</h1>
          <p className="mt-1 text-sm text-ink-muted">
            对比当前持仓与目标结构；本页仅展示差异，不直接编辑持仓。
          </p>
          {executionInProgress && (
            <p className="mt-2 text-sm text-warning" data-testid="execution-blocking-hint">
              当前有进行中的调仓执行。请先完成或放弃调仓，再进行资产变更。
            </p>
          )}
          {executionInProgress && active && (
            <p className="mt-1 text-sm text-ink-muted">
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
                className="inline-flex min-h-11 cursor-not-allowed items-center rounded-md border border-line bg-surface-muted px-4 text-sm font-medium text-ink-muted"
                data-testid="asset-refresh-primary-disabled"
                aria-disabled="true"
              >
                资产变更
              </span>
            ) : (
              <Button
                href={`/plans/${planId}/asset-refresh`}
                variant="secondary"
                data-testid="asset-refresh-primary"
              >
                资产变更
              </Button>
            )}
            {executionInProgress ? (
              <Button
                href={executionHref}
                data-testid="continue-rebalance-execution"
              >
                继续调仓执行
              </Button>
            ) : (
              <Button
                data-testid="start-rebalance-execution"
                disabled={createExecution.isPending}
                onClick={() => createExecution.mutate()}
              >
                调仓执行
              </Button>
            )}
          </div>
        )}
      </div>

      {createExecution.error && (
        <Alert variant="danger">{queryErrorMessage(createExecution.error, "创建调仓执行失败")}</Alert>
      )}

      {!hasEnabledHoldings ? (
        <section className="rounded-lg border border-dashed border-line p-8 text-center">
          <h2 className="font-medium text-ink">尚未录入持仓</h2>
          <p className="mt-2 text-sm text-ink-muted">请先通过资产变更录入当前真实持仓。</p>
          <Button
            href={`/plans/${planId}/asset-refresh`}
            className="mt-4"
            data-testid="asset-refresh-primary"
          >
            资产变更
          </Button>
        </section>
      ) : (
        <section>
          <h2 className="font-medium text-ink">结构偏差汇总</h2>
          <div className="mt-3 overflow-x-auto rounded-lg border border-line">
            <table className="min-w-full text-sm">
              <thead className="bg-surface-muted text-ink-muted">
                <tr>
                  <th className="px-3 py-2 text-left font-medium">维度</th>
                  <th className="px-3 py-2 text-right font-medium">目标占比</th>
                  <th className="px-3 py-2 text-right font-medium">现状占比</th>
                  <th className="px-3 py-2 text-right font-medium">目标金额</th>
                  <th className="px-3 py-2 text-right font-medium">当前金额</th>
                  <th className="px-3 py-2 text-right font-medium">待投入 / 偏差</th>
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
                      className={`border-t border-line ${
                        row.level === "holding" ? "bg-surface hover:bg-surface-muted" : "bg-surface-muted/60"
                      }`}
                    >
                      <td className={`px-3 py-2 ${dimensionClass(row)}`}>
                        {row.level === "holding" && row.instrument_id ? (
                          <Link
                            href={`/assets/${row.instrument_id}`}
                            className="font-medium text-brand underline-offset-2 hover:underline"
                          >
                            {dimensionLabel(row)}
                          </Link>
                        ) : (
                          dimensionLabel(row)
                        )}
                        {row.level === "holding" && row.instrument_code && (
                          <span className="block text-xs font-normal text-ink-muted">
                            {row.instrument_code}
                          </span>
                        )}
                        {execHint && (
                          <span
                            className="mt-1 block text-xs text-info"
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
