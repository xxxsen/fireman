"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import {
  applyOptimization,
  getCollection,
  getOptimization,
  type ResearchCollectionDetail,
  type ResearchOptimizationResultItem,
  type ResearchOptimizationRun,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { formatDateTimeFromMs, formatNullablePercent, formatPercent } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Tooltip } from "@/components/ui/Tooltip";
import { HelpLabel } from "@/components/ui/HelpLabel";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { runStatusBadge } from "@/components/research/runStatus";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";
import type { ResearchRebalancePolicy } from "@/lib/api/research";
import { isTaskActive } from "@/lib/api/tasks";
import { useTaskStatus } from "@/hooks/useTaskStatus";

type TabKey = "cagr" | "drawdown" | "cvar" | "calmar";
const TABS: { key: TabKey; label: string }[] = [
  { key: "cagr", label: "最高收益" },
  { key: "drawdown", label: "最低回撤" },
  { key: "cvar", label: "最低尾部损失" },
  { key: "calmar", label: "收益回撤平衡" },
];

function scoreFmt(tab: TabKey, score: number): string {
  if (tab === "cvar") return formatPercent(-score);
  if (tab === "drawdown") return formatPercent(score);
  if (tab === "calmar") return score.toFixed(3);
  return formatPercent(score);
}

