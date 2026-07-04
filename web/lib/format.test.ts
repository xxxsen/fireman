import { describe, expect, it } from "vitest";
import { annualCompletenessLabel, compressYears, dataSourceLabel, formatAnnualPeriod, formatDateFromMs, formatMoneyInlineUnit, formatMoneyScaled, formatMoneyUnitHint, formatMoneyWan, representativePercentileRank, sortRepresentativePaths } from "./format";

describe("formatMoneyWan", () => {
  it("converts minor to 万元 with two decimals and no separators", () => {
    expect(formatMoneyWan(5_787_302_02)).toBe("¥578.73w");
    expect(formatMoneyWan(100_000_00)).toBe("¥10.00w");
  });

  it("keeps the negative sign before the symbol", () => {
    expect(formatMoneyWan(-12_300_00)).toBe("-¥1.23w");
  });

  it("renders empty/NaN values as —", () => {
    expect(formatMoneyWan(null)).toBe("—");
    expect(formatMoneyWan(undefined)).toBe("—");
    expect(formatMoneyWan(Number.NaN)).toBe("—");
  });
});

describe("representative path ordering", () => {
  it("ranks percentiles p00<p25<p50<p75<p95, unknown last", () => {
    expect(representativePercentileRank("p00")).toBeLessThan(representativePercentileRank("p95"));
    expect(representativePercentileRank("p50")).toBe(2);
    expect(representativePercentileRank("weird")).toBe(5);
    expect(representativePercentileRank(undefined)).toBe(5);
  });

  it("sorts ascending by percentile with stable path_no tiebreak", () => {
    const sorted = sortRepresentativePaths([
      { representative_percentile: "p95", path_no: 9 },
      { representative_percentile: "p00", path_no: 1 },
      { representative_percentile: "p50", path_no: 5 },
      { representative_percentile: "p25", path_no: 3 },
      { representative_percentile: "p75", path_no: 7 },
    ]);
    expect(sorted.map((p) => p.representative_percentile)).toEqual([
      "p00",
      "p25",
      "p50",
      "p75",
      "p95",
    ]);
  });
});

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

describe("formatMoneyScaled", () => {
  it("scales amounts to 元 / 万元 / 亿元", () => {
    expect(formatMoneyScaled(500_00)).toBe("¥500.00 元");
    expect(formatMoneyScaled(12_345_600_00)).toBe("¥1,234.56 万元");
    expect(formatMoneyScaled(123_456_700_00)).toBe("¥1.23 亿元");
  });

  it("handles zero and negative amounts", () => {
    expect(formatMoneyScaled(0)).toBe("¥0.00 元");
    expect(formatMoneyScaled(-12_345_600_00)).toBe("¥-1,234.56 万元");
  });
});

describe("formatDateFromMs", () => {
  it("formats millisecond timestamps", () => {
    const ts = Date.UTC(2026, 5, 19);
    expect(formatDateFromMs(ts)).toBe(new Date(ts).toLocaleDateString("zh-CN"));
  });

  it("returns dash for empty values", () => {
    expect(formatDateFromMs(0)).toBe("—");
    expect(formatDateFromMs(null)).toBe("—");
    expect(formatDateFromMs(undefined)).toBe("—");
  });
});

describe("compressYears", () => {
  it("compresses a single continuous range", () => {
    expect(compressYears([2006, 2007, 2008, 2009])).toBe("2006-2009");
  });

  it("splits non-contiguous years into multiple ranges", () => {
    expect(compressYears([2006, 2007, 2008, 2010, 2011])).toBe("2006-2008、2010-2011");
  });

  it("keeps single years standalone and handles unordered input", () => {
    expect(compressYears([2014, 2006])).toBe("2006、2014");
  });

  it("returns dash for empty list", () => {
    expect(compressYears([])).toBe("—");
  });
});

describe("dataSourceLabel", () => {
  it("maps known adapter ids", () => {
    expect(dataSourceLabel("ak.stock_zh_a_hist_tx")).toBe("腾讯财经 · 前复权");
    expect(dataSourceLabel("ak.fund_etf_hist_sina")).toBe("新浪财经 · ETF");
  });

  it("maps known id with a data-type suffix", () => {
    expect(dataSourceLabel("ak.fund_open_fund_info_em:累计净值走势")).toBe(
      "东方财富 · 公募基金 · 累计净值走势",
    );
    expect(dataSourceLabel("ak.fund_open_fund_info_em:单位净值走势")).toBe(
      "东方财富 · 公募基金 · 单位净值走势",
    );
  });

  it("maps known id without a suffix", () => {
    expect(dataSourceLabel("ak.fund_open_fund_info_em")).toBe("东方财富 · 公募基金");
  });

  it("maps the TickFlow daily kline source", () => {
    expect(dataSourceLabel("tickflow.klines:1d")).toBe("TickFlow · 日K");
  });

  it("never exposes raw adapter ids for unknown sources", () => {
    expect(dataSourceLabel("ak.custom_source")).toBe("行情数据");
    expect(dataSourceLabel("ak.custom_source:某字段")).toBe("行情数据 · 某字段");
    expect(dataSourceLabel("ak.custom_source")).not.toContain("ak.");
  });

  it("renders a placeholder for empty input", () => {
    expect(dataSourceLabel(undefined)).toBe("—");
    expect(dataSourceLabel(null)).toBe("—");
    expect(dataSourceLabel("")).toBe("—");
  });
});
