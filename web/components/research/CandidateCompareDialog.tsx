"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useQueries } from "@tanstack/react-query";
import ReactECharts from "echarts-for-react";
import { getMarketAssetDetail } from "@/lib/api/market-assets";
import type { ResearchAssetView } from "@/lib/api/research";
import {
  averageCorrelation,
  annualReturnMatrix,
  correlationMatrix,
  normalizeCandidates,
  type NormalizedSeries,
} from "@/lib/research/candidate-analysis";
import { formatNullablePercent, formatPercent } from "@/lib/format";
import { downloadCsv } from "@/lib/csv";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { LoadingState } from "@/components/ui/LoadingState";

type SortKey =
  | "return_1y"
  | "return_3y"
  | "return_5y"
  | "cagr"
  | "annual_volatility"
  | "max_drawdown"
  | "sharpe"
  | "calmar";

const METRIC_COLUMNS: { key: SortKey; label: string }[] = [
  { key: "return_1y", label: "近 1 年" },
  { key: "return_3y", label: "近 3 年" },
  { key: "return_5y", label: "近 5 年" },
  { key: "cagr", label: "CAGR" },
  { key: "annual_volatility", label: "波动率" },
  { key: "max_drawdown", label: "最大回撤" },
  { key: "sharpe", label: "Sharpe" },
  { key: "calmar", label: "Calmar" },
];

const RATIO_KEYS: SortKey[] = ["sharpe", "calmar"];

function metricValue(c: ResearchAssetView, key: SortKey): number | null {
  const v = c.metrics?.[key];
  return v == null || Number.isNaN(v) ? null : v;
}

function LinesChart({
  normalized,
  mode,
}: {
  normalized: NormalizedSeries[];
  mode: "nav" | "drawdown";
}) {
  if (normalized.length === 0) {
    return (
      <div className="flex h-56 items-center justify-center text-sm text-ink-muted">
        无共同区间数据，无法绘制曲线。
      </div>
    );
  }
  const dates = normalized[0]!.dates;
  const option = {
    tooltip: { trigger: "axis" as const },
    legend: { type: "scroll" as const, top: 0 },
    grid: { left: 56, right: 16, bottom: 32, top: 36 },
    xAxis: { type: "category" as const, data: dates, boundaryGap: false },
    yAxis: {
      type: "value" as const,
      scale: true,
      axisLabel: {
        formatter: (v: number) =>
          mode === "nav" ? v.toFixed(2) : `${(v * 100).toFixed(0)}%`,
      },
    },
    series: normalized.map((s) => ({
      name: s.name,
      type: "line" as const,
      data: mode === "nav" ? s.navs : s.drawdowns,
      symbol: "none",
      lineStyle: { width: 1.5 },
    })),
  };
  return <ReactECharts option={option} style={{ height: 280 }} notMerge />;
}

export interface CandidateCompareDialogProps {
  open: boolean;
  onClose: () => void;
  candidates: ResearchAssetView[];
  onRemove: (assetKey: string) => void;
  onAverageCorrelation?: (value: number | null) => void;
  /** Multi-select action: add the checked assets to a collection (td/099 §3.2). */
  onAddSelected?: (assets: ResearchAssetView[]) => void;
  addSelectedLabel?: string;
}

