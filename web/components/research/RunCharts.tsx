"use client";

import { useMemo, useState } from "react";
import ReactECharts from "echarts-for-react";
import type { ResearchRunPoint, ResearchRunSummary } from "@/lib/api/research";
import { monthlyDownsample } from "@/lib/research/run-analysis";
import type { NormalizedSeries } from "@/lib/research/candidate-analysis";
import { formatPercent } from "@/lib/format";

type ScaleMode = "linear" | "log";
type FreqMode = "daily" | "monthly";

interface TooltipParam {
  dataIndex?: number;
  axisValue?: string;
}

export interface RunChartsProps {
  points: ResearchRunPoint[];
  summary?: ResearchRunSummary;
  /** asset_key -> display name, for hover contribution lines. */
  assetNames: Record<string, string>;
  /** Optional per-asset normalized curves (loaded on demand). */
  assetSeries?: NormalizedSeries[];
  showAssetSeries: boolean;
  onToggleAssetSeries: (show: boolean) => void;
  assetSeriesLoading?: boolean;
  hasBenchmark: boolean;
}

function toggleCls(active: boolean, position: "l" | "m" | "r"): string {
  const rounded =
    position === "l" ? "rounded-l-md" : position === "r" ? "rounded-r-md" : "";
  return active
    ? `${rounded} bg-brand px-2.5 py-1 text-xs font-medium text-surface`
    : `${rounded} px-2.5 py-1 text-xs text-ink-muted hover:bg-surface-muted`;
}

