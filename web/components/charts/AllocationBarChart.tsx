"use client";

import ReactECharts from "echarts-for-react";
import { assetClassLabel } from "@/lib/format";

interface Bar {
  asset_class: string;
  target_weight: number;
  current_weight: number;
}

export function AllocationBarChart({ bars }: { bars: Bar[] }) {
  const categories = bars.map((b) => assetClassLabel(b.asset_class));
  const option = {
    tooltip: { trigger: "axis" as const },
    legend: { data: ["目标", "当前"] },
    grid: { left: 48, right: 16, bottom: 32, top: 40 },
    xAxis: { type: "category" as const, data: categories },
    yAxis: {
      type: "value" as const,
      axisLabel: { formatter: (v: number) => `${(v * 100).toFixed(0)}%` },
      max: 1,
    },
    series: [
      {
        name: "目标",
        type: "bar" as const,
        data: bars.map((b) => b.target_weight),
        itemStyle: { color: "#64748b" },
      },
      {
        name: "当前",
        type: "bar" as const,
        data: bars.map((b) => b.current_weight),
        itemStyle: { color: "#0f172a" },
      },
    ],
  };
  return <ReactECharts option={option} style={{ height: 280 }} />;
}