function ResultTable({
  items,
  tab,
  onApply,
}: {
  items: ResearchOptimizationResultItem[];
  tab: TabKey;
  onApply: (item: ResearchOptimizationResultItem) => void;
}) {
  if (items.length === 0) {
    return <p className="py-4 text-center text-sm text-ink-muted">{tab === "cvar" ? "没有候选达到最低 CAGR 门槛。降低门槛或关闭该限制后重新运行调优。" : "无结果"}</p>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[980px] text-sm" data-testid={`result-table-${tab}`}>
        <thead>
          <tr className="border-b border-line text-left text-xs text-ink-muted">
            <th className="px-2 py-2 font-medium">#</th>
            <th className="px-2 py-2 font-medium"><HelpLabel label={tab === "cvar" ? "CVaR" : "得分"} termKey="optimization_ranking" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="年化收益" termKey="metric_cagr" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="累计收益" termKey="cumulative_return" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="最大回撤" termKey="metric_max_drawdown" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="波动率" termKey="metric_annual_volatility" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="VaR loss" termKey="metric_var_loss" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="CVaR loss" termKey="metric_cvar_loss" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="夏普比率" termKey="sharpe_ratio" /></th>
            <th className="px-2 py-2 font-medium"><HelpLabel label="卡玛比率" termKey="calmar_ratio" /></th>
            <th className="px-2 py-2 font-medium">权重分配</th>
            <th className="px-2 py-2 text-right font-medium">操作</th>
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
              <TailLossCell item={item} field="var_loss" />
              <TailLossCell item={item} field="cvar_loss" />
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {item.summary.sharpe != null ? item.summary.sharpe.toFixed(2) : "—"}
              </td>
              <td className="px-2 py-2 font-mono-numeric text-ink">
                {item.summary.calmar != null ? item.summary.calmar.toFixed(2) : "—"}
              </td>
              <td className="px-2 py-2">
                <WeightBar weights={item.weights} />
              </td>
              <td className="px-2 py-2 text-right">
                <Button
                  variant="secondary"
                  onClick={() => onApply(item)}
                  data-testid={`apply-result-${tab}-${item.rank}`}
                >
                  应用
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function TailLossCell({
  item,
  field,
}: {
  item: ResearchOptimizationResultItem;
  field: "var_loss" | "cvar_loss";
}) {
  const tail = item.summary.tail_risk;
  if (!tail) return <td className="px-2 py-2 text-ink-muted">—</td>;
  const value = tail[field];
  const details = `${tail.horizon_days} 日 / ${(tail.confidence * 100).toFixed(0)}%，${tail.scenario_count} 个场景，尾部计数 ${tail.tail_count}`;
  return (
    <td className={`px-2 py-2 font-mono-numeric ${value > 0 ? "text-danger" : "text-positive"}`}>
      <Tooltip content={details} contentClassName="max-w-64">
        <span className="cursor-help">{formatPercent(value)}</span>
      </Tooltip>
    </td>
  );
}

function WeightBar({
  weights,
}: {
  weights: ResearchOptimizationResultItem["weights"];
}) {
  const normalized = weights.map((w, index) => {
    return {
      key: `${w.item_id}-${index}`,
      name: w.name || w.asset_key || "未命名资产",
      weight: w.weight,
      locked: w.locked,
    };
  });
  const active = normalized.filter((w) => w.weight > 0);
  if (active.length === 0) return <span className="text-ink-muted">—</span>;
  const title = active.map((w) => `${w.name}: ${formatPercent(w.weight)}`).join(" / ");

  return (
    <Tooltip content={title} clickToggle contentClassName="max-w-80 whitespace-normal">
    <div className="max-w-[22rem] space-y-0.5" tabIndex={0} aria-label={`权重分配：${title}`}>
      {active.map((w) => (
        <div
          key={w.key}
          className="grid grid-cols-[56px_minmax(7rem,1fr)_auto_auto] items-center gap-1.5 text-xs"
        >
          <div
            className="h-2 rounded-sm bg-brand/70"
            style={{ width: `${Math.max(4, Math.min(56, w.weight * 56))}px` }}
          />
          <span className="truncate text-ink">
            {w.name}
          </span>
          <span className="font-mono-numeric text-ink-muted">
            {formatPercent(w.weight)}
          </span>
          {w.locked && <span className="text-[10px] text-warning">锁</span>}
        </div>
      ))}
    </div>
    </Tooltip>
  );
}

function progressLabel(opt: ResearchOptimizationRun): string {
  if (!opt.task) return "排队中…";
  const { phase, progress_current, progress_total } = opt.task;
  const attempt_count = opt.task.attempt_count ?? 0;
  const retryPrefix = attempt_count > 0 ? `任务中断后自动重试（${attempt_count}/1）：` : "";
  if (phase === "retrying") return `任务中断后自动重试（${attempt_count}/1），等待执行…`;
  if (phase === "loading") return `${retryPrefix}加载数据中…`;
  if (phase === "evaluating" && progress_total > 0)
    return `${retryPrefix}评估中 ${progress_current}/${progress_total}`;
  if (phase === "done") return "完成";
  return `${retryPrefix}计算中…`;
}

function optimizationFailureMessage(opt: ResearchOptimizationRun): string {
  if (opt.error_code === "worker_interrupted") {
    return "执行进程中断，自动重试仍未完成。请重新发起调优。";
  }
  return opt.error_message || "自动调优失败。";
}

export default function OptimizationDetailPage() {
  const params = useParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const collectionId = params.id as string;
  const optimizationId = params.optimizationId as string;
  const [activeTab, setActiveTab] = useState<TabKey>("cagr");
  const [selectedResult, setSelectedResult] = useState<ResearchOptimizationResultItem | null>(null);
  const [applyError, setApplyError] = useState<string | null>(null);

  const optQuery = useQuery({
    queryKey: ["research", "optimization", optimizationId],
    queryFn: () => getOptimization(optimizationId),
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return isTaskActive(status) ? 2000 : false;
    },
  });

  const collectionQuery = useQuery({
    queryKey: ["research", "collection", collectionId],
    queryFn: () => getCollection(collectionId),
  });

  const opt = optQuery.data;
  const detail = collectionQuery.data;
  const taskState = useTaskStatus(opt?.task_id, {
    onComplete: () => {
      void queryClient.invalidateQueries({
        queryKey: ["research", "optimization", optimizationId],
      });
      void queryClient.invalidateQueries({
        queryKey: ["research", "latest-optimization", collectionId],
      });
      void queryClient.invalidateQueries({ queryKey: ["research", "collection", collectionId] });
      void queryClient.invalidateQueries({ queryKey: ["research", "collections"] });
    },
    onFailed: () => {
      void queryClient.invalidateQueries({
        queryKey: ["research", "optimization", optimizationId],
      });
      void queryClient.invalidateQueries({
        queryKey: ["research", "latest-optimization", collectionId],
      });
    },
    onCanceled: () => {
      void queryClient.invalidateQueries({
        queryKey: ["research", "optimization", optimizationId],
      });
      void queryClient.invalidateQueries({
        queryKey: ["research", "latest-optimization", collectionId],
      });
    },
  });
  const effectiveStatus = taskState.task?.status ?? opt?.status;
  const collectionName = detail?.name ?? "研究集合";

  const activeItems = useMemo(() => {
    if (!opt?.result) return [];
    switch (activeTab) {
      case "cagr":
        return opt.result.best_by_cagr ?? [];
      case "drawdown":
        return opt.result.best_by_drawdown ?? [];
      case "cvar":
        return opt.result.best_by_cvar ?? [];
      case "calmar":
        return opt.result.best_by_calmar ?? [];
    }
  }, [opt, activeTab]);

  const applyPreview = useMemo(() => {
    if (!selectedResult || !detail) return null;
    try {
      return { value: buildApplyPreview(detail, selectedResult), error: null };
    } catch (err) {
      return {
        value: null,
        error: err instanceof Error ? err.message : "调优结果异常，请重新运行调优。",
      };
    }
  }, [detail, selectedResult]);

  const applyMutation = useMutation({
    mutationFn: async (result: ResearchOptimizationResultItem) => {
      if (!detail) throw new Error("集合尚未加载完成");
      if (!opt) throw new Error("调优结果尚未加载完成");
      buildPositiveWeights(detail, result);
      return applyOptimization(optimizationId, {
        objective: result.objective,
        rank: result.rank,
        expected_collection_updated_at: detail.updated_at,
      });
    },
    onSuccess: () => {
      setApplyError(null);
      setSelectedResult(null);
      void queryClient.invalidateQueries({ queryKey: ["research", "collection", collectionId] });
      void queryClient.invalidateQueries({ queryKey: ["research", "readiness", collectionId] });
      void queryClient.invalidateQueries({
        queryKey: ["research", "optimization-readiness", collectionId],
      });
      router.push(`/research/collections/${collectionId}?optimized_applied=1`);
    },
    onError: (err) => setApplyError(queryErrorMessage(err)),
  });

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

      <dl className="mb-4 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-3 lg:grid-cols-6">
        <div>
          <dt className="text-ink-muted"><HelpLabel label="权重步长" termKey="optimization_weight_step" /></dt>
          <dd className="font-medium text-ink">
            {opt.config?.weight_step != null
              ? formatPercent(opt.config.weight_step)
              : "—"}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted"><HelpLabel label="CVaR 口径" termKey="metric_cvar_loss" /></dt>
          <dd className="font-medium text-ink">
            {opt.config?.tail_risk ? `${opt.config.tail_risk.horizon_days} 日 / ${opt.config.tail_risk.confidence * 100}%` : "—"}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted"><HelpLabel label="最低 CAGR" termKey="metric_cagr" /></dt>
          <dd className="font-medium text-ink">
            {opt.config?.minimum_cagr != null ? formatPercent(opt.config.minimum_cagr) : "未限制"}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted"><HelpLabel label="Top K" termKey="optimization_constraints" /></dt>
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

      {!["research_optimizer_v5", "research_optimizer_v6"].includes(opt.engine_version) && (
        <p className="mb-4 text-sm text-warning" role="note">
          该历史调优版本未计研究交易成本；候选排序不可与新版本直接比较。
        </p>
      )}

      {isTaskActive(effectiveStatus) && (
        <div
          className="rounded-lg border border-info/25 bg-info/5 px-4 py-6 text-center"
          role="status"
        >
          <LoadingState label={progressLabel(opt)} className="justify-center" />
          <div className="mt-3 flex justify-center">
            <TaskCancelButton
              task={taskState.task ?? (opt.task
                ? { id: opt.task_id, status: opt.task.status }
                : null)}
              onCanceled={async () => {
                await taskState.refetch();
                await optQuery.refetch();
              }}
            />
          </div>
          {taskState.pollError && (
            <p className="mt-2 text-xs text-warning">状态更新暂时失败，正在重试。</p>
          )}
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
          message={optimizationFailureMessage(opt)}
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

      {opt.status === "complete" && opt.result && (
        <div className="space-y-4">
          {opt.result.skipped_count > 0 && (
            <p className="text-xs text-warning">
              {opt.result.skipped_count} 个候选回测失败已跳过。
            </p>
          )}

          {opt.result.warnings?.map((warning) => (
            <p key={warning.code} className="text-xs text-warning">{warning.message}</p>
          ))}

          <div className="flex gap-1 overflow-x-auto border-b border-line">
            {TABS.map((tab) => {
              const active = activeTab === tab.key;
              return (
                <div
                  key={tab.key}
                  className={
                    "flex shrink-0 items-center whitespace-nowrap border-b-2 px-4 transition-colors " +
                    (active
                      ? "border-brand text-brand"
                      : "text-ink-muted hover:text-ink")
                  }
                >
                  <button
                    type="button"
                    onClick={() => setActiveTab(tab.key)}
                    className="py-2 text-sm font-medium"
                    data-testid={`tab-${tab.key}`}
                  >
                    {tab.label}
                  </button>
                  {tab.key === "cvar" && <MetricHelp termKey="metric_cvar_loss" />}
                </div>
              );
            })}
          </div>

          <p className="rounded-md bg-surface-muted px-3 py-2 text-xs leading-relaxed text-ink-muted">
            <HelpLabel label="当前排序" termKey="optimization_ranking" />：
            {activeTab === "cagr" && "按 CAGR 从高到低，优先展示历史复合收益较高的候选。"}
            {activeTab === "drawdown" && "按最大回撤幅度从低到高，优先展示历史最深回撤较小的候选。"}
            {activeTab === "cvar" && "先应用最低 CAGR 约束，再按 CVaR loss 从低到高展示尾部平均损失较小的候选；这不保证年化收益更高。"}
            {activeTab === "calmar" && "按 Calmar 比率从高到低，平衡 CAGR 与最大回撤。"}
            所有结果只在当前离散权重步长和已评估候选中排名；主指标相同时使用稳定并列规则。<MetricHelp termKey="optimization_tie_break" />
          </p>

          <ResultTable
            items={activeItems}
            tab={activeTab}
            onApply={(item) => {
              setApplyError(null);
              setSelectedResult(item);
            }}
          />
        </div>
      )}

      <ConfirmDialog
        open={selectedResult !== null}
        title="应用调优结果"
        confirmLabel="应用到组合"
        pending={applyMutation.isPending}
        error={applyError}
        onClose={() => {
          if (applyMutation.isPending) return;
          setSelectedResult(null);
          setApplyError(null);
        }}
        onConfirm={() => {
          if (selectedResult) applyMutation.mutate(selectedResult);
        }}
        description={
          applyPreview?.error ? (
            <p className="text-danger">{applyPreview.error}</p>
          ) : applyPreview?.value ? (
            <div className="space-y-2">
              <p>目标组合：{collectionName}</p>
              <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
                <dt className="text-ink-muted">启用并锁定</dt>
                <dd className="font-medium text-ink">{applyPreview.value.enabledLockedCount} 个资产</dd>
                <dt className="text-ink-muted">取消启用</dt>
                <dd className="font-medium text-ink">{applyPreview.value.disabledCount} 个资产</dd>
                <dt className="text-ink-muted">权重合计</dt>
                <dd className="font-medium text-ink">{formatPercent(applyPreview.value.weightSum)}</dd>
                <dt className="text-ink-muted">回测区间</dt>
                <dd className="font-medium text-ink">
                  {opt.window_start} ~ {opt.window_end}
                </dd>
                <dt className="text-ink-muted">尾部风险口径</dt>
                <dd className="font-medium text-ink">
                  {opt.config?.tail_risk ? `${opt.config.tail_risk.horizon_days} 日 / ${opt.config.tail_risk.confidence * 100}%` : "—"}
                </dd>
                <dt className="text-ink-muted">最低 CAGR</dt>
                <dd className="font-medium text-ink">仅用于本次筛选，不写入集合</dd>
              </dl>
              <p className="text-xs text-warning">
                应用后会同步当前组合的启用、锁定、权重、回测区间和尾部风险口径。
              </p>
            </div>
          ) : (
            "正在准备应用预览…"
          )
        }
      />
    </div>
  );
}

function buildPositiveWeights(
  detail: ResearchCollectionDetail,
  result: ResearchOptimizationResultItem,
): Map<string, number> {
  const positive = new Map<string, number>();
  const detailByID = new Map(detail.items.map((item) => [item.id, item]));

  for (const raw of result.weights) {
    const itemId = raw.item_id;
    const weight = raw.weight;
    const detailItem = detailByID.get(itemId);
    if (!detailItem || detailItem.asset_key !== raw.asset_key) {
      throw new Error("调优结果与当前组合资产不一致，请重新运行调优。");
    }
    if (!Number.isFinite(weight) || weight < 0) {
      throw new Error("调优结果权重异常，请重新运行调优。");
    }
    if (weight > 0) positive.set(itemId, weight);
  }

  const sum = Array.from(positive.values()).reduce((s, v) => s + v, 0);
  if (Math.abs(sum - 1) > 1e-12) {
    throw new Error("调优结果权重合计异常，请重新运行调优。");
  }
  return positive;
}

function buildApplyPreview(
  detail: ResearchCollectionDetail,
  result: ResearchOptimizationResultItem,
) {
  const positive = buildPositiveWeights(detail, result);
  return {
    enabledLockedCount: positive.size,
    disabledCount: detail.items.length - positive.size,
    weightSum: Array.from(positive.values()).reduce((s, v) => s + v, 0),
  };
}
