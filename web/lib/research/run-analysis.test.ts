import { describe, expect, it } from "vitest";
import type { ResearchRunPoint } from "@/lib/api/research";
import {
  annualWeightDeviation,
  monthlyDownsample,
  rollingMaxDrawdown,
  rollingReturn,
  rollingVolatility,
} from "./run-analysis";

function point(date: string, nav: number, extra: Partial<ResearchRunPoint> = {}): ResearchRunPoint {
  return {
    date,
    nav,
    cumulative_return: nav - 1,
    period_return: 0,
    drawdown: 0,
    ...extra,
  };
}

/** Monthly points over `months` months starting 2020-01-31, nav growing 1% per month. */
function monthlySeries(months: number): ResearchRunPoint[] {
  const out: ResearchRunPoint[] = [];
  let nav = 1;
  for (let i = 0; i < months; i++) {
    const d = new Date(Date.UTC(2020, i + 1, 0)); // last day of month i
    nav *= 1.01;
    out.push(point(d.toISOString().slice(0, 10), nav, { period_return: 0.01 }));
  }
  return out;
}

describe("rollingReturn", () => {
  it("is null until a full window exists, then matches nav ratio", () => {
    const points = monthlySeries(24);
    const rolled = rollingReturn(points, 12);
    expect(rolled[0]!.value).toBeNull();
    expect(rolled[5]!.value).toBeNull();
    const last = rolled[rolled.length - 1]!;
    // 12 months of +1% compounding.
    expect(last.value).toBeCloseTo(Math.pow(1.01, 12) - 1, 6);
  });

  it("returns all nulls for a series shorter than the window", () => {
    const rolled = rollingReturn(monthlySeries(6), 12);
    expect(rolled.every((p) => p.value === null)).toBe(true);
  });
});

describe("rollingVolatility", () => {
  it("needs at least 20 observations and reports annualized stddev", () => {
    const points: ResearchRunPoint[] = [];
    for (let i = 0; i < 60; i++) {
      const d = new Date(Date.UTC(2024, 0, 1 + i));
      points.push(
        point(d.toISOString().slice(0, 10), 1, { period_return: i % 2 === 0 ? 0.01 : -0.01 }),
      );
    }
    const vol = rollingVolatility(points, 12);
    expect(vol[5]!.value).toBeNull();
    const last = vol[vol.length - 1]!.value;
    expect(last).not.toBeNull();
    // stddev of ±1% alternating ≈ 0.01005, annualized ×√252.
    expect(last!).toBeGreaterThan(0.1);
  });
});

describe("rollingMaxDrawdown", () => {
  it("captures the deepest drawdown inside the trailing window", () => {
    const points = [
      point("2024-01-01", 1),
      point("2024-02-01", 1.2),
      point("2024-03-01", 0.9),
      point("2024-04-01", 1.1),
    ];
    const dd = rollingMaxDrawdown(points, 12);
    expect(dd[2]!.value).toBeCloseTo(0.9 / 1.2 - 1, 10);
    expect(dd[3]!.value).toBeCloseTo(0.9 / 1.2 - 1, 10);
    expect(dd[0]!.value).toBeNull();
  });
});

describe("monthlyDownsample", () => {
  it("keeps only the last point of each month", () => {
    const points = [
      point("2024-01-05", 1),
      point("2024-01-31", 1.1),
      point("2024-02-10", 1.2),
      point("2024-02-28", 1.3),
      point("2024-03-01", 1.4),
    ];
    const sampled = monthlyDownsample(points);
    expect(sampled.map((p) => p.date)).toEqual(["2024-01-31", "2024-02-28", "2024-03-01"]);
  });
});

describe("annualWeightDeviation", () => {
  it("computes max deviation at year start and end", () => {
    const points = [
      point("2024-01-02", 1, { weights: { A: 0.62, B: 0.38 } }),
      point("2024-06-30", 1.1, { weights: { A: 0.7, B: 0.3 } }),
      point("2024-12-31", 1.2, { weights: { A: 0.55, B: 0.45 } }),
    ];
    const dev = annualWeightDeviation(points, { A: 0.6, B: 0.4 });
    const y = dev.get(2024)!;
    expect(y.start).toBeCloseTo(0.02, 10);
    expect(y.end).toBeCloseTo(0.05, 10);
  });

  it("returns null deviations without weights", () => {
    const dev = annualWeightDeviation([point("2024-01-02", 1)], { A: 1 });
    expect(dev.get(2024)).toEqual({ start: null, end: null });
  });
});
