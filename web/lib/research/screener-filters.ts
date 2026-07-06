import type { ResearchAssetListParams } from "@/lib/api/research";

/** UI state of the screener filter panel; serialized into saved filters. */
export interface ScreenerFilters {
  market: string;
  instrumentTypes: string[];
  q: string;
  currencies: string[];
  includeInactive: boolean;
  historyStatus: "" | "synced" | "missing" | "stale" | "syncing" | "failed";
  dataAsOfMin: string;
  minHistoryYears: string;
  minCagr: string;
  minReturn1y: string;
  minReturn3y: string;
  minReturn5y: string;
  maxVolatility: string;
  minMaxDrawdown: string;
  minSharpe: string;
  minCalmar: string;
  maxDownsideVolatility: string;
  minReturnDrawdown: string;
  backtestReady: boolean;
}

export const EMPTY_FILTERS: ScreenerFilters = {
  market: "",
  instrumentTypes: [],
  q: "",
  currencies: [],
  includeInactive: false,
  historyStatus: "",
  dataAsOfMin: "",
  minHistoryYears: "",
  minCagr: "",
  minReturn1y: "",
  minReturn3y: "",
  minReturn5y: "",
  maxVolatility: "",
  minMaxDrawdown: "",
  minSharpe: "",
  minCalmar: "",
  maxDownsideVolatility: "",
  minReturnDrawdown: "",
  backtestReady: false,
};

/** Percent-style fields entered as "8" meaning 8%; converted to 0.08. */
const PERCENT_FIELDS = new Set([
  "minCagr",
  "minReturn1y",
  "minReturn3y",
  "minReturn5y",
  "maxVolatility",
  "minMaxDrawdown",
  "maxDownsideVolatility",
]);

function parseNumeric(field: keyof ScreenerFilters, raw: string): number | undefined {
  const trimmed = raw.trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  if (!Number.isFinite(n)) return undefined;
  return PERCENT_FIELDS.has(field) ? n / 100 : n;
}

/** Convert filter UI state into API list params (percent inputs → ratios). */
export function filtersToParams(filters: ScreenerFilters): ResearchAssetListParams {
  const params: ResearchAssetListParams = {};
  if (filters.market) params.market = filters.market;
  if (filters.instrumentTypes.length) params.instrumentTypes = filters.instrumentTypes;
  if (filters.q.trim()) params.q = filters.q.trim();
  if (filters.currencies.length) params.currencies = filters.currencies;
  if (filters.includeInactive) params.includeInactive = true;
  if (filters.historyStatus) params.historyStatus = filters.historyStatus;
  if (filters.dataAsOfMin) params.dataAsOfMin = filters.dataAsOfMin;
  const minHistoryYears = parseNumeric("minHistoryYears", filters.minHistoryYears);
  if (minHistoryYears !== undefined) params.minHistoryYears = minHistoryYears;
  const minCagr = parseNumeric("minCagr", filters.minCagr);
  if (minCagr !== undefined) params.minCagr = minCagr;
  const minReturn1y = parseNumeric("minReturn1y", filters.minReturn1y);
  if (minReturn1y !== undefined) params.minReturn1y = minReturn1y;
  const minReturn3y = parseNumeric("minReturn3y", filters.minReturn3y);
  if (minReturn3y !== undefined) params.minReturn3y = minReturn3y;
  const minReturn5y = parseNumeric("minReturn5y", filters.minReturn5y);
  if (minReturn5y !== undefined) params.minReturn5y = minReturn5y;
  const maxVolatility = parseNumeric("maxVolatility", filters.maxVolatility);
  if (maxVolatility !== undefined) params.maxVolatility = maxVolatility;
  const minMaxDrawdown = parseNumeric("minMaxDrawdown", filters.minMaxDrawdown);
  if (minMaxDrawdown !== undefined) params.minMaxDrawdown = minMaxDrawdown;
  const minSharpe = parseNumeric("minSharpe", filters.minSharpe);
  if (minSharpe !== undefined) params.minSharpe = minSharpe;
  const minCalmar = parseNumeric("minCalmar", filters.minCalmar);
  if (minCalmar !== undefined) params.minCalmar = minCalmar;
  const maxDownside = parseNumeric("maxDownsideVolatility", filters.maxDownsideVolatility);
  if (maxDownside !== undefined) params.maxDownsideVolatility = maxDownside;
  const minReturnDrawdown = parseNumeric("minReturnDrawdown", filters.minReturnDrawdown);
  if (minReturnDrawdown !== undefined) params.minReturnDrawdown = minReturnDrawdown;
  if (filters.backtestReady) params.backtestReady = true;
  return params;
}

/** Serialize for saved-filter storage. */
export function filtersToJSON(filters: ScreenerFilters): Record<string, unknown> {
  return { ...filters };
}

/** Restore from saved-filter JSON, tolerating missing/unknown fields. */
export function filtersFromJSON(raw: unknown): ScreenerFilters {
  const out: ScreenerFilters = { ...EMPTY_FILTERS, instrumentTypes: [], currencies: [] };
  if (typeof raw !== "object" || raw === null) return out;
  const obj = raw as Record<string, unknown>;
  for (const key of Object.keys(EMPTY_FILTERS) as (keyof ScreenerFilters)[]) {
    const v = obj[key];
    if (v === undefined) continue;
    if (key === "instrumentTypes" || key === "currencies") {
      if (Array.isArray(v)) {
        out[key] = v.filter((x): x is string => typeof x === "string");
      }
    } else if (key === "includeInactive" || key === "backtestReady") {
      if (typeof v === "boolean") out[key] = v;
    } else if (typeof v === "string") {
      (out as unknown as Record<string, unknown>)[key] = v;
    }
  }
  return out;
}

/** Count of active (non-default) filter conditions for the UI badge. */
export function activeFilterCount(filters: ScreenerFilters): number {
  let count = 0;
  for (const key of Object.keys(EMPTY_FILTERS) as (keyof ScreenerFilters)[]) {
    const v = filters[key];
    const def = EMPTY_FILTERS[key];
    if (Array.isArray(v)) {
      if (v.length > 0) count++;
    } else if (v !== def) {
      count++;
    }
  }
  return count;
}
