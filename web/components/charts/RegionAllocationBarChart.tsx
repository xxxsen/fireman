"use client";

import ReactECharts from "echarts-for-react";
import {
  comparisonBarChartLayout,
  formatComparisonBarTooltip,
} from "@/components/charts/chartOptions";
import { regionLabel } from "@/lib/format";

interface Bar {
  region: string;
  target_weight: number;
  current_weight: number;
}

export function RegionAllocationBarChart({ bars }: { bars: Bar[] }) {
  const option = {
    tooltip: {
      trigger: "axis" as const,
      formatter: formatComparisonBarTooltip,
    },
    legend: comparisonBarChartLayout.legend,
    grid: comparisonBarChartLayout.grid,
    xAxis: {
      type: "category" as const,
      data: bars.map((bar) => regionLabel(bar.region)),
    },
    yAxis: {
      type: "value" as const,
      axisLabel: { formatter: (value: number) => `${(value * 100).toFixed(0)}%` },
      max: 1,
    },
    series: [
      {
        name: "目标",
        type: "bar" as const,
        data: bars.map((bar) => bar.target_weight),
        itemStyle: { color: "#cbd5e1" },
      },
      {
        name: "当前",
        type: "bar" as const,
        data: bars.map((bar) => bar.current_weight),
        itemStyle: { color: "#0f172a" },
      },
    ],
  };

  return <ReactECharts option={option} style={{ height: 280 }} />;
}