export function CandidateCompareDialog({
  open,
  onClose,
  candidates,
  onRemove,
  onAverageCorrelation,
  onAddSelected,
  addSelectedLabel = "将选中加入集合",
}: CandidateCompareDialogProps) {
  const [sortKey, setSortKey] = useState<SortKey>("cagr");
  const [sortDesc, setSortDesc] = useState(true);
  const [chartTab, setChartTab] = useState<"nav" | "drawdown">("nav");
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());

  function toggleSelected(assetKey: string) {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(assetKey)) {
        next.delete(assetKey);
      } else {
        next.add(assetKey);
      }
      return next;
    });
  }

  const nonCash = useMemo(() => candidates.filter((c) => !c.is_cash), [candidates]);

  const details = useQueries({
    queries: nonCash.map((c) => ({
      queryKey: ["market-asset-detail", c.asset_key, c.adjust_policy, c.point_type],
      queryFn: () =>
        getMarketAssetDetail(c.asset_key, {
          adjustPolicy: c.adjust_policy,
          pointType: c.point_type,
        }),
      enabled: open && c.has_history,
      staleTime: 5 * 60 * 1000,
    })),
    combine: (results) => ({
      loading: results.some((r) => r.isLoading),
      data: results.map((r) => r.data),
    }),
  });

  const loading = open && details.loading;

  const normalized = useMemo(() => {
    if (loading) return [];
    const series = nonCash
      .map((c, idx) => {
        const detail = details.data[idx];
        if (!detail || detail.points.length === 0) return null;
        return { assetKey: c.asset_key, name: c.name, points: detail.points };
      })
      .filter((s): s is NonNullable<typeof s> => s !== null);
    if (series.length < 2) return [];
    return normalizeCandidates(series);
  }, [loading, nonCash, details.data]);

  const corrMatrix = useMemo(() => correlationMatrix(normalized), [normalized]);
  const avgCorr = useMemo(() => averageCorrelation(corrMatrix), [corrMatrix]);
  const annual = useMemo(() => annualReturnMatrix(normalized), [normalized]);

  useEffect(() => {
    if (open && normalized.length >= 2) {
      onAverageCorrelation?.(avgCorr);
    }
  }, [open, normalized.length, avgCorr, onAverageCorrelation]);

  const sorted = useMemo(() => {
    return [...candidates].sort((a, b) => {
      const va = metricValue(a, sortKey);
      const vb = metricValue(b, sortKey);
      if (va == null && vb == null) return 0;
      if (va == null) return 1;
      if (vb == null) return -1;
      return sortDesc ? vb - va : va - vb;
    });
  }, [candidates, sortKey, sortDesc]);

  function exportCSV() {
    const headers = [
      "资产",
      "代码",
      "币种",
      "数据区间",
      "来源",
      ...METRIC_COLUMNS.map((c) => c.label),
    ];
    const rows = sorted.map((c) => [
      c.name,
      c.symbol,
      c.currency,
      c.metrics ? `${c.metrics.start_date}~${c.metrics.end_date}` : "",
      c.history_source ?? "",
      ...METRIC_COLUMNS.map((col) => {
        const v = metricValue(c, col.key);
        if (v == null) return "";
        return RATIO_KEYS.includes(col.key) ? v.toFixed(3) : (v * 100).toFixed(2) + "%";
      }),
    ]);
    downloadCsv("research-candidates.csv", headers, rows);
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={`候选比较（${candidates.length} 只）`}
      className="max-w-5xl"
      footer={
        <div className="flex flex-wrap justify-between gap-2">
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={exportCSV} data-testid="compare-export-csv">
              导出 CSV
            </Button>
            {onAddSelected && (
              <Button
                variant="secondary"
                disabled={selectedKeys.size === 0}
                onClick={() =>
                  onAddSelected(candidates.filter((c) => selectedKeys.has(c.asset_key)))
                }
                data-testid="compare-add-selected"
              >
                {addSelectedLabel}（{selectedKeys.size}）
              </Button>
            )}
          </div>
          <Button variant="secondary" onClick={onClose}>
            关闭
          </Button>
        </div>
      }
    >
      <div className="space-y-6">
        <section>
          <div className="mb-2 flex items-center gap-2">
            <h4 className="text-sm font-semibold text-ink">
              {chartTab === "nav" ? "归一化收益曲线" : "回撤曲线"}
            </h4>
            <div className="flex rounded-md border border-line text-xs">
              <button
                type="button"
                onClick={() => setChartTab("nav")}
                className={
                  chartTab === "nav"
                    ? "rounded-l-md bg-brand px-2.5 py-1 font-medium text-surface"
                    : "rounded-l-md px-2.5 py-1 text-ink-muted hover:bg-surface-muted"
                }
              >
                收益
              </button>
              <button
                type="button"
                onClick={() => setChartTab("drawdown")}
                className={
                  chartTab === "drawdown"
                    ? "rounded-r-md bg-brand px-2.5 py-1 font-medium text-surface"
                    : "rounded-r-md px-2.5 py-1 text-ink-muted hover:bg-surface-muted"
                }
              >
                回撤
              </button>
            </div>
            {avgCorr != null && (
              <span className="ml-auto text-xs text-ink-muted">
                平均相关性 <span className="font-medium text-ink">{avgCorr.toFixed(2)}</span>
              </span>
            )}
          </div>
          {loading ? (
            <LoadingState label="加载候选历史数据…" className="py-16" />
          ) : (
            <LinesChart normalized={normalized} mode={chartTab} />
          )}
        </section>

        <section>
          <h4 className="mb-2 text-sm font-semibold text-ink">指标对比</h4>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-sm" data-testid="compare-metrics-table">
              <thead>
                <tr className="border-b border-line text-left text-xs text-ink-muted">
                  {onAddSelected && (
                    <th className="w-8 px-2 py-1.5">
                      <input
                        type="checkbox"
                        aria-label="全选候选资产"
                        checked={selectedKeys.size === candidates.length && candidates.length > 0}
                        onChange={(e) =>
                          setSelectedKeys(
                            e.target.checked
                              ? new Set(candidates.map((c) => c.asset_key))
                              : new Set(),
                          )
                        }
                        data-testid="compare-select-all"
                      />
                    </th>
                  )}
                  <th className="px-2 py-1.5 font-medium">资产</th>
                  {METRIC_COLUMNS.map((col) => (
                    <th key={col.key} className="px-2 py-1.5 font-medium">
                      <button
                        type="button"
                        onClick={() => {
                          if (sortKey === col.key) {
                            setSortDesc(!sortDesc);
                          } else {
                            setSortKey(col.key);
                            setSortDesc(true);
                          }
                        }}
                        className="inline-flex items-center gap-0.5 hover:text-ink"
                      >
                        {col.label}
                        {sortKey === col.key && <span>{sortDesc ? "↓" : "↑"}</span>}
                      </button>
                    </th>
                  ))}
                  <th className="px-2 py-1.5 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {sorted.map((c) => (
                  <tr key={c.asset_key} className="border-b border-line/60 last:border-0">
                    {onAddSelected && (
                      <td className="px-2 py-1.5">
                        <input
                          type="checkbox"
                          aria-label={`选择 ${c.name}`}
                          checked={selectedKeys.has(c.asset_key)}
                          onChange={() => toggleSelected(c.asset_key)}
                          data-testid={`compare-select-${c.asset_key}`}
                        />
                      </td>
                    )}
                    <td className="px-2 py-1.5">
                      <span className="block max-w-40 truncate font-medium text-ink">{c.name}</span>
                      <span className="block text-xs text-ink-muted">
                        {c.symbol} · {c.currency}
                        {c.metrics && (
                          <> · {c.metrics.start_date}~{c.metrics.end_date}</>
                        )}
                      </span>
                    </td>
                    {METRIC_COLUMNS.map((col) => {
                      const v = metricValue(c, col.key);
                      return (
                        <td key={col.key} className="px-2 py-1.5 font-mono-numeric text-xs">
                          {v == null
                            ? "—"
                            : RATIO_KEYS.includes(col.key)
                              ? v.toFixed(2)
                              : formatPercent(v)}
                        </td>
                      );
                    })}
                    <td className="px-2 py-1.5">
                      <span className="flex items-center gap-1.5 text-xs">
                        <Link
                          href={`/assets/market/${encodeURIComponent(c.asset_key)}`}
                          className="text-brand underline-offset-2 hover:underline"
                        >
                          详情
                        </Link>
                        <button
                          type="button"
                          onClick={() => onRemove(c.asset_key)}
                          className="text-ink-muted hover:text-danger"
                        >
                          移除
                        </button>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        {annual.years.length > 0 && (
          <section>
            <h4 className="mb-2 text-sm font-semibold text-ink">年度收益矩阵</h4>
            <div className="overflow-x-auto">
              <table className="w-full text-xs" data-testid="compare-annual-matrix">
                <thead>
                  <tr className="border-b border-line text-left text-ink-muted">
                    <th className="px-2 py-1.5 font-medium">资产</th>
                    {annual.years.map((y) => (
                      <th key={y} className="px-2 py-1.5 text-right font-medium">
                        {y}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {normalized.map((s, i) => (
                    <tr key={s.assetKey} className="border-b border-line/60 last:border-0">
                      <td className="max-w-40 truncate px-2 py-1.5 font-medium text-ink">{s.name}</td>
                      {annual.rows[i]?.map((v, j) => (
                        <td
                          key={annual.years[j]}
                          className={
                            "px-2 py-1.5 text-right font-mono-numeric " +
                            (v == null ? "text-ink-muted" : v >= 0 ? "text-positive" : "text-danger")
                          }
                        >
                          {formatNullablePercent(v)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {normalized.length >= 2 && (
          <section>
            <h4 className="mb-2 text-sm font-semibold text-ink">相关系数</h4>
            <div className="overflow-x-auto">
              <table className="text-xs" data-testid="compare-correlation-matrix">
                <thead>
                  <tr className="text-left text-ink-muted">
                    <th className="px-2 py-1.5"></th>
                    {normalized.map((s) => (
                      <th key={s.assetKey} className="max-w-24 truncate px-2 py-1.5 font-medium">
                        {s.name}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {normalized.map((s, i) => (
                    <tr key={s.assetKey}>
                      <td className="max-w-24 truncate px-2 py-1.5 font-medium text-ink">{s.name}</td>
                      {corrMatrix[i]?.map((v, j) => (
                        <td key={j} className="px-2 py-1.5 text-center font-mono-numeric">
                          {v == null ? "—" : v.toFixed(2)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}
      </div>
    </Dialog>
  );
}
