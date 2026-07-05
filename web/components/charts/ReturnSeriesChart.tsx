"use client";

import ReactECharts from "echarts-for-react";
import { formatPercent, pointTypeLabel } from "@/lib/format";

export interface ReturnSeriesPoint {
  date: string;
  value: number;
  cumulative_return: number;
}

type AxisTooltipParam = { dataIndex?: number };

export function ReturnSeriesChart({
  points,
  pointType,
}: {
  points: ReturnSeriesPoint[];
  pointType?: string;
}) {
  if (!points.length) {
    return (
      <div className="flex h-64 items-center justify-center text-sm text-ink-muted">
        暂无收益曲线数据
      </div>
    );
  }

  const valueLabel = pointTypeLabel(pointType);

  const formatter = (params: AxisTooltipParam | AxisTooltipParam[]): string => {
    const items = Array.isArray(params) ? params : [params];
    const idx = items[0]?.dataIndex ?? 0;
    const p = points[idx];
    if (!p) return "";
    return [
      p.date,
      `累计收益 ${formatPercent(p.cumulative_return)}`,
      `${valueLabel} ${p.value.toLocaleString("zh-CN", { maximumFractionDigits: 4 })}`,
    ].join("<br/>");
  };

  const option = {
    tooltip: { trigger: "axis" as const, formatter },
    grid: { left: 56, right: 16, bottom: 32, top: 16 },
    xAxis: {
      type: "category" as const,
      data: points.map((p) => p.date),
      boundaryGap: false,
    },
    yAxis: {
      type: "value" as const,
      scale: true,
      axisLabel: { formatter: (v: number) => `${(v * 100).toFixed(1)}%` },
    },
    series: [
      {
        name: "累计收益",
        type: "line" as const,
        data: points.map((p) => p.cumulative_return),
        symbol: "none",
        smooth: true,
        lineStyle: { width: 2, color: "#0f172a" },
        areaStyle: { color: "rgba(15,23,42,0.08)" },
      },
    ],
  };

  return <ReactECharts option={option} style={{ height: 280 }} />;
}
