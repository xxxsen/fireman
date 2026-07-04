"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { AssetClassHoldingPicker } from "@/components/plans/AssetClassHoldingPicker";
import { Button } from "@/components/ui/Button";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PageHeader } from "@/components/ui/PageHeader";
import { PercentInput } from "@/components/ui/PercentInput";
import { Stepper } from "@/components/ui/Stepper";
import { createPlanWizard } from "@/lib/api/plans";
import { listScenarios } from "@/lib/api/allocation";
import { createSimulation } from "@/lib/api/simulations";
import { assetClassLabel, formatMoney, formatPercent } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import {
  buildRegionTargetsPayload,
  buildWizardPortfolioReview,
  complementRegionWeight,
  defaultWizardRegionTargets,
  formatPendingAmount,
  formatRegionTargetsSummary,
  getWizardAllocationGroups,
  pruneSelectedByRegionTargets,
  pruneSelectedByScenario,
  summarizeHoldingsByRegion,
  validateWizardGroupWeights,
  WIZARD_ASSET_CLASS_ORDER,
  type WizardHoldingSelection,
  type WizardRegionEditableClass,
  type WizardRegionTargets,
} from "@/lib/wizard-allocation";
import type { PlanParameters } from "@/types/api";
import { ApiError } from "@/lib/api/client";

const STEPS = ["计划目标", "建立持仓", "确认组合"] as const;
const GOAL_STEP = 0;
const HOLDINGS_STEP = 1;
const CONFIRM_STEP = 2;
const DEFAULT_RUNS = 10000;
const FIRE_DURATION_PRESETS = [30, 40, 50] as const;

/**
 * Advanced FIRE parameters edited inside the collapsible disclosure on the
 * 计划目标 step. The defaults mirror the previous hard-coded wizard submission
 * exactly, so an untouched wizard sends the same payload as before; opening the
 * panel and editing values flows through to POST /plans/wizard.
 */
interface AdvancedFireParams {
  inflation_mode: string;
  fixed_inflation_rate: number;
  inflation_mu: number;
  inflation_phi: number;
  inflation_sigma: number;
  withdrawal_type: string;
  withdrawal_rate: number;
  withdrawal_floor_ratio: number;
  withdrawal_ceiling_ratio: number;
  withdrawal_tax_rate: number;
  taxable_withdrawal_ratio: number;
}

const DEFAULT_ADVANCED: AdvancedFireParams = {
  inflation_mode: "fixed_real",
  fixed_inflation_rate: 0.03,
  inflation_mu: 0.03,
  inflation_phi: 0.5,
  inflation_sigma: 0.01,
  withdrawal_type: "fixed_real",
  withdrawal_rate: 0.04,
  withdrawal_floor_ratio: 0.7,
  withdrawal_ceiling_ratio: 1.3,
  withdrawal_tax_rate: 0,
  taxable_withdrawal_ratio: 0,
};

/** High fixed-inflation threshold that requires an explicit risk acknowledgement. */
const HIGH_FIXED_INFLATION = 0.15;

/** Whether the advanced params still equal the documented defaults. */
function advancedIsDefault(a: AdvancedFireParams): boolean {
  return (Object.keys(DEFAULT_ADVANCED) as (keyof AdvancedFireParams)[]).every(
    (k) => a[k] === DEFAULT_ADVANCED[k],
  );
}

/**
 * Client mirror of the server's validateParameterAdvanced ranges. All
 * fields are checked unconditionally so a value edited in one withdrawal/inflation
 * mode and then hidden by switching modes cannot slip past to a server rejection.
 */
function validateAdvancedParams(a: AdvancedFireParams): string[] {
  const errs: string[] = [];
  const within = (v: number, min: number, max: number) => v >= min && v <= max;
  if (!within(a.fixed_inflation_rate, -0.02, 0.2)) errs.push("固定通胀率需在 -2% 到 20% 之间。");
  if (!within(a.inflation_mu, -0.02, 0.2)) errs.push("通胀均值 μ 需在 -2% 到 20% 之间。");
  if (!within(a.inflation_sigma, 0, 0.2)) errs.push("通胀波动 σ 需在 0% 到 20% 之间。");
  if (!within(a.inflation_phi, 0, 1)) errs.push("通胀自回归 φ 需在 0 到 1 之间。");
  if (!within(a.withdrawal_rate, 0, 1)) errs.push("提取率需在 0% 到 100% 之间。");
  if (!(a.withdrawal_floor_ratio > 0 && a.withdrawal_floor_ratio <= 1))
    errs.push("护栏下限比例需大于 0% 且不超过 100%。");
  if (!within(a.withdrawal_ceiling_ratio, 1, 2)) errs.push("护栏上限比例需在 100% 到 200% 之间。");
  if (a.withdrawal_floor_ratio >= a.withdrawal_ceiling_ratio)
    errs.push("护栏下限比例需小于上限比例。");
  if (!within(a.withdrawal_tax_rate, 0, 1)) errs.push("有效提取税率需在 0% 到 100% 之间。");
  if (!within(a.taxable_withdrawal_ratio, 0, 1)) errs.push("应税提取比例需在 0% 到 100% 之间。");
  if (a.withdrawal_tax_rate * a.taxable_withdrawal_ratio >= 1)
    errs.push("有效提取税率 × 应税提取比例需小于 1。");
  return errs;
}

