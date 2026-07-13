"use client";

import { memo, useMemo } from "react";
import ReactECharts from "echarts-for-react";
import type { QuickFireYear } from "@/lib/api/quick-fire";
import { formatMoney } from "@/lib/format";
import { ChartFrame } from "@/components/charts/ChartFrame";

interface TooltipSeriesParam {
  axisValueLabel?: string;
  seriesName?: string;
  value?: number;
  marker?: string;
}

export function formatQuickFireTooltip(raw: unknown): string {
  if (!Array.isArray(raw) || raw.length === 0) return "无数据";
  const params = raw as TooltipSeriesParam[];
  const values = new Map(params.map((item) => [item.seriesName, Number(item.value)]));
  const nominal = values.get("名义资产");
  const required = values.get("所需资本");
  const gap = typeof nominal === "number" && Number.isFinite(nominal) && typeof required === "number" && Number.isFinite(required)
    ? nominal - required
    : null;
  const rows = params.map((item) => {
    const value = Number(item.value);
    return `${item.marker ?? ""}${item.seriesName ?? "数据"}：${Number.isFinite(value) ? formatMoney(value, "CNY") : "无数据"}`;
  });
  if (gap !== null) rows.push(`资金${gap >= 0 ? "富余" : "缺口"}：${formatMoney(Math.abs(gap), "CNY")}`);
  return [`年龄：${params[0]?.axisValueLabel ?? "未知"}`, ...rows].join("<br/>");
}

export const QuickFireChart = memo(function QuickFireChart({ years }: { years: QuickFireYear[] }) {
  const option = useMemo(() => ({
    animation: false,
    aria: {
      enabled: true,
      description: "按年龄展示名义资产、折算为当前购买力的真实资产和同一年龄所需资本。",
    },
    grid: { left: 18, right: 20, top: 42, bottom: 48, containLabel: true },
    tooltip: {
      trigger: "axis",
      confine: true,
      formatter: formatQuickFireTooltip,
    },
    legend: { top: 2, data: ["名义资产", "真实资产", "所需资本"] },
    xAxis: { type: "category", name: "年龄", nameLocation: "middle", nameGap: 30, data: years.map((row) => `${row.age} 岁`) },
    yAxis: { type: "value", name: "资产金额（元）", nameGap: 12, axisLabel: { formatter: (value: number) => formatMoney(value, "CNY") } },
    series: [
      {
        name: "名义资产",
        type: "line",
        data: years.map((row) => row.end_wealth_minor),
        smooth: true,
        symbol: "none",
        lineStyle: { color: "#147d64", width: 2 },
      },
      {
        name: "真实资产",
        type: "line",
        data: years.map((row) => row.real_end_wealth_minor),
        smooth: true,
        symbol: "none",
        lineStyle: { color: "#2563eb", width: 2 },
      },
      {
        name: "所需资本",
        type: "line",
        data: years.map((row) => row.required_wealth_minor),
        smooth: true,
        symbol: "none",
        lineStyle: { color: "#c77700", width: 1.5, type: "dashed" },
      },
    ],
  }), [years]);
  return (
    <ChartFrame
      title="资产与所需资本"
      termKey="nominal_vs_real"
      xAxis="年龄"
      yAxis="资产金额"
      unit="元"
      legend="实线分别表示届时账户名义金额和折算后的当前购买力；虚线表示从该年龄开始退休所需的资本。"
      interpretation="同一年龄下，名义资产高于所需资本表示固定假设下有富余；曲线经过离散年度点的平滑连接不代表额外计算。将鼠标移到曲线或在触屏上点按可查看三条数值和富余/缺口。"
    >
      <ReactECharts option={option} style={{ height: 340 }} notMerge />
    </ChartFrame>
  );
});
