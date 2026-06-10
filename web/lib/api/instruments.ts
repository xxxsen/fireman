import type { Instrument, InstrumentImportRequest } from "@/types/api";
import { apiDelete, apiGet, apiPost } from "./client";

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
      excluded_years: number[];
      complete_year_count: number;
      historical_cagr: number;
      annual_volatility: number;
      max_drawdown: number;
      observation_count: number;
      fee_treatment: string;
      expense_ratio_status: string;
      quality_status: string;
    };
    historical_snapshots: {
      id: string;
      plan_id?: string;
      inclusion_date: string;
      complete_year_count: number;
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
  return apiPost("/api/v1/instruments/resolve", body);
}

export function importAsync(body: { ticket_id: string; asset_class: string }): Promise<ImportAsyncResult> {
  return apiPost("/api/v1/instruments/import-async", body);
}

export function getFetchStatus(id: string): Promise<FetchStatusResult> {
  return apiGet(`/api/v1/instruments/${id}/fetch-status`);
}

export function retryFetch(id: string): Promise<ImportAsyncResult> {
  return apiPost(`/api/v1/instruments/${id}/retry-fetch`, {});
}

export function refreshInstrument(id: string, options?: { force?: boolean }): Promise<Instrument> {
  return apiPost(`/api/v1/instruments/${id}/refresh`, {
    force: options?.force === true,
  });
}

export function deleteInstrument(id: string): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/instruments/${id}`);
}

export function getAnnualReturns(id: string) {
  return apiGet<{ annual_returns: unknown[] }>(`/api/v1/instruments/${id}/annual-returns`);
}