const WITHDRAWAL_TYPE_LABEL: Record<string, string> = {
  fixed_real: "固定实际支出",
  fixed_portfolio: "组合百分比",
  guardrail: "动态提取（护栏）",
};

function defaultPlanName(): string {
  const today = new Date().toISOString().slice(0, 10);
  return `我的 FIRE 计划 (${today})`;
}

function buildParameters(
  base: {
    totalAssets: number;
    annualSpending: number;
    annualSavings: number;
    scenarioId: string;
    ages: { current: number; retirement: number; end: number };
  },
  advanced: AdvancedFireParams,
): PlanParameters {
  return {
    plan_id: "",
    current_age: base.ages.current,
    retirement_age: base.ages.retirement,
    end_age: base.ages.end,
    total_assets_minor: base.totalAssets,
    annual_savings_minor: base.annualSavings,
    annual_savings_growth_rate: 0,
    annual_spending_minor: base.annualSpending,
    terminal_wealth_floor_minor: 0,
    selected_scenario_id: base.scenarioId,
    inflation_mode: advanced.inflation_mode,
    fixed_inflation_rate: advanced.fixed_inflation_rate,
    inflation_mu: advanced.inflation_mu,
    inflation_phi: advanced.inflation_phi,
    inflation_sigma: advanced.inflation_sigma,
    withdrawal_type: advanced.withdrawal_type,
    withdrawal_rate: advanced.withdrawal_rate,
    withdrawal_floor_ratio: advanced.withdrawal_floor_ratio,
    withdrawal_ceiling_ratio: advanced.withdrawal_ceiling_ratio,
    withdrawal_tax_rate: advanced.withdrawal_tax_rate,
    taxable_withdrawal_ratio: advanced.taxable_withdrawal_ratio,
    rebalance_frequency: "annual",
    rebalance_threshold: 0.03,
    transaction_cost_rate: 0,
    simulation_runs: DEFAULT_RUNS,
    student_t_df: 7,
    return_assumption_mode: "blended_prior",
    assumption_selection_mode: "follow_global",
    return_assumption_set_id: "",
    return_assumption_set_version: 0,
    return_assumption_scenario: "baseline",
    updated_at: Date.now(),
  };
}

