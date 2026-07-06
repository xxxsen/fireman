"use client";

import { useMemo, useState } from "react";
import {
  type ResearchCollectionDetail,
  type ResearchCollectionItemView,
  type ResearchItemUpdate,
  type ResearchReadiness,
  type ResearchReadinessAssetView,
} from "@/lib/api/research";
import { formatPercent, instrumentTypeLabel } from "@/lib/format";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";

const ADJUST_POLICY_OPTIONS = [
  { value: "qfq", label: "前复权" },
  { value: "none", label: "不复权" },
];

const POINT_TYPE_OPTIONS = [
  { value: "adjusted_close", label: "复权收盘价" },
  { value: "nav", label: "单位净值" },
  { value: "total_return_index", label: "累计净值" },
];

/** Weight edits produced by batch tools, applied by the parent. */
export type WeightUpdates = { itemId: string; weight: number }[];

export interface WeightEditorProps {
  detail: ResearchCollectionDetail;
  readiness?: ResearchReadiness;
  pending: boolean;
  onUpdateItem: (itemId: string, patch: ResearchItemUpdate) => void;
  onDeleteItem: (itemId: string) => void;
  onApplyWeights: (updates: WeightUpdates) => void;
  onNormalize: () => void;
  onReorder: (orderedItemIds: string[]) => void;
  onAddAsset: () => void;
}

function round6(v: number): number {
  return Math.round(v * 1e6) / 1e6;
}

/** Equal weights that sum exactly to 1 (last item takes the remainder). */
export function equalWeights(count: number): number[] {
  if (count <= 0) return [];
  const each = round6(1 / count);
  const weights = Array(count).fill(each) as number[];
  weights[count - 1] = round6(1 - each * (count - 1));
  return weights;
}

/** Equal weight per group, then equal split within each group. */
export function groupEqualWeights(
  items: { id: string; group: string }[],
): Map<string, number> {
  const out = new Map<string, number>();
  const groups = new Map<string, string[]>();
  for (const item of items) {
    const list = groups.get(item.group) ?? [];
    list.push(item.id);
    groups.set(item.group, list);
  }
  const groupCount = groups.size;
  if (groupCount === 0) return out;
  const perGroup = 1 / groupCount;
  for (const ids of groups.values()) {
    const each = round6(perGroup / ids.length);
    for (const id of ids) out.set(id, each);
  }
  // Fix rounding drift on an arbitrary item so the sum is exactly 1.
  const sum = Array.from(out.values()).reduce((s, v) => s + v, 0);
  const drift = round6(1 - sum);
  if (drift !== 0 && items.length > 0) {
    const lastID = items[items.length - 1]!.id;
    out.set(lastID, round6((out.get(lastID) ?? 0) + drift));
  }
  return out;
}

/** Distribute the gap to 100% equally among unlocked enabled items. */
export function distributeRemainder(
  items: { id: string; weight: number; locked: boolean }[],
): Map<string, number> {
  const out = new Map<string, number>();
  const total = items.reduce((s, it) => s + it.weight, 0);
  const remainder = 1 - total;
  const unlocked = items.filter((it) => !it.locked);
  if (unlocked.length === 0 || Math.abs(remainder) < 1e-9) return out;
  const each = remainder / unlocked.length;
  for (const it of unlocked) {
    out.set(it.id, round6(Math.max(0, it.weight + each)));
  }
  return out;
}

function WeightInput({
  value,
  disabled,
  onCommit,
}: {
  value: number;
  disabled: boolean;
  onCommit: (weight: number) => void;
}) {
  const display = round6(value * 100);
  const [draft, setDraft] = useState<string | null>(null);

  function commit() {
    if (draft === null) return;
    const n = Number(draft);
    setDraft(null);
    if (!Number.isFinite(n) || n < 0) return;
    const next = round6(n / 100);
    if (next !== value) onCommit(next);
  }

  return (
    <span className="flex items-center gap-1">
      <input
        type="number"
        step="0.01"
        min="0"
        value={draft ?? String(display)}
        disabled={disabled}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") (e.target as HTMLInputElement).blur();
        }}
        className="w-20 rounded-md border border-line bg-surface px-2 py-1 text-right font-mono-numeric text-sm text-ink focus:border-brand focus:outline-none disabled:bg-surface-muted disabled:text-ink-muted"
        aria-label="权重百分比"
      />
      <span className="text-xs text-ink-muted">%</span>
    </span>
  );
}

