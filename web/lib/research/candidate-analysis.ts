/**
 * Client-side analytics for the screener candidate pool. Works on the raw
 * point series returned by the market-asset detail API; all computations are
 * pure so they can be unit-tested without network access.
 */

export interface CandidateSeries {
  assetKey: string;
  name: string;
  points: { date: string; value: number }[];
}

export interface NormalizedSeries {
  assetKey: string;
  name: string;
  dates: string[];
  /** NAV normalized to 1.0 at the common start. */
  navs: number[];
  /** Drawdown (<= 0) per date. */
  drawdowns: number[];
}

/** Intersection of [first, last] date ranges across candidates; null when empty. */
export function commonWindow(
  series: { points: { date: string }[] }[],
): { start: string; end: string } | null {
  let start = "";
  let end = "\uffff";
  for (const s of series) {
    if (s.points.length === 0) return null;
    const first = s.points[0]!.date;
    const last = s.points[s.points.length - 1]!.date;
    if (first > start) start = first;
    if (last < end) end = last;
  }
  if (!start || end === "\uffff" || end < start) return null;
  return { start, end };
}

/**
 * Align candidates to their common window on the union of observed dates,
 * forward-filling values, and normalize NAVs to 1.0 at the window start.
 */
export function normalizeCandidates(series: CandidateSeries[]): NormalizedSeries[] {
  const window = commonWindow(series);
  if (!window) return [];

  const dateSet = new Set<string>();
  for (const s of series) {
    for (const p of s.points) {
      if (p.date >= window.start && p.date <= window.end) dateSet.add(p.date);
    }
  }
  const dates = Array.from(dateSet).sort();
  if (dates.length === 0) return [];

  return series.map((s) => {
    const byDate = new Map(s.points.map((p) => [p.date, p.value]));
    const values: number[] = [];
    let lastValue: number | null = null;
    for (const p of s.points) {
      if (p.date < window.start) lastValue = p.value;
    }
    for (const d of dates) {
      const v = byDate.get(d);
      if (v !== undefined && v > 0) lastValue = v;
      values.push(lastValue ?? 0);
    }
    const base = values[0] || 1;
    const navs = values.map((v) => v / base);
    const drawdowns: number[] = [];
    let peak = -Infinity;
    for (const nav of navs) {
      if (nav > peak) peak = nav;
      drawdowns.push(peak > 0 ? nav / peak - 1 : 0);
    }
    return { assetKey: s.assetKey, name: s.name, dates, navs, drawdowns };
  });
}

/** Calendar-year returns per candidate over the aligned window. */
export function annualReturnMatrix(normalized: NormalizedSeries[]): {
  years: number[];
  /** rows[assetIdx][yearIdx] = annual return or null when the year is absent. */
  rows: (number | null)[][];
} {
  if (normalized.length === 0) return { years: [], rows: [] };
  const dates = normalized[0]!.dates;
  const yearSet = new Set<number>();
  for (const d of dates) yearSet.add(Number(d.slice(0, 4)));
  const years = Array.from(yearSet).sort((a, b) => a - b);

  const rows = normalized.map((s) => {
    return years.map((year) => {
      let firstIdx = -1;
      let lastIdx = -1;
      for (let i = 0; i < dates.length; i++) {
        if (Number(dates[i]!.slice(0, 4)) !== year) continue;
        if (firstIdx === -1) firstIdx = i;
        lastIdx = i;
      }
      if (firstIdx === -1 || lastIdx === firstIdx) return null;
      // Use previous year's close as base when available so the return covers
      // the full year rather than intra-year only.
      const baseIdx = firstIdx > 0 ? firstIdx - 1 : firstIdx;
      const base = s.navs[baseIdx]!;
      const end = s.navs[lastIdx]!;
      if (base <= 0) return null;
      return end / base - 1;
    });
  });
  return { years, rows };
}

/** Pearson correlation of daily returns between two aligned NAV series. */
export function returnCorrelation(a: number[], b: number[]): number | null {
  const n = Math.min(a.length, b.length);
  if (n < 3) return null;
  const ra: number[] = [];
  const rb: number[] = [];
  for (let i = 1; i < n; i++) {
    if (a[i - 1]! <= 0 || b[i - 1]! <= 0) continue;
    ra.push(a[i]! / a[i - 1]! - 1);
    rb.push(b[i]! / b[i - 1]! - 1);
  }
  if (ra.length < 2) return null;
  const meanA = ra.reduce((s, v) => s + v, 0) / ra.length;
  const meanB = rb.reduce((s, v) => s + v, 0) / rb.length;
  let cov = 0;
  let varA = 0;
  let varB = 0;
  for (let i = 0; i < ra.length; i++) {
    const da = ra[i]! - meanA;
    const db = rb[i]! - meanB;
    cov += da * db;
    varA += da * da;
    varB += db * db;
  }
  if (varA <= 0 || varB <= 0) return null;
  return cov / Math.sqrt(varA * varB);
}

/** Full pairwise correlation matrix (diagonal = 1). */
export function correlationMatrix(normalized: NormalizedSeries[]): (number | null)[][] {
  const n = normalized.length;
  const matrix: (number | null)[][] = [];
  for (let i = 0; i < n; i++) {
    const row: (number | null)[] = [];
    for (let j = 0; j < n; j++) {
      if (i === j) {
        row.push(1);
      } else if (j < i && matrix[j]) {
        row.push(matrix[j]![i]!);
      } else {
        row.push(returnCorrelation(normalized[i]!.navs, normalized[j]!.navs));
      }
    }
    matrix.push(row);
  }
  return matrix;
}

/** Mean of off-diagonal pairwise correlations; null when none computable. */
export function averageCorrelation(matrix: (number | null)[][]): number | null {
  let sum = 0;
  let count = 0;
  for (let i = 0; i < matrix.length; i++) {
    for (let j = i + 1; j < matrix.length; j++) {
      const v = matrix[i]?.[j];
      if (v != null && Number.isFinite(v)) {
        sum += v;
        count++;
      }
    }
  }
  return count > 0 ? sum / count : null;
}

/** Currency counts of the candidate pool, e.g. { CNY: 3, USD: 1 }. */
export function currencyDistribution(candidates: { currency: string }[]): Record<string, number> {
  const dist: Record<string, number> = {};
  for (const c of candidates) {
    const cur = c.currency || "?";
    dist[cur] = (dist[cur] ?? 0) + 1;
  }
  return dist;
}

/**
 * Estimated common window from screener metadata (metrics start/end dates)
 * without loading full point series. Cash assets (no history) are skipped.
 */
export function estimateCommonWindow(
  candidates: { is_cash: boolean; metrics?: { start_date: string; end_date: string } | null }[],
): { start: string; end: string } | null {
  let start = "";
  let end = "\uffff";
  let counted = 0;
  for (const c of candidates) {
    if (c.is_cash) continue;
    if (!c.metrics?.start_date || !c.metrics?.end_date) return null;
    counted++;
    if (c.metrics.start_date > start) start = c.metrics.start_date;
    if (c.metrics.end_date < end) end = c.metrics.end_date;
  }
  if (counted === 0 || !start || end === "\uffff" || end < start) return null;
  return { start, end };
}
