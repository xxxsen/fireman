"use client";

import ReactECharts from "echarts-for-react";
import { formatMoneyWan } from "@/lib/format";
import type { QuantilePoint } from "@/types/api";
import { ChartFrame } from "./ChartFrame";

const BAND_BASE_SERIES = "__p25_base";
const BAND_SERIES = "P25-P75";
const MEDIAN_SERIES = "P50";

type AxisTooltipParam = { dataIndex?: number };

export function formatWealthPathTooltip(
  series: QuantilePoint[],
  params: AxisTooltipParam | AxisTooltipParam[],
): string {
  const items = Array.isArray(params) ? params : [params];
  const point = series[items[0]?.dataIndex ?? 0];
  if (!point) return "无数据";
  return [
    `第 ${point.month_offset} 月`,
    `${BAND_SERIES}：${formatMoneyWan(point.p25_minor)} - ${formatMoneyWan(point.p75_minor)}`,
    `${MEDIAN_SERIES}：${formatMoneyWan(point.p50_minor)}`,
  ].join("<br/>");
}

/**
 * Build the ECharts option for the wealth path chart. Kept as a pure function so
 * the legend, stacking baseline and tooltip formatter can be unit-tested without
 * rendering ECharts.
 *
 * The P25-P75 band is drawn with two stacked series: an invisible baseline at
 * `p25_minor` and a visible blue area of width `p75_minor - p25_minor`. The
 * baseline is excluded from both legend and tooltip. P50 is a separate dark line.
 */
export function buildWealthPathOption(series: QuantilePoint[]) {
  const months = series.map((p) => p.month_offset);
  const base = series.map((p) => p.p25_minor);
  const band = series.map((p) => p.p75_minor - p.p25_minor);
  const median = series.map((p) => p.p50_minor);

  return {
    aria: {
      enabled: true,
      description: "按规划月份展示模拟资产的 P25 到 P75 分位带和 P50 中位数。",
    },
    tooltip: {
      trigger: "axis" as const,
      confine: true,
      formatter: (params: AxisTooltipParam | AxisTooltipParam[]): string =>
        formatWealthPathTooltip(series, params),
    },
    legend: { data: [BAND_SERIES, MEDIAN_SERIES], bottom: 0 },
    grid: { left: 64, right: 16, bottom: 48, top: 16 },
    xAxis: {
      type: "category" as const,
      data: months,
      name: "月",
      boundaryGap: false,
    },
    yAxis: {
      type: "value" as const,
      name: "资产金额（万元）",
      nameGap: 12,
      axisLabel: { formatter: (v: number) => formatMoneyWan(v) },
    },
    series: [
      {
        name: BAND_BASE_SERIES,
        type: "line" as const,
        data: base,
        stack: "band",
        symbol: "none",
        lineStyle: { opacity: 0 },
        areaStyle: { opacity: 0 },
        tooltip: { show: false },
        silent: true,
      },
      {
        name: BAND_SERIES,
        type: "line" as const,
        data: band,
        stack: "band",
        symbol: "none",
        lineStyle: { opacity: 0 },
        areaStyle: { color: "rgba(37,99,235,0.18)" },
      },
      {
        name: MEDIAN_SERIES,
        type: "line" as const,
        data: median,
        symbol: "none",
        lineStyle: { width: 2, color: "#1f2937" },
      },
    ],
  };
}

export function WealthPathChart({ series, caliber = "nominal" }: { series: QuantilePoint[]; caliber?: "nominal" | "real" }) {
  if (!series?.length) {
    return (
      <div className="flex h-64 items-center justify-center text-sm text-ink-muted">
        暂无财富路径数据
      </div>
    );
  }

  const endpoints = series.length === 1 ? [series[0]!] : [series[0]!, series[series.length - 1]!];

  return (
    <ChartFrame
      title="财富分位走势"
      termKey="p_quantiles"
      xAxis="规划月份"
      yAxis={caliber === "real" ? "起点购买力资产" : "名义资产"}
      unit="万元"
      legend="蓝色带覆盖每个月样本的 P25–P75，中间深色线是 P50；它们是逐月重新排序得到的统计位置。"
      interpretation="分位带不是一组固定路径，P50 也不是一条从头到尾都处于中位数的预测路径。悬浮或点按可查看该月的 P25、P50 和 P75。"
      dataTable={
        <table className="min-w-full text-left text-xs" aria-label="财富分位起止数据">
          <thead><tr><th className="pr-3">月份</th><th className="pr-3">P25</th><th className="pr-3">P50</th><th>P75</th></tr></thead>
          <tbody>{endpoints.map((point) => <tr key={point.month_offset}><td>{point.month_offset}</td><td>{formatMoneyWan(point.p25_minor)}</td><td>{formatMoneyWan(point.p50_minor)}</td><td>{formatMoneyWan(point.p75_minor)}</td></tr>)}</tbody>
        </table>
      }
    >
      <ReactECharts option={buildWealthPathOption(series)} style={{ height: 360 }} notMerge />
    </ChartFrame>
  );
}