export function RunCharts({
  points,
  summary,
  assetNames,
  assetSeries,
  showAssetSeries,
  onToggleAssetSeries,
  assetSeriesLoading,
  hasBenchmark,
}: RunChartsProps) {
  const [scale, setScale] = useState<ScaleMode>("linear");
  const [freq, setFreq] = useState<FreqMode>("daily");

  const displayPoints = useMemo(
    () => (freq === "monthly" ? monthlyDownsample(points) : points),
    [points, freq],
  );

  const option = useMemo(() => {
    const dates = displayPoints.map((p) => p.date);
    const byDate = new Map(displayPoints.map((p) => [p.date, p]));

    const formatter = (params: TooltipParam | TooltipParam[]): string => {
      const items = Array.isArray(params) ? params : [params];
      const date = items[0]?.axisValue ?? "";
      const p = byDate.get(date);
      if (!p) return date;
      const lines = [
        `<b>${date}</b>`,
        `组合净值 ${p.nav.toFixed(4)}（累计 ${formatPercent(p.cumulative_return)}）`,
        `回撤 ${formatPercent(p.drawdown)}`,
      ];
      if (p.benchmark_return != null) {
        lines.push(`基准累计 ${formatPercent(p.benchmark_return)}`);
      }
      if (p.weights && Object.keys(p.weights).length > 0) {
        lines.push("<hr style='margin:4px 0;border-color:#ddd'/>");
        for (const [key, weight] of Object.entries(p.weights)) {
          const contribution = p.contributions?.[key];
          const name = assetNames[key] ?? key;
          lines.push(
            `${name}：权重 ${formatPercent(weight)}` +
              (contribution != null ? `，当期贡献 ${formatPercent(contribution)}` : ""),
          );
        }
      }
      return lines.join("<br/>");
    };

    const navSeries: Record<string, unknown>[] = [
      {
        name: "组合净值",
        type: "line",
        data: displayPoints.map((p) => p.nav),
        symbol: "none",
        lineStyle: { width: 2, color: "#0f172a" },
        xAxisIndex: 0,
        yAxisIndex: 0,
      },
    ];
    if (hasBenchmark) {
      navSeries.push({
        name: "基准",
        type: "line",
        data: displayPoints.map((p) => p.benchmark_nav ?? null),
        symbol: "none",
        lineStyle: { width: 1.5, color: "#64748b", type: "dashed" },
        xAxisIndex: 0,
        yAxisIndex: 0,
      });
    }
    if (showAssetSeries && assetSeries) {
      for (const s of assetSeries) {
        const byDateAsset = new Map(s.dates.map((d, i) => [d, s.navs[i]!]));
        navSeries.push({
          name: s.name,
          type: "line",
          data: dates.map((d) => byDateAsset.get(d) ?? null),
          symbol: "none",
          lineStyle: { width: 1 },
          xAxisIndex: 0,
          yAxisIndex: 0,
        });
      }
    }

    const ddSeries: Record<string, unknown>[] = [
      {
        name: "组合回撤",
        type: "line",
        data: displayPoints.map((p) => p.drawdown),
        symbol: "none",
        lineStyle: { width: 1.5, color: "#dc2626" },
        areaStyle: { color: "rgba(220,38,38,0.12)" },
        xAxisIndex: 1,
        yAxisIndex: 1,
        ...(summary?.max_drawdown_start && summary.max_drawdown_trough
          ? {
              markArea: {
                silent: true,
                itemStyle: { color: "rgba(220,38,38,0.08)" },
                data: [
                  [
                    { xAxis: summary.max_drawdown_start },
                    { xAxis: summary.max_drawdown_recovery || summary.max_drawdown_trough },
                  ],
                ],
              },
            }
          : {}),
      },
    ];
    if (hasBenchmark) {
      // Benchmark drawdown derived from benchmark NAV.
      let peak = -Infinity;
      const benchDD = displayPoints.map((p) => {
        if (p.benchmark_nav == null) return null;
        if (p.benchmark_nav > peak) peak = p.benchmark_nav;
        return peak > 0 ? p.benchmark_nav / peak - 1 : null;
      });
      ddSeries.push({
        name: "基准回撤",
        type: "line",
        data: benchDD,
        symbol: "none",
        lineStyle: { width: 1, color: "#64748b", type: "dashed" },
        xAxisIndex: 1,
        yAxisIndex: 1,
      });
    }

    return {
      tooltip: { trigger: "axis" as const, formatter },
      axisPointer: { link: [{ xAxisIndex: "all" }] },
      legend: { type: "scroll" as const, top: 0 },
      grid: [
        { left: 64, right: 16, top: 32, height: "48%" },
        { left: 64, right: 16, top: "66%", height: "24%" },
      ],
      xAxis: [
        { type: "category" as const, data: dates, boundaryGap: false, gridIndex: 0 },
        { type: "category" as const, data: dates, boundaryGap: false, gridIndex: 1 },
      ],
      yAxis: [
        {
          type: scale === "log" ? ("log" as const) : ("value" as const),
          scale: true,
          gridIndex: 0,
          axisLabel: { formatter: (v: number) => v.toFixed(2) },
        },
        {
          type: "value" as const,
          gridIndex: 1,
          max: 0,
          axisLabel: { formatter: (v: number) => `${(v * 100).toFixed(0)}%` },
        },
      ],
      series: [...navSeries, ...ddSeries],
    };
  }, [displayPoints, summary, assetNames, assetSeries, showAssetSeries, hasBenchmark, scale]);

  if (points.length === 0) {
    return (
      <div className="flex h-72 items-center justify-center rounded-lg border border-line bg-surface text-sm text-ink-muted">
        暂无回测曲线数据。
      </div>
    );
  }

  return (
    <div className="rounded-lg border border-line bg-surface p-4" data-testid="run-charts">
      <div className="mb-2 flex flex-wrap items-center gap-3">
        <h3 className="text-sm font-semibold text-ink">收益与回撤曲线</h3>
        <div className="flex rounded-md border border-line" role="group" aria-label="坐标模式">
          <button
            type="button"
            onClick={() => setScale("linear")}
            className={toggleCls(scale === "linear", "l")}
            data-testid="scale-linear"
          >
            线性
          </button>
          <button
            type="button"
            onClick={() => setScale("log")}
            className={toggleCls(scale === "log", "r")}
            data-testid="scale-log"
          >
            对数
          </button>
        </div>
        <div className="flex rounded-md border border-line" role="group" aria-label="频率">
          <button
            type="button"
            onClick={() => setFreq("daily")}
            className={toggleCls(freq === "daily", "l")}
            data-testid="freq-daily"
          >
            日度
          </button>
          <button
            type="button"
            onClick={() => setFreq("monthly")}
            className={toggleCls(freq === "monthly", "r")}
            data-testid="freq-monthly"
          >
            月度
          </button>
        </div>
        <label className="ml-auto flex items-center gap-1.5 text-xs text-ink-muted">
          <input
            type="checkbox"
            checked={showAssetSeries}
            onChange={(e) => onToggleAssetSeries(e.target.checked)}
            data-testid="toggle-asset-series"
          />
          单资产归一化曲线
          {assetSeriesLoading && <span>（加载中…）</span>}
        </label>
      </div>
      <ReactECharts option={option} style={{ height: 480 }} notMerge />
    </div>
  );
}