export default function NewPlanWizardPage() {
  const router = useRouter();
  const [step, setStep] = useState(0);
  const [name, setName] = useState(defaultPlanName);
  const [valuationDate] = useState(new Date().toISOString().slice(0, 10));
  const [currentAge, setCurrentAge] = useState(35);
  const [retirementAge, setRetirementAge] = useState(35);
  const [fireDurationYears, setFireDurationYears] = useState(30);
  const [totalAssets, setTotalAssets] = useState(4_000_000_00);
  const [annualSpending, setAnnualSpending] = useState(120_000_00);
  const [annualSavings, setAnnualSavings] = useState(100_000_00);
  const [scenarioId, setScenarioId] = useState("");
  const [regionTargets, setRegionTargets] = useState<WizardRegionTargets>(defaultWizardRegionTargets);
  const [advanced, setAdvanced] = useState<AdvancedFireParams>(DEFAULT_ADVANCED);
  const [highInflationConfirmed, setHighInflationConfirmed] = useState(false);
  const [selectedInstruments, setSelectedInstruments] = useState<WizardHoldingSelection[]>([]);
  const [removedByTargets, setRemovedByTargets] = useState<string[]>([]);
  const [runSimulation, setRunSimulation] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [holdingTab, setHoldingTab] = useState<string>("equity");

  const scenariosQ = useQuery({ queryKey: ["scenarios"], queryFn: listScenarios });

  const endAge = retirementAge + fireDurationYears;

  const advancedErrors = useMemo(() => validateAdvancedParams(advanced), [advanced]);
  const needsHighInflationConfirm = advanced.fixed_inflation_rate > HIGH_FIXED_INFLATION;
  const advancedBlocked =
    advancedErrors.length > 0 || (needsHighInflationConfirm && !highInflationConfirmed);

  const updateAdvanced = <K extends keyof AdvancedFireParams>(key: K, value: AdvancedFireParams[K]) =>
    setAdvanced((prev) => ({ ...prev, [key]: value }));

  const finishMut = useMutation({
    mutationFn: async () => {
      const holdings = selectedInstruments.map((s, i) => ({
        instrument_id: s.inst.id,
        enabled: true,
        weight_within_group: s.weight,
        current_amount_minor: s.amount,
        sort_order: i * 10,
      }));
      const plan = await createPlanWizard({
        name,
        valuation_date: valuationDate,
        selected_scenario_id: scenarioId,
        parameters: buildParameters(
          {
            totalAssets,
            annualSpending,
            annualSavings,
            scenarioId,
            ages: { current: currentAge, retirement: retirementAge, end: endAge },
          },
          advanced,
        ),
        holdings,
        region_targets: buildRegionTargetsPayload(regionTargets),
        apply_unallocated_to_cash: assetGap > 100,
      });
      if (!runSimulation) return { plan, sim: null, simulationFailed: false };
      try {
        const sim = await createSimulation(plan.id, { runs: DEFAULT_RUNS });
        return { plan, sim, simulationFailed: false };
      } catch {
        return { plan, sim: null, simulationFailed: true };
      }
    },
    onSuccess: ({ plan, sim, simulationFailed }) => {
      setError(null);
      const suffix = sim
        ? `?job_id=${encodeURIComponent(sim.job_id)}`
        : simulationFailed
          ? "?simulation_error=1"
          : "";
      router.push(`/plans/${plan.id}/overview${suffix}`);
    },
    onError: (e) => {
      if (e instanceof ApiError) {
        setError(e.message);
        return;
      }
      setError(e instanceof Error ? e.message : "创建失败");
    },
  });

  const selectedScenario = useMemo(
    () => scenariosQ.data?.scenarios.find((s) => s.id === scenarioId),
    [scenariosQ.data?.scenarios, scenarioId],
  );

  const allocationGroups = useMemo(() => {
    if (!selectedScenario) return [];
    return getWizardAllocationGroups(selectedScenario.weights, regionTargets);
  }, [selectedScenario, regionTargets]);

  const groupWeightChecks = useMemo(
    () =>
      validateWizardGroupWeights(selectedInstruments, allocationGroups, {
        skipImplicitCash: true,
      }),
    [selectedInstruments, allocationGroups],
  );

  const regionTargetChecks = useMemo(() => {
    if (!selectedScenario) return [];
    const weightByClass = new Map(
      selectedScenario.weights.map((w) => [w.asset_class, w.weight]),
    );
    return (["equity", "bond"] as WizardRegionEditableClass[])
      .filter((ac) => (weightByClass.get(ac) ?? 0) > 0.0001)
      .map((ac) => {
        const rt = regionTargets[ac];
        const check = validatePercentSum([
          { label: "国内", value: rt.domestic },
          { label: "国外", value: rt.foreign },
        ]);
        return { assetClass: ac, label: assetClassLabel(ac), ...check };
      });
  }, [selectedScenario, regionTargets]);

  const holdingsSum = useMemo(
    () => selectedInstruments.reduce((a, s) => a + s.amount, 0),
    [selectedInstruments],
  );
  const assetGap = totalAssets - holdingsSum;

  const activeScenarioClasses = useMemo(() => {
    if (!selectedScenario) return [];
    const weightByClass = new Map(
      selectedScenario.weights.map((w) => [w.asset_class, w.weight]),
    );
    return WIZARD_ASSET_CLASS_ORDER.filter((ac) => (weightByClass.get(ac) ?? 0) > 0.0001).map(
      (ac) => ({
        assetClass: ac,
        classWeight: weightByClass.get(ac) ?? 0,
      }),
    );
  }, [selectedScenario]);

  const instrumentTabs = useMemo(
    () => activeScenarioClasses.filter((c) => c.assetClass !== "cash"),
    [activeScenarioClasses],
  );

  const effectiveHoldingTab = instrumentTabs.some((t) => t.assetClass === holdingTab)
    ? holdingTab
    : (instrumentTabs[0]?.assetClass ?? holdingTab);

  const portfolioReview = useMemo(() => {
    if (!selectedScenario) return null;
    return buildWizardPortfolioReview({
      scenarioWeights: selectedScenario.weights,
      regionTargets,
      selectedInstruments,
      totalAssetsMinor: totalAssets,
      gapToCash: true,
      assetGapMinor: assetGap,
      implicitCash: true,
    });
  }, [selectedScenario, regionTargets, selectedInstruments, totalAssets, assetGap]);

  const holdingsByRegion = useMemo(
    () => summarizeHoldingsByRegion(selectedInstruments),
    [selectedInstruments],
  );

  // Validation that runs when leaving 计划目标: a scenario must be chosen, region
  // splits must sum to 100%, and incompatible already-picked instruments are
  // pruned by scenario/region. Returns false (with an error set) to block.
  const leaveGoalStep = (): boolean => {
    if (!scenarioId) {
      setError("请选择配置模板。");
      return false;
    }
    if (!regionTargetChecks.every((c) => c.passed)) {
      setError("各「大类」国内与国外配比须合计 100%。");
      return false;
    }
    if (advancedErrors.length > 0) {
      setError(`高级 FIRE 参数无效：${advancedErrors[0]}`);
      return false;
    }
    if (needsHighInflationConfirm && !highInflationConfirmed) {
      setError("固定通胀率超过 15%，请在「高级 FIRE 参数」中勾选确认。");
      return false;
    }
    if (selectedScenario) {
      const afterScenario = pruneSelectedByScenario(selectedInstruments, selectedScenario.weights);
      const { selected, removed } = pruneSelectedByRegionTargets(afterScenario, regionTargets);
      const scenarioRemoved = selectedInstruments.filter((s) => !afterScenario.includes(s));
      const removedAll = [...scenarioRemoved, ...removed];
      setRemovedByTargets(
        removedAll.map((s) => `${s.inst.name}（${s.inst.code}）`),
      );
      setSelectedInstruments(selected);
    }
    return true;
  };

  const leaveHoldingsStep = (): boolean => {
    if (selectedInstruments.length === 0) {
      setError("请至少选择一个标的。");
      return false;
    }
    if (!groupWeightChecks.every((g) => g.passed)) {
      setError("各「大类 × 地区」组内权重须合计 100%。");
      return false;
    }
    const unavailable = selectedInstruments.filter(
      (s) => s.inst.quality_status === "insufficient_history",
    );
    if (unavailable.length > 0) {
      setError(
        `以下标的历史不足，不能用于模拟：${unavailable.map((s) => s.inst.code).join("、")}`,
      );
      return false;
    }
    const sum = selectedInstruments.reduce((a, s) => a + s.amount, 0);
    if (sum > totalAssets + 100) {
      setError("持仓合计不能超过基准规模，请调整金额。");
      return false;
    }
    return true;
  };

  return (
    <div className="content-enter mx-auto w-full max-w-[96rem]">
      <PageHeader
        backHref="/"
        backLabel="计划列表"
        title="新建计划向导"
        className="mb-0"
      />
      <Stepper steps={STEPS} current={step} className="mt-4" />

      <div
        data-testid="wizard-step-card"
        className="mt-8 w-full space-y-4 rounded-lg border border-line p-6"
      >
        {step === GOAL_STEP && (
          <div className="max-w-6xl space-y-8">
            <section className="space-y-6">
              <h2 className="text-sm font-semibold text-ink">基本资料</h2>
              <label className="block text-sm">
                计划名称
                <input
                  className="input-base mt-1 max-w-3xl"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </label>
              <div>
                <h3 className="flex items-center text-sm font-medium">
                  FIRE 模拟参数
                  <MetricHelp termKey="fire_params_for_simulation" />
                </h3>
                <div className="mt-2 grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                  <label className="text-sm">
                    当前年龄
                    <input
                      type="number"
                      className="input-base mt-1"
                      value={currentAge}
                      onChange={(e) => setCurrentAge(Number(e.target.value))}
                    />
                  </label>
                  <label className="text-sm">
                    退休年龄
                    <input
                      type="number"
                      className="input-base mt-1"
                      value={retirementAge}
                      onChange={(e) => setRetirementAge(Number(e.target.value))}
                    />
                  </label>
                  <label className="text-sm">
                    预计 FIRE 时长
                    <div className="mt-1 space-y-1">
                      <input
                        type="number"
                        min={1}
                        className="input-base"
                        value={fireDurationYears}
                        onChange={(e) => setFireDurationYears(Number(e.target.value))}
                        aria-label="预计 FIRE 时长（年）"
                      />
                      <select
                        className="input-base text-xs text-ink-muted"
                        aria-label="常用 FIRE 时长预设"
                        value={
                          FIRE_DURATION_PRESETS.includes(
                            fireDurationYears as (typeof FIRE_DURATION_PRESETS)[number],
                          )
                            ? String(fireDurationYears)
                            : ""
                        }
                        onChange={(e) => {
                          if (e.target.value) setFireDurationYears(Number(e.target.value));
                        }}
                      >
                        <option value="">选择常用时长</option>
                        {FIRE_DURATION_PRESETS.map((years) => (
                          <option key={years} value={years}>
                            {years} 年
                          </option>
                        ))}
                      </select>
                    </div>
                  </label>
                </div>
              </div>
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                <MoneyInput
                  label={
                    <span className="flex items-center">
                      基准规模
                      <MetricHelp termKey="configured_total_assets" />
                    </span>
                  }
                  valueMinor={totalAssets}
                  onChange={setTotalAssets}
                  plain
                />
                <MoneyInput label="当前年支出" valueMinor={annualSpending} onChange={setAnnualSpending} plain />
                <label className="block text-sm">
                  <span className="mb-1 flex items-center gap-1">
                    年储蓄
                    <MetricHelp termKey="annual_savings_wizard" />
                  </span>
                  <MoneyInput valueMinor={annualSavings} onChange={setAnnualSavings} plain />
                </label>
              </div>
            </section>

            <section className="space-y-4">
              <h2 className="text-sm font-semibold text-ink">目标配置</h2>
              <label className="block max-w-3xl text-sm">
                配置模板
                <select
                  className="input-base mt-1"
                  value={scenarioId}
                  onChange={(e) => setScenarioId(e.target.value)}
                >
                  <option value="">请选择</option>
                  {scenariosQ.data?.scenarios.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name} —{" "}
                      {s.weights.map((w) => `${assetClassLabel(w.asset_class)} ${formatPercent(w.weight)}`).join(" / ")}
                    </option>
                  ))}
                </select>
              </label>
              <p className="max-w-3xl text-sm text-ink-muted">
                权益与债券的国内/国外比例在此设定，将写入计划目标；创建后仍可在「计划设置」中修改。
              </p>
              {selectedScenario && regionTargetChecks.length > 0 && (
                <div className="space-y-3">
                  <h3 className="text-sm font-medium">地区组内权重</h3>
                  <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                    {regionTargetChecks.map((check) => {
                      const ac = check.assetClass as WizardRegionEditableClass;
                      const rt = regionTargets[ac];
                      return (
                        <div key={ac} className="space-y-2 rounded-md border border-line p-3">
                          <p className="text-sm font-medium">{check.label}</p>
                          <div className="flex flex-wrap items-end gap-4">
                            <PercentInput
                              label="国内"
                              value={rt.domestic}
                              onChange={(v) =>
                                setRegionTargets((prev) => ({
                                  ...prev,
                                  [ac]: { domestic: v, foreign: complementRegionWeight(v) },
                                }))
                              }
                            />
                            <PercentInput
                              label="国外"
                              value={rt.foreign}
                              onChange={(v) =>
                                setRegionTargets((prev) => ({
                                  ...prev,
                                  [ac]: { domestic: complementRegionWeight(v), foreign: v },
                                }))
                              }
                            />
                          </div>
                          <p
                            className={`text-xs ${check.passed ? "text-positive" : "text-danger"}`}
                          >
                            {check.label} 地区配比：{check.message}
                          </p>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </section>

            <AdvancedFireParamsSection
              advanced={advanced}
              onChange={updateAdvanced}
              errors={advancedErrors}
              isDefault={advancedIsDefault(advanced)}
              highInflationConfirmed={highInflationConfirmed}
              onHighInflationConfirmChange={setHighInflationConfirmed}
            />
          </div>
        )}

        {step === HOLDINGS_STEP && (
          <div className="space-y-4">
            {removedByTargets.length > 0 && (
              <div
                className="flex items-start justify-between gap-3 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm text-warning"
                role="status"
                data-testid="wizard-removed-by-targets"
              >
                <span>因地区目标调整，已移除：{removedByTargets.join("、")}</span>
                <Button
                  variant="ghost"
                  className="shrink-0 px-2 py-1 text-xs underline"
                  aria-label="关闭已移除提示"
                  onClick={() => setRemovedByTargets([])}
                >
                  知道了
                </Button>
              </div>
            )}
            <div className="grid gap-4 md:grid-cols-[2fr_1fr]">
              <div className="space-y-2">
                <p className="text-sm text-ink-muted">
                  按大类分标签页搜索并添加标的；组内占比将自动均分，手动调整后其余标的自动补齐。未配置资金默认计入
                  现金/其他。预期资金 = 基准规模 × 大类权重 × 地区权重 × 组内占比。
                </p>
                <Link href="/assets/import" className="inline-block text-sm underline">
                  需要新标的？从 AKShare 录入
                </Link>
                {groupWeightChecks.map((g) => (
                  <p
                    key={g.label}
                    className={`text-sm ${g.passed ? "text-positive" : "text-danger"}`}
                  >
                    {g.label} 组内权重：{g.message}
                  </p>
                ))}
              </div>
              <div className="space-y-1 rounded-md border border-line p-3 text-sm md:text-right">
                <p className="text-ink-muted">持仓合计</p>
                <p className="font-medium text-ink">
                  {(holdingsSum / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元
                </p>
                <p className="text-xs text-ink-muted">
                  基准规模 {(totalAssets / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元
                </p>
                {assetGap > 100 && (
                  <p className="text-xs text-ink-muted">
                    未配置 {(assetGap / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元将自动计入现金/其他
                  </p>
                )}
              </div>
            </div>
            {instrumentTabs.length > 0 && (
              <>
                <div
                  className="mt-4 flex gap-1 border-b border-line"
                  role="tablist"
                  aria-label="资产大类"
                >
                  {instrumentTabs.map(({ assetClass, classWeight }) => (
                    <button
                      key={assetClass}
                      type="button"
                      role="tab"
                      id={`holding-tab-${assetClass}`}
                      aria-controls={`holding-panel-${assetClass}`}
                      aria-selected={effectiveHoldingTab === assetClass}
                      className={`px-4 py-2 text-sm font-medium ${
                        effectiveHoldingTab === assetClass
                          ? "border-b-2 border-brand text-ink"
                          : "text-ink-muted hover:text-ink"
                      }`}
                      onClick={() => setHoldingTab(assetClass)}
                    >
                      {assetClassLabel(assetClass)}（{formatPercent(classWeight)}）
                    </button>
                  ))}
                </div>
                {instrumentTabs
                  .filter((t) => t.assetClass === effectiveHoldingTab)
                  .map(({ assetClass, classWeight }) => {
                    const classSelected = selectedInstruments.filter(
                      (s) => s.inst.asset_class === assetClass,
                    );
                    const mergeSelected = (next: WizardHoldingSelection[]) => {
                      const other = selectedInstruments.filter(
                        (s) => s.inst.asset_class !== assetClass,
                      );
                      setRemovedByTargets([]);
                      setSelectedInstruments([...other, ...next]);
                    };

                    const rt =
                      regionTargets[assetClass as WizardRegionEditableClass] ??
                      defaultWizardRegionTargets()[assetClass as WizardRegionEditableClass];
                    const domesticEnabled = rt.domestic > 0.0001;
                    const foreignEnabled = rt.foreign > 0.0001;
                    const splitBoth = domesticEnabled && foreignEnabled;

                    return (
                      <section
                        key={assetClass}
                        className="mt-4 rounded-lg border border-line p-4"
                        role="tabpanel"
                        id={`holding-panel-${assetClass}`}
                        aria-labelledby={`holding-tab-${assetClass}`}
                      >
                        {!splitBoth ? (
                          <AssetClassHoldingPicker
                            assetClass={assetClass}
                            classWeight={classWeight}
                            regionWeight={foreignEnabled ? rt.foreign : rt.domestic}
                            region={foreignEnabled ? "foreign" : "domestic"}
                            totalAssetsMinor={totalAssets}
                            selected={classSelected}
                            onSelectedChange={mergeSelected}
                          />
                        ) : (
                          <>
                            <p className="mb-3 text-sm text-ink-muted">
                              国内 {formatPercent(rt.domestic)} / 国外 {formatPercent(rt.foreign)}
                            </p>
                            <AssetClassHoldingPicker
                              assetClass={assetClass}
                              classWeight={classWeight}
                              regionWeight={rt.domestic}
                              region="domestic"
                              totalAssetsMinor={totalAssets}
                              selected={classSelected.filter((s) => s.inst.region === "domestic")}
                              onSelectedChange={(domesticNext) => {
                                const foreign = classSelected.filter(
                                  (s) => s.inst.region === "foreign",
                                );
                                mergeSelected([...domesticNext, ...foreign]);
                              }}
                              subTitle={`国内（占${assetClassLabel(assetClass)} ${formatPercent(rt.domestic)}）`}
                              nested
                            />
                            <AssetClassHoldingPicker
                              assetClass={assetClass}
                              classWeight={classWeight}
                              regionWeight={rt.foreign}
                              region="foreign"
                              totalAssetsMinor={totalAssets}
                              selected={classSelected.filter((s) => s.inst.region === "foreign")}
                              onSelectedChange={(foreignNext) => {
                                const domestic = classSelected.filter(
                                  (s) => s.inst.region === "domestic",
                                );
                                mergeSelected([...domestic, ...foreignNext]);
                              }}
                              subTitle={`国外（占${assetClassLabel(assetClass)} ${formatPercent(rt.foreign)}）`}
                              nested
                            />
                          </>
                        )}
                      </section>
                    );
                  })}
              </>
            )}
          </div>
        )}

        {step === CONFIRM_STEP && (
          <>
            <ul className="list-disc pl-5 text-sm text-ink">
              <li>组内权重：{groupWeightChecks.every((g) => g.passed) ? "通过" : "未通过"}</li>
              <li>全组合目标权重：{portfolioReview?.passed ? "通过" : "未通过"}</li>
              <li>已选标的：{selectedInstruments.length} 个</li>
            </ul>

            {selectedScenario && (
              <>
                <p className="mt-3 text-sm text-ink-muted">
                  配置模板「{selectedScenario.name}」目标：
                  {selectedScenario.weights
                    .map((w) => `${assetClassLabel(w.asset_class)} ${formatPercent(w.weight)}`)
                    .join(" / ")}
                </p>
                <p className="text-sm text-ink-muted">
                  地区目标：{formatRegionTargetsSummary(selectedScenario.weights, regionTargets)}
                </p>
                <p className="text-sm text-ink-muted" data-testid="wizard-advanced-summary">
                  高级参数：
                  {advancedIsDefault(advanced) ? "使用默认值 · " : "已自定义 · "}
                  通胀{" "}
                  {advanced.inflation_mode === "random_ar1"
                    ? `随机（μ ${formatPercent(advanced.inflation_mu)}）`
                    : `固定 ${formatPercent(advanced.fixed_inflation_rate)}`}
                  {" · "}
                  提取 {WITHDRAWAL_TYPE_LABEL[advanced.withdrawal_type] ?? advanced.withdrawal_type}
                  {advanced.withdrawal_type !== "fixed_real"
                    ? `（${formatPercent(advanced.withdrawal_rate)}）`
                    : ""}
                </p>
                {selectedInstruments.length > 0 && (
                  <p className="text-sm text-ink-muted">
                    已选持仓：国内 {formatMoney(holdingsByRegion.domesticMinor)}（
                    {formatPercent(holdingsByRegion.domesticPct)}）· 国外{" "}
                    {formatMoney(holdingsByRegion.foreignMinor)}（
                    {formatPercent(holdingsByRegion.foreignPct)}）
                  </p>
                )}
              </>
            )}

            {portfolioReview && (
              <div className="mt-4 space-y-3">
                <p
                  className={`text-sm ${portfolioReview.passed ? "text-positive" : "text-warning"}`}
                  role="status"
                >
                  {portfolioReview.message}
                </p>
                <div className="space-y-3 md:hidden" data-testid="wizard-review-cards">
                  {WIZARD_ASSET_CLASS_ORDER.filter((ac) =>
                    portfolioReview.rows.some((row) => row.assetClass === ac),
                  ).map((ac) => {
                    const rows = portfolioReview.rows.filter((row) => row.assetClass === ac);
                    return (
                      <div key={ac} className="rounded-lg border border-line p-3">
                        <p className="text-sm font-medium text-ink">{assetClassLabel(ac)}</p>
                        <ul className="mt-2 space-y-2">
                          {rows.map((row) => (
                            <li
                              key={row.key}
                              className="grid grid-cols-2 gap-x-3 gap-y-0.5 text-sm"
                            >
                              <span className="col-span-2 truncate">
                                {row.instrumentName}
                                <span className="ml-1 text-xs text-ink-muted">
                                  {row.instrumentCode} · {row.regionLabel}
                                </span>
                              </span>
                              <span className="text-xs text-ink-muted">
                                目标 {formatPercent(row.portfolioTargetWeight)}
                              </span>
                              <span className="text-right text-xs text-ink-muted">
                                已投入 {formatMoney(row.currentAmountMinor)}
                              </span>
                            </li>
                          ))}
                        </ul>
                      </div>
                    );
                  })}
                  <p className="text-right text-sm font-medium">
                    全组合目标合计 {formatPercent(portfolioReview.portfolioSum)}
                  </p>
                </div>
                <div className="hidden overflow-x-auto rounded-lg border border-line md:block">
                  <table className="min-w-full text-sm">
                    <caption className="sr-only">组合确认明细</caption>
                    <thead className="bg-surface-muted text-left">
                      <tr>
                        <th scope="col" className="px-3 py-2 font-medium">方向</th>
                        <th scope="col" className="px-3 py-2 font-medium">资产名称</th>
                        <th scope="col" className="px-3 py-2 font-medium">编号</th>
                        <th scope="col" className="px-3 py-2 font-medium text-right">组内占比</th>
                        <th scope="col" className="px-3 py-2 font-medium text-right">全组合目标</th>
                        <th scope="col" className="px-3 py-2 font-medium">国别</th>
                        <th scope="col" className="px-3 py-2 font-medium text-right">已投入</th>
                        <th scope="col" className="px-3 py-2 font-medium text-right">待投入/减配</th>
                      </tr>
                    </thead>
                    <tbody>
                      {portfolioReview.rows.map((row) => (
                        <tr key={row.key} className="border-t">
                          <td className="px-3 py-2">{row.assetClassLabel}</td>
                          <td className="px-3 py-2">{row.instrumentName}</td>
                          <td className="px-3 py-2 font-mono text-xs">{row.instrumentCode}</td>
                          <td className="px-3 py-2 text-right">{formatPercent(row.groupWeight)}</td>
                          <td className="px-3 py-2 text-right">
                            {formatPercent(row.portfolioTargetWeight)}
                          </td>
                          <td className="px-3 py-2">{row.regionLabel}</td>
                          <td className="px-3 py-2 text-right">
                            {formatMoney(row.currentAmountMinor)}
                          </td>
                          <td className="px-3 py-2 text-right">
                            {formatPendingAmount(row.pendingAmountMinor)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                    <tfoot className="border-t bg-surface-muted">
                      <tr>
                        <td className="px-3 py-2 font-medium" colSpan={4}>
                          合计
                        </td>
                        <td className="px-3 py-2 text-right font-medium">
                          {formatPercent(portfolioReview.portfolioSum)}
                        </td>
                        <td className="px-3 py-2" colSpan={3} />
                      </tr>
                    </tfoot>
                  </table>
                </div>
                {!portfolioReview.passed && portfolioReview.missingClasses.length > 0 && (
                  <p className="text-sm text-warning">
                    建议：返回「建立持仓」补充
                    {portfolioReview.missingClasses.map((m) => m.label).join("、")}
                    类资产；若暂时无法配置，可先调整配置模板或稍后在计划内完善持仓。
                  </p>
                )}
              </div>
            )}

            {assetGap > 100 && (
              <p className="mt-4 text-sm text-ink-muted">
                未配置差额{" "}
                {(assetGap / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元将自动计入
                「现金/其他」。
              </p>
            )}
            {selectedInstruments
              .filter(
                (s) => s.inst.simulation_eligible && s.inst.history_depth === "one_year",
              )
              .map((s) => (
                <p
                  key={s.inst.id}
                  className="mt-2 text-sm text-warning"
                  data-testid="wizard-short-history"
                >
                  {s.inst.name}（{s.inst.code}）历史样本有限，模拟长期估计不确定性较高。
                </p>
              ))}
            {assetGap < -100 && (
              <p className="text-sm text-danger">持仓合计超过基准规模，请返回上一步调整。</p>
            )}
            <label className="mt-4 flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={runSimulation}
                onChange={(event) => setRunSimulation(event.target.checked)}
              />
              创建后运行 FIRE 模拟（{DEFAULT_RUNS.toLocaleString()} 次）
              <MetricHelp termKey="fire_simulation_optional" />
            </label>
          </>
        )}
      </div>

      {error && (
        <p className="mt-4 text-sm text-danger" role="alert">
          {error}
        </p>
      )}

      <div className="mt-6 flex w-full justify-between">
        <Button
          variant="secondary"
          size="lg"
          disabled={step === GOAL_STEP}
          onClick={() => {
            setError(null);
            setStep((s) => s - 1);
          }}
        >
          上一步
        </Button>
        {step < CONFIRM_STEP ? (
          <Button
            size="lg"
            onClick={() => {
              setError(null);
              if (step === GOAL_STEP && !leaveGoalStep()) return;
              if (step === HOLDINGS_STEP && !leaveHoldingsStep()) return;
              setStep((s) => s + 1);
            }}
          >
            下一步
          </Button>
        ) : (
          <Button
            size="lg"
            pending={finishMut.isPending}
            disabled={
              !groupWeightChecks.every((g) => g.passed) ||
              !portfolioReview?.passed ||
              selectedInstruments.length === 0 ||
              !scenarioId ||
              assetGap < -100 ||
              advancedBlocked
            }
            title={
              portfolioReview && !portfolioReview.passed
                ? portfolioReview.message
                : undefined
            }
            onClick={() => finishMut.mutate()}
          >
            {runSimulation ? "创建并运行模拟" : "创建计划"}
          </Button>
        )}
      </div>
    </div>
  );
}

/**
 * Collapsible advanced FIRE parameters. Defaults are shown closed; opening and
 * editing reuses the plan parameters page's controls (PercentInput, the same
 * inflation/withdrawal field semantics and ranges) so the wizard and parameters
 * page stay consistent. Guardrail withdrawal exposes its floor/ceiling with an
 * explanation of the existing dynamic-withdrawal model.
 */
function AdvancedFireParamsSection({
  advanced,
  onChange,
  errors,
  isDefault,
  highInflationConfirmed,
  onHighInflationConfirmChange,
}: {
  advanced: AdvancedFireParams;
  onChange: <K extends keyof AdvancedFireParams>(key: K, value: AdvancedFireParams[K]) => void;
  errors: string[];
  isDefault: boolean;
  highInflationConfirmed: boolean;
  onHighInflationConfirmChange: (value: boolean) => void;
}) {
  const hasIssues = errors.length > 0;
  return (
    <details
      className="rounded-md border border-line p-3"
      data-testid="wizard-advanced-params"
      open={hasIssues || undefined}
    >
      <summary className="cursor-pointer text-sm font-medium">
        高级 FIRE 参数（{isDefault ? "使用默认值" : "已自定义"}）
        {hasIssues && (
          <span className="ml-2 rounded-full bg-danger/10 px-2 py-0.5 text-xs text-danger">
            {errors.length} 项待修正
          </span>
        )}
      </summary>
      <div className="mt-3 grid gap-4 sm:grid-cols-2">
        <label className="block text-sm">
          通胀模式
          <select
            className="input-base mt-1"
            value={advanced.inflation_mode}
            onChange={(e) => onChange("inflation_mode", e.target.value)}
          >
            <option value="fixed_real">固定通胀率</option>
            <option value="random_ar1">随机通胀</option>
          </select>
        </label>
        <PercentInput
          label="固定通胀率"
          value={advanced.fixed_inflation_rate}
          onChange={(v) => onChange("fixed_inflation_rate", v)}
        />
        {advanced.fixed_inflation_rate > HIGH_FIXED_INFLATION && (
          <label className="flex items-center gap-2 text-sm sm:col-span-2">
            <input
              type="checkbox"
              checked={highInflationConfirmed}
              onChange={(e) => onHighInflationConfirmChange(e.target.checked)}
            />
            确认固定通胀率超过 15%（非常规假设）
          </label>
        )}
        {advanced.inflation_mode === "random_ar1" && (
          <>
            <PercentInput
              label="通胀均值 μ"
              value={advanced.inflation_mu}
              onChange={(v) => onChange("inflation_mu", v)}
            />
            <PercentInput
              label="通胀波动 σ"
              value={advanced.inflation_sigma}
              onChange={(v) => onChange("inflation_sigma", v)}
            />
            <label className="block text-sm">
              通胀自回归 φ
              <input
                type="number"
                step={0.01}
                min={0}
                max={1}
                className="input-base mt-1"
                value={advanced.inflation_phi}
                onChange={(e) => onChange("inflation_phi", Number(e.target.value))}
              />
            </label>
          </>
        )}
        <label className="block text-sm">
          提取策略
          <select
            className="input-base mt-1"
            value={advanced.withdrawal_type}
            onChange={(e) => onChange("withdrawal_type", e.target.value)}
          >
            <option value="fixed_real">固定实际支出</option>
            <option value="fixed_portfolio">组合百分比</option>
            <option value="guardrail">动态提取（护栏）</option>
          </select>
        </label>
        {(advanced.withdrawal_type === "fixed_portfolio" ||
          advanced.withdrawal_type === "guardrail") && (
          <PercentInput
            label="提取率"
            value={advanced.withdrawal_rate}
            onChange={(v) => onChange("withdrawal_rate", v)}
          />
        )}
        {advanced.withdrawal_type === "guardrail" && (
          <>
            <PercentInput
              label="护栏下限比例"
              value={advanced.withdrawal_floor_ratio}
              onChange={(v) => onChange("withdrawal_floor_ratio", v)}
            />
            <PercentInput
              label="护栏上限比例"
              value={advanced.withdrawal_ceiling_ratio}
              onChange={(v) => onChange("withdrawal_ceiling_ratio", v)}
            />
            <p className="text-xs text-ink-muted sm:col-span-2">
              系统在退休周年按当前提取率相对初始提取率调整年度支出（过高下调、过低上调），再受上下限约束。
            </p>
          </>
        )}
        <PercentInput
          label="有效提取税率"
          value={advanced.withdrawal_tax_rate}
          onChange={(v) => onChange("withdrawal_tax_rate", v)}
        />
        <PercentInput
          label="应税提取比例"
          value={advanced.taxable_withdrawal_ratio}
          onChange={(v) => onChange("taxable_withdrawal_ratio", v)}
        />
        {errors.length > 0 && (
          <ul
            className="space-y-1 text-xs text-danger sm:col-span-2"
            data-testid="wizard-advanced-errors"
          >
            {errors.map((msg) => (
              <li key={msg}>{msg}</li>
            ))}
          </ul>
        )}
      </div>
    </details>
  );
}
