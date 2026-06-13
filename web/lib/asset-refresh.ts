/** Asset refresh validation helpers. */

import type { PlanHolding } from "@/types/api";

export const ASSET_REFRESH_TOLERANCE_MINOR = 100;

export interface AssetRefreshRow {
  instrument_id: string;
  current_amount_minor: number;
}

export interface AssetRefreshHolding {
  id: string;
  instrument_id: string;
  label: string;
  code: string;
  asset_class: string;
  region: string;
  enabled: boolean;
  current_amount_minor: number;
  weight_within_group: number;
  sort_order: number;
  is_system: boolean;
}

export function holdingFromPlan(holding: PlanHolding, isSystem = false): AssetRefreshHolding {
  return {
    id: holding.id,
    instrument_id: holding.instrument_id,
    label: holding.instrument_name ?? holding.instrument_code ?? holding.instrument_id,
    code: holding.instrument_code ?? holding.instrument_id,
    asset_class: holding.asset_class,
    region: holding.region,
    enabled: holding.enabled,
    current_amount_minor: holding.current_amount_minor,
    weight_within_group: holding.weight_within_group,
    sort_order: holding.sort_order,
    is_system: isSystem,
  };
}

export function hasAssetRefreshStructureChange(
  before: PlanHolding[],
  after: AssetRefreshHolding[],
): boolean {
  const beforeByInstrument = new Map(
    before.map((holding) => [holding.instrument_id, holding.enabled] as const),
  );
  const afterByInstrument = new Map(
    after.map((holding) => [holding.instrument_id, holding.enabled] as const),
  );
  if (beforeByInstrument.size !== afterByInstrument.size) return true;
  for (const [instrumentId, enabled] of beforeByInstrument) {
    if (!afterByInstrument.has(instrumentId)) return true;
    if (afterByInstrument.get(instrumentId) !== enabled) return true;
  }
  return false;
}

export function redistributeEnabledWeightsInGroup(
  holdings: AssetRefreshHolding[],
  assetClass: string,
  region: string,
): AssetRefreshHolding[] {
  const enabledInGroup = holdings.filter(
    (holding) =>
      holding.asset_class === assetClass && holding.region === region && holding.enabled,
  );
  if (enabledInGroup.length === 0) return holdings;
  const each = 1 / enabledInGroup.length;
  return holdings.map((holding) =>
    holding.asset_class === assetClass && holding.region === region && holding.enabled
      ? { ...holding, weight_within_group: each }
      : holding,
  );
}

export function buildHoldingsUpdateItems(holdings: AssetRefreshHolding[]) {
  return holdings.map((holding, index) => ({
    instrument_id: holding.instrument_id,
    enabled: holding.enabled,
    weight_within_group: holding.weight_within_group,
    current_amount_minor: holding.current_amount_minor,
    sort_order: holding.sort_order ?? index * 10,
  }));
}

export function sumHoldingsMinor(rows: AssetRefreshRow[]): number {
  return rows.reduce((sum, row) => sum + row.current_amount_minor, 0);
}

export function validateAssetRefreshTotal(
  rows: AssetRefreshRow[],
  totalAssetsMinor: number,
): { ok: boolean; message?: string } {
  const sum = sumHoldingsMinor(rows);
  const gap = Math.abs(sum - totalAssetsMinor);
  if (gap > ASSET_REFRESH_TOLERANCE_MINOR) {
    return {
      ok: false,
      message: `分项合计与资产总值相差 ${(gap / 100).toFixed(2)} 元，请核对后再提交`,
    };
  }
  return { ok: true };
}

export function buildAssetRefreshBody(
  configVersion: number,
  rows: AssetRefreshRow[],
  totalAssetsMinor: number,
  syncTotalAssetsMinor: boolean,
  configChanged = false,
) {
  return {
    config_version: configVersion,
    holdings: rows.map((row) => ({
      instrument_id: row.instrument_id,
      current_amount_minor: row.current_amount_minor,
    })),
    total_assets_minor: totalAssetsMinor,
    sync_total_assets_minor: syncTotalAssetsMinor,
    config_changed: configChanged,
  };
}
