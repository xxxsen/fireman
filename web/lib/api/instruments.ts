import type { Instrument, InstrumentImportRequest } from "@/types/api";
import { apiDelete, apiGet, apiPost } from "./client";

export function listInstruments(): Promise<{ instruments: Instrument[] }> {
  return apiGet("/api/v1/instruments");
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

export function refreshInstrument(id: string): Promise<Instrument> {
  return apiPost(`/api/v1/instruments/${id}/refresh`);
}

export function deleteInstrument(id: string): Promise<{ deleted: boolean }> {
  return apiDelete(`/api/v1/instruments/${id}`);
}

export function getAnnualReturns(id: string) {
  return apiGet<{ annual_returns: unknown[] }>(`/api/v1/instruments/${id}/annual-returns`);
}
