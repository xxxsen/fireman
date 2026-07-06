import { describe, expect, it } from "vitest";
import {
  activeFilterCount,
  EMPTY_FILTERS,
  filtersFromJSON,
  filtersToJSON,
  filtersToParams,
  type ScreenerFilters,
} from "./screener-filters";

function base(overrides: Partial<ScreenerFilters> = {}): ScreenerFilters {
  return { ...EMPTY_FILTERS, instrumentTypes: [], currencies: [], ...overrides };
}

describe("filtersToParams", () => {
  it("omits empty fields entirely", () => {
    expect(filtersToParams(base())).toEqual({});
  });

  it("converts percent inputs to ratios and passes ratio fields through", () => {
    const params = filtersToParams(
      base({
        minCagr: "8",
        maxVolatility: "25",
        minMaxDrawdown: "-30",
        minSharpe: "0.5",
        minHistoryYears: "3",
      }),
    );
    expect(params.minCagr).toBeCloseTo(0.08, 12);
    expect(params.maxVolatility).toBeCloseTo(0.25, 12);
    expect(params.minMaxDrawdown).toBeCloseTo(-0.3, 12);
    expect(params.minSharpe).toBeCloseTo(0.5, 12);
    expect(params.minHistoryYears).toBe(3);
  });

  it("ignores invalid numeric input", () => {
    expect(filtersToParams(base({ minCagr: "abc" })).minCagr).toBeUndefined();
  });

  it("maps downside volatility (percent) and return/drawdown ratio", () => {
    const params = filtersToParams(
      base({ maxDownsideVolatility: "15", minReturnDrawdown: "2.5" }),
    );
    expect(params.maxDownsideVolatility).toBeCloseTo(0.15, 12);
    expect(params.minReturnDrawdown).toBeCloseTo(2.5, 12);
  });

  it("passes lists, booleans and enums", () => {
    const params = filtersToParams(
      base({
        market: "cn",
        instrumentTypes: ["cn_exchange_fund", "cash"],
        currencies: ["CNY"],
        includeInactive: true,
        backtestReady: true,
        historyStatus: "stale",
        q: "  510300 ",
      }),
    );
    expect(params.market).toBe("cn");
    expect(params.instrumentTypes).toEqual(["cn_exchange_fund", "cash"]);
    expect(params.currencies).toEqual(["CNY"]);
    expect(params.includeInactive).toBe(true);
    expect(params.backtestReady).toBe(true);
    expect(params.historyStatus).toBe("stale");
    expect(params.q).toBe("510300");
  });
});

describe("filters JSON round trip", () => {
  it("restores every field", () => {
    const filters = base({
      market: "us",
      instrumentTypes: ["us_etf"],
      q: "SPY",
      currencies: ["USD"],
      includeInactive: true,
      historyStatus: "synced",
      minCagr: "5",
      backtestReady: true,
    });
    const restored = filtersFromJSON(filtersToJSON(filters));
    expect(restored).toEqual(filters);
  });

  it("tolerates junk input", () => {
    expect(filtersFromJSON(null)).toEqual(base());
    expect(filtersFromJSON("nope")).toEqual(base());
    expect(
      filtersFromJSON({ market: 42, instrumentTypes: "x", unknown_field: true }),
    ).toEqual(base());
    expect(filtersFromJSON({ instrumentTypes: ["a", 1, "b"] }).instrumentTypes).toEqual([
      "a",
      "b",
    ]);
  });
});

describe("activeFilterCount", () => {
  it("counts non-default conditions", () => {
    expect(activeFilterCount(base())).toBe(0);
    expect(
      activeFilterCount(base({ market: "cn", currencies: ["CNY"], minCagr: "5" })),
    ).toBe(3);
  });
});
