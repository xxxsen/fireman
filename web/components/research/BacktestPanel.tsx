"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import {
  createBacktest,
  createOptimization,
  getLatestOptimization,
  getOptimizationReadiness,
  type ResearchCollectionDetail,
  type ResearchOptimizationReadiness,
  type ResearchOptimizationRun,
  type ResearchReadiness,
  type ResearchRunView,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import {
  formatDateTimeFromMs,
  formatNullablePercent,
  formatPercent,
} from "@/lib/format";
import { Button } from "@/components/ui/Button";
import { runStatusBadge } from "@/components/research/runStatus";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";
import { OptimizationConfigDialog } from "@/components/research/OptimizationConfigDialog";
import type { OptimizationSubmitConfig } from "@/components/research/OptimizationConfigDialog";

/**
 * The run button's disabled explanation, derived from readiness in priority
 * order (td/099 §4.4): weight -> missing history -> active sync -> FX ->
 * window -> ready.
 */
export function runDisabledReason(
  readiness: ResearchReadiness | undefined,
): string | null {
  if (!readiness) return "正在检查数据就绪状态…";
  if (readiness.ready) return null;
  const reasons = new Set(readiness.blocking_reasons.map((r) => r.reason));
  if (
    reasons.has("no_enabled_assets") ||
    reasons.has("weight_sum_invalid") ||
    reasons.has("negative_weight") ||
    reasons.has("weight_exceeds_100")
  ) {
    if (reasons.has("no_enabled_assets")) return "集合没有启用的资产";
    return `当前权重合计 ${formatPercent(readiness.weight_sum)}，未达到 100%，仅允许执行最优组合查找或调整权重`;
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
  if (
    reasons.has("common_window_empty") ||
    reasons.has("common_window_too_short")
  ) {
    const blockers = readiness.assets
      .filter((a) => a.limits_common_start || a.limits_common_end)
      .map((a) => a.name);
    return (
      "共同历史区间不足" +
      (blockers.length > 0 ? `（受限于 ${blockers.join("、")}）` : "")
    );
  }
  return readiness.blocking_reasons[0]?.message ?? "数据未就绪";
}

export function optimizationDisabledReason(
  readiness: ResearchReadiness | undefined,
  optReadiness: ResearchOptimizationReadiness | undefined,
): string | null {
  if (!readiness || !optReadiness) return "正在检查调优就绪状态…";
  if (
    optReadiness.enabled_count === 0 ||
    optReadiness.blocking_reasons.some((b) => b.reason === "no_enabled_assets")
  ) {
    return "集合没有启用的资产";
  }
  if (optReadiness.ready) return null;
  let fallbackReason: string | null = null;
  for (const b of optReadiness.blocking_reasons) {
    // Candidate count depends on the weight step selected inside the dialog.
    // Keep the entry available so the user can inspect and adjust that input.
    if (b.reason === "candidate_count_exceeds_limit") continue;
    if (b.reason === "no_enabled_assets") return "集合没有启用的资产";
    if (b.reason === "too_many_enabled_assets")
      return `启用资产 ${optReadiness.enabled_count} 个超过上限 10 个`;
    if (b.reason === "locked_weight_exceeds_100")
      return `锁定权重合计 ${formatPercent(optReadiness.locked_weight_sum)} 超过 100%`;
    if (b.reason === "candidate_count_zero")
      return b.message || "当前设置无法生成候选组合";
    if (b.reason === "history_missing" || b.reason === "history_sync_failed")
      return "存在缺历史资产，请先同步数据";
    if (b.reason === "history_syncing" || b.reason === "fx_syncing")
      return "数据同步任务进行中，完成后可运行";
    if (b.reason === "fx_missing" || b.reason === "fx_gap_exceeded")
      return "汇率数据缺失或存在缺口";
    fallbackReason ??= b.message || "调优条件未满足";
  }
  return fallbackReason;
}

export interface BacktestPanelProps {
  detail: ResearchCollectionDetail;
  readiness?: ResearchReadiness;
  latestRuns: ResearchRunView[];
}

export function BacktestPanel({
  detail,
  readiness,
  latestRuns,
}: BacktestPanelProps) {
  const router = useRouter();
  const [reusedNotice, setReusedNotice] = useState(false);
  const [optDialogOpen, setOptDialogOpen] = useState(false);

  const disabledReason = useMemo(
    () => runDisabledReason(readiness),
    [readiness],
  );

  const optReadinessQuery = useQuery({
    queryKey: [
      "research",
      "optimization-readiness",
      detail.id,
      detail.tail_risk_confidence,
      detail.tail_risk_horizon_days,
    ],
    queryFn: () =>
      getOptimizationReadiness(detail.id, {
        confidence: detail.tail_risk_confidence,
        horizonDays: detail.tail_risk_horizon_days,
      }),
    enabled: !!readiness,
  });

  const latestOptQuery = useQuery({
    queryKey: ["research", "latest-optimization", detail.id],
    queryFn: () => getLatestOptimization(detail.id),
  });

  const optDisabledReason = useMemo(
    () => optimizationDisabledReason(readiness, optReadinessQuery.data),
    [readiness, optReadinessQuery.data],
  );

  const runMutation = useMutation({
    mutationFn: () => createBacktest(detail.id),
    onSuccess: (result) => {
      setReusedNotice(result.reused);
      router.push(`/research/collections/${detail.id}/runs/${result.run.id}`);
    },
  });

  const optimizeMutation = useMutation({
    mutationFn: (config: OptimizationSubmitConfig) =>
      createOptimization(detail.id, config),
    onSuccess: (result) => {
      setOptDialogOpen(false);
      router.push(
        `/research/collections/${detail.id}/optimizations/${result.optimization.id}`,
      );
    },
  });

  const latest = latestRuns[0];

  return (
    <section
      className="rounded-lg border border-line bg-surface p-4"
      data-testid="backtest-panel"
    >
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-base font-semibold text-ink">回测</h2>
        <Link
          href={`/research/collections/${detail.id}/runs`}
          className="text-sm text-brand underline-offset-2 hover:underline"
        >
          全部运行记录
        </Link>
      </div>

      <dl className="mb-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-3 lg:grid-cols-6">
        <div>
          <dt className="text-ink-muted">基准币种</dt>
          <dd className="font-medium text-ink">{detail.base_currency}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">再平衡</dt>
          <dd className="font-medium text-ink">
            {REBALANCE_POLICY_LABELS[detail.rebalance_policy] ??
              detail.rebalance_policy}
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
        <div>
          <dt className="text-ink-muted">CVaR 口径</dt>
          <dd className="font-medium text-ink">
            {readiness?.tail_risk
              ? `${readiness.tail_risk.horizon_days} 日 / ${readiness.tail_risk.confidence * 100}%`
              : `${detail.tail_risk_horizon_days} 日 / ${detail.tail_risk_confidence * 100}%`}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">CVaR 场景</dt>
          <dd
            className="font-medium text-ink"
            data-testid="backtest-cvar-scenarios"
          >
            {readiness?.tail_risk
              ? `${readiness.tail_risk.scenario_count} / 最少 ${readiness.tail_risk.minimum_scenario_count}`
              : "待数据就绪"}
          </dd>
        </div>
      </dl>

      {/* Normal backtest button + its own disabled reason */}
      <div className="flex flex-wrap items-center gap-3">
        <span className="inline-flex" title={disabledReason ?? undefined}>
          <Button
            className={disabledReason ? "pointer-events-none w-32" : "w-32"}
            disabled={disabledReason !== null}
            pending={runMutation.isPending}
            onClick={() => runMutation.mutate()}
            data-testid="run-backtest"
          >
            运行回测
          </Button>
        </span>
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

      {/* Optimization button + its own disabled reason */}
      <div className="mt-3 flex flex-wrap items-center gap-3">
        <span className="inline-flex" title={optDisabledReason ?? undefined}>
          <Button
            variant="secondary"
            className={optDisabledReason ? "pointer-events-none w-32" : "w-32"}
            disabled={optDisabledReason !== null}
            pending={optimizeMutation.isPending}
            onClick={() => setOptDialogOpen(true)}
            data-testid="find-optimal"
          >
            寻找最优组合
          </Button>
        </span>
        {optDisabledReason && (
          <p className="text-xs text-warning" data-testid="opt-disabled-reason">
            {optDisabledReason}
          </p>
        )}
      </div>

      {optimizeMutation.isError && (
        <p className="mt-2 text-sm text-danger" role="alert">
          创建调优失败：{queryErrorMessage(optimizeMutation.error)}
        </p>
      )}

      {/* Remount dialog on open so state resets to defaults */}
      {optDialogOpen && (
        <OptimizationConfigDialog
          open={optDialogOpen}
          onClose={() => setOptDialogOpen(false)}
          pending={optimizeMutation.isPending}
          onSubmit={(config) => optimizeMutation.mutate(config)}
          collectionId={detail.id}
          defaultConfidence={detail.tail_risk_confidence}
          defaultHorizonDays={detail.tail_risk_horizon_days}
        />
      )}

      {latest && (
        <div
          className="mt-4 border-t border-line pt-3"
          data-testid="latest-run"
        >
          <h3 className="mb-1.5 text-sm font-semibold text-ink">
            最近一次回测
          </h3>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
            <Link
              href={`/research/collections/${detail.id}/runs/${latest.id}`}
              className="text-brand underline-offset-2 hover:underline"
            >
              {latest.window_start} ~ {latest.window_end}
            </Link>
            {runStatusBadge(latest.status)}
            {latest.status === "complete" && latest.summary && (
              <span className="text-xs text-ink-muted">
                CAGR {formatPercent(latest.summary.cagr)} · 回撤{" "}
                {formatPercent(latest.summary.max_drawdown)} · 波动{" "}
                {formatNullablePercent(latest.summary.annual_volatility)}
              </span>
            )}
            <span className="text-xs text-ink-muted">
              {formatDateTimeFromMs(latest.created_at)}
            </span>
          </div>
        </div>
      )}

      {latestOptQuery.data && (
        <LatestOptimizationEntry
          collectionId={detail.id}
          optimization={latestOptQuery.data}
        />
      )}
    </section>
  );
}

function LatestOptimizationEntry({
  collectionId,
  optimization,
}: {
  collectionId: string;
  optimization: ResearchOptimizationRun;
}) {
  return (
    <div
      className="mt-4 border-t border-line pt-3"
      data-testid="latest-optimization"
    >
      <h3 className="mb-1.5 text-sm font-semibold text-ink">
        最近一次自动调优
      </h3>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
        <Link
          href={`/research/collections/${collectionId}/optimizations/${optimization.id}`}
          className="text-brand underline-offset-2 hover:underline"
        >
          {optimization.window_start} ~ {optimization.window_end}
        </Link>
        {runStatusBadge(optimization.status)}
        {optimization.status === "complete" && (
          <span className="text-xs text-ink-muted">
            候选 {optimization.candidate_count} · 已评估{" "}
            {optimization.evaluated_count}
          </span>
        )}
        <span className="text-xs text-ink-muted">
          {formatDateTimeFromMs(optimization.created_at)}
        </span>
      </div>
    </div>
  );
}
