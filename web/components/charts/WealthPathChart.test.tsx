import { describe, expect, it } from "vitest";
import { buildWealthPathOption } from "./WealthPathChart";
import type { QuantilePoint } from "@/types/api";

function point(month: number, p25: number, p50: number, p75: number): QuantilePoint {
  return {
    month_offset: month,
    p00_minor: p25 - 1000,
    p05_minor: p25 - 500,
    p25_minor: p25,
    p50_minor: p50,
    p75_minor: p75,
    p95_minor: p75 + 1000,
  };
}

describe("buildWealthPathOption", () => {
  const series = [
    point(0, 100_000_00, 120_000_00, 150_000_00),
    point(1, 110_000_00, 130_000_00, 170_000_00),
  ];
  const option = buildWealthPathOption(series);

  it("only lists P25-P75 and P50 in the legend, bottom-aligned", () => {
    expect(option.legend.data).toEqual(["P25-P75", "P50"]);
    expect(option.legend.bottom).toBe(0);
    expect(option.grid.bottom).toBeGreaterThanOrEqual(48);
  });

  it("uses p25 as the band baseline and p75-p25 as the band width", () => {
    const base = option.series[0]!;
    const band = option.series[1]!;
    expect(base.name).toBe("__p25_base");
    expect(base.stack).toBe("band");
    expect(base.data).toEqual([100_000_00, 110_000_00]);
    expect(band.name).toBe("P25-P75");
    expect(band.stack).toBe("band");
    // width = p75 - p25, never p50-based
    expect(band.data).toEqual([50_000_00, 60_000_00]);
  });

  it("renders P50 as a separate dark median line from p50_minor", () => {
    const median = option.series[2]!;
    expect(median.name).toBe("P50");
    expect(median.stack).toBeUndefined();
    expect(median.data).toEqual([120_000_00, 130_000_00]);
  });

  it("hides the baseline series from legend and tooltip", () => {
    const base = option.series[0]!;
    expect(option.legend.data).not.toContain("__p25_base");
    expect(base.tooltip?.show).toBe(false);
    expect(base.silent).toBe(true);
  });

  it("formats axis labels as ¥xx.xxw", () => {
    expect(option.yAxis.axisLabel.formatter(1_234_500)).toBe("¥1.23w");
  });

  it("tooltip shows month, P25-P75 range and P50 in w format without internals", () => {
    const html = option.tooltip.formatter([{ dataIndex: 0 }]);
    expect(html).toContain("第 0 月");
    expect(html).toContain("P25-P75：¥10.00w - ¥15.00w");
    expect(html).toContain("P50：¥12.00w");
    expect(html).not.toContain("_p75");
    expect(html).not.toContain("__p25_base");
    expect(html).not.toContain("P05-P95");
  });

  it("does not crash on degenerate p25=p50=p75 sample", () => {
    const flat = buildWealthPathOption([point(0, 100_000_00, 100_000_00, 100_000_00)]);
    expect(flat.series[1]!.data).toEqual([0]);
    expect(flat.tooltip.formatter([{ dataIndex: 0 }])).toContain("¥10.00w");
  });
});
