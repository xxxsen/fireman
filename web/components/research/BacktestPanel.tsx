"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import {
  createBacktest,
  type ResearchCollectionDetail,
  type ResearchReadiness,
  type ResearchRunView,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { formatDateTimeFromMs, formatNullablePercent, formatPercent } from "@/lib/format";
import { Button } from "@/components/ui/Button";
import { runStatusBadge } from "@/components/research/runStatus";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";

/**
 * The run button's disabled explanation, derived from readiness in priority
 * order (td/099 §4.4): weight -> missing history -> active sync -> FX ->
 * window -> ready.
 */
export function runDisabledReason(readiness: ResearchReadiness | undefined): string | null {
  if (!readiness) return "正在检查数据就绪状态…";
  if (readiness.ready) return null;
  const reasons = new Set(readiness.blocking_reasons.map((r) => r.reason));
  if (
    reasons.has("no_enabled_assets") ||
    reasons.has("weight_sum_invalid") ||
    reasons.has("negative_weight") ||
    reasons.has("weight_exceeds_100")
  ) {
    const gap = 1 - readiness.weight_sum;
    if (reasons.has("no_enabled_assets")) return "集合没有启用的资产";
    return `权重合计 ${formatPercent(readiness.weight_sum)}，差 ${formatPercent(gap)} 才能运行`;
  }
  if (reasons.has("history_missing") || reasons.has("history_sync_failed")) {
    return "存在缺历史资产，请先「更新组合数据」";
  }
  if (reasons.has("history_syncing") || reasons.has("fx_syncing")) {
    return "数据同步任务进行中，完成后可运行";
  }
  if (reasons.has("fx_missing") || reasons.has("fx_gap_exceeded")) {
    return "汇率数据缺失或存在缺口，请同步汇率";
  }
  if (reasons.has("common_window_empty") || reasons.has("common_window_too_short")) {
    const blockers = readiness.assets
      .filter((a) => a.limits_common_start || a.limits_common_end)
      .map((a) => a.name);
    return (
      "共同历史区间不足" + (blockers.length > 0 ? `（受限于 ${blockers.join("、")}）` : "")
    );
  }
  return readiness.blocking_reasons[0]?.message ?? "数据未就绪";
}

export interface BacktestPanelProps {
  detail: ResearchCollectionDetail;
  readiness?: ResearchReadiness;
  latestRuns: ResearchRunView[];
}

export function BacktestPanel({ detail, readiness, latestRuns }: BacktestPanelProps) {
  const router = useRouter();
  const [reusedNotice, setReusedNotice] = useState(false);

  const disabledReason = useMemo(() => runDisabledReason(readiness), [readiness]);

  const runMutation = useMutation({
    mutationFn: () => createBacktest(detail.id),
    onSuccess: (result) => {
      setReusedNotice(result.reused);
      router.push(`/research/collections/${detail.id}/runs/${result.run.id}`);
    },
  });

  const latest = latestRuns[0];

  return (
    <section className="rounded-lg border border-line bg-surface p-4" data-testid="backtest-panel">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-base font-semibold text-ink">回测</h2>
        <Link
          href={`/research/collections/${detail.id}/runs`}
          className="text-sm text-brand underline-offset-2 hover:underline"
        >
          全部运行记录
        </Link>
      </div>

      <dl className="mb-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
        <div>
          <dt className="text-ink-muted">基准币种</dt>
          <dd className="font-medium text-ink">{detail.base_currency}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">再平衡</dt>
          <dd className="font-medium text-ink">
            {REBALANCE_POLICY_LABELS[detail.rebalance_policy] ?? detail.rebalance_policy}
            {detail.rebalance_policy === "threshold" &&
              `（${formatPercent(detail.rebalance_threshold)}）`}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">回测区间</dt>
          <dd className="font-medium text-ink" data-testid="backtest-window">
            {readiness?.window_start && readiness.window_end
              ? `${readiness.window_start} ~ ${readiness.window_end}`
              : "待数据就绪"}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">基准资产</dt>
          <dd className="truncate font-medium text-ink">
            {detail.benchmark_asset_key || "无"}
          </dd>
        </div>
      </dl>

      <div className="flex flex-wrap items-center gap-3">
        <Button
          disabled={disabledReason !== null}
          pending={runMutation.isPending}
          onClick={() => runMutation.mutate()}
          data-testid="run-backtest"
        >
          运行回测
        </Button>
        {disabledReason && (
          <p className="text-xs text-warning" data-testid="run-disabled-reason">
            {disabledReason}
          </p>
        )}
        {reusedNotice && (
          <p className="text-xs text-info" role="status">
            输入未变化，已复用此前成功的回测结果。
          </p>
        )}
      </div>

      {runMutation.isError && (
        <p className="mt-2 text-sm text-danger" role="alert">
          创建回测失败：{queryErrorMessage(runMutation.error)}
        </p>
      )}

      {latest && (
        <div className="mt-4 border-t border-line pt-3" data-testid="latest-run">
          <h3 className="mb-1.5 text-sm font-semibold text-ink">最近一次运行</h3>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            <Link
              href={`/research/collections/${detail.id}/runs/${latest.id}`}
              className="text-brand underline-offset-2 hover:underline"
            >
              {latest.window_start} ~ {latest.window_end}
            </Link>
            {runStatusBadge(latest.status)}
            {latest.status === "succeeded" && latest.summary && (
              <span className="text-xs text-ink-muted">
                CAGR {formatPercent(latest.summary.cagr)} · 回撤{" "}
                {formatPercent(latest.summary.max_drawdown)} · 波动{" "}
                {formatNullablePercent(latest.summary.annual_volatility)}
              </span>
            )}
            <span className="text-xs text-ink-muted">{formatDateTimeFromMs(latest.created_at)}</span>
          </div>
        </div>
      )}
    </section>
  );
}
