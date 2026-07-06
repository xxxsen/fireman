"use client";

import { useMemo } from "react";
import ReactECharts from "echarts-for-react";
import type { ResearchRunPoint } from "@/lib/api/research";
import {
  rollingMaxDrawdown,
  rollingReturn,
  rollingVolatility,
} from "@/lib/research/run-analysis";

/** Rolling 12m/36m return, 12m volatility and rolling max drawdown charts. */
export function RunRollingCharts({ points }: { points: ResearchRunPoint[] }) {
  const option = useMemo(() => {
    const r12 = rollingReturn(points, 12);
    const r36 = rollingReturn(points, 36);
    const vol12 = rollingVolatility(points, 12);
    const dd12 = rollingMaxDrawdown(points, 12);
    const dates = points.map((p) => p.date);

    return {
      tooltip: {
        trigger: "axis" as const,
        valueFormatter: (v: unknown) =>
          typeof v === "number" ? `${(v * 100).toFixed(2)}%` : "—",
      },
      legend: { type: "scroll" as const, top: 0 },
      grid: { left: 56, right: 16, bottom: 32, top: 32 },
      xAxis: { type: "category" as const, data: dates, boundaryGap: false },
      yAxis: {
        type: "value" as const,
        axisLabel: { formatter: (v: number) => `${(v * 100).toFixed(0)}%` },
      },
      series: [
        {
          name: "12 个月滚动收益",
          type: "line" as const,
          data: r12.map((p) => p.value),
          symbol: "none",
          lineStyle: { width: 1.5 },
        },
        {
          name: "36 个月滚动收益",
          type: "line" as const,
          data: r36.map((p) => p.value),
          symbol: "none",
          lineStyle: { width: 1.5 },
        },
        {
          name: "12 个月滚动波动率",
          type: "line" as const,
          data: vol12.map((p) => p.value),
          symbol: "none",
          lineStyle: { width: 1.5, type: "dashed" as const },
        },
        {
          name: "滚动最大回撤",
          type: "line" as const,
          data: dd12.map((p) => p.value),
          symbol: "none",
          lineStyle: { width: 1.5, color: "#dc2626" },
        },
      ],
    };
  }, [points]);

  if (points.length < 30) {
    return <p className="text-sm text-ink-muted">数据点不足，无法计算滚动指标。</p>;
  }

  return <ReactECharts option={option} style={{ height: 320 }} notMerge />;
}
