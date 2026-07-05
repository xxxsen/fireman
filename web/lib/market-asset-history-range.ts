import type { MarketAssetPoint } from "@/lib/api/market-assets";

/**
 * History range shortcuts for the asset detail chart (td/093). Filtering is
 * frontend-only over the already-loaded points; the range end is the last
 * history point's date (not today) so a stale series still shows a full
 * window.
 */
export type HistoryRangeKey = "7d" | "1m" | "3m" | "6m" | "1y" | "3y" | "5y" | "all";

export interface HistoryRangeOption {
  key: HistoryRangeKey;
  label: string;
  unit?: "day" | "month" | "year";
  amount?: number;
}

export const HISTORY_RANGE_OPTIONS: readonly HistoryRangeOption[] = [
  { key: "7d", label: "近 7 天", unit: "day", amount: 7 },
  { key: "1m", label: "近 1 月", unit: "month", amount: 1 },
  { key: "3m", label: "近 3 月", unit: "month", amount: 3 },
  { key: "6m", label: "近 6 月", unit: "month", amount: 6 },
  { key: "1y", label: "近 1 年", unit: "year", amount: 1 },
  { key: "3y", label: "近 3 年", unit: "year", amount: 3 },
  { key: "5y", label: "近 5 年", unit: "year", amount: 5 },
  { key: "all", label: "全部" },
];

export function historyRangeLabel(key: HistoryRangeKey): string {
  return HISTORY_RANGE_OPTIONS.find((o) => o.key === key)?.label ?? key;
}

/**
 * Parses `YYYY-MM-DD` as a local date. `new Date("YYYY-MM-DD")` parses as
 * UTC midnight and can shift a day in non-UTC timezones.
 */
export function parseDateOnly(date: string): Date {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1);
}

/**
 * Start date of a range ending at `end` (inclusive). Month/year shifts rely
 * on Date constructor normalization, matching Go's AddDate semantics
 * (e.g. Mar 31 minus 1 month normalizes to Mar 3).
 */
function historyRangeStart(end: Date, option: HistoryRangeOption): Date {
  const amount = option.amount ?? 0;
  switch (option.unit) {
    case "day":
      return new Date(end.getFullYear(), end.getMonth(), end.getDate() - amount);
    case "month":
      return new Date(end.getFullYear(), end.getMonth() - amount, end.getDate());
    case "year":
      return new Date(end.getFullYear() - amount, end.getMonth(), end.getDate());
    default:
      return new Date(0);
  }
}

/**
 * Keeps the points inside the range ending at the last point's date.
 * `all` (and an empty series) returns the input untouched.
 */
export function filterHistoryPoints(
  points: MarketAssetPoint[],
  rangeKey: HistoryRangeKey,
): MarketAssetPoint[] {
  if (rangeKey === "all" || points.length === 0) return points;
  const option = HISTORY_RANGE_OPTIONS.find((o) => o.key === rangeKey);
  if (!option || option.key === "all") return points;
  const end = parseDateOnly(points[points.length - 1]!.date);
  const start = historyRangeStart(end, option).getTime();
  return points.filter((p) => parseDateOnly(p.date).getTime() >= start);
}

/**
 * A range is usable when it leaves at least 2 points to draw a line;
 * `all` is always offered.
 */
export function isHistoryRangeAvailable(
  points: MarketAssetPoint[],
  rangeKey: HistoryRangeKey,
): boolean {
  if (rangeKey === "all") return true;
  return filterHistoryPoints(points, rangeKey).length >= 2;
}

/**
 * Default selection follows the coverage of the loaded series: more than a
 * year of data starts at `1y`, more than 3 months at `3m`, otherwise `all`.
 * Falls back to `all` when the candidate range has too few points.
 */
export function defaultHistoryRange(points: MarketAssetPoint[]): HistoryRangeKey {
  if (points.length < 2) return "all";
  const first = parseDateOnly(points[0]!.date).getTime();
  const end = parseDateOnly(points[points.length - 1]!.date);
  const candidates: HistoryRangeKey[] = [];
  const oneYear = HISTORY_RANGE_OPTIONS.find((o) => o.key === "1y")!;
  const threeMonths = HISTORY_RANGE_OPTIONS.find((o) => o.key === "3m")!;
  if (first < historyRangeStart(end, oneYear).getTime()) {
    candidates.push("1y");
  } else if (first < historyRangeStart(end, threeMonths).getTime()) {
    candidates.push("3m");
  }
  for (const key of candidates) {
    if (isHistoryRangeAvailable(points, key)) return key;
  }
  return "all";
}

/**
 * Chart points with cumulative return re-zeroed on the first visible point,
 * so a filtered range always starts at 0%.
 */
export function toChartPoints(points: MarketAssetPoint[]) {
  if (!points.length) return [];
  const base = points[0]!.value;
  return points.map((p) => ({
    date: p.date,
    value: p.value,
    cumulative_return: base > 0 ? p.value / base - 1 : 0,
  }));
}
