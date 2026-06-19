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

const MONEY_UNIT_HINTS: { threshold: number; unit: string; divisor: number }[] = [
  { threshold: 1_000_000_000_000, unit: "十亿", divisor: 1_000_000_000_000 },
  { threshold: 100_000_000, unit: "亿", divisor: 100_000_000 },
  { threshold: 10_000_000, unit: "千万", divisor: 10_000_000 },
  { threshold: 1_000_000, unit: "百万", divisor: 1_000_000 },
  { threshold: 100_000, unit: "十万", divisor: 100_000 },
  { threshold: 10_000, unit: "万", divisor: 10_000 },
  { threshold: 1_000, unit: "千", divisor: 1_000 },
  { threshold: 100, unit: "百", divisor: 100 },
];

/** Inline unit label for plain money input: `CNY(万)` — no value conversion. */
export function formatMoneyInlineUnit(currency: string, rawValue: string): string {
  const cleaned = rawValue.replace(/,/g, "").trim();
  if (cleaned === "" || cleaned === "0") return currency;
  const n = Number(cleaned);
  if (!Number.isFinite(n) || n === 0) return currency;
  const abs = Math.abs(n);
  for (const item of MONEY_UNIT_HINTS) {
    if (abs >= item.threshold) {
      return `${currency}(${item.unit})`;
    }
  }
  return `${currency}(元)`;
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

/** Auxiliary unit hint for plain numeric money input (major units, yuan). */
export function formatMoneyUnitHint(major: number): string | null {
  if (!Number.isFinite(major) || major === 0) return null;
  const abs = Math.abs(major);

  if (abs >= 10_000) {
    const wan = major / 10_000;
    if (Math.abs(wan) >= 1 && Math.abs(wan) < 10_000) {
      return `约 ${wan.toFixed(2)} 万`;
    }
  }

  for (const item of [...MONEY_UNIT_HINTS].reverse()) {
    if (item.unit === "万") continue;
    if (abs < item.threshold) continue;
    const scaled = major / item.divisor;
    if (Math.abs(scaled) >= 1 && Math.abs(scaled) < 10_000) {
      return `约 ${scaled.toFixed(2)} ${item.unit}`;
    }
  }

  for (const item of MONEY_UNIT_HINTS) {
    if (abs >= item.threshold) {
      const scaled = major / item.divisor;
      return `约 ${scaled.toFixed(2)} ${item.unit}`;
    }
  }

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

export { formatPercent };

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

export function qualityStatusLabel(status: string): string {
  const map: Record<string, string> = {
    available: "可用",
    insufficient_history: "历史不足",
    provider_data_anomaly: "数据异常",
    pending_sync: "待同步",
    classification_failed: "分类失败",
    data_anomaly: "数据异常",
    pending_fetch: "抓取中",
    fetch_failed: "抓取失败",
    active: "正常",
  };
  return map[status] ?? status;
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

export function metricStatusLabel(status: string | undefined | null): string {
  if (!status) return "—";
  const map: Record<string, string> = {
    available: "可用",
    insufficient_complete_years: "完整年度不足",
    insufficient_monthly_coverage: "月份覆盖不足",
    invalid_metric_value: "指标无效",
    unavailable: "不可用",
  };
  return map[status] ?? status;
}

export function instrumentSimulationStatusLabel(inst: {
  status: string;
  simulation_eligible?: boolean;
  history_depth?: string;
  quality_status?: string;
}): string | null {
  if (inst.status === "pending_fetch" || inst.status === "fetch_failed") {
    return null;
  }
  if (inst.simulation_eligible && inst.history_depth === "one_year") {
    return "可用于模拟·历史样本有限";
  }
  if (inst.simulation_eligible) {
    return "可用于模拟";
  }
  return null;
}

export function excludedYearReasonLabel(reason: string): string {
  const map: Record<string, string> = {
    missing_opening_anchor: "成立首年，缺少年初锚点",
    current_year: "当前年度尚未结束",
    incomplete_year: "不完整年度",
    insufficient_monthly_coverage: "月份覆盖不足",
  };
  return map[reason] ?? reason;
}

export function formatNullablePercent(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return "—";
  return formatPercent(value);
}

export function instrumentStatusLabel(status: string): string {
  const map: Record<string, string> = {
    pending_fetch: "抓取中",
    fetch_failed: "抓取失败",
    active: "正常",
  };
  return map[status] ?? status;
}

const DATA_SOURCE_MAP: Record<string, string> = {
  "ak.fund_etf_hist_em": "东方财富 · ETF 日线",
  "ak.stock_zh_a_hist_tx": "腾讯财经 · 前复权",
  "ak.fund_etf_hist_sina": "新浪财经 · ETF",
  "ak.stock_zh_a_hist": "东方财富 · A 股日线",
  "ak.stock_zh_a_daily": "新浪财经 · A 股",
  "ak.fund_lof_hist_em": "东方财富 · LOF",
  "ak.fund_etf_fund_info_em": "东方财富 · ETF 净值",
  "ak.fund_open_fund_info_em": "东方财富 · 公募基金",
  "ak.fund_money_fund_info_em": "东方财富 · 货币基金",
  "ak.stock_us_daily": "美股 · 日线",
  "ak.currency_boc_sina": "新浪 · 外汇",
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
    adjusted_close: "前复权收盘价",
    nav: "单位净值",
    total_return_index: "累计净值",
    fx_rate: "汇率",
  };
  return map[pointType] ?? pointType;
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
  const sorted = Array.from(new Set(years.filter((y) => Number.isFinite(y)))).sort(
    (a, b) => a - b,
  );
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

export function failureReasonLabel(reason: string): string {
  const map: Record<string, string> = {
    early_sequence_risk: "早期序列风险（前期回撤/支出冲击导致资产耗尽）",
    high_inflation: "高通胀（实际购买力不足）",
    spending_shock: "支出冲击（突发大额支出）",
    longevity_risk: "长寿风险（超出预期寿命）",
    other: "其他原因",
  };
  return map[reason] ?? reason;
}
