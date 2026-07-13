"use client";

import ReactECharts from "echarts-for-react";
import { comparisonBarChartLayout } from "@/components/charts/chartOptions";
import { assetClassLabel, formatMoneyScaled, formatPercent } from "@/lib/format";
import type { AllocationBar } from "@/types/api";
import { ChartFrame } from "./ChartFrame";

const MAX_TOOLTIP_HOLDINGS = 8;

type AxisTooltipParam = { dataIndex?: number };

export function formatAllocationBarTooltip(
  bars: AllocationBar[],
  currency: string,
  params: AxisTooltipParam | AxisTooltipParam[],
): string {
  const items = Array.isArray(params) ? params : [params];
  const idx = items[0]?.dataIndex ?? 0;
  const bar = bars[idx];
  if (!bar) return "无数据";

  const head = [
    `<strong>${assetClassLabel(bar.asset_class)}</strong>`,
    `目标比例 ${formatPercent(bar.target_weight)} · 当前比例 ${formatPercent(bar.current_weight)}`,
    `目标金额 ${formatMoneyScaled(bar.target_amount_minor, currency)} · 当前已投 ${formatMoneyScaled(
      bar.current_amount_minor,
      currency,
    )}`,
  ];

  const holdings = bar.holdings ?? [];
  let detail: string[];
  if (holdings.length === 0) {
    detail = ["暂无资产明细"];
  } else {
    const shown = holdings.slice(0, MAX_TOOLTIP_HOLDINGS);
    detail = shown.map((holding) => {
      const name = holding.instrument_name || "—";
      const code = holding.instrument_code ? `（${holding.instrument_code}）` : "";
      return `${name}${code}：当前 ${formatMoneyScaled(
        holding.current_amount_minor,
        currency,
      )} / 目标 ${formatMoneyScaled(holding.target_amount_minor, currency)}`;
    });
    const remaining = holdings.length - shown.length;
    if (remaining > 0) detail.push(`…另有 ${remaining} 个资产`);
  }

  return [...head, "—", ...detail].join("<br/>");
}

export function AllocationBarChart({
  bars,
  currency = "CNY",
}: {
  bars: AllocationBar[];
  currency?: string;
}) {
  const categories = bars.map((b) => assetClassLabel(b.asset_class));

  const option = {
    aria: { enabled: true, description: "按资产大类比较占全组合的目标比例与当前持仓比例。" },
    tooltip: {
      trigger: "axis" as const,
      confine: true,
      formatter: (params: AxisTooltipParam | AxisTooltipParam[]) =>
        formatAllocationBarTooltip(bars, currency, params),
    },
    legend: comparisonBarChartLayout.legend,
    grid: comparisonBarChartLayout.grid,
    xAxis: { type: "category" as const, data: categories, name: "资产大类" },
    yAxis: {
      type: "value" as const,
      name: "组合占比（%）",
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
  return (
    <ChartFrame
      title="大类目标与当前占比"
      termKey="asset_class_allocation"
      xAxis="资产大类"
      yAxis="全组合占比"
      unit="%"
      legend="目标以计划基准规模和目标权重为口径；当前以已启用持仓市值合计为分母。"
      interpretation="同一大类两根柱用于观察结构偏离，不表示收益。悬浮或点按可查看目标/当前比例、金额和下属标的明细。"
    >
      <ReactECharts option={option} style={{ height: 280 }} />
    </ChartFrame>
  );
}
