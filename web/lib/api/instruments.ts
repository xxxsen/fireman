import type { Instrument, InstrumentImportRequest } from "@/types/api";
import { apiDelete, apiGet, apiPost } from "./client";

export function listInstruments(): Promise<{ instruments: Instrument[] }> {
  return apiGet("/api/v1/instruments");
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

export function previewImport(body: InstrumentImportRequest): Promise<Record<string, unknown>> {
  return apiPost("/api/v1/instruments/import/preview", body);
}

export function importInstrument(body: InstrumentImportRequest): Promise<Instrument> {
  return apiPost("/api/v1/instruments/import", body);
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
