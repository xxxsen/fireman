"use client";

import ReactECharts from "echarts-for-react";
import { comparisonBarChartLayout } from "@/components/charts/chartOptions";
import { formatMoneyScaled, formatPercent, regionLabel } from "@/lib/format";
import type { RegionBar } from "@/types/api";

const MAX_TOOLTIP_HOLDINGS = 8;

type AxisTooltipParam = { dataIndex?: number };

export function RegionAllocationBarChart({
  bars,
  currency = "CNY",
}: {
  bars: RegionBar[];
  currency?: string;
}) {
  const formatter = (params: AxisTooltipParam | AxisTooltipParam[]): string => {
    const items = Array.isArray(params) ? params : [params];
    const idx = items[0]?.dataIndex ?? 0;
    const bar = bars[idx];
    if (!bar) return "";

    const head = [
      `<strong>${regionLabel(bar.region)}</strong>`,
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
      detail = shown.map((h) => {
        const name = h.instrument_name || "—";
        const code = h.instrument_code ? `（${h.instrument_code}）` : "";
        return `${name}${code}：当前 ${formatMoneyScaled(
          h.current_amount_minor,
          currency,
        )} / 目标 ${formatMoneyScaled(h.target_amount_minor, currency)}`;
      });
      const remaining = holdings.length - shown.length;
      if (remaining > 0) detail.push(`…另有 ${remaining} 个资产`);
    }

    return [...head, "—", ...detail].join("<br/>");
  };

  const option = {
    tooltip: {
      trigger: "axis" as const,
      formatter,
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
