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
      {curves.map((c) => {
        // Baseline = the unperturbed point ("基准") if present, else the middle
        // point of a symmetric perturbation sweep, used for relative deltas.
        const baseline =
          c.points.find((p) => p.label.includes("基准")) ??
          c.points[Math.floor(c.points.length / 2)];
        const baseVal = baseline?.success_probability ?? 0;
        return (
          <div key={c.parameter_name}>
            <p className="mb-1 text-xs font-medium text-ink-muted">{c.parameter_name}</p>
            <ReactECharts
              style={{ height: 280 }}
              option={{
                tooltip: {
                  trigger: "axis",
                  formatter: (params: Array<{ axisValue?: string; name?: string; value: number }>) => {
                    const item = params[0];
                    if (!item) return "";
                    const label = item.axisValue ?? item.name ?? "";
                    const v = typeof item.value === "number" ? item.value : 0;
                    const delta = v - baseVal;
                    const sign = delta >= 0 ? "+" : "";
                    return `${c.parameter_name}<br/>扰动 ${label}<br/>成功率 ${formatPercent(v)}<br/>相对基准 ${sign}${formatPercent(delta)}`;
                  },
                },
                grid: { top: 30, bottom: 52, left: 60, right: 16 },
                xAxis: {
                  type: "category",
                  name: "参数扰动",
                  nameLocation: "middle",
                  nameGap: 30,
                  data: c.points.map((p) => p.label),
                },
                yAxis: {
                  type: "value",
                  name: "成功率",
                  min: 0,
                  max: 1,
                  axisLabel: { formatter: (v: number) => formatPercent(v) },
                },
                series: [{ type: "line", data: c.points.map((p) => p.success_probability), smooth: true }],
              }}
            />
          </div>
        );
      })}
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
        grid: { height: "62%", top: "8%", left: 80, right: 24 },
        xAxis: {
          type: "category",
          name: "支出扰动",
          nameLocation: "middle",
          nameGap: 32,
          data: spendingLabels,
          splitArea: { show: true },
        },
        yAxis: {
          type: "category",
          name: "收益扰动",
          nameLocation: "middle",
          nameGap: 56,
          data: returnLabels,
          splitArea: { show: true },
        },
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
