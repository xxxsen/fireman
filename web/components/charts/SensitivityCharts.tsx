"use client";

import ReactECharts from "echarts-for-react";
import { formatPercent } from "@/lib/format";
import { ChartFrame } from "./ChartFrame";

export function formatTornadoTooltip(
  params: Array<{ name?: string; value?: number }>,
  item?: { low_label?: string; high_label?: string },
): string {
  const percent = (value: number | undefined) =>
    typeof value === "number" && Number.isFinite(value) ? formatPercent(value) : "无数据";
  return `${params[0]?.name ?? "参数"}<br/>低扰动（${item?.low_label ?? "低值"}）：${percent(params[0]?.value)}<br/>高扰动（${item?.high_label ?? "高值"}）：${percent(params[1]?.value)}`;
}

export function formatParameterCurveTooltip(
  parameterName: string,
  baseValue: number,
  params: Array<{ axisValue?: string; name?: string; value?: number }>,
): string {
  const item = params[0];
  if (!item || typeof item.value !== "number") return `${parameterName}<br/>无数据`;
  const label = item.axisValue ?? item.name ?? "未知扰动";
  const delta = item.value - baseValue;
  const sign = delta >= 0 ? "+" : "";
  return `${parameterName}<br/>实际扰动：${label}<br/>成功率：${formatPercent(item.value)}<br/>相对基准：${sign}${formatPercent(delta)}`;
}

export function formatHeatmapTooltip(
  data: [number, number, number],
  spendingLabels: string[],
  returnLabels: string[],
): string {
  const [x, y, value] = data;
  return `收益扰动：${returnLabels[y] ?? "未知"}<br/>支出扰动：${spendingLabels[x] ?? "未知"}<br/>成功率：${Number.isFinite(value) ? formatPercent(value) : "无数据"}`;
}

export function TornadoChart({
  items,
}: {
  items: Array<{ parameter_name: string; low_label?: string; high_label?: string; low_success: number; high_success: number }>;
}) {
  if (items.length === 0) return null;
  const names = items.map((t) => t.parameter_name);
  const low = items.map((t) => t.low_success);
  const high = items.map((t) => t.high_success);
  return (
    <ChartFrame
      title="参数影响范围"
      termKey="sensitivity_test"
      xAxis="成功率"
      yAxis="被扰动参数"
      unit="%"
      legend="每个参数分别显示低扰动和高扰动的实际评估结果。"
      interpretation="条形差距越大，当前计划的成功率对该参数越敏感；这只是预设离散扰动，不代表参数会按该幅度变化。悬浮或点按可查看实际扰动标签。"
    >
    <ReactECharts
      style={{ height: 280 }}
      option={{
        aria: { enabled: true, description: "比较各参数低扰动和高扰动下的模拟成功率。" },
        tooltip: { trigger: "axis", confine: true, formatter: (p: Array<{ name?: string; value?: number }>) => {
          const item = items.find((candidate) => candidate.parameter_name === p[0]?.name);
          return formatTornadoTooltip(p, item);
        } },
        legend: { data: ["低扰动", "高扰动"] },
        grid: { left: 120, right: 20, bottom: 44, containLabel: true },
        xAxis: { type: "value", name: "成功率", nameLocation: "middle", nameGap: 30, min: 0, max: 1, axisLabel: { formatter: (v: number) => formatPercent(v) } },
        yAxis: { type: "category", data: names },
        series: [
          { name: "低扰动", type: "bar", data: low },
          { name: "高扰动", type: "bar", data: high },
        ],
      }}
    />
    </ChartFrame>
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
          <ChartFrame
            key={c.parameter_name}
            title={c.parameter_name}
            termKey="sensitivity_test"
            xAxis="实际参数扰动"
            yAxis="成功率"
            unit="%"
            interpretation="带“基准”的点使用未扰动输入；其他点是实际评估过的离散档位，平滑连线只帮助观察方向，不代表档位之间已经计算。"
          >
            <ReactECharts
              style={{ height: 280 }}
              option={{
                tooltip: {
                  trigger: "axis",
                  confine: true,
                  formatter: (params: Array<{ axisValue?: string; name?: string; value: number }>) => {
                    return formatParameterCurveTooltip(c.parameter_name, baseVal, params);
                  },
                },
                aria: { enabled: true, description: `${c.parameter_name}不同扰动档位对应的模拟成功率。` },
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
          </ChartFrame>
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
    <ChartFrame
      title="收益与支出二维敏感性"
      termKey="sensitivity_test"
      xAxis="支出扰动"
      yAxis="收益扰动"
      unit="成功率 %"
      legend="每个色块是一个实际评估的收益扰动与支出扰动组合；格内数字为成功率。"
      interpretation="颜色和数字用于比较方向，不能推出格点之间的连续结果。悬浮或点按显示两个实际扰动标签和成功率。"
    >
    <ReactECharts
      style={{ height: 320 }}
      option={{
        aria: { enabled: true, description: "收益扰动与支出扰动组合下的模拟成功率热力图。" },
        tooltip: {
          position: "top",
          confine: true,
          formatter: (p: { data: [number, number, number] }) => {
            return formatHeatmapTooltip(p.data, spendingLabels, returnLabels);
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
    </ChartFrame>
  );
}
