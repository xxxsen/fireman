import type { Instrument, InstrumentImportRequest } from "@/types/api";
import {
  apiDelete,
  apiGet,
  apiPatch,
  apiPost,
  INSTRUMENT_REFRESH_TIMEOUT_MS,
  MARKET_OPERATION_TIMEOUT_MS,
} from "./client";

export interface ResolveCandidate {
  code: string;
  provider_symbol: string;
  name: string;
  exchange: string;
  instrument_kind: string;
  candidate_id?: string;
  is_importable?: boolean;
  ticket_id?: string;
}

/** Stable unique key for a resolve candidate (list keys, selection, test ids). */
export function candidateIdentity(candidate: ResolveCandidate): string {
  if (candidate.candidate_id) {
    return candidate.candidate_id;
  }
  if (candidate.ticket_id) {
    return candidate.ticket_id;
  }
  return `${candidate.code}|${candidate.provider_symbol}|${candidate.instrument_kind}|${candidate.exchange}`;
}

export function isSameCandidate(
  a: ResolveCandidate | null | undefined,
  b: ResolveCandidate | null | undefined,
): boolean {
  if (!a || !b) {
    return false;
  }
  return candidateIdentity(a) === candidateIdentity(b);
}

export interface ResolveResult {
  ambiguous: boolean;
  resolved?: ResolveCandidate;
  candidates?: ResolveCandidate[];
}

export interface ImportAsyncResult {
  instrument_id: string;
  job_id: string;
  status: string;
}

export interface FetchStatusResult {
  instrument_id: string;
  instrument_status: string;
  job_id?: string;
  job_status?: string;
  phase?: string;
  progress_current: number;
  progress_total: number;
  error_code?: string;
  error_message?: string;
}

export function listInstruments(options?: { valuationDate?: string }): Promise<{ instruments: Instrument[] }> {
  const query = options?.valuationDate
    ? `?valuation_date=${encodeURIComponent(options.valuationDate)}`
    : "";
  return apiGet(`/api/v1/instruments${query}`);
}

export interface InstrumentSearchParams {
  q?: string;
  assetClass?: string;
  region?: string;
  status?: string;
  excludeIds?: string[];
  limit?: number;
  cursor?: number;
}

export interface InstrumentSearchResult {
  instruments: Instrument[];
  next_cursor: number | null;
  total: number;
}

export function searchInstruments(params: InstrumentSearchParams): Promise<InstrumentSearchResult> {
  const qs = new URLSearchParams();
  qs.set("limit", String(params.limit ?? 10));
  qs.set("cursor", String(params.cursor ?? 0));
  if (params.q) qs.set("q", params.q);
  if (params.assetClass) qs.set("asset_class", params.assetClass);
  if (params.region) qs.set("region", params.region);
  if (params.status) qs.set("status", params.status);
  if (params.excludeIds && params.excludeIds.length > 0) {
    qs.set("exclude_ids", params.excludeIds.join(","));
  }
  return apiGet(`/api/v1/instruments?${qs.toString()}`);
}

export function getInstrumentDetail(id: string) {
  return apiGet<{
    instrument: Instrument;
    annual_returns: {
      year: number;
      annual_return: number;
      is_partial: boolean;
      in_simulation?: boolean;
      start_date?: string;
      end_date?: string;
    }[];
    simulation_window: {
      inclusion_date: string;
      selected_years: number[];
      excluded_years: Array<{ year: number; reason: string }>;
      complete_year_count: number;
      daily_observation_count: number;
      monthly_return_count: number;
      historical_cagr: number | null;
      annual_volatility: number | null;
      max_drawdown: number | null;
      cagr_status?: string;
      volatility_status?: string;
      drawdown_status?: string;
      quality_status: string;
      simulation_eligible?: boolean;
      history_depth?: string;
      volatility_method?: string;
      metrics_version?: string;
      warnings?: string[];
      fee_treatment: string;
      expense_ratio_status: string;
    };
    trailing_returns?: {
      as_of_date: string;
      point_type: string;
      source_name: string;
      periods: Record<
        string,
        {
          status: string;
          target_start_date: string;
          start_date: string | null;
          end_date: string;
          actual_days: number | null;
          cumulative_return: number | null;
          annualized_return: number | null;
        }
      >;
    };
    historical_snapshots: {
      id: string;
      plan_id?: string;
      inclusion_date: string;
      complete_year_count: number;
      monthly_return_count: number;
      history_depth: string;
      metrics_version: string;
      warnings: string[];
      created_at: number;
    }[];
    referencing_plans: {
      plan_id: string;
      plan_name: string;
      snapshot_inclusion_date: string;
    }[];
  }>(`/api/v1/instruments/${id}/detail`);
}

