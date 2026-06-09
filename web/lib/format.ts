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
  };
  return map[status] ?? status;
}
