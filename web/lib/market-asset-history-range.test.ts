import { describe, expect, it } from "vitest";
import type { MarketAssetPoint } from "@/lib/api/market-assets";
import {
  defaultHistoryRange,
  filterHistoryPoints,
  HISTORY_RANGE_OPTIONS,
  historyRangeLabel,
  isHistoryRangeAvailable,
  parseDateOnly,
  toChartPoints,
} from "./market-asset-history-range";

const pt = (date: string, value: number): MarketAssetPoint => ({ date, value });

describe("parseDateOnly", () => {
  it("parses YYYY-MM-DD as a local date without timezone shift", () => {
    const d = parseDateOnly("2026-07-01");
    expect(d.getFullYear()).toBe(2026);
    expect(d.getMonth()).toBe(6);
    expect(d.getDate()).toBe(1);
  });
});

describe("filterHistoryPoints", () => {
  it("7d keeps only the last 7 calendar days ending at the last point", () => {
    const points = [
      pt("2026-06-20", 1),
      pt("2026-06-24", 2),
      pt("2026-06-28", 3),
      pt("2026-07-01", 4),
    ];
    // end 2026-07-01 → start 2026-06-24 (inclusive).
    expect(filterHistoryPoints(points, "7d").map((p) => p.date)).toEqual([
      "2026-06-24",
      "2026-06-28",
      "2026-07-01",
    ]);
  });

  it("1m crosses month boundaries", () => {
    const points = [
      pt("2026-05-30", 1),
      pt("2026-06-01", 2),
      pt("2026-06-15", 3),
      pt("2026-07-01", 4),
    ];
    // end 2026-07-01 → start 2026-06-01.
    expect(filterHistoryPoints(points, "1m").map((p) => p.date)).toEqual([
      "2026-06-01",
      "2026-06-15",
      "2026-07-01",
    ]);
  });

  it("3m crosses a year boundary", () => {
    const points = [
      pt("2025-10-14", 1),
      pt("2025-10-15", 2),
      pt("2025-12-31", 3),
      pt("2026-01-15", 4),
    ];
    // end 2026-01-15 → start 2025-10-15.
    expect(filterHistoryPoints(points, "3m").map((p) => p.date)).toEqual([
      "2025-10-15",
      "2025-12-31",
      "2026-01-15",
    ]);
  });

  it("1y crosses a year boundary", () => {
    const points = [
      pt("2025-06-30", 1),
      pt("2025-07-01", 2),
      pt("2026-01-01", 3),
      pt("2026-07-01", 4),
    ];
    expect(filterHistoryPoints(points, "1y").map((p) => p.date)).toEqual([
      "2025-07-01",
      "2026-01-01",
      "2026-07-01",
    ]);
  });

  it("normalizes month-end shifts like Go AddDate (Mar 31 - 1m → Mar 3)", () => {
    const points = [pt("2026-03-02", 1), pt("2026-03-03", 2), pt("2026-03-31", 3)];
    expect(filterHistoryPoints(points, "1m").map((p) => p.date)).toEqual([
      "2026-03-03",
      "2026-03-31",
    ]);
  });

  it("all returns the full series untouched", () => {
    const points = [pt("2020-01-01", 1), pt("2026-07-01", 2)];
    expect(filterHistoryPoints(points, "all")).toBe(points);
  });

  it("empty input stays empty for any range", () => {
    expect(filterHistoryPoints([], "1y")).toEqual([]);
  });
});

describe("toChartPoints", () => {
  it("re-zeroes cumulative return on the first visible point", () => {
    const filtered = filterHistoryPoints(
      [pt("2025-01-01", 100), pt("2025-08-01", 150), pt("2026-07-01", 300)],
      "1y",
    );
    const chart = toChartPoints(filtered);
    expect(chart.map((p) => p.date)).toEqual(["2025-08-01", "2026-07-01"]);
    expect(chart[0]?.cumulative_return).toBe(0);
    expect(chart[1]?.cumulative_return).toBeCloseTo(1);
  });

  it("guards a non-positive base", () => {
    const chart = toChartPoints([pt("2026-01-01", 0), pt("2026-02-01", 10)]);
    expect(chart.map((p) => p.cumulative_return)).toEqual([0, 0]);
  });
});

describe("isHistoryRangeAvailable", () => {
  const twoYearsSparse = [pt("2024-07-01", 1), pt("2026-07-01", 2)];

  it("all is always available", () => {
    expect(isHistoryRangeAvailable([], "all")).toBe(true);
  });

  it("requires at least 2 points inside the range", () => {
    // Only the last point falls inside 1y.
    expect(isHistoryRangeAvailable(twoYearsSparse, "1y")).toBe(false);
    expect(isHistoryRangeAvailable(twoYearsSparse, "3y")).toBe(true);
  });
});

describe("defaultHistoryRange", () => {
  it("uses 1y when coverage exceeds one year", () => {
    const points = [pt("2024-07-01", 1), pt("2025-08-01", 2), pt("2026-07-01", 3)];
    expect(defaultHistoryRange(points)).toBe("1y");
  });

  it("uses 3m when coverage is between 3 months and 1 year", () => {
    const points = [pt("2026-01-01", 1), pt("2026-05-01", 2), pt("2026-07-01", 3)];
    expect(defaultHistoryRange(points)).toBe("3m");
  });

  it("uses all when coverage is 3 months or less", () => {
    const points = [pt("2026-05-01", 1), pt("2026-07-01", 2)];
    expect(defaultHistoryRange(points)).toBe("all");
  });

  it("falls back to all when the candidate range has too few points", () => {
    // Coverage spans 2 years but the 1y window only holds the last point.
    const points = [pt("2024-07-01", 1), pt("2026-07-01", 2)];
    expect(defaultHistoryRange(points)).toBe("all");
  });

  it("uses all for an empty or single-point series", () => {
    expect(defaultHistoryRange([])).toBe("all");
    expect(defaultHistoryRange([pt("2026-07-01", 1)])).toBe("all");
  });
});

describe("historyRangeLabel", () => {
  it("maps every option key to its label", () => {
    for (const option of HISTORY_RANGE_OPTIONS) {
      expect(historyRangeLabel(option.key)).toBe(option.label);
    }
  });
});
