import { assetClassLabel, formatMoney, formatPercent, regionLabel } from "@/lib/format";
import type { AssetClassTarget, Instrument } from "@/types/api";

/** Default region split used when creating a plan from the wizard. */
export const WIZARD_DEFAULT_REGION_WEIGHT = {
  domestic: 1,
  foreign: 0,
} as const;

export const WIZARD_ASSET_CLASS_ORDER = ["equity", "bond", "cash"] as const;

export function computeExpectedAmountMinor(
  totalAssetsMinor: number,
  classWeight: number,
  weightWithinClass: number,
): number {
  return Math.round(totalAssetsMinor * classWeight * weightWithinClass);
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

export interface WizardHoldingSelection {
  inst: Instrument;
  weight: number;
  amount: number;
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

function regionWeightWithinClass(region: string): number {
  if (region === "domestic") return WIZARD_DEFAULT_REGION_WEIGHT.domestic;
  if (region === "foreign") return WIZARD_DEFAULT_REGION_WEIGHT.foreign;
  return 0;
}

function portfolioTargetWeight(
  classWeights: Record<string, number>,
  assetClass: string,
  region: string,
  weightWithinGroup: number,
): number {
  const acW = classWeights[assetClass] ?? 0;
  return acW * regionWeightWithinClass(region) * weightWithinGroup;
}

export function buildWizardPortfolioReview(input: {
  scenarioWeights: AssetClassTarget[];
  selectedInstruments: WizardHoldingSelection[];
  totalAssetsMinor: number;
  gapToCash: boolean;
  assetGapMinor: number;
}): WizardPortfolioReview {
  const classWeights = scenarioClassWeights(input.scenarioWeights);
  const rows: WizardAllocationRow[] = input.selectedInstruments.map((s) => {
    const ptw = portfolioTargetWeight(classWeights, s.inst.asset_class, s.inst.region, s.weight);
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

  if (input.gapToCash && input.assetGapMinor > 100) {
    const cashClassWeight = classWeights.cash ?? 0;
    rows.push({
      key: "virtual-cash-gap",
      assetClass: "cash",
      assetClassLabel: assetClassLabel("cash"),
      instrumentName: "现金/其他（未分配差额）",
      instrumentCode: "CASH",
      region: "domestic",
      regionLabel: regionLabel("domestic"),
      groupWeight: 1,
      portfolioTargetWeight: cashClassWeight,
      currentAmountMinor: input.assetGapMinor,
      targetAmountMinor: Math.round(input.totalAssetsMinor * cashClassWeight),
      pendingAmountMinor: Math.round(input.totalAssetsMinor * cashClassWeight) - input.assetGapMinor,
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
