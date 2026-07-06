"use client";

import { useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { createCollection, type ResearchAssetView } from "@/lib/api/research";
import {
  currencyDistribution,
  estimateCommonWindow,
} from "@/lib/research/candidate-analysis";
import { queryErrorMessage } from "@/lib/query-error";
import { equalWeights } from "@/components/research/WeightEditor";
import { Button } from "@/components/ui/Button";

export interface CandidatePoolPanelProps {
  candidates: ResearchAssetView[];
  averageCorrelation?: number | null;
  onRemove: (assetKey: string) => void;
  onClear: () => void;
  onCompare: () => void;
}

export function CandidatePoolPanel({
  candidates,
  averageCorrelation,
  onRemove,
  onClear,
  onCompare,
}: CandidatePoolPanelProps) {
  const router = useRouter();
  const [name, setName] = useState("");

  const distribution = useMemo(() => currencyDistribution(candidates), [candidates]);
  const window = useMemo(() => estimateCommonWindow(candidates), [candidates]);

  const createMutation = useMutation({
    mutationFn: () => {
      // Last item absorbs rounding drift so the sum is exactly 1.
      const weights = equalWeights(candidates.length);
      return createCollection({
        name: name.trim() || "候选组合",
        items: candidates.map((c, idx) => ({
          asset_key: c.asset_key,
          weight: weights[idx]!,
          enabled: true,
          adjust_policy: c.adjust_policy,
          point_type: c.point_type,
          sort_order: idx,
        })),
      });
    },
    onSuccess: (detail) => {
      router.push(`/research/collections/${detail.id}`);
    },
  });

  if (candidates.length === 0) {
    return (
      <div
        className="rounded-lg border border-dashed border-line px-4 py-6 text-center text-sm text-ink-muted"
        data-testid="candidate-pool-empty"
      >
        候选池为空。
        <br />
        在结果表中点击「加入候选」开始比较。
      </div>
    );
  }

  return (
    <div className="space-y-4" data-testid="candidate-pool">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-ink">
          候选池
          <span className="ml-1.5 rounded-full bg-brand/10 px-2 py-0.5 text-xs font-medium text-brand">
            {candidates.length}
          </span>
        </h3>
        <button
          type="button"
          onClick={onClear}
          className="text-xs text-ink-muted underline-offset-2 hover:text-ink hover:underline"
        >
          清空
        </button>
      </div>

      <ul className="max-h-64 space-y-1 overflow-y-auto">
        {candidates.map((c) => (
          <li
            key={c.asset_key}
            className="flex items-center gap-2 rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm"
          >
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium text-ink">{c.name}</span>
              <span className="block truncate text-xs text-ink-muted">
                {c.symbol} · {c.currency}
              </span>
            </span>
            <button
              type="button"
              onClick={() => onRemove(c.asset_key)}
              className="shrink-0 rounded px-1.5 py-0.5 text-xs text-ink-muted hover:bg-surface-muted hover:text-danger"
              aria-label={`移除 ${c.name}`}
            >
              移除
            </button>
          </li>
        ))}
      </ul>

      <dl className="space-y-1.5 rounded-md bg-surface-muted/60 px-3 py-2.5 text-xs">
        <div className="flex justify-between">
          <dt className="text-ink-muted">币种分布</dt>
          <dd className="text-ink">
            {Object.entries(distribution)
              .map(([cur, n]) => `${cur}×${n}`)
              .join("，")}
          </dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-ink-muted">共同区间预估</dt>
          <dd className="text-ink" data-testid="common-window-estimate">
            {window ? `${window.start} ~ ${window.end}` : "无法预估（存在缺历史资产）"}
          </dd>
        </div>
        <div className="flex justify-between">
          <dt className="text-ink-muted">平均相关性</dt>
          <dd className="text-ink" data-testid="avg-correlation">
            {averageCorrelation != null ? averageCorrelation.toFixed(2) : "打开比较后计算"}
          </dd>
        </div>
      </dl>

      <Button
        variant="secondary"
        className="w-full"
        disabled={candidates.length < 2}
        onClick={onCompare}
        data-testid="compare-candidates"
      >
        候选比较
      </Button>

      <div className="space-y-2">
        <input
          type="text"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="新集合名称（默认「候选组合」）"
          className="w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
          data-testid="candidate-collection-name"
        />
        <Button
          className="w-full"
          pending={createMutation.isPending}
          onClick={() => createMutation.mutate()}
          data-testid="create-collection-from-pool"
        >
          创建集合（等权 {candidates.length} 只）
        </Button>
        {createMutation.isError && (
          <p className="text-xs text-danger" role="alert">
            创建失败：{queryErrorMessage(createMutation.error)}
          </p>
        )}
      </div>
    </div>
  );
}
