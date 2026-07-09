"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import {
  getCollection,
  getOptimization,
  updateCollection,
  updateCollectionItem,
  type ResearchCollectionDetail,
  type ResearchOptimizationResultItem,
  type ResearchOptimizationRun,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { formatDateTimeFromMs, formatNullablePercent, formatPercent } from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Tooltip } from "@/components/ui/Tooltip";
import { runStatusBadge } from "@/components/research/runStatus";
import { REBALANCE_POLICY_LABELS } from "@/components/research/CollectionParamsForm";
import type { ResearchRebalancePolicy } from "@/lib/api/research";

type TabKey = "cagr" | "drawdown" | "calmar";
type LegacyOptimizationWeightEntry = ResearchOptimizationResultItem["weights"][number] & {
  ItemID?: string;
  itemId?: string;
  AssetKey?: string;
  assetKey?: string;
  Name?: string;
  Weight?: number;
  Locked?: boolean;
};

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

function weightValue(w: LegacyOptimizationWeightEntry): number {
  return w.weight ?? w.Weight ?? 0;
}

function firstNonBlank(...values: Array<string | undefined>): string | null {
  for (const value of values) {
    if (value && value.trim().length > 0) return value;
  }
  return null;
}

function weightName(w: LegacyOptimizationWeightEntry): string {
  return firstNonBlank(w.name, w.Name, w.asset_key, w.AssetKey, w.assetKey) ?? "未命名资产";
}

function weightKey(w: LegacyOptimizationWeightEntry): string {
  return firstNonBlank(
    w.item_id,
    w.ItemID,
    w.itemId,
    w.asset_key,
    w.AssetKey,
    w.assetKey,
    w.name,
    w.Name,
  ) ?? "weight";
}

function weightItemID(w: LegacyOptimizationWeightEntry): string | null {
  return firstNonBlank(w.item_id, w.ItemID, w.itemId);
}

function weightAssetKey(w: LegacyOptimizationWeightEntry): string | null {
  return firstNonBlank(w.asset_key, w.AssetKey, w.assetKey);
}

