/** Rebalance plan draft fund pool and validation helpers. */

export const REBALANCE_FUND_TOLERANCE_MINOR = 100;
export const SYSTEM_CASH_INSTRUMENT_ID = "system_cash_cny";

export interface FundPoolLine {
  baseline_current_minor: number;
  planned_current_minor: number;
}

export interface FundPoolSummary {
  releasedMinor: number;
  usedMinor: number;
  netMinor: number;
}

export function computeFundPool(lines: FundPoolLine[]): FundPoolSummary {
  let releasedMinor = 0;
  let usedMinor = 0;
  for (const line of lines) {
    const delta = line.planned_current_minor - line.baseline_current_minor;
    if (delta < 0) releasedMinor += -delta;
    else if (delta > 0) usedMinor += delta;
  }
  return { releasedMinor, usedMinor, netMinor: releasedMinor - usedMinor };
}

export function isFundPoolBalanced(netMinor: number): boolean {
  return Math.abs(netMinor) <= REBALANCE_FUND_TOLERANCE_MINOR;
}

export function countStagedChanges(
  lines: { baseline_current_minor: number; planned_current_minor: number }[],
): number {
  return lines.filter(
    (line) => line.planned_current_minor !== line.baseline_current_minor,
  ).length;
}

export interface PackageDeltaLine {
  instrument_name?: string;
  instrument_code?: string;
  recommended_package_delta_minor: number;
}

export function recommendedPlannedMinor(
  baselineMinor: number,
  packageDeltaMinor: number,
): number {
  return baselineMinor + packageDeltaMinor;
}

export function formatPackageDeltaLabel(deltaMinor: number): string {
  if (deltaMinor === 0) return "—";
  const sign = deltaMinor > 0 ? "+" : "−";
  const abs = Math.abs(deltaMinor);
  const yuan = abs / 100;
  if (yuan >= 10_000) {
    return `${sign}${Math.round(yuan / 10_000)}w`;
  }
  return `${sign}${Math.round(yuan)}`;
}

export function buildReferencePackageItems(lines: PackageDeltaLine[]): string[] {
  return lines
    .filter((line) => line.recommended_package_delta_minor !== 0)
    .map((line) => {
      const label = line.instrument_name ?? line.instrument_code ?? "标的";
      return `${label} ${formatPackageDeltaLabel(line.recommended_package_delta_minor)}`;
    });
}

export function hasReferencePackage(lines: PackageDeltaLine[]): boolean {
  return lines.some((line) => line.recommended_package_delta_minor !== 0);
}

export interface CashSweepCandidate {
  holding_id: string;
  instrument_id: string;
  instrument_name?: string;
  current_amount_minor: number;
}

export function findCashSweepHolding(
  holdings: {
    id: string;
    instrument_id: string;
    instrument_name?: string;
    enabled: boolean;
    asset_class: string;
    sort_order: number;
    current_amount_minor: number;
  }[],
): CashSweepCandidate | null {
  let fallback: (CashSweepCandidate & { sort_order: number }) | null = null;
  for (const h of holdings) {
    if (!h.enabled) continue;
    if (h.instrument_id === SYSTEM_CASH_INSTRUMENT_ID) {
      return {
        holding_id: h.id,
        instrument_id: h.instrument_id,
        instrument_name: h.instrument_name,
        current_amount_minor: h.current_amount_minor,
      };
    }
    if (h.asset_class === "cash" && (!fallback || h.sort_order < fallback.sort_order)) {
      fallback = {
        holding_id: h.id,
        instrument_id: h.instrument_id,
        instrument_name: h.instrument_name,
        current_amount_minor: h.current_amount_minor,
        sort_order: h.sort_order,
      };
    }
  }
  if (!fallback) return null;
  const { sort_order: _sort, ...candidate } = fallback;
  return candidate;
}

export function applyRecommendedOneLine(
  line: {
    id: string;
    baseline_current_minor: number;
    recommended_package_delta_minor: number;
  },
): { line_id: string; planned_current_minor: number } {
  return {
    line_id: line.id,
    planned_current_minor: recommendedPlannedMinor(
      line.baseline_current_minor,
      line.recommended_package_delta_minor,
    ),
  };
}

export function lineChangeStatus(
  line: {
    baseline_current_minor: number;
    planned_current_minor: number;
    last_saved_at?: number | null;
  },
  undoneLineIds: Set<string>,
  lineId: string,
): "unchanged" | "staged" | "undone" {
  if (undoneLineIds.has(lineId)) return "undone";
  if (line.planned_current_minor === line.baseline_current_minor) return "unchanged";
  if (line.last_saved_at) return "staged";
  return "unchanged";
}
