/** Canonical display order for asset classes across plan UI. */
export const ASSET_CLASS_ORDER = ["equity", "bond", "cash"] as const;

export const REGION_ORDER = ["domestic", "foreign"] as const;

export function assetClassSortIndex(assetClass: string): number {
  const index = ASSET_CLASS_ORDER.indexOf(assetClass as (typeof ASSET_CLASS_ORDER)[number]);
  return index === -1 ? ASSET_CLASS_ORDER.length : index;
}

export function regionSortIndex(region: string): number {
  const index = REGION_ORDER.indexOf(region as (typeof REGION_ORDER)[number]);
  return index === -1 ? REGION_ORDER.length : index;
}
