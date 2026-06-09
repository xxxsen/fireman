"use client";

import ReactECharts from "echarts-for-react";
import { assetClassLabel } from "@/lib/format";

interface Slice {
  name: string;
  value: number;
}

export function DonutChart({ slices, title }: { slices: Slice[]; title?: string }) {
  const option = {
    title: title ? { text: title, left: "center", textStyle: { fontSize: 14 } } : undefined,
    tooltip: {
      trigger: "item" as const,
      formatter: (p: { name: string; percent: number }) =>
        `${p.name}: ${p.percent.toFixed(1)}%`,
    },
    series: [
      {
        type: "pie" as const,
        radius: ["45%", "70%"],
        data: slices.map((s) => ({
          name: assetClassLabel(s.name) || s.name,
          value: Math.max(0, s.value * 100),
        })),
        label: { formatter: "{b}: {d}%" },
      },
    ],
  };
  return <ReactECharts option={option} style={{ height: 260 }} />;
}
