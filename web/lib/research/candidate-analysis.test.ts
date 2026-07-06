import { describe, expect, it } from "vitest";
import {
  annualReturnMatrix,
  averageCorrelation,
  commonWindow,
  correlationMatrix,
  currencyDistribution,
  estimateCommonWindow,
  normalizeCandidates,
  returnCorrelation,
} from "./candidate-analysis";

function series(assetKey: string, points: [string, number][]) {
  return {
    assetKey,
    name: assetKey,
    points: points.map(([date, value]) => ({ date, value })),
  };
}

describe("commonWindow", () => {
  it("returns the intersection of date ranges", () => {
    const win = commonWindow([
      series("A", [["2024-01-01", 1], ["2024-06-30", 1.1]]),
      series("B", [["2024-03-01", 2], ["2024-12-31", 2.2]]),
    ]);
    expect(win).toEqual({ start: "2024-03-01", end: "2024-06-30" });
  });

  it("returns null when ranges do not overlap or a series is empty", () => {
    expect(
      commonWindow([
        series("A", [["2024-01-01", 1], ["2024-02-01", 1]]),
        series("B", [["2024-03-01", 2], ["2024-04-01", 2]]),
      ]),
    ).toBeNull();
    expect(commonWindow([series("A", []), series("B", [["2024-01-01", 1]])])).toBeNull();
  });
});

describe("normalizeCandidates", () => {
  it("normalizes each series to 1.0 at the common start with forward fill", () => {
    const normalized = normalizeCandidates([
      series("A", [
        ["2024-01-01", 10],
        ["2024-01-02", 11],
        ["2024-01-04", 12],
      ]),
      series("B", [
        ["2024-01-01", 100],
        ["2024-01-03", 110],
        ["2024-01-04", 121],
      ]),
    ]);
    expect(normalized).toHaveLength(2);
    const a = normalized[0]!;
    const b = normalized[1]!;
    expect(a.dates).toEqual(["2024-01-01", "2024-01-02", "2024-01-03", "2024-01-04"]);
    expect(a.navs[0]).toBe(1);
    // A forward-fills 01-03 at 11.
    expect(a.navs[2]).toBeCloseTo(1.1, 10);
    expect(b.navs[0]).toBe(1);
    expect(b.navs[3]).toBeCloseTo(1.21, 10);
  });

  it("computes non-positive drawdowns from the running peak", () => {
    const normalized = normalizeCandidates([
      series("A", [
        ["2024-01-01", 10],
        ["2024-01-02", 8],
        ["2024-01-03", 12],
      ]),
      series("B", [
        ["2024-01-01", 1],
        ["2024-01-02", 1],
        ["2024-01-03", 1],
      ]),
    ]);
    const a = normalized[0]!;
    expect(a.drawdowns[0]).toBe(0);
    expect(a.drawdowns[1]).toBeCloseTo(-0.2, 10);
    expect(a.drawdowns[2]).toBe(0);
  });
});

describe("annualReturnMatrix", () => {
  it("uses the prior year close as base when available", () => {
    const normalized = normalizeCandidates([
      series("A", [
        ["2023-12-29", 10],
        ["2024-06-01", 12],
        ["2024-12-30", 15],
      ]),
      series("B", [
        ["2023-12-29", 1],
        ["2024-06-01", 1],
        ["2024-12-30", 1],
      ]),
    ]);
    const matrix = annualReturnMatrix(normalized);
    expect(matrix.years).toEqual([2023, 2024]);
    const a2024 = matrix.rows[0]![1];
    expect(a2024).toBeCloseTo(0.5, 10);
  });

  it("returns empty for no series", () => {
    expect(annualReturnMatrix([])).toEqual({ years: [], rows: [] });
  });
});

describe("returnCorrelation / correlationMatrix / averageCorrelation", () => {
  it("detects perfect positive correlation", () => {
    const a = [1, 1.1, 1.2, 1.1, 1.3];
    const b = [2, 2.2, 2.4, 2.2, 2.6];
    expect(returnCorrelation(a, b)).toBeCloseTo(1, 8);
  });

  it("detects perfect negative correlation", () => {
    const a = [1, 1.1, 1, 1.1, 1];
    const b = [1, 0.9, 1, 0.9, 1];
    const corr = returnCorrelation(a, b);
    expect(corr).not.toBeNull();
    expect(corr!).toBeLessThan(-0.9);
  });

  it("returns null for flat series and short series", () => {
    expect(returnCorrelation([1, 1, 1, 1], [1, 1.1, 1.2, 1.3])).toBeNull();
    expect(returnCorrelation([1, 2], [1, 2])).toBeNull();
  });

  it("builds a symmetric matrix with unit diagonal and averages off-diagonals", () => {
    const normalized = normalizeCandidates([
      series("A", [
        ["2024-01-01", 1],
        ["2024-01-02", 1.1],
        ["2024-01-03", 1.2],
        ["2024-01-04", 1.1],
      ]),
      series("B", [
        ["2024-01-01", 2],
        ["2024-01-02", 2.2],
        ["2024-01-03", 2.4],
        ["2024-01-04", 2.2],
      ]),
    ]);
    const matrix = correlationMatrix(normalized);
    expect(matrix[0]![0]).toBe(1);
    expect(matrix[1]![1]).toBe(1);
    expect(matrix[0]![1]).toBeCloseTo(matrix[1]![0]!, 12);
    expect(averageCorrelation(matrix)).toBeCloseTo(1, 8);
  });

  it("averageCorrelation returns null when nothing computable", () => {
    expect(averageCorrelation([[1]])).toBeNull();
  });
});

describe("currencyDistribution", () => {
  it("counts currencies", () => {
    expect(
      currencyDistribution([{ currency: "CNY" }, { currency: "CNY" }, { currency: "USD" }]),
    ).toEqual({ CNY: 2, USD: 1 });
  });
});

describe("estimateCommonWindow", () => {
  it("intersects metrics windows and skips cash", () => {
    const win = estimateCommonWindow([
      { is_cash: false, metrics: { start_date: "2020-01-01", end_date: "2026-06-30" } },
      { is_cash: false, metrics: { start_date: "2022-05-01", end_date: "2026-05-31" } },
      { is_cash: true, metrics: null },
    ]);
    expect(win).toEqual({ start: "2022-05-01", end: "2026-05-31" });
  });

  it("returns null when a non-cash candidate lacks metrics", () => {
    expect(
      estimateCommonWindow([
        { is_cash: false, metrics: { start_date: "2020-01-01", end_date: "2026-06-30" } },
        { is_cash: false, metrics: null },
      ]),
    ).toBeNull();
  });

  it("returns null for cash-only pools", () => {
    expect(estimateCommonWindow([{ is_cash: true }])).toBeNull();
  });
});
