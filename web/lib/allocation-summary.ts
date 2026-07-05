import {
  ASSET_CLASS_ORDER,
  assetClassSortIndex,
  regionSortIndex,
} from "@/lib/asset-class-order";
import type { HoldingTargetLine, RebalanceLine, TargetView } from "@/types/api";

export interface AllocationSummaryRow {
  key: string;
  level: "asset_class" | "region";
  asset_class: string;
  region?: string;
  target_weight: number;
  /** Region rows: domestic/foreign share within the parent asset class (0–1). */
  target_weight_within_parent?: number;
  current_weight: number;
  /** Region rows: domestic/foreign share within parent from actual holdings. */
  current_weight_within_parent?: number;
  target_amount_minor: number;
  current_amount_minor: number;
  gap_amount_minor: number;
  gap_weight: number;
}

function aggregateStructural(
  key: string,
  level: AllocationSummaryRow["level"],
  assetClass: string,
  region: string | undefined,
  lines: HoldingTargetLine[],
): AllocationSummaryRow {
  const targetWeight = lines.reduce((sum, line) => sum + line.portfolio_target_weight, 0);
  const currentWeight = lines.reduce((sum, line) => sum + line.structural_current_weight, 0);
  const targetAmount = lines.reduce((sum, line) => sum + line.structural_target_amount_minor, 0);
  const currentAmount = lines.reduce((sum, line) => sum + line.current_amount_minor, 0);
  const gapAmount = lines.reduce((sum, line) => sum + line.structural_gap_amount_minor, 0);
  const gapWeight = lines.reduce((sum, line) => sum + line.structural_gap_weight, 0);
  return {
    key,
    level,
    asset_class: assetClass,
    region,
    target_weight: targetWeight,
    current_weight: currentWeight,
    target_amount_minor: targetAmount,
    current_amount_minor: currentAmount,
    gap_amount_minor: gapAmount,
    gap_weight: gapWeight,
  };
}

export function buildAllocationSummary(targets: TargetView): AllocationSummaryRow[] {
  const enabled = targets.holdings.filter((line) => line.enabled);
  const rows: AllocationSummaryRow[] = [];

  const activeClasses = ASSET_CLASS_ORDER.filter((assetClass) => {
    const classTarget = targets.asset_class_targets.find(
      (target) => target.asset_class === assetClass,
    );
    const hasHoldings = enabled.some((line) => line.asset_class === assetClass);
    return (classTarget?.weight ?? 0) > 0 || hasHoldings;
  });

  for (const assetClass of activeClasses) {
    const classLines = enabled.filter((line) => line.asset_class === assetClass);
    const classRow = aggregateStructural(
      assetClass,
      "asset_class",
      assetClass,
      undefined,
      classLines,
    );
    rows.push(classRow);

    const regionWeightByKey = new Map<string, number>();
    for (const target of targets.region_targets) {
      if (target.asset_class !== assetClass) continue;
      if (target.weight_within_class > 0) {
        regionWeightByKey.set(target.region, target.weight_within_class);
      }
    }
    for (const line of classLines) {
      if (regionWeightByKey.has(line.region)) continue;
      const configured = targets.region_targets.find(
        (target) => target.asset_class === assetClass && target.region === line.region,
      );
      regionWeightByKey.set(line.region, configured?.weight_within_class ?? 0);
    }

    const regionTargets = [...regionWeightByKey.entries()]
      .sort(
        (left, right) => regionSortIndex(left[0]) - regionSortIndex(right[0]),
      )
      .map(([region, weight_within_class]) => ({ region, weight_within_class }));

    for (const regionTarget of regionTargets) {
      const regionLines = classLines.filter(
        (line) => line.region === regionTarget.region,
      );
      const regionRow = aggregateStructural(
        `${assetClass}:${regionTarget.region}`,
        "region",
        assetClass,
        regionTarget.region,
        regionLines,
      );
      rows.push({
        ...regionRow,
        target_weight_within_parent: regionTarget.weight_within_class,
        current_weight_within_parent:
          classRow.current_weight > 0
            ? regionRow.current_weight / classRow.current_weight
            : 0,
      });
    }
  }

  return rows.sort((left, right) => {
    const classDelta = assetClassSortIndex(left.asset_class) - assetClassSortIndex(right.asset_class);
    if (classDelta !== 0) return classDelta;
    if (left.level !== right.level) {
      return left.level === "asset_class" ? -1 : 1;
    }
    return regionSortIndex(left.region ?? "") - regionSortIndex(right.region ?? "");
  });
}

