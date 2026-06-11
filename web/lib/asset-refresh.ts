/** Asset refresh validation helpers. */

export const ASSET_REFRESH_TOLERANCE_MINOR = 100;

export interface AssetRefreshRow {
  instrument_id: string;
  current_amount_minor: number;
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
