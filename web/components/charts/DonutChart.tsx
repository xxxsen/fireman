"use client";

import ReactECharts from "echarts-for-react";
import { assetClassLabel } from "@/lib/format";
import { ChartFrame } from "./ChartFrame";

interface Slice {
  name: string;
  value: number;
}

export function formatDonutTooltip(
  denominator: string,
  point: { name?: string; percent?: number; value?: number },
): string {
  const percent = typeof point.percent === "number" && Number.isFinite(point.percent)
    ? `${point.percent.toFixed(1)}%`
    : "无数据";
  const value = typeof point.value === "number" && Number.isFinite(point.value)
    ? `${point.value.toFixed(2)}%`
    : "无数据";
  return `${point.name || "未命名分类"}<br/>占${denominator}：${percent}<br/>输入权重：${value}`;
}

export function DonutChart({ slices, title, denominator = "全组合" }: { slices: Slice[]; title?: string; denominator?: string }) {
  const positiveSlices = slices.filter((slice) => slice.value > 0);
  if (positiveSlices.length === 0) {
    return <p className="flex h-48 items-center justify-center text-sm text-ink-muted">暂无可展示的配置占比。</p>;
  }
  const option = {
    aria: { enabled: true, description: `${title ?? "配置"}环形图，比例分母为${denominator}。` },
    tooltip: {
      trigger: "item" as const,
      confine: true,
      formatter: (point: { name?: string; percent?: number; value?: number }) =>
        formatDonutTooltip(denominator, point),
    },
    series: [
      {
        type: "pie" as const,
        radius: ["45%", "70%"],
        data: positiveSlices.map((s) => ({
          name: assetClassLabel(s.name) || s.name,
          value: Math.max(0, s.value * 100),
        })),
        label: { formatter: "{b}: {d}%" },
      },
    ],
  };
  return (
    <ChartFrame
      title={title ?? "配置占比"}
      termKey="portfolio_weight"
      xAxis="分类"
      yAxis={`占${denominator}比例`}
      unit="%"
      interpretation={`每个扇区面积表示该项占${denominator}的比例，所有非零扇区合计为 100%。悬浮或点按可查看比例。`}
    >
      <ReactECharts option={option} style={{ height: 260 }} />
    </ChartFrame>
  );
}
