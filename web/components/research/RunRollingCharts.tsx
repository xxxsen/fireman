"use client";

import { useMemo } from "react";
import ReactECharts from "echarts-for-react";
import type { ResearchRunPoint } from "@/lib/api/research";
import {
  rollingMaxDrawdown,
  rollingReturn,
  rollingVolatility,
} from "@/lib/research/run-analysis";
import { ChartFrame } from "@/components/charts/ChartFrame";

export function formatRollingValue(value: unknown): string {
  return typeof value === "number" && Number.isFinite(value)
    ? `${(value * 100).toFixed(2)}%`
    : "无数据（历史不足）";
}

/** Rolling 12m/36m return, 12m volatility and rolling max drawdown charts. */
export function RunRollingCharts({ points }: { points: ResearchRunPoint[] }) {
  const option = useMemo(() => {
    const r12 = rollingReturn(points, 12);
    const r36 = rollingReturn(points, 36);
    const vol12 = rollingVolatility(points, 12);
    const dd12 = rollingMaxDrawdown(points, 12);
    const dates = points.map((p) => p.date);

    return {
      aria: { enabled: true, description: "展示 12 和 36 个月滚动收益、12 个月滚动波动率及滚动最大回撤。" },
      tooltip: {
        trigger: "axis" as const,
        confine: true,
        valueFormatter: formatRollingValue,
      },
      legend: { type: "scroll" as const, top: 0 },
      grid: { left: 56, right: 16, bottom: 32, top: 32 },
      xAxis: { type: "category" as const, data: dates, boundaryGap: false, name: "窗口终止日期" },
      yAxis: {
        type: "value" as const,
        name: "收益 / 风险（%）",
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

  return (
    <ChartFrame
      title="滚动收益与风险"
      termKey="rolling_metric"
      xAxis="窗口终止日期"
      yAxis="收益 / 风险"
      unit="%"
      legend="实线为滚动收益，虚线为 12 个月年化波动率，红线为窗口内最大回撤。"
      interpretation="每个点只使用截至该日向前固定窗口的数据；开头历史不足时不显示数值。相邻点共享大部分样本，因此不能视为相互独立。"
    >
      <ReactECharts option={option} style={{ height: 320 }} notMerge />
    </ChartFrame>
  );
}
