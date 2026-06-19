"use client";

import ReactECharts from "echarts-for-react";
import { formatMoneyWan } from "@/lib/format";
import type { QuantilePoint } from "@/types/api";

const BAND_BASE_SERIES = "__p25_base";
const BAND_SERIES = "P25-P75";
const MEDIAN_SERIES = "P50";

type AxisTooltipParam = { dataIndex?: number };

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
    tooltip: {
      trigger: "axis" as const,
      formatter: (params: AxisTooltipParam | AxisTooltipParam[]): string => {
        const items = Array.isArray(params) ? params : [params];
        const idx = items[0]?.dataIndex ?? 0;
        const point = series[idx];
        if (!point) return "";
        return [
          `第 ${months[idx]} 月`,
          `${BAND_SERIES}：${formatMoneyWan(point.p25_minor)} - ${formatMoneyWan(point.p75_minor)}`,
          `${MEDIAN_SERIES}：${formatMoneyWan(point.p50_minor)}`,
        ].join("<br/>");
      },
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

export function WealthPathChart({ series }: { series: QuantilePoint[] }) {
  if (!series?.length) {
    return (
      <div className="flex h-64 items-center justify-center text-sm text-ink-muted">
        暂无财富路径数据
      </div>
    );
  }

  return (
    <ReactECharts
      option={buildWealthPathOption(series)}
      style={{ height: 360 }}
      notMerge
    />
  );
}
