"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import {
  getCollection,
  getOptimization,
  type ResearchOptimizationResultItem,
  type ResearchOptimizationRun,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { formatDateTimeFromMs, formatNullablePercent, formatPercent } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { runStatusBadge } from "@/components/research/runStatus";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";
import type { ResearchRebalancePolicy } from "@/lib/api/research";

type TabKey = "cagr" | "drawdown" | "calmar";

const TABS: { key: TabKey; label: string }[] = [
  { key: "cagr", label: "最高收益" },
  { key: "drawdown", label: "最低回撤" },
  { key: "calmar", label: "收益回撤平衡" },
];

function scoreFmt(tab: TabKey, score: number): string {
  if (tab === "drawdown") return formatPercent(score);
  if (tab === "calmar") return score.toFixed(3);
  return formatPercent(score);
}

function ResultTable({
  items,
  tab,
}: {
  items: ResearchOptimizationResultItem[];
  tab: TabKey;
}) {
  if (items.length === 0) {
    return <p className="py-4 text-center text-sm text-ink-muted">无结果</p>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[700px] text-sm" data-testid={`result-table-${tab}`}>
        <thead>
          <tr className="border-b border-line text-left text-xs text-ink-muted">
            <th className="px-2 py-2 font-medium">#</th>
            <th className="px-2 py-2 font-medium">得分</th>
            <th className="px-2 py-2 font-medium">CAGR</th>
            <th className="px-2 py-2 font-medium">累计收益</th>
            <th className="px-2 py-2 font-medium">最大回撤</th>
            <th className="px-2 py-2 font-medium">波动率</th>
            <th className="px-2 py-2 font-medium">Sharpe</th>
            <th className="px-2 py-2 font-medium">Calmar</th>
            <th className="px-2 py-2 font-medium">权重分配</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr
              key={item.rank}
              className="border-b border-line/60 last:border-0"
              data-testid={`result-row-${tab}-${item.rank}`}
            >
              <td className="px-2 py-2 font-medium text-ink">{item.rank}</td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {scoreFmt(tab, item.score)}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {formatPercent(item.summary.cagr)}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {formatPercent(item.summary.cumulative_return)}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-danger">
                {formatPercent(item.summary.max_drawdown)}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {formatNullablePercent(item.summary.annual_volatility)}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {item.summary.sharpe != null ? item.summary.sharpe.toFixed(2) : "—"}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {item.summary.calmar != null ? item.summary.calmar.toFixed(2) : "—"}
              </td>
              <td className="px-2 py-2">
                <WeightBar weights={item.weights} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function WeightBar({
  weights,
}: {
  weights: ResearchOptimizationResultItem["weights"];
}) {
  const active = weights.filter((w) => w.weight > 0);
  if (active.length === 0) return <span className="text-ink-muted">—</span>;

  return (
    <div className="space-y-0.5">
      {active.map((w) => (
        <div key={w.item_id} className="flex items-center gap-1.5 text-xs">
          <div
            className="h-2 rounded-sm bg-brand/70"
            style={{ width: `${Math.max(4, w.weight * 100)}px` }}
          />
          <span className="truncate text-ink" title={w.name}>
            {w.name}
          </span>
          <span className="font-mono-numeric text-ink-muted">
            {formatPercent(w.weight)}
          </span>
          {w.locked && <span className="text-[10px] text-warning">锁</span>}
        </div>
      ))}
    </div>
  );
}

function progressLabel(opt: ResearchOptimizationRun): string {
  if (!opt.job) return "排队中…";
  const { phase, progress_current, progress_total } = opt.job;
  if (phase === "loading") return "加载数据中…";
  if (phase === "evaluating" && progress_total > 0)
    return `评估中 ${progress_current}/${progress_total}`;
  if (phase === "done") return "完成";
  return "计算中…";
}

export default function OptimizationDetailPage() {
  const params = useParams();
  const collectionId = params.id as string;
  const optimizationId = params.optimizationId as string;
  const [activeTab, setActiveTab] = useState<TabKey>("cagr");

  const optQuery = useQuery({
    queryKey: ["research", "optimization", optimizationId],
    queryFn: () => getOptimization(optimizationId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "queued" || status === "running" ? 2000 : false;
    },
  });

  const collectionQuery = useQuery({
    queryKey: ["research", "collection", collectionId],
    queryFn: () => getCollection(collectionId),
  });

  const opt = optQuery.data;
  const collectionName = collectionQuery.data?.name ?? "研究集合";

  const activeItems = useMemo(() => {
    if (!opt?.result) return [];
    switch (activeTab) {
      case "cagr":
        return opt.result.best_by_cagr ?? [];
      case "drawdown":
        return opt.result.best_by_drawdown ?? [];
      case "calmar":
        return opt.result.best_by_calmar ?? [];
    }
  }, [opt, activeTab]);

  if (optQuery.isLoading) {
    return (
      <div className="content-enter">
        <LoadingState label="加载调优结果…" />
      </div>
    );
  }

  if (optQuery.isError || !opt) {
    return (
      <div className="content-enter">
        <ErrorState
          message="加载调优结果失败。"
          onRetry={() => void optQuery.refetch()}
          backHref={`/research/collections/${collectionId}`}
          technicalDetail={optQuery.isError ? queryErrorMessage(optQuery.error) : undefined}
        />
      </div>
    );
  }

  return (
    <div className="content-enter">
      <PageHeader
        backHref={`/research/collections/${collectionId}`}
        backLabel={collectionName}
        title="自动调优结果"
        status={runStatusBadge(opt.status)}
        description={`${opt.window_start} ~ ${opt.window_end} · ${
          REBALANCE_POLICY_LABELS[opt.rebalance_policy as ResearchRebalancePolicy] ??
          opt.rebalance_policy
        } · ${opt.base_currency} · ${formatDateTimeFromMs(opt.created_at)}`}
      />

      <dl className="mb-4 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
        <div>
          <dt className="text-ink-muted">权重步长</dt>
          <dd className="font-medium text-ink">
            {opt.config?.weight_step != null
              ? formatPercent(opt.config.weight_step)
              : "—"}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">Top K</dt>
          <dd className="font-medium text-ink">{opt.config?.top_k ?? "—"}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">候选数量</dt>
          <dd className="font-medium text-ink">{opt.candidate_count.toLocaleString()}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">已评估</dt>
          <dd className="font-medium text-ink">{opt.evaluated_count.toLocaleString()}</dd>
        </div>
      </dl>

      {(opt.status === "queued" || opt.status === "running") && (
        <div
          className="rounded-lg border border-info/25 bg-info/5 px-4 py-6 text-center"
          role="status"
        >
          <LoadingState label={progressLabel(opt)} className="justify-center" />
          {opt.candidate_count > 0 && opt.evaluated_count > 0 && (
            <div className="mt-2">
              <div className="mx-auto h-2 w-64 overflow-hidden rounded-full bg-surface-muted">
                <div
                  className="h-full rounded-full bg-brand transition-all"
                  style={{
                    width: `${Math.min(100, (opt.evaluated_count / opt.candidate_count) * 100)}%`,
                  }}
                />
              </div>
              <p className="mt-1 text-xs text-ink-muted">
                {opt.evaluated_count} / {opt.candidate_count}
              </p>
            </div>
          )}
        </div>
      )}

      {opt.status === "failed" && (
        <ErrorState
          title="调优失败"
          message={opt.error_message || "自动调优失败。"}
          technicalDetail={opt.error_code}
          backHref={`/research/collections/${collectionId}`}
          backLabel="返回集合"
        />
      )}

      {opt.status === "canceled" && (
        <p className="rounded-lg border border-line bg-surface px-4 py-6 text-center text-sm text-ink-muted">
          该调优已取消。
        </p>
      )}

      {opt.status === "succeeded" && opt.result && (
        <div className="space-y-4">
          {opt.result.skipped_count > 0 && (
            <p className="text-xs text-warning">
              {opt.result.skipped_count} 个候选回测失败已跳过。
            </p>
          )}

          <div className="flex gap-1 border-b border-line">
            {TABS.map((tab) => (
              <button
                key={tab.key}
                type="button"
                onClick={() => setActiveTab(tab.key)}
                className={
                  "px-4 py-2 text-sm font-medium transition-colors " +
                  (activeTab === tab.key
                    ? "border-b-2 border-brand text-brand"
                    : "text-ink-muted hover:text-ink")
                }
                data-testid={`tab-${tab.key}`}
              >
                {tab.label}
              </button>
            ))}
          </div>

          <ResultTable items={activeItems} tab={activeTab} />
        </div>
      )}
    </div>
  );
}
