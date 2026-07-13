"use client";

import ReactECharts from "echarts-for-react";
import { comparisonBarChartLayout } from "@/components/charts/chartOptions";
import { formatMoneyScaled, formatPercent, regionLabel } from "@/lib/format";
import type { RegionBar } from "@/types/api";
import { ChartFrame } from "./ChartFrame";

const MAX_TOOLTIP_HOLDINGS = 8;

type AxisTooltipParam = { dataIndex?: number };

export function formatRegionAllocationTooltip(
  bars: RegionBar[],
  currency: string,
  params: AxisTooltipParam | AxisTooltipParam[],
): string {
  const items = Array.isArray(params) ? params : [params];
  const idx = items[0]?.dataIndex ?? 0;
  const bar = bars[idx];
  if (!bar) return "无数据";

  const head = [
    `<strong>${regionLabel(bar.region)}</strong>`,
    `目标比例 ${formatPercent(bar.target_weight)} · 当前比例 ${formatPercent(bar.current_weight)}`,
    `目标金额 ${formatMoneyScaled(bar.target_amount_minor, currency)} · 当前已投 ${formatMoneyScaled(
      bar.current_amount_minor,
      currency,
    )}`,
  ];
  const holdings = bar.holdings ?? [];
  const shown = holdings.slice(0, MAX_TOOLTIP_HOLDINGS);
  const detail = shown.map((holding) => {
    const name = holding.instrument_name || "—";
    const code = holding.instrument_code ? `（${holding.instrument_code}）` : "";
    return `${name}${code}：当前 ${formatMoneyScaled(holding.current_amount_minor, currency)} / 目标 ${formatMoneyScaled(holding.target_amount_minor, currency)}`;
  });
  if (holdings.length === 0) detail.push("暂无资产明细");
  const remaining = holdings.length - shown.length;
  if (remaining > 0) detail.push(`…另有 ${remaining} 个资产`);
  return [...head, "—", ...detail].join("<br/>");
}

export function RegionAllocationBarChart({
  bars,
  currency = "CNY",
}: {
  bars: RegionBar[];
  currency?: string;
}) {
  const option = {
    aria: { enabled: true, description: "按地区比较占全组合的目标比例与当前持仓比例。" },
    tooltip: {
      trigger: "axis" as const,
      confine: true,
      formatter: (params: AxisTooltipParam | AxisTooltipParam[]) =>
        formatRegionAllocationTooltip(bars, currency, params),
    },
    legend: comparisonBarChartLayout.legend,
    grid: comparisonBarChartLayout.grid,
    xAxis: {
      type: "category" as const,
      name: "地区",
      data: bars.map((bar) => regionLabel(bar.region)),
    },
    yAxis: {
      type: "value" as const,
      name: "组合占比（%）",
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

  return (
    <ChartFrame
      title="地区目标与当前占比"
      termKey="region_allocation"
      xAxis="地区"
      yAxis="全组合占比"
      unit="%"
      legend="地区目标由资产大类权重和大类内地区配比共同决定；当前以已启用持仓市值合计为分母。"
      interpretation="柱形比较的是组合结构，不表示地区收益。悬浮或点按可查看目标/当前比例、金额和下属标的明细。"
    >
      <ReactECharts option={option} style={{ height: 280 }} />
    </ChartFrame>
  );
}
