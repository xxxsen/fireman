"use client";

import ReactECharts from "echarts-for-react";
import { formatPercent } from "@/lib/format";

export function TornadoChart({
  items,
}: {
  items: Array<{ parameter_name: string; low_success: number; high_success: number }>;
}) {
  if (items.length === 0) return null;
  const names = items.map((t) => t.parameter_name);
  const low = items.map((t) => t.low_success);
  const high = items.map((t) => t.high_success);
  return (
    <ReactECharts
      style={{ height: 280 }}
      option={{
        tooltip: { trigger: "axis", formatter: (p: { name: string; value: number }[]) =>
          `${p[0]?.name}<br/>低: ${formatPercent(p[0]?.value ?? 0)}<br/>高: ${formatPercent(p[1]?.value ?? 0)}` },
        legend: { data: ["低扰动", "高扰动"] },
        grid: { left: 120, right: 20 },
        xAxis: { type: "value", axisLabel: { formatter: (v: number) => formatPercent(v) } },
        yAxis: { type: "category", data: names },
        series: [
          { name: "低扰动", type: "bar", data: low },
          { name: "高扰动", type: "bar", data: high },
        ],
      }}
    />
  );
}

export function ParameterCurvesChart({
  curves,
}: {
  curves: Array<{ parameter_name: string; points: Array<{ label: string; success_probability: number }> }>;
}) {
  if (curves.length === 0) return null;
  return (
    <div className="space-y-4">
      {curves.map((c) => (
        <div key={c.parameter_name}>
          <p className="mb-1 text-xs font-medium text-ink-muted">{c.parameter_name}</p>
          <ReactECharts
            style={{ height: 180 }}
            option={{
              tooltip: { trigger: "axis" },
              xAxis: { type: "category", data: c.points.map((p) => p.label) },
              yAxis: { type: "value", axisLabel: { formatter: (v: number) => formatPercent(v) } },
              series: [{ type: "line", data: c.points.map((p) => p.success_probability), smooth: true }],
            }}
          />
        </div>
      ))}
    </div>
  );
}

export function SensitivityHeatmap({
  heatmap,
}: {
  heatmap: Array<Array<{ spending_label: string; return_label: string; success_probability: number }>>;
}) {
  if (heatmap.length === 0) return null;
  const spendingLabels = heatmap[0]?.map((c) => c.spending_label) ?? [];
  const returnLabels = heatmap.map((row) => row[0]?.return_label ?? "");
  const data: [number, number, number][] = [];
  heatmap.forEach((row, ri) => {
    row.forEach((cell, ci) => {
      data.push([ci, ri, cell.success_probability]);
    });
  });
  return (
    <ReactECharts
      style={{ height: 320 }}
      option={{
        tooltip: {
          position: "top",
          formatter: (p: { data: [number, number, number] }) => {
            const [x, y, v] = p.data;
            return `${returnLabels[y]} / ${spendingLabels[x]}<br/>${formatPercent(v)}`;
          },
        },
        grid: { height: "70%", top: "10%" },
        xAxis: { type: "category", data: spendingLabels, splitArea: { show: true } },
        yAxis: { type: "category", data: returnLabels, splitArea: { show: true } },
        visualMap: {
          min: 0,
          max: 1,
          calculable: true,
          orient: "horizontal",
          left: "center",
          bottom: 0,
          formatter: (v: number) => formatPercent(v),
        },
        series: [{
          type: "heatmap",
          data,
          label: { show: true, formatter: (p: { data: [number, number, number] }) => formatPercent(p.data[2]) },
        }],
      }}
    />
  );
}
