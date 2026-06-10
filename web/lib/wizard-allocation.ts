import { assetClassLabel, formatMoney, formatPercent, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import type { AssetClassTarget, Instrument, RegionTarget } from "@/types/api";

/** Default region split used when creating a plan from the wizard. */
export const WIZARD_DEFAULT_REGION_WEIGHT = {
  domestic: 1,
  foreign: 0,
} as const;

export const WIZARD_ASSET_CLASS_ORDER = ["equity", "bond", "cash"] as const;

export type WizardRegionEditableClass = "equity" | "bond";

export type WizardRegionTargets = Record<
  WizardRegionEditableClass,
  { domestic: number; foreign: number }
>;

export function defaultWizardRegionTargets(): WizardRegionTargets {
  return {
    equity: { ...WIZARD_DEFAULT_REGION_WEIGHT },
    bond: { ...WIZARD_DEFAULT_REGION_WEIGHT },
  };
}

export function computeExpectedAmountMinor(
  totalAssetsMinor: number,
  classWeight: number,
  regionWeight: number,
  weightWithinGroup: number,
): number {
  return Math.round(totalAssetsMinor * classWeight * regionWeight * weightWithinGroup);
}

export function pruneSelectedByScenario(
  selected: WizardHoldingSelection[],
  scenarioWeights: AssetClassTarget[],
): WizardHoldingSelection[] {
  const active = new Set(
    scenarioWeights.filter((w) => w.weight > 0.0001).map((w) => w.asset_class),
  );
  return selected.filter((s) => active.has(s.inst.asset_class));
}

/** Remove foreign holdings when a class's foreign region target is zero. */
export function pruneSelectedByRegionTargets(
  selected: WizardHoldingSelection[],
  regionTargets: WizardRegionTargets,
): WizardHoldingSelection[] {
  return selected.filter((s) => {
    const ac = s.inst.asset_class;
    if (ac !== "equity" && ac !== "bond") return true;
    if (s.inst.region === "foreign" && regionTargets[ac].foreign <= 0.0001) {
      return false;
    }
    return true;
  });
}

export interface WizardHoldingSelection {
  inst: Instrument;
  weight: number;
  amount: number;
  /** User manually edited weight; excluded from auto redistribution. */
  weightManual?: boolean;
}

export function complementRegionWeight(value: number): number {
  const clamped = Math.max(0, Math.min(1, value));
  return Math.max(0, Math.min(1, 1 - clamped));
}

/** Redistribute weights: locked items keep weight; others split the remainder equally. */
export function redistributeGroupWeights(items: WizardHoldingSelection[]): WizardHoldingSelection[] {
  if (items.length === 0) return [];
  const lockedSum = items
    .filter((s) => s.weightManual)
    .reduce((sum, s) => sum + s.weight, 0);
  const unlocked = items.filter((s) => !s.weightManual);
  if (unlocked.length === 0) return items;
  const remaining = Math.max(0, 1 - lockedSum);
  const each = remaining / unlocked.length;
  return items.map((s) => (s.weightManual ? s : { ...s, weight: each }));
}

export function addInstrumentToGroup(
  items: WizardHoldingSelection[],
  inst: Instrument,
): WizardHoldingSelection[] {
  return redistributeGroupWeights([
    ...items,
    { inst, weight: 0, amount: 0, weightManual: false },
  ]);
}

export function updateInstrumentWeightInGroup(
  items: WizardHoldingSelection[],
  instrumentId: string,
  weight: number,
): WizardHoldingSelection[] {
  const next = items.map((s) =>
    s.inst.id === instrumentId ? { ...s, weight, weightManual: true } : s,
  );
  return redistributeGroupWeights(next);
}

export function removeInstrumentFromGroup(
  items: WizardHoldingSelection[],
  instrumentId: string,
): WizardHoldingSelection[] {
  return redistributeGroupWeights(items.filter((s) => s.inst.id !== instrumentId));
}

export interface WizardAllocationGroup {
  key: string;
  label: string;
  assetClass: string;
  region?: string;
}

export interface WizardAllocationRow {
  key: string;
  assetClass: string;
  assetClassLabel: string;
  instrumentName: string;
  instrumentCode: string;
  region: string;
  regionLabel: string;
  groupWeight: number;
  portfolioTargetWeight: number;
  currentAmountMinor: number;
  targetAmountMinor: number;
  pendingAmountMinor: number;
  isVirtualCash?: boolean;
}

export interface WizardMissingClass {
  assetClass: string;
  label: string;
  target: number;
  covered: number;
  gap: number;
}

export interface WizardPortfolioReview {
  rows: WizardAllocationRow[];
  portfolioSum: number;
  missingClasses: WizardMissingClass[];
  configuredSummary: string;
  passed: boolean;
  message: string;
}

function scenarioClassWeights(weights: AssetClassTarget[]): Record<string, number> {
  const out: Record<string, number> = { equity: 0, bond: 0, cash: 0 };
  for (const w of weights) {
    out[w.asset_class] = w.weight;
  }
  return out;
}

function regionWeightFromTargets(
  regionTargets: WizardRegionTargets,
  assetClass: string,
  region: string,
): number {
  if (assetClass === "cash") {
    return region === "domestic" ? 1 : 0;
  }
  if (assetClass === "equity" || assetClass === "bond") {
    if (region === "domestic") return regionTargets[assetClass].domestic;
    if (region === "foreign") return regionTargets[assetClass].foreign;
  }
  return 0;
}

function portfolioTargetWeight(
  classWeights: Record<string, number>,
  regionTargets: WizardRegionTargets,
  assetClass: string,
  region: string,
  weightWithinGroup: number,
): number {
  const acW = classWeights[assetClass] ?? 0;
  const regW = regionWeightFromTargets(regionTargets, assetClass, region);
  return acW * regW * weightWithinGroup;
}

/** Groups that participate in step-2 group weight validation. */
export function getWizardAllocationGroups(
  scenarioWeights: AssetClassTarget[],
  regionTargets: WizardRegionTargets,
): WizardAllocationGroup[] {
  const groups: WizardAllocationGroup[] = [];
  const weightByClass = new Map(scenarioWeights.map((w) => [w.asset_class, w.weight]));

  for (const ac of WIZARD_ASSET_CLASS_ORDER) {
    const classWeight = weightByClass.get(ac) ?? 0;
    if (classWeight <= 0.0001) continue;

    if (ac === "cash") {
      groups.push({
        key: "cash",
        label: assetClassLabel("cash"),
        assetClass: "cash",
      });
      continue;
    }

    const rt = regionTargets[ac];
    if (rt.foreign > 0.0001) {
      groups.push({
        key: `${ac}-domestic`,
        label: `${assetClassLabel(ac)} · ${regionLabel("domestic")}`,
        assetClass: ac,
        region: "domestic",
      });
      groups.push({
        key: `${ac}-foreign`,
        label: `${assetClassLabel(ac)} · ${regionLabel("foreign")}`,
        assetClass: ac,
        region: "foreign",
      });
    } else {
      groups.push({
        key: `${ac}-domestic`,
        label: assetClassLabel(ac),
        assetClass: ac,
        region: "domestic",
      });
    }
  }
  return groups;
}

export function validateWizardGroupWeights(
  selected: WizardHoldingSelection[],
  groups: WizardAllocationGroup[],
  options?: { skipImplicitCash?: boolean },
): { key: string; label: string; passed: boolean; message: string }[] {
  return groups
    .filter((g) => {
      if (options?.skipImplicitCash && g.assetClass === "cash") return false;
      return true;
    })
    .map((g) => {
      const items = selected
        .filter((s) => s.inst.asset_class === g.assetClass)
        .filter((s) => (g.region ? s.inst.region === g.region : true))
        .map((s) => ({ label: s.inst.code, value: s.weight }));
      const check = validatePercentSum(items);
      return { key: g.key, label: g.label, passed: check.passed, message: check.message };
    });
}

export function buildRegionTargetsPayload(regionTargets: WizardRegionTargets): RegionTarget[] {
  const out: RegionTarget[] = [];
  for (const ac of WIZARD_ASSET_CLASS_ORDER) {
    if (ac === "cash") {
      out.push(
        { asset_class: "cash", region: "domestic", weight_within_class: 1 },
        { asset_class: "cash", region: "foreign", weight_within_class: 0 },
      );
      continue;
    }
    const rt = regionTargets[ac];
    out.push(
      { asset_class: ac, region: "domestic", weight_within_class: rt.domestic },
      { asset_class: ac, region: "foreign", weight_within_class: rt.foreign },
    );
  }
  return out;
}

export function formatRegionTargetsSummary(
  scenarioWeights: AssetClassTarget[],
  regionTargets: WizardRegionTargets,
): string {
  const weightByClass = new Map(scenarioWeights.map((w) => [w.asset_class, w.weight]));
  const parts: string[] = [];
  for (const ac of ["equity", "bond"] as const) {
    if ((weightByClass.get(ac) ?? 0) <= 0.0001) continue;
    const rt = regionTargets[ac];
    parts.push(
      `${assetClassLabel(ac)} 国内 ${formatPercent(rt.domestic)} / 国外 ${formatPercent(rt.foreign)}`,
    );
  }
  return parts.join(" · ");
}

export function summarizeHoldingsByRegion(selected: WizardHoldingSelection[]): {
  domesticMinor: number;
  foreignMinor: number;
  domesticPct: number;
  foreignPct: number;
} {
  let domesticMinor = 0;
  let foreignMinor = 0;
  for (const s of selected) {
    if (s.inst.region === "foreign") foreignMinor += s.amount;
    else domesticMinor += s.amount;
  }
  const total = domesticMinor + foreignMinor;
  const domesticPct = total > 0 ? domesticMinor / total : 0;
  const foreignPct = total > 0 ? foreignMinor / total : 0;
  return { domesticMinor, foreignMinor, domesticPct, foreignPct };
}

export function buildWizardPortfolioReview(input: {
  scenarioWeights: AssetClassTarget[];
  regionTargets: WizardRegionTargets;
  selectedInstruments: WizardHoldingSelection[];
  totalAssetsMinor: number;
  gapToCash: boolean;
  assetGapMinor: number;
  /** When true, unallocated cash class weight is satisfied without picking cash instruments. */
  implicitCash?: boolean;
}): WizardPortfolioReview {
  const classWeights = scenarioClassWeights(input.scenarioWeights);
  const rows: WizardAllocationRow[] = input.selectedInstruments.map((s) => {
    const ptw = portfolioTargetWeight(
      classWeights,
      input.regionTargets,
      s.inst.asset_class,
      s.inst.region,
      s.weight,
    );
    const targetAmountMinor = Math.round(input.totalAssetsMinor * ptw);
    return {
      key: s.inst.id,
      assetClass: s.inst.asset_class,
      assetClassLabel: assetClassLabel(s.inst.asset_class),
      instrumentName: s.inst.name,
      instrumentCode: s.inst.code,
      region: s.inst.region,
      regionLabel: regionLabel(s.inst.region),
      groupWeight: s.weight,
      portfolioTargetWeight: ptw,
      currentAmountMinor: s.amount,
      targetAmountMinor,
      pendingAmountMinor: targetAmountMinor - s.amount,
    };
  });

  const cashClassWeight = classWeights.cash ?? 0;
  const hasCashInstruments = input.selectedInstruments.some((s) => s.inst.asset_class === "cash");
  const shouldAddVirtualCash =
    cashClassWeight > 0.0001 &&
    ((input.gapToCash && input.assetGapMinor > 100) ||
      (input.implicitCash && !hasCashInstruments));
  if (shouldAddVirtualCash) {
    const targetAmountMinor = Math.round(input.totalAssetsMinor * cashClassWeight);
    const currentAmountMinor =
      input.assetGapMinor > 100 ? input.assetGapMinor : Math.max(0, targetAmountMinor);
    rows.push({
      key: "virtual-cash-gap",
      assetClass: "cash",
      assetClassLabel: assetClassLabel("cash"),
      instrumentName: "现金/其他（未配置部分）",
      instrumentCode: "CASH",
      region: "domestic",
      regionLabel: regionLabel("domestic"),
      groupWeight: 1,
      portfolioTargetWeight: cashClassWeight,
      currentAmountMinor,
      targetAmountMinor,
      pendingAmountMinor: targetAmountMinor - currentAmountMinor,
      isVirtualCash: true,
    });
  }

  const coveredByClass: Record<string, number> = { equity: 0, bond: 0, cash: 0 };
  for (const row of rows) {
    coveredByClass[row.assetClass] = (coveredByClass[row.assetClass] ?? 0) + row.portfolioTargetWeight;
  }

  const missingClasses: WizardMissingClass[] = [];
  for (const w of input.scenarioWeights) {
    if (w.weight <= 0.0001) continue;
    const covered = coveredByClass[w.asset_class] ?? 0;
    const gap = w.weight - covered;
    if (gap > 0.0001) {
      missingClasses.push({
        assetClass: w.asset_class,
        label: assetClassLabel(w.asset_class),
        target: w.weight,
        covered,
        gap,
      });
    }
  }

  const portfolioSum = rows.reduce((sum, row) => sum + row.portfolioTargetWeight, 0);
  const configuredParts = Object.entries(coveredByClass)
    .filter(([, v]) => v > 0.0001)
    .map(([ac, v]) => `${assetClassLabel(ac)} ${formatPercent(v)}`);
  const configuredSummary = configuredParts.join("、");

  const gap = 1 - portfolioSum;
  const passed = Math.abs(gap) <= 0.0001 && missingClasses.length === 0;
  let message = passed
    ? "全组合目标权重合计 100.00%，通过"
    : `全组合目标权重合计 ${formatPercent(portfolioSum)}，还差 ${formatPercent(Math.max(gap, 0))}。`;
  if (!passed && missingClasses.length > 0) {
    message +=
      "还缺少：" +
      missingClasses.map((m) => `${m.label}（目标 ${formatPercent(m.target)}）`).join("、") +
      "。";
  }
  if (!passed && configuredSummary) {
    message += `已配置：${configuredSummary}。`;
  }
  if (!passed) {
    message += "请返回上一步补充对应方向标的，或调整场景配置。";
  }

  return {
    rows,
    portfolioSum,
    missingClasses,
    configuredSummary,
    passed,
    message,
  };
}

export function formatPendingAmount(minor: number, currency = "CNY"): string {
  if (minor === 0) return "—";
  const prefix = minor > 0 ? "待投入 " : "待减配 ";
  return prefix + formatMoney(Math.abs(minor), currency);
}
