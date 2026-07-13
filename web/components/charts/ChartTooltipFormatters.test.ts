import { describe, expect, it } from "vitest";
import type { ResearchRunPoint } from "@/lib/api/research";
import type { AllocationBar, QuantilePoint, RegionBar } from "@/types/api";
import { formatQuickFireTooltip } from "@/components/quick-fire/QuickFireChart";
import { formatRunChartTooltip } from "@/components/research/RunCharts";
import { formatRollingValue } from "@/components/research/RunRollingCharts";
import { formatAllocationBarTooltip } from "./AllocationBarChart";
import { formatDonutTooltip } from "./DonutChart";
import { formatRegionAllocationTooltip } from "./RegionAllocationBarChart";
import { formatReturnSeriesTooltip } from "./ReturnSeriesChart";
import {
  formatHeatmapTooltip,
  formatParameterCurveTooltip,
  formatTornadoTooltip,
} from "./SensitivityCharts";
import { formatWealthPathTooltip } from "./WealthPathChart";

const holding = {
  instrument_name: "全球股票基金",
  instrument_code: "FUND-1",
  current_amount_minor: 1_234_567_890_00,
  target_amount_minor: 1_500_000_000_00,
  current_weight: 0.45,
  target_weight: 0.5,
};

describe("chart tooltip pure formatters", () => {
  it("formats first and last allocation bars, large money and missing indices", () => {
    const allocation: AllocationBar[] = [
      { asset_class: "equity", target_weight: 0.5, current_weight: 0.45, target_amount_minor: holding.target_amount_minor, current_amount_minor: holding.current_amount_minor, holdings: [holding] },
      { asset_class: "cash", target_weight: 0.5, current_weight: 0.55, target_amount_minor: 10_000_00, current_amount_minor: 11_000_00, holdings: [] },
    ];
    expect(formatAllocationBarTooltip(allocation, "CNY", [{ dataIndex: 0 }]))
      .toContain("¥12.35 亿元");
    expect(formatAllocationBarTooltip(allocation, "CNY", [{ dataIndex: 1 }]))
      .toContain("暂无资产明细");
    expect(formatAllocationBarTooltip(allocation, "CNY", [{ dataIndex: 9 }]))
      .toBe("无数据");

    const regions: RegionBar[] = [
      { region: "domestic", target_weight: 0.5, current_weight: 0.45, target_amount_minor: holding.target_amount_minor, current_amount_minor: holding.current_amount_minor, holdings: [holding] },
    ];
    expect(formatRegionAllocationTooltip(regions, "CNY", [{ dataIndex: 0 }]))
      .toContain("全球股票基金（FUND-1）");
    expect(formatRegionAllocationTooltip(regions, "CNY", [{ dataIndex: 1 }]))
      .toBe("无数据");
  });

  it("formats donut values and treats missing values as missing", () => {
    expect(formatDonutTooltip("全组合", { name: "权益", percent: 62.345, value: 62.345 }))
      .toBe("权益<br/>占全组合：62.3%<br/>输入权重：62.34%");
    expect(formatDonutTooltip("大类内部", {})).toContain("无数据");
  });

  it("formats quick FIRE large and negative gaps without inventing missing data", () => {
    const html = formatQuickFireTooltip([
      { axisValueLabel: "55 岁", seriesName: "名义资产", value: 400_000_00 },
      { axisValueLabel: "55 岁", seriesName: "所需资本", value: 800_000_00 },
    ]);
    expect(html).toContain("年龄：55 岁");
    expect(html).toContain("资金缺口：¥400,000.00");
    expect(formatQuickFireTooltip([])).toBe("无数据");
    expect(formatQuickFireTooltip([{ seriesName: "名义资产", value: Number.NaN }]))
      .toContain("名义资产：无数据");
  });

  it("formats first, last and missing wealth and return-series points", () => {
    const wealth: QuantilePoint[] = [
      { month_offset: 0, p00_minor: -10_000_00, p05_minor: 0, p25_minor: 100_000_00, p50_minor: 120_000_00, p75_minor: 150_000_00, p95_minor: 180_000_00 },
      { month_offset: 120, p00_minor: -100_000_00, p05_minor: -50_000_00, p25_minor: -20_000_00, p50_minor: 1_200_000_00, p75_minor: 5_000_000_00, p95_minor: 8_000_000_00 },
    ];
    expect(formatWealthPathTooltip(wealth, [{ dataIndex: 0 }])).toContain("第 0 月");
    expect(formatWealthPathTooltip(wealth, [{ dataIndex: 1 }])).toContain("-¥2.00w");
    expect(formatWealthPathTooltip(wealth, [{ dataIndex: 2 }])).toBe("无数据");

    const returns = [
      { date: "2020-01-01", value: 1, cumulative_return: 0 },
      { date: "2026-01-01", value: 0.8, cumulative_return: -0.2 },
    ];
    expect(formatReturnSeriesTooltip(returns, "nav", [{ dataIndex: 0 }])).toContain("2020-01-01");
    expect(formatReturnSeriesTooltip(returns, "nav", [{ dataIndex: 1 }])).toContain("-20%");
    expect(formatReturnSeriesTooltip(returns, "nav", [{ dataIndex: 9 }])).toBe("无数据");
  });

  it("formats sensitivity values, negative deltas and missing values", () => {
    expect(formatTornadoTooltip([{ name: "收益", value: 0.6 }], { low_label: "-2%" }))
      .toContain("高扰动（高值）：无数据");
    expect(formatParameterCurveTooltip("支出", 0.8, [{ axisValue: "+10%", value: 0.65 }]))
      .toContain("相对基准：-15%");
    expect(formatParameterCurveTooltip("支出", 0.8, [])).toContain("无数据");
    expect(formatHeatmapTooltip([0, 0, Number.NaN], ["+5%"], ["-1%"])).toContain("成功率：无数据");
  });

  it("formats research first/last points, contributions and insufficient rolling history", () => {
    const points: ResearchRunPoint[] = [
      { date: "2020-01-01", nav: 1, cumulative_return: 0, period_return: 0, drawdown: 0 },
      { date: "2026-01-01", nav: 0.75, cumulative_return: -0.25, period_return: -0.1, drawdown: -0.4, weights: { stock: 0.8 }, contributions: { stock: -0.12 } },
    ];
    expect(formatRunChartTooltip(points, {}, [{ axisValue: "2020-01-01" }])).toContain("组合净值 1.0000");
    const last = formatRunChartTooltip(points, { stock: "股票" }, [{ axisValue: "2026-01-01" }]);
    expect(last).toContain("回撤 -40%");
    expect(last).toContain("股票：权重 80%，当期贡献 -12%");
    expect(formatRunChartTooltip(points, {}, [{ axisValue: "2030-01-01" }])).toBe("2030-01-01");
    expect(formatRollingValue(null)).toBe("无数据（历史不足）");
    expect(formatRollingValue(-0.1234)).toBe("-12.34%");
  });
});
