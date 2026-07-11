"use client";

import ReactECharts from "echarts-for-react";
import type { QuickFireYear } from "@/lib/api/quick-fire";
import { formatMoney } from "@/lib/format";

export function QuickFireChart({ years }: { years: QuickFireYear[] }) {
  const option = {
    animation: false,
    grid: { left: 14, right: 20, top: 38, bottom: 28, containLabel: true },
    tooltip: {
      trigger: "axis",
      valueFormatter: (value: number) => formatMoney(value, "CNY"),
    },
    legend: { top: 2, data: ["名义资产", "真实资产", "所需资本"] },
    xAxis: { type: "category", data: years.map((row) => `${row.age} 岁`) },
    yAxis: { type: "value", axisLabel: { formatter: (value: number) => formatMoney(value, "CNY") } },
    series: [
      {
        name: "名义资产",
        type: "line",
        data: years.map((row) => row.end_wealth_minor),
        smooth: true,
        symbol: "none",
        lineStyle: { color: "#147d64", width: 2 },
      },
      {
        name: "真实资产",
        type: "line",
        data: years.map((row) => row.real_end_wealth_minor),
        smooth: true,
        symbol: "none",
        lineStyle: { color: "#2563eb", width: 2 },
      },
      {
        name: "所需资本",
        type: "line",
        data: years.map((row) => row.required_wealth_minor),
        smooth: true,
        symbol: "none",
        lineStyle: { color: "#c77700", width: 1.5, type: "dashed" },
      },
    ],
  };
  return <ReactECharts option={option} style={{ height: 340 }} notMerge />;
}
