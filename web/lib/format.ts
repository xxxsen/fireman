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
    pending_sync: "待同步",
    classification_failed: "分类失败",
    data_anomaly: "数据异常",
    pending_fetch: "抓取中",
    fetch_failed: "抓取失败",
    active: "正常",
  };
  return map[status] ?? status;
}

export function instrumentStatusLabel(status: string): string {
  const map: Record<string, string> = {
    pending_fetch: "抓取中",
    fetch_failed: "抓取失败",
    active: "正常",
  };
  return map[status] ?? status;
}

/** Human-readable label for AKShare adapter source ids. */
export function dataSourceLabel(sourceName: string | undefined | null): string {
  if (!sourceName) return "—";
  const map: Record<string, string> = {
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
  return map[sourceName] ?? sourceName;
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