export interface RebalanceWorkspaceRow {
  key: string;
  level: "asset_class" | "region" | "holding";
  asset_class: string;
  region?: string;
  label: string;
  instrument_code?: string;
  asset_key?: string;
  holding_id?: string;
  target_weight: number;
  target_weight_within_parent?: number;
  current_weight: number;
  current_weight_within_parent?: number;
  target_amount_minor: number;
  current_amount_minor: number;
  gap_amount_minor: number;
  gap_weight: number;
  action?: string;
  suggested_trade_minor?: number;
  plan_scale_action?: string;
  plan_scale_suggested_trade_minor?: number;
  plan_gap_amount_minor?: number;
}

function summaryToWorkspaceRow(row: AllocationSummaryRow): RebalanceWorkspaceRow {
  return {
    key: row.key,
    level: row.level,
    asset_class: row.asset_class,
    region: row.region,
    label:
      row.level === "asset_class"
        ? row.asset_class
        : row.region ?? "",
    target_weight: row.target_weight,
    target_weight_within_parent: row.target_weight_within_parent,
    current_weight: row.current_weight,
    current_weight_within_parent: row.current_weight_within_parent,
    target_amount_minor: row.target_amount_minor,
    current_amount_minor: row.current_amount_minor,
    gap_amount_minor: row.gap_amount_minor,
    gap_weight: row.gap_weight,
  };
}

function holdingToWorkspaceRow(line: RebalanceLine): RebalanceWorkspaceRow {
  return {
    key: line.holding_id,
    level: "holding",
    asset_class: line.asset_class,
    region: line.region,
    label: line.instrument_name ?? line.instrument_code ?? line.asset_key,
    instrument_code: line.instrument_code,
    asset_key: line.asset_key,
    holding_id: line.holding_id,
    target_weight: line.portfolio_target_weight,
    current_weight: line.structural_current_weight,
    target_amount_minor: line.structural_target_amount_minor,
    current_amount_minor: line.current_amount_minor,
    gap_amount_minor: line.structural_gap_amount_minor,
    gap_weight: line.structural_gap_weight,
    action: line.action,
    suggested_trade_minor: line.suggested_trade_minor,
    plan_scale_action: line.plan_scale_action,
    plan_scale_suggested_trade_minor: line.plan_scale_suggested_trade_minor,
    plan_gap_amount_minor: line.plan_gap_amount_minor,
  };
}

export function buildRebalanceWorkspaceRows(
  targets: TargetView,
  rebalanceLines: RebalanceLine[],
  actionFilter: string = "all",
): RebalanceWorkspaceRow[] {
  const summaryRows = buildAllocationSummary(targets);
  const holdingsByRegion = new Map<string, RebalanceLine[]>();

  for (const line of rebalanceLines) {
    if (!line.enabled) continue;
    if (actionFilter !== "all" && line.action !== actionFilter) continue;
    const key = `${line.asset_class}:${line.region}`;
    const bucket = holdingsByRegion.get(key) ?? [];
    bucket.push(line);
    holdingsByRegion.set(key, bucket);
  }

  const output: RebalanceWorkspaceRow[] = [];
  for (const summary of summaryRows) {
    output.push(summaryToWorkspaceRow(summary));
    if (summary.level !== "region") continue;
    const holdings = [...(holdingsByRegion.get(summary.key) ?? [])].sort(
      (left, right) => left.sort_order - right.sort_order,
    );
    for (const holding of holdings) {
      output.push(holdingToWorkspaceRow(holding));
    }
  }
  return output;
}
