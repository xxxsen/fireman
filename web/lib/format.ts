import { formatPercent } from "./percent";

const CURRENCY_SYMBOL: Record<string, string> = {
  CNY: "¥",
  USD: "$",
  HKD: "HK$",
};

export function formatMoney(minor: number, currency = "CNY"): string {
  const major = minor / 100;
  const symbol = CURRENCY_SYMBOL[currency] ?? currency + " ";
  return `${symbol}${major.toLocaleString("zh-CN", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
}

/**
 * Money format dedicated to FIRE simulation path pages: always `¥xx.xxw` in 万元
 * with two decimals and no thousands separators (e.g. `¥578.73w`). Negative keeps
 * its sign before the symbol (`-¥12.30w`); empty values render as `—`.
 * Only used on FIRE path-related views; other pages keep their existing formats.
 */
export function formatMoneyWan(
  minor: number | null | undefined,
  currency = "CNY",
): string {
  if (minor == null || Number.isNaN(minor)) return "—";
  const symbol = CURRENCY_SYMBOL[currency] ?? currency + " ";
  const wan = minor / 100 / 10000;
  const sign = wan < 0 ? "-" : "";
  return `${sign}${symbol}${Math.abs(wan).toFixed(2)}w`;
}

const REPRESENTATIVE_PERCENTILE_ORDER = ["p00", "p25", "p50", "p75", "p95"];

/** Business rank for representative path percentiles; unknown/empty sort last. */
export function representativePercentileRank(
  label: string | null | undefined,
): number {
  if (!label) return REPRESENTATIVE_PERCENTILE_ORDER.length;
  const idx = REPRESENTATIVE_PERCENTILE_ORDER.indexOf(label.toLowerCase());
  return idx === -1 ? REPRESENTATIVE_PERCENTILE_ORDER.length : idx;
}

/** Stable ascending sort of representative paths by percentile, then path_no. */
export function sortRepresentativePaths<
  T extends { representative_percentile?: string; path_no: number },
>(paths: T[]): T[] {
  return [...paths].sort((a, b) => {
    const ra = representativePercentileRank(a.representative_percentile);
    const rb = representativePercentileRank(b.representative_percentile);
    if (ra !== rb) return ra - rb;
    return a.path_no - b.path_no;
  });
}

export function parseMoneyInput(input: string): number | null {
  const cleaned = input.replace(/,/g, "").trim();
  if (cleaned === "") return null;
  const n = Number(cleaned);
  if (!Number.isFinite(n)) return null;
  return Math.round(n * 100);
}

export function formatMoneyInput(minor: number): string {
  return (minor / 100).toLocaleString("zh-CN", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

/** Plain numeric string for money inputs (no grouping). */
export function formatMoneyPlain(minor: number): string {
  if (minor === 0) return "";
  return String(minor / 100);
}

/**
 * Format a minor-unit amount with an automatic magnitude unit (元 / 万元 / 亿元)
 * so large balances stay readable, e.g. `¥1,234.56 万元`.
 */
export function formatMoneyScaled(minor: number, currency = "CNY"): string {
  const symbol = CURRENCY_SYMBOL[currency] ?? currency + " ";
  const major = minor / 100;
  const abs = Math.abs(major);
  let value = major;
  let unit = "元";
  if (abs >= 100_000_000) {
    value = major / 100_000_000;
    unit = "亿元";
  } else if (abs >= 10_000) {
    value = major / 10_000;
    unit = "万元";
  }
  const formatted = value.toLocaleString("zh-CN", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
  return `${symbol}${formatted} ${unit}`;
}

/**
 * Auxiliary magnitude hint for plain numeric money input (major units, yuan).
 * Only 万 / 亿 magnitudes are hinted — that is where users misread zeros;
 * smaller amounts return null so inputs stay uncluttered.
 */
export function formatMoneyUnitHint(major: number): string | null {
  if (!Number.isFinite(major) || major === 0) return null;
  const abs = Math.abs(major);
  if (abs >= 100_000_000) return `约 ${(major / 100_000_000).toFixed(2)} 亿`;
  if (abs >= 10_000) return `约 ${(major / 10_000).toFixed(2)} 万`;
  return null;
}

/**
 * Format a millisecond epoch timestamp (matching backend `time.Now().UnixMilli()`)
 * as a localized date. Returns "—" for empty/invalid values.
 */
export function formatDateFromMs(ts?: number | null): string {
  if (!ts) return "—";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("zh-CN");
}

/**
 * Format a millisecond epoch timestamp as a localized date+time, for task and
 * sync freshness displays. Returns "—" for empty/invalid values.
 */
export function formatDateTimeFromMs(ts?: number | null): string {
  return formatDateTimeFromMsInTimeZone(ts);
}

export function formatDateTimeFromMsInTimeZone(
  ts?: number | null,
  timeZone?: string,
): string {
  if (!ts) return "—";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    timeZone,
  });
}

export { formatPercent };

export function instrumentTypeLabel(t: string | undefined | null): string {
  if (!t) return "—";
  const map: Record<string, string> = {
    cn_exchange_fund: "场内 ETF / LOF",
    cn_exchange_stock: "A 股",
    cn_mutual_fund: "公募基金",
    hk_etf: "香港 ETF",
    hk_stock: "港股",
    us_etf: "美国 ETF",
    us_stock: "美国股票",
    cash: "现金",
  };
  return map[t] ?? t;
}

export function assetClassLabel(ac: string): string {
  const map: Record<string, string> = {
    equity: "权益",
    bond: "债券",
    cash: "现金/其他",
  };
  return map[ac] ?? ac;
}

export function regionLabel(region: string): string {
  const map: Record<string, string> = {
    domestic: "国内",
    foreign: "国外",
  };
  return map[region] ?? region;
}

export function rebalanceActionLabel(action: string): string {
  const map: Record<string, string> = {
    increase: "增配",
    decrease: "减配",
    hold: "不动",
    disabled: "未启用",
  };
  return map[action] ?? action;
}

export function historyDepthLabel(depth: string | undefined): string {
  const map: Record<string, string> = {
    insufficient: "历史不足",
    one_year: "历史样本有限",
    two_to_four_years: "历史样本较短",
    five_plus_years: "历史样本充足",
    system: "系统固定参数",
  };
  return depth ? (map[depth] ?? depth) : "—";
}

export function formatNullablePercent(
  value: number | null | undefined,
): string {
  if (value == null || Number.isNaN(value)) return "—";
  return formatPercent(value);
}

const DATA_SOURCE_MAP: Record<string, string> = {
  "tickflow.klines:1d": "TickFlow · 日K",
  "ak.fund_etf_hist_em": "东方财富 · ETF 日线",
  "ak.stock_zh_a_hist_tx": "腾讯财经 · 日线",
  "ak.fund_etf_hist_sina": "新浪财经 · ETF",
  "ak.stock_zh_a_hist": "东方财富 · A 股日线",
  "ak.stock_zh_a_daily": "新浪财经 · A 股",
  "ak.fund_lof_hist_em": "东方财富 · LOF",
  "ak.fund_etf_fund_info_em": "东方财富 · ETF 净值",
  "ak.fund_open_fund_info_em": "东方财富 · 公募基金",
  "em.fund_open_history": "东方财富 · 公募基金净值",
  "ak.fund_money_fund_info_em": "东方财富 · 货币基金",
  "ak.stock_us_daily": "美股 · 日线",
  "ak.currency_boc_sina": "新浪 · 外汇",
  "em.hk_equity_list": "东方财富 · 港股列表",
  "em.hk_fund_list": "东方财富 · 港股基金列表",
  "em.us_equity_list": "东方财富 · 美股列表",
  "em.us_etf_list": "东方财富 · 美股 ETF 列表",
  test_fixture: "测试数据",
};

/**
 * Human-readable label for AKShare adapter source ids. The backend may append a
 * data-type suffix after a colon (e.g. `ak.fund_open_fund_info_em:累计净值走势`);
 * we map the id segment and keep the readable suffix. Unknown ids are never shown
 * raw to the user — they collapse to a generic "行情数据" label.
 */
export function dataSourceLabel(sourceName: string | undefined | null): string {
  if (!sourceName) return "—";
  const exact = DATA_SOURCE_MAP[sourceName.trim()];
  if (exact) return exact;
  const colon = sourceName.indexOf(":");
  const id = colon >= 0 ? sourceName.slice(0, colon).trim() : sourceName.trim();
  const suffix = colon >= 0 ? sourceName.slice(colon + 1).trim() : "";
  const base = DATA_SOURCE_MAP[id];
  if (base) {
    return suffix ? `${base} · ${suffix}` : base;
  }
  return suffix ? `行情数据 · ${suffix}` : "行情数据";
}

export function pointTypeLabel(pointType: string | undefined | null): string {
  if (!pointType) return "—";
  const map: Record<string, string> = {
    close: "未复权收盘价",
    adjusted_close: "复权收盘价",
    nav: "单位净值",
    total_return_index: "累计净值",
    fx_rate: "汇率",
  };
  return map[pointType] ?? pointType;
}

export function adjustPolicyLabel(
  adjustPolicy: string | undefined | null,
): string {
  if (!adjustPolicy) return "—";
  const map: Record<string, string> = {
    none: "未复权",
    hfq: "后复权",
  };
  return map[adjustPolicy] ?? adjustPolicy;
}

/** Label for annual return row completeness (distinct from calendar-year UI). */
export function annualCompletenessLabel(row: {
  year: number;
  is_partial: boolean;
  end_date?: string;
}): string {
  if (row.is_partial) return "不完整";
  const currentYear = new Date().getFullYear();
  if (row.year >= currentYear) return "年内统计";
  if (row.end_date) {
    const endMonth = Number.parseInt(row.end_date.slice(5, 7), 10);
    if (Number.isFinite(endMonth) && endMonth < 11) return "年内统计";
  }
  return "完整";
}

/**
 * Compress a list of years into contiguous ranges, e.g.
 * `[2006..2025]` -> "2006-2025", `[2006..2012, 2014..2025]` -> "2006-2012、2014-2025".
 * Returns "—" for an empty list.
 */
export function compressYears(years: number[]): string {
  const sorted = Array.from(
    new Set(years.filter((y) => Number.isFinite(y))),
  ).sort((a, b) => a - b);
  if (sorted.length === 0) return "—";
  const ranges: string[] = [];
  let start = sorted[0]!;
  let prev = sorted[0]!;
  for (let i = 1; i < sorted.length; i++) {
    const y = sorted[i]!;
    if (y === prev + 1) {
      prev = y;
      continue;
    }
    ranges.push(start === prev ? `${start}` : `${start}-${prev}`);
    start = y;
    prev = y;
  }
  ranges.push(start === prev ? `${start}` : `${start}-${prev}`);
  return ranges.join("、");
}

export function formatAnnualPeriod(start?: string, end?: string): string {
  if (!start || !end) return "—";
  return `${start} ~ ${end}`;
}

export function failureStatusLabel(reason: string): string {
  const map: Record<string, string> = {
    insufficient_funds: "当月资金不足",
    wealth_depleted: "资产已耗尽",
    terminal_floor_not_met: "期末资产未达到最低目标",
    other: "其他失败状态",
  };
  return map[reason] ?? reason;
}
