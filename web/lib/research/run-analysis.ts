import type { ResearchRunPoint } from "@/lib/api/research";

/**
 * Client-side derivations over research backtest points: rolling metrics,
 * monthly downsampling and weight deviations. Pure functions, unit-tested.
 */

function dateToUTC(date: string): number {
  return Date.parse(`${date}T00:00:00Z`);
}

function monthsEarlier(date: string, months: number): number {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCMonth(d.getUTCMonth() - months);
  return d.getTime();
}

export interface RollingPoint {
  date: string;
  value: number | null;
}

/**
 * Rolling N-month cumulative return: nav(t) / nav(t - N months) - 1. Null
 * until a full window is available.
 */
export function rollingReturn(points: ResearchRunPoint[], months: number): RollingPoint[] {
  const out: RollingPoint[] = [];
  let startIdx = 0;
  for (const p of points) {
    const windowStart = monthsEarlier(p.date, months);
    while (startIdx < points.length && dateToUTC(points[startIdx]!.date) < windowStart) {
      startIdx++;
    }
    // Base = the observation exactly at t-N months when it exists, otherwise
    // the last observation before it (the value in effect at that time).
    let base = points[startIdx];
    if (!base || dateToUTC(base.date) !== windowStart) {
      if (startIdx === 0) {
        // The very first point can never span a full window.
        out.push({ date: p.date, value: null });
        continue;
      }
      base = points[startIdx - 1];
    }
    if (!base || dateToUTC(base.date) > windowStart || base.nav <= 0) {
      out.push({ date: p.date, value: null });
      continue;
    }
    out.push({ date: p.date, value: p.nav / base.nav - 1 });
  }
  return out;
}

/**
 * Rolling N-month annualized volatility from daily period returns
 * (sample stddev × √252). Null until the window holds ≥ 20 observations.
 */
export function rollingVolatility(points: ResearchRunPoint[], months: number): RollingPoint[] {
  const out: RollingPoint[] = [];
  for (let i = 0; i < points.length; i++) {
    const p = points[i]!;
    const windowStart = monthsEarlier(p.date, months);
    const returns: number[] = [];
    for (let j = i; j >= 1; j--) {
      if (dateToUTC(points[j]!.date) < windowStart) break;
      returns.push(points[j]!.period_return);
    }
    if (returns.length < 20) {
      out.push({ date: p.date, value: null });
      continue;
    }
    const mean = returns.reduce((s, v) => s + v, 0) / returns.length;
    const variance =
      returns.reduce((s, v) => s + (v - mean) * (v - mean), 0) / (returns.length - 1);
    out.push({ date: p.date, value: Math.sqrt(variance) * Math.sqrt(252) });
  }
  return out;
}

/** Rolling N-month max drawdown (≤ 0) within the trailing window. */
export function rollingMaxDrawdown(points: ResearchRunPoint[], months: number): RollingPoint[] {
  const out: RollingPoint[] = [];
  for (let i = 0; i < points.length; i++) {
    const p = points[i]!;
    const windowStart = monthsEarlier(p.date, months);
    let peak = -Infinity;
    let maxDD = 0;
    let count = 0;
    for (let j = 0; j <= i; j++) {
      const q = points[j]!;
      if (dateToUTC(q.date) < windowStart) continue;
      count++;
      if (q.nav > peak) peak = q.nav;
      if (peak > 0) {
        const dd = q.nav / peak - 1;
        if (dd < maxDD) maxDD = dd;
      }
    }
    out.push({ date: p.date, value: count >= 2 ? maxDD : null });
  }
  return out;
}

/** Keep only each month's last point (for the monthly chart view). */
export function monthlyDownsample(points: ResearchRunPoint[]): ResearchRunPoint[] {
  const out: ResearchRunPoint[] = [];
  for (let i = 0; i < points.length; i++) {
    const cur = points[i]!;
    const next = points[i + 1];
    if (!next || next.date.slice(0, 7) !== cur.date.slice(0, 7)) {
      out.push(cur);
    }
  }
  return out;
}

/**
 * Max |actual - target| weight deviation at the first and last point of each
 * year; requires points fetched with include_weights.
 */
export function annualWeightDeviation(
  points: ResearchRunPoint[],
  targetWeights: Record<string, number>,
): Map<number, { start: number | null; end: number | null }> {
  const byYear = new Map<number, ResearchRunPoint[]>();
  for (const p of points) {
    const year = Number(p.date.slice(0, 4));
    const list = byYear.get(year) ?? [];
    list.push(p);
    byYear.set(year, list);
  }
  const out = new Map<number, { start: number | null; end: number | null }>();
  for (const [year, list] of byYear) {
    out.set(year, {
      start: weightDeviation(list[0], targetWeights),
      end: weightDeviation(list[list.length - 1], targetWeights),
    });
  }
  return out;
}

function weightDeviation(
  point: ResearchRunPoint | undefined,
  targetWeights: Record<string, number>,
): number | null {
  if (!point?.weights) return null;
  let max = 0;
  for (const [key, target] of Object.entries(targetWeights)) {
    const actual = point.weights[key] ?? 0;
    const dev = Math.abs(actual - target);
    if (dev > max) max = dev;
  }
  return max;
}
