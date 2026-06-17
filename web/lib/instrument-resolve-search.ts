import { ApiError } from "@/lib/api/client";
import {
  getFetchStatus,
  getInstrument,
  importAsync,
  resolveImport,
  type ResolveCandidate,
  type ResolveResult,
} from "@/lib/api/instruments";

export function looksLikeFundCode(query: string): boolean {
  const trimmed = query.trim();
  return trimmed.length >= 4 && /^[a-zA-Z0-9.]+$/.test(trimmed);
}

export async function resolveCNInstrumentCode(code: string): Promise<ResolveResult> {
  try {
    return await resolveImport({ market: "CN", instrument_type: "cn_exchange_fund", code: code.trim() });
  } catch (error) {
    if (
      error instanceof ApiError &&
      error.code === "instrument_type_mismatch" &&
      error.details?.suggested_instrument_type === "cn_mutual_fund"
    ) {
      return resolveImport({ market: "CN", instrument_type: "cn_mutual_fund", code: code.trim() });
    }
    throw error;
  }
}

export function flattenResolveCandidates(result: ResolveResult): ResolveCandidate[] {
  if (result.resolved) {
    return [result.resolved];
  }
  if (result.ambiguous && result.candidates?.length) {
    return result.candidates.filter((c) => c.is_importable !== false);
  }
  return [];
}

export async function waitForInstrumentActive(instrumentId: string, maxAttempts = 60): Promise<void> {
  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const status = await getFetchStatus(instrumentId);
    if (status.instrument_status === "active") {
      return;
    }
    if (status.job_status === "failed" || status.instrument_status === "failed") {
      throw new Error(status.error_message ?? "历史数据抓取失败");
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error("录入超时，请稍后在资产资料库查看进度");
}

export async function importResolvedCandidate(
  candidate: ResolveCandidate,
  assetClass: string,
  region: string,
): Promise<import("@/types/api").Instrument> {
  if (!candidate.ticket_id) {
    throw new Error("缺少 resolution ticket，请重新查询");
  }
  const imported = await importAsync({
    ticket_id: candidate.ticket_id,
    asset_class: assetClass,
    region,
  });
  await waitForInstrumentActive(imported.instrument_id);
  return getInstrument(imported.instrument_id);
}