export function getInstrument(id: string): Promise<Instrument> {
  return apiGet(`/api/v1/instruments/${id}`);
}

/** @deprecated Use resolveImport + importAsync instead. */
export function previewImport(body: InstrumentImportRequest): Promise<Record<string, unknown>> {
  return apiPost("/api/v1/instruments/import/preview", body);
}

/** @deprecated Use importAsync instead. */
export function importInstrument(body: InstrumentImportRequest): Promise<Instrument> {
  return apiPost("/api/v1/instruments/import", body);
}

export function resolveImport(body: InstrumentImportRequest): Promise<ResolveResult> {
  return apiPost("/api/v1/instruments/resolve", body, undefined, {
    timeoutMs: MARKET_OPERATION_TIMEOUT_MS,
  });
}

export function importAsync(body: {
  ticket_id: string;
  asset_class: string;
  region: string;
}): Promise<ImportAsyncResult> {
  return apiPost("/api/v1/instruments/import-async", body, undefined, {
    timeoutMs: MARKET_OPERATION_TIMEOUT_MS,
  });
}

export function getFetchStatus(id: string): Promise<FetchStatusResult> {
  return apiGet(`/api/v1/instruments/${id}/fetch-status`);
}

export function retryFetch(id: string): Promise<ImportAsyncResult> {
  return apiPost(`/api/v1/instruments/${id}/retry-fetch`, {});
}

export function refreshInstrument(id: string): Promise<Instrument> {
  // Manual library refresh is always an immediate full refresh. The
  // legacy `force` field is still sent for backend compatibility.
  return apiPost(
    `/api/v1/instruments/${id}/refresh`,
    { force: true },
    undefined,
    { timeoutMs: INSTRUMENT_REFRESH_TIMEOUT_MS },
  );
}

export interface ClassificationUpdateResult {
  instrument: Instrument;
  referencing_plan_count: number;
  classification_sync_scope: string;
}

export function updateInstrumentClassification(
  id: string,
  body: { asset_class: string; region: string; expected_updated_at: number },
): Promise<ClassificationUpdateResult> {
  return apiPatch(`/api/v1/instruments/${id}/classification`, body);
}

export function deleteInstrument(id: string): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/instruments/${id}`);
}

export function getAnnualReturns(id: string) {
  return apiGet<{ annual_returns: unknown[] }>(`/api/v1/instruments/${id}/annual-returns`);
}

export type ReturnSeriesRange =
  | "3d"
  | "1w"
  | "1m"
  | "3m"
  | "6m"
  | "1y"
  | "3y"
  | "5y"
  | "all";

export interface ReturnSeriesPoint {
  date: string;
  value: number;
  cumulative_return: number;
}

export interface ReturnSeries {
  as_of_date: string;
  range: ReturnSeriesRange;
  point_type: string;
  source_name: string;
  status: string;
  points: ReturnSeriesPoint[];
}

export function getReturnSeries(id: string, range: ReturnSeriesRange): Promise<ReturnSeries> {
  return apiGet(`/api/v1/instruments/${id}/return-series?range=${encodeURIComponent(range)}`);
}
