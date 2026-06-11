/** Shared ECharts layout for target vs current comparison bar charts. */
export const comparisonBarChartLayout = {
  legend: {
    data: ["目标", "当前"],
    bottom: 0,
    left: "center",
  },
  grid: {
    left: 48,
    right: 16,
    top: 16,
    bottom: 40,
  },
} as const;

type AxisTooltipParam = {
  axisValue?: string;
  seriesName?: string;
  value?: number;
  marker?: string;
};

/** Format axis tooltip values as percentages with up to 2 decimal places. */
export function formatComparisonBarTooltip(
  params: AxisTooltipParam | AxisTooltipParam[],
): string {
  const items = Array.isArray(params) ? params : [params];
  if (items.length === 0) return "";
  const title = items[0]?.axisValue ?? "";
  const lines = items.map((item) => {
    const value = Number(item.value ?? 0);
    const pct = `${(value * 100).toFixed(2)}%`;
    return `${item.marker ?? ""}${item.seriesName ?? ""}: ${pct}`;
  });
  return title ? `${title}<br/>${lines.join("<br/>")}` : lines.join("<br/>");
}
