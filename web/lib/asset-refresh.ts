/** Asset refresh validation helpers. */

import type { PlanHolding } from "@/types/api";
import { assetClassLabel, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";

export const ASSET_REFRESH_TOLERANCE_MINOR = 100;

const WEIGHT_EPSILON = 1e-6;

function weightWithinGroupChanged(before: number, after: number): boolean {
  return Math.abs(before - after) > WEIGHT_EPSILON;
}

export interface AssetRefreshRow {
  asset_key: string;
  current_amount_minor: number;
}

export interface AssetRefreshHolding {
  id: string;
  asset_key: string;
  label: string;
  code: string;
  asset_class: string;
  region: string;
  current_amount_minor: number;
  weight_within_group: number;
  sort_order: number;
  is_system: boolean;
}

export function holdingFromPlan(holding: PlanHolding, isSystem = false): AssetRefreshHolding {
  return {
    id: holding.id,
    asset_key: holding.asset_key,
    label: holding.instrument_name ?? holding.instrument_code ?? holding.asset_key,
    code: holding.instrument_code ?? holding.asset_key,
    asset_class: holding.asset_class,
    region: holding.region,
    current_amount_minor: holding.current_amount_minor,
    weight_within_group: holding.weight_within_group,
    sort_order: holding.sort_order,
    is_system: isSystem,
  };
}

export function countAssetRefreshChanges(
  before: PlanHolding[],
  after: AssetRefreshHolding[],
): number {
  const beforeByInstrument = new Map(
    before.map((holding) => [holding.asset_key, holding] as const),
  );
  const afterByInstrument = new Map(
    after.map((holding) => [holding.asset_key, holding] as const),
  );
  const instrumentIds = new Set([
    ...beforeByInstrument.keys(),
    ...afterByInstrument.keys(),
  ]);
  let count = 0;
  for (const instrumentId of instrumentIds) {
    const beforeHolding = beforeByInstrument.get(instrumentId);
    const afterHolding = afterByInstrument.get(instrumentId);
    if (!beforeHolding || !afterHolding) {
      count++;
      continue;
    }
    if (beforeHolding.current_amount_minor !== afterHolding.current_amount_minor) {
      count++;
      continue;
    }
    if (weightWithinGroupChanged(beforeHolding.weight_within_group, afterHolding.weight_within_group)) {
      count++;
    }
  }
  return count;
}

export function hasAssetRefreshDraftChanges(
  before: PlanHolding[],
  after: AssetRefreshHolding[],
  totalAssetsMinor: number,
): boolean {
  const beforeTotal = sumHoldingsMinor(
    before.map((holding) => ({
      asset_key: holding.asset_key,
      current_amount_minor: holding.current_amount_minor,
    })),
  );
  return countAssetRefreshChanges(before, after) > 0 || totalAssetsMinor !== beforeTotal;
}

export function hasAssetRefreshStructureChange(
  before: PlanHolding[],
  after: AssetRefreshHolding[],
): boolean {
  return countAssetRefreshChanges(before, after) > 0;
}

export function defaultWeightWithinGroup(
  holdings: AssetRefreshHolding[],
  assetClass: string,
  region: string,
): number {
  const inGroup = holdings.filter(
    (holding) => holding.asset_class === assetClass && holding.region === region,
  );
  const used = inGroup.reduce((sum, holding) => sum + holding.weight_within_group, 0);
  const remaining = 1 - used;
  return remaining > 0 ? remaining : 0;
}

export function validateAssetRefreshGroupWeights(
  holdings: AssetRefreshHolding[],
): { ok: boolean; message?: string } {
  const groups = new Map<string, AssetRefreshHolding[]>();
  for (const holding of holdings) {
    const key = `${holding.asset_class}:${holding.region}`;
    const bucket = groups.get(key) ?? [];
    bucket.push(holding);
    groups.set(key, bucket);
  }
  for (const [key, rows] of groups) {
    const [assetClass, region] = key.split(":");
    const check = validatePercentSum(
      rows.map((row) => ({ label: row.asset_key, value: row.weight_within_group })),
    );
    if (!check.passed) {
      return {
        ok: false,
        message: `${assetClassLabel(assetClass)} · ${regionLabel(region)} 组内配比：${check.message}`,
      };
    }
  }
  return { ok: true };
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
  draftHoldings: AssetRefreshHolding[],
  totalAssetsMinor: number,
  syncTotalAssetsMinor: boolean,
  configChanged = false,
  scenarioId?: string | null,
) {
  return {
    config_version: configVersion,
    ...(scenarioId ? { scenario_id: scenarioId } : {}),
    holdings: draftHoldings.map((row, index) => ({
      asset_key: row.asset_key,
      asset_class: row.asset_class,
      region: row.region,
      current_amount_minor: row.current_amount_minor,
      weight_within_group: row.weight_within_group,
      sort_order: row.sort_order ?? index * 10,
    })),
    total_assets_minor: totalAssetsMinor,
    sync_total_assets_minor: syncTotalAssetsMinor,
    config_changed: configChanged,
  };
}
