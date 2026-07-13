"use client";

import ReactECharts from "echarts-for-react";
import { formatPercent, pointTypeLabel } from "@/lib/format";
import { ChartFrame } from "./ChartFrame";

export interface ReturnSeriesPoint {
  date: string;
  value: number;
  cumulative_return: number;
}

type AxisTooltipParam = { dataIndex?: number };

export function formatReturnSeriesTooltip(
  points: ReturnSeriesPoint[],
  pointType: string | undefined,
  params: AxisTooltipParam | AxisTooltipParam[],
): string {
  const items = Array.isArray(params) ? params : [params];
  const point = points[items[0]?.dataIndex ?? 0];
  if (!point) return "无数据";
  return [
    `日期：${point.date}`,
    `累计收益：${Number.isFinite(point.cumulative_return) ? formatPercent(point.cumulative_return) : "无数据"}`,
    `${pointTypeLabel(pointType)}：${Number.isFinite(point.value) ? point.value.toLocaleString("zh-CN", { maximumFractionDigits: 4 }) : "无数据"}`,
  ].join("<br/>");
}

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

  const option = {
    aria: { enabled: true, description: `展示使用${valueLabel}计算的归一化累计收益历史。` },
    tooltip: { trigger: "axis" as const, confine: true, formatter: (params: AxisTooltipParam | AxisTooltipParam[]) => formatReturnSeriesTooltip(points, pointType, params) },
    grid: { left: 64, right: 16, bottom: 48, top: 24, containLabel: true },
    xAxis: {
      type: "category" as const,
      data: points.map((p) => p.date),
      boundaryGap: false,
      name: "日期",
    },
    yAxis: {
      type: "value" as const,
      scale: true,
      name: "累计收益（%）",
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

  return (
    <ChartFrame
      title="归一化累计收益"
      termKey="cumulative_return"
      xAxis="日期"
      yAxis="累计收益"
      unit="%"
      legend={`曲线以所选区间首个有效${valueLabel}为起点归一化。`}
      interpretation="曲线表示历史区间内相对起点的累计变化，不是资产价格预测。悬浮或点按可查看日期、累计收益和原始点值。平滑连线只用于显示。"
    >
      <ReactECharts option={option} style={{ height: 280 }} />
    </ChartFrame>
  );
}