function dataStatusBadge(view: ResearchReadinessAssetView | undefined, isCash: boolean) {
  if (isCash) return <Badge variant="positive">现金（无需历史）</Badge>;
  if (!view) return <Badge variant="neutral">—</Badge>;
  if (view.sync_status === "pending" || view.sync_status === "running") {
    return <Badge variant="info">同步中</Badge>;
  }
  if (!view.has_history) {
    return <Badge variant="danger">缺历史</Badge>;
  }
  if (view.sync_status === "failed") {
    return <Badge variant="danger">同步失败</Badge>;
  }
  if (view.stale) return <Badge variant="warning">数据过期</Badge>;
  if (view.listing_status && view.listing_status !== "active") {
    return <Badge variant="warning">停更/退市</Badge>;
  }
  return <Badge variant="positive">正常</Badge>;
}

export function WeightEditor({
  detail,
  readiness,
  pending,
  onUpdateItem,
  onDeleteItem,
  onApplyWeights,
  onNormalize,
  onReorder,
  onAddAsset,
}: WeightEditorProps) {
  const [dragId, setDragId] = useState<string | null>(null);

  const items = detail.items;
  const enabled = useMemo(() => items.filter((it) => it.enabled), [items]);
  const readinessByItem = useMemo(() => {
    const map = new Map<string, ResearchReadinessAssetView>();
    for (const a of readiness?.assets ?? []) map.set(a.item_id, a);
    return map;
  }, [readiness]);

  const summary = useMemo(() => {
    const weightSum = enabled.reduce((s, it) => s + it.weight, 0);
    const byCurrency = new Map<string, number>();
    const byMarket = new Map<string, number>();
    const byType = new Map<string, number>();
    let maxWeight = 0;
    let maxWeightName = "";
    for (const it of enabled) {
      byCurrency.set(it.currency, (byCurrency.get(it.currency) ?? 0) + it.weight);
      byMarket.set(it.market, (byMarket.get(it.market) ?? 0) + it.weight);
      const typeLabel = it.instrument_type_label || instrumentTypeLabel(it.instrument_type);
      byType.set(typeLabel, (byType.get(typeLabel) ?? 0) + it.weight);
      if (it.weight > maxWeight) {
        maxWeight = it.weight;
        maxWeightName = it.name;
      }
    }
    let missingHistory = 0;
    let fxMissing = 0;
    if (readiness) {
      missingHistory = readiness.data_dependencies.missing_history_count;
      fxMissing = readiness.blocking_reasons.filter((r) => r.reason === "fx_missing").length;
    }
    return { weightSum, byCurrency, byMarket, byType, maxWeight, maxWeightName, missingHistory, fxMissing };
  }, [enabled, readiness]);

  const weightValid = Math.abs(summary.weightSum - 1) <= 1e-6;

  function applyEqualWeight() {
    const weights = equalWeights(enabled.length);
    onApplyWeights(enabled.map((it, idx) => ({ itemId: it.id, weight: weights[idx]! })));
  }

  function applyGroupEqual(groupOf: (it: ResearchCollectionItemView) => string) {
    const updates = groupEqualWeights(
      enabled.map((it) => ({ id: it.id, group: groupOf(it) || "其他" })),
    );
    onApplyWeights(
      Array.from(updates.entries()).map(([itemId, weight]) => ({ itemId, weight })),
    );
  }

  function applyRemainder() {
    const updates = distributeRemainder(
      enabled.map((it) => ({ id: it.id, weight: it.weight, locked: it.weight_locked })),
    );
    if (updates.size === 0) return;
    onApplyWeights(
      Array.from(updates.entries()).map(([itemId, weight]) => ({ itemId, weight })),
    );
  }

  function handleDrop(targetId: string) {
    if (!dragId || dragId === targetId) {
      setDragId(null);
      return;
    }
    const ids = items.map((it) => it.id);
    const from = ids.indexOf(dragId);
    const to = ids.indexOf(targetId);
    if (from === -1 || to === -1) {
      setDragId(null);
      return;
    }
    ids.splice(from, 1);
    ids.splice(to, 0, dragId);
    setDragId(null);
    onReorder(ids);
  }

  function exposureLine(map: Map<string, number>): string {
    if (map.size === 0) return "—";
    return Array.from(map.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([k, v]) => `${k} ${formatPercent(v)}`)
      .join(" · ");
  }

  return (
    <section className="rounded-lg border border-line bg-surface p-4" data-testid="weight-editor">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-base font-semibold text-ink">资产与权重</h2>
        <div className="flex flex-wrap gap-1.5">
          <Button variant="secondary" onClick={onAddAsset} data-testid="add-asset">
            添加资产
          </Button>
          <Button
            variant="secondary"
            disabled={pending || enabled.length === 0}
            onClick={applyEqualWeight}
            data-testid="equal-weight"
          >
            等权
          </Button>
          <Button
            variant="secondary"
            disabled={pending || enabled.length === 0}
            onClick={() => applyGroupEqual((it) => it.asset_class)}
            data-testid="equal-by-class"
          >
            按类别等权
          </Button>
          <Button
            variant="secondary"
            disabled={pending || enabled.length === 0}
            onClick={() => applyGroupEqual((it) => it.market)}
            data-testid="equal-by-market"
          >
            按市场等权
          </Button>
          <Button
            variant="secondary"
            disabled={pending || enabled.length === 0 || weightValid}
            onClick={applyRemainder}
            data-testid="distribute-remainder"
          >
            剩余分配
          </Button>
          <Button
            variant="secondary"
            disabled={pending || enabled.length === 0}
            onClick={onNormalize}
            data-testid="normalize-weights"
          >
            锁定归一化
          </Button>
        </div>
      </div>

      {items.length === 0 ? (
        <p className="rounded-md border border-dashed border-line px-4 py-8 text-center text-sm text-ink-muted">
          集合还没有资产，点击「添加资产」或从筛选器加入。
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[980px] text-sm" data-testid="weight-table">
            <thead>
              <tr className="border-b border-line text-left text-xs text-ink-muted">
                <th className="px-2 py-2 font-medium">启用</th>
                <th className="px-2 py-2 font-medium">资产</th>
                <th className="px-2 py-2 font-medium">权重</th>
                <th className="px-2 py-2 font-medium">锁定</th>
                <th className="px-2 py-2 font-medium">本币</th>
                <th className="px-2 py-2 font-medium">类型 / 口径</th>
                <th className="px-2 py-2 font-medium">数据截至</th>
                <th className="px-2 py-2 font-medium">历史起点</th>
                <th className="px-2 py-2 font-medium">历史终点</th>
                <th className="px-2 py-2 font-medium">数据状态</th>
                <th className="px-2 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((it) => {
                const rv = readinessByItem.get(it.id);
                return (
                  <tr
                    key={it.id}
                    draggable
                    onDragStart={() => setDragId(it.id)}
                    onDragOver={(e) => e.preventDefault()}
                    onDrop={() => handleDrop(it.id)}
                    className={
                      "border-b border-line/60 last:border-0 " +
                      (dragId === it.id ? "opacity-50 " : "") +
                      (it.enabled ? "" : "bg-surface-muted/40 text-ink-muted")
                    }
                    data-testid={`item-row-${it.id}`}
                  >
                    <td className="px-2 py-2">
                      <input
                        type="checkbox"
                        checked={it.enabled}
                        disabled={pending}
                        onChange={(e) => onUpdateItem(it.id, { enabled: e.target.checked })}
                        aria-label={`启用 ${it.name}`}
                      />
                    </td>
                    <td className="px-2 py-2">
                      <span className="flex items-center gap-1.5">
                        <span className="cursor-grab text-ink-muted" aria-hidden>
                          ⠿
                        </span>
                        <span className="min-w-0">
                          <span className="block max-w-44 truncate font-medium text-ink">
                            {it.name}
                            {it.is_cash && (
                              <Badge variant="neutral" className="ml-1.5">
                                现金
                              </Badge>
                            )}
                          </span>
                          <span className="block text-xs text-ink-muted">{it.symbol || it.asset_key}</span>
                        </span>
                      </span>
                    </td>
                    <td className="px-2 py-2">
                      <WeightInput
                        value={it.weight}
                        disabled={pending || !it.enabled}
                        onCommit={(weight) => onUpdateItem(it.id, { weight })}
                      />
                    </td>
                    <td className="px-2 py-2">
                      <input
                        type="checkbox"
                        checked={it.weight_locked}
                        disabled={pending || !it.enabled}
                        onChange={(e) => onUpdateItem(it.id, { weight_locked: e.target.checked })}
                        aria-label={`锁定 ${it.name} 权重`}
                      />
                    </td>
                    <td className="px-2 py-2 text-xs">{it.currency}</td>
                    <td className="px-2 py-2">
                      <span className="block text-xs">
                        {it.instrument_type_label || instrumentTypeLabel(it.instrument_type)}
                      </span>
                      {!it.is_cash && (
                        <span className="mt-0.5 flex gap-1">
                          <select
                            value={it.adjust_policy}
                            disabled={pending}
                            onChange={(e) => onUpdateItem(it.id, { adjust_policy: e.target.value })}
                            className="rounded border border-line bg-surface px-1 py-0.5 text-xs text-ink-muted"
                            aria-label="复权口径"
                          >
                            {ADJUST_POLICY_OPTIONS.map((o) => (
                              <option key={o.value} value={o.value}>
                                {o.label}
                              </option>
                            ))}
                          </select>
                          <select
                            value={it.point_type}
                            disabled={pending}
                            onChange={(e) => onUpdateItem(it.id, { point_type: e.target.value })}
                            className="rounded border border-line bg-surface px-1 py-0.5 text-xs text-ink-muted"
                            aria-label="点位类型"
                          >
                            {POINT_TYPE_OPTIONS.map((o) => (
                              <option key={o.value} value={o.value}>
                                {o.label}
                              </option>
                            ))}
                          </select>
                        </span>
                      )}
                    </td>
                    <td className="px-2 py-2 font-mono-numeric text-xs">{rv?.data_as_of ?? "—"}</td>
                    <td className="px-2 py-2 font-mono-numeric text-xs">{rv?.history_start ?? "—"}</td>
                    <td className="px-2 py-2 font-mono-numeric text-xs">
                      {rv?.history_end ?? "—"}
                      {rv?.limits_common_start && (
                        <span className="ml-1 text-warning" title="该资产决定了共同起点">
                          ⤒
                        </span>
                      )}
                      {rv?.limits_common_end && (
                        <span className="ml-1 text-warning" title="该资产决定了共同终点">
                          ⤓
                        </span>
                      )}
                    </td>
                    <td className="px-2 py-2">{dataStatusBadge(rv, it.is_cash)}</td>
                    <td className="px-2 py-2 text-right">
                      <button
                        type="button"
                        disabled={pending}
                        onClick={() => onDeleteItem(it.id)}
                        className="text-xs text-ink-muted hover:text-danger disabled:opacity-50"
                        data-testid={`delete-item-${it.id}`}
                      >
                        移除
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <dl
        className="mt-4 grid gap-x-6 gap-y-1.5 rounded-md bg-surface-muted/60 px-4 py-3 text-xs sm:grid-cols-2"
        data-testid="weight-summary"
      >
        <div className="flex justify-between gap-3">
          <dt className="text-ink-muted">权重合计</dt>
          <dd
            className={weightValid ? "font-medium text-positive" : "font-medium text-warning"}
            data-testid="weight-sum"
          >
            {formatPercent(summary.weightSum)}
            {!weightValid && enabled.length > 0 && (
              <span className="ml-1">（差 {formatPercent(1 - summary.weightSum)}）</span>
            )}
          </dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-ink-muted">单资产最大权重</dt>
          <dd className="text-ink">
            {summary.maxWeight > 0
              ? `${summary.maxWeightName} ${formatPercent(summary.maxWeight)}`
              : "—"}
          </dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-ink-muted">币种暴露</dt>
          <dd className="text-ink">{exposureLine(summary.byCurrency)}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-ink-muted">市场暴露</dt>
          <dd className="text-ink">{exposureLine(summary.byMarket)}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-ink-muted">资产类型暴露</dt>
          <dd className="text-ink">{exposureLine(summary.byType)}</dd>
        </div>
        <div className="flex justify-between gap-3">
          <dt className="text-ink-muted">缺历史 / 缺 FX</dt>
          <dd className="text-ink">
            {summary.missingHistory} / {summary.fxMissing}
          </dd>
        </div>
        <div className="flex justify-between gap-3 sm:col-span-2">
          <dt className="text-ink-muted">共同区间预估</dt>
          <dd className="text-ink" data-testid="common-window">
            {readiness?.common_start && readiness.common_end
              ? `${readiness.common_start} ~ ${readiness.common_end}`
              : "待数据就绪后计算"}
          </dd>
        </div>
      </dl>
    </section>
  );
}
