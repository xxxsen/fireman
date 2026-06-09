"use client";

import ReactECharts from "echarts-for-react";
import type { QuantilePoint } from "@/types/api";

export function WealthPathChart({ series }: { series: QuantilePoint[] }) {
  if (!series?.length) {
    return (
      <div className="flex h-64 items-center justify-center text-sm text-slate-500">
        暂无财富路径数据
      </div>
    );
  }

  const months = series.map((p) => p.month_offset);
  const toMajor = (arr: number[]) => arr.map((v) => v / 100);

  const option = {
    tooltip: { trigger: "axis" as const },
    legend: { data: ["P25-P75", "P05-P95", "P50"] },
    grid: { left: 56, right: 16, bottom: 32, top: 40 },
    xAxis: { type: "category" as const, data: months, name: "月" },
    yAxis: {
      type: "value" as const,
      axisLabel: { formatter: (v: number) => `${(v / 10000).toFixed(0)}万` },
    },
    series: [
      {
        name: "P25-P75",
        type: "line" as const,
        data: toMajor(series.map((p) => p.p50_minor)),
        lineStyle: { opacity: 0 },
        stack: "band-inner",
        symbol: "none",
        areaStyle: { color: "rgba(15,23,42,0.15)" },
      },
      {
        name: "_p75",
        type: "line" as const,
        data: toMajor(series.map((p) => p.p75_minor - p.p25_minor)),
        lineStyle: { opacity: 0 },
        stack: "band-inner",
        symbol: "none",
        areaStyle: { color: "rgba(15,23,42,0.15)" },
        showInLegend: false,
      },
      {
        name: "P50",
        type: "line" as const,
        data: toMajor(series.map((p) => p.p50_minor)),
        symbol: "none",
        lineStyle: { width: 2, color: "#0f172a" },
      },
    ],
  };

  return <ReactECharts option={option} style={{ height: 320 }} />;
}
