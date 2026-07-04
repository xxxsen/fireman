import { ApiError } from "@/lib/api/client";
import { getInstrument } from "@/lib/api/instruments";
import {
  importFromMarketAsset,
  listMarketAssets,
  type MarketAsset,
} from "@/lib/api/market-assets";
import type { Instrument } from "@/types/api";

export function looksLikeFundCode(query: string): boolean {
  const trimmed = query.trim();
  return trimmed.length >= 4 && /^[a-zA-Z0-9.]+$/.test(trimmed);
}

/**
 * Searches the local market asset directory (never remote sources) for
 * candidates matching the query. Only active entries are offered for import.
 */
export async function searchMarketAssetCandidates(
  query: string,
  limit = 10,
): Promise<MarketAsset[]> {
  const result = await listMarketAssets({ q: query.trim(), limit });
  return result.assets.filter((a) => a.active);
}

/** Raised when the market asset has no locally synced history yet. */
export class MarketAssetHistoryEmptyError extends Error {
  constructor(public readonly assetKey: string) {
    super("该资产还没有本地历史数据，请先在资产详情页同步历史数据后再录入。");
    this.name = "MarketAssetHistoryEmptyError";
  }
}

/**
 * Imports a market asset into the user library. Import is a purely local
 * projection of already-synced history, so the instrument is usable
 * immediately; no fetch polling is required.
 */
export async function importMarketAssetCandidate(
  asset: MarketAsset,
  assetClass: string,
  region: string,
): Promise<Instrument> {
  try {
    return await importFromMarketAsset({
      asset_key: asset.asset_key,
      asset_class: assetClass,
      region,
    });
  } catch (error) {
    if (error instanceof ApiError && error.code === "market_asset_history_empty") {
      throw new MarketAssetHistoryEmptyError(asset.asset_key);
    }
    if (error instanceof ApiError && error.code === "instrument_already_exists") {
      const instId = error.details?.instrument_id;
      if (typeof instId === "string" && instId) {
        return getInstrument(instId);
      }
    }
    throw error;
  }
}
