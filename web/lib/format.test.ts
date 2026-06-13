import { describe, expect, it } from "vitest";
import { annualCompletenessLabel, dataSourceLabel, formatAnnualPeriod, formatMoneyInlineUnit, formatMoneyUnitHint } from "./format";

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

describe("formatMoneyInlineUnit", () => {
  it("shows inline unit without converting the raw value", () => {
    expect(formatMoneyInlineUnit("CNY", "150")).toBe("CNY(百)");
    expect(formatMoneyInlineUnit("CNY", "1500")).toBe("CNY(千)");
    expect(formatMoneyInlineUnit("CNY", "15000")).toBe("CNY(万)");
    expect(formatMoneyInlineUnit("CNY", "1500000")).toBe("CNY(百万)");
    expect(formatMoneyInlineUnit("CNY", "")).toBe("CNY");
  });
});

describe("formatMoneyUnitHint", () => {
  it("shows wan hint for plain numeric amounts", () => {
    expect(formatMoneyUnitHint(15000)).toBe("约 1.50 万");
    expect(formatMoneyUnitHint(2500000)).toBe("约 250.00 万");
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