function weightLocked(w: LegacyOptimizationWeightEntry): boolean {
  return w.locked ?? w.Locked ?? false;
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
    return <p className="py-4 text-center text-sm text-ink-muted">无结果</p>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[780px] text-sm" data-testid={`result-table-${tab}`}>
        <thead>
          <tr className="border-b border-line text-left text-xs text-ink-muted">
            <th className="px-2 py-2 font-medium">#</th>
            <th className="px-2 py-2 font-medium">得分</th>
            <th className="px-2 py-2 font-medium">年化收益</th>
            <th className="px-2 py-2 font-medium">累计收益</th>
            <th className="px-2 py-2 font-medium">最大回撤</th>
            <th className="px-2 py-2 font-medium">波动率</th>
            <th className="px-2 py-2 font-medium">
              <MetricHeader
                label="夏普比率"
                help="衡量单位波动风险带来的超额收益，数值越高代表风险调整后收益越好。"
              />
            </th>
            <th className="px-2 py-2 font-medium">
              <MetricHeader
                label="卡玛比率"
                help="衡量年化收益相对最大回撤的表现，数值越高代表在控制回撤下的收益更好。"
              />
            </th>
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

function MetricHeader({ label, help }: { label: string; help: string }) {
  return (
    <span className="inline-flex items-center gap-1 whitespace-nowrap">
      {label}
      <Tooltip content={help} contentClassName="max-w-64" contentTestId={`metric-help-${label}`}>
        <button
          type="button"
          className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-line text-[10px] text-ink-muted hover:border-brand hover:text-brand"
          aria-label={`${label}说明`}
        >
          ?
        </button>
      </Tooltip>
    </span>
  );
}

function WeightBar({
  weights,
}: {
  weights: ResearchOptimizationResultItem["weights"];
}) {
  const normalized = weights.map((w, index) => {
    const entry = w as LegacyOptimizationWeightEntry;
    return {
      key: `${weightKey(entry)}-${index}`,
      name: weightName(entry),
      weight: weightValue(entry),
      locked: weightLocked(entry),
    };
  });
  const active = normalized.filter((w) => w.weight > 0);
  if (active.length === 0) return <span className="text-ink-muted">—</span>;
  const title = active.map((w) => `${w.name}: ${formatPercent(w.weight)}`).join(" / ");

  return (
    <div className="max-w-[22rem] space-y-0.5" title={title}>
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
      return status === "queued" || status === "running" ? 2000 : false;
    },
  });

  const collectionQuery = useQuery({
    queryKey: ["research", "collection", collectionId],
    queryFn: () => getCollection(collectionId),
  });

  const opt = optQuery.data;
  const detail = collectionQuery.data;
  const collectionName = detail?.name ?? "研究集合";

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
      const patches = buildApplyPatches(detail, result);
      let latest: ResearchCollectionDetail | null = null;
      for (const patch of patches) {
        latest = await updateCollectionItem(collectionId, patch.itemId, patch.patch);
      }
      if (
        detail.start_policy !== "custom_range" ||
        detail.window_start !== opt.window_start ||
        detail.window_end !== opt.window_end
      ) {
        latest = await updateCollection(collectionId, {
          start_policy: "custom_range",
          window_start: opt.window_start,
          window_end: opt.window_end,
        });
      }
      return latest;
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
              </dl>
              <p className="text-xs text-warning">
                应用后会覆盖当前组合的启用、锁定、权重和回测区间设置。
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
  const itemIDsByAssetKey = new Map<string, string[]>();
  for (const item of detail.items) {
    const ids = itemIDsByAssetKey.get(item.asset_key) ?? [];
    ids.push(item.id);
    itemIDsByAssetKey.set(item.asset_key, ids);
  }

  for (const raw of result.weights) {
    const entry = raw as LegacyOptimizationWeightEntry;
    let itemId = weightItemID(entry);
    const weight = weightValue(entry);
    if (weight <= 0) continue;

    if (itemId) {
      if (!detailByID.has(itemId)) {
        throw new Error("调优结果与当前组合资产不一致，请重新运行调优。");
      }
    } else {
      const assetKey = weightAssetKey(entry);
      const ids = assetKey ? itemIDsByAssetKey.get(assetKey) ?? [] : [];
      if (ids.length !== 1) {
        throw new Error("调优结果与当前组合资产不一致，请重新运行调优。");
      }
      itemId = ids[0]!;
    }

    positive.set(itemId, weight);
  }

  for (const itemId of positive.keys()) {
    if (!Number.isFinite(positive.get(itemId)!)) {
      throw new Error("调优结果权重异常，请重新运行调优。");
    }
  }

  const sum = Array.from(positive.values()).reduce((s, v) => s + v, 0);
  if (Math.abs(sum - 1) > 1e-4) {
    throw new Error("调优结果权重合计异常，请重新运行调优。");
  }
  if (positive.size > 0 && Math.abs(sum - 1) > 1e-9) {
    const last = Array.from(positive.keys()).at(-1)!;
    positive.set(last, positive.get(last)! + (1 - sum));
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

function buildApplyPatches(
  detail: ResearchCollectionDetail,
  result: ResearchOptimizationResultItem,
): { itemId: string; patch: { enabled: boolean; weight: number; weight_locked: boolean } }[] {
  const positive = buildPositiveWeights(detail, result);
  const patches: { itemId: string; patch: { enabled: boolean; weight: number; weight_locked: boolean } }[] = [];
  for (const item of detail.items) {
    const weight = positive.get(item.id) ?? 0;
    const patch = {
      enabled: weight > 0,
      weight,
      weight_locked: weight > 0,
    };
    if (
      item.enabled !== patch.enabled ||
      Math.abs(item.weight - patch.weight) > 1e-9 ||
      item.weight_locked !== patch.weight_locked
    ) {
      patches.push({ itemId: item.id, patch });
    }
  }
  return patches;
}
