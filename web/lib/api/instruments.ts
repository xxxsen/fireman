import type { Instrument } from "@/types/api";
import { apiDelete, apiGet, apiPatch, apiPost } from "./client";

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

/** Imports a user instrument from a market asset directory entry (td/078). */
export function importInstrument(body: {
  asset_key: string;
  asset_class: string;
  region: string;
}): Promise<Instrument> {
  return apiPost("/api/v1/instruments/import", body);
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
