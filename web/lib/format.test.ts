import { describe, expect, it } from "vitest";
import { annualCompletenessLabel, dataSourceLabel, formatAnnualPeriod } from "./format";

describe("annualCompletenessLabel", () => {
  it("marks current year as in-year stats", () => {
    const year = new Date().getFullYear();
    expect(annualCompletenessLabel({ year, is_partial: false, end_date: `${year}-06-09` })).toBe(
      "年内统计",
    );
  });

  it("marks missing anchor as incomplete", () => {
    expect(annualCompletenessLabel({ year: 2010, is_partial: true })).toBe("不完整");
  });
});

describe("formatAnnualPeriod", () => {
  it("shows full cross-year range", () => {
    expect(formatAnnualPeriod("2024-12-30", "2025-12-29")).toBe("2024-12-30 ~ 2025-12-29");
  });
});

describe("dataSourceLabel", () => {
  it("maps known adapter ids", () => {
    expect(dataSourceLabel("ak.stock_zh_a_hist_tx")).toBe("腾讯财经 · 前复权");
    expect(dataSourceLabel("ak.fund_etf_hist_sina")).toBe("新浪财经 · ETF");
  });

  it("falls back to raw id", () => {
    expect(dataSourceLabel("ak.custom_source")).toBe("ak.custom_source");
    expect(dataSourceLabel(undefined)).toBe("—");
  });
});
