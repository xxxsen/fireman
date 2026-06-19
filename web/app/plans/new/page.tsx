"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { AssetClassHoldingPicker } from "@/components/plans/AssetClassHoldingPicker";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
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

const STEPS = ["计划基础", "目标配置", "建立持仓", "确认组合"] as const;
const DEFAULT_RUNS = 10000;
const FIRE_DURATION_PRESETS = [30, 40, 50] as const;

function defaultPlanName(): string {
  const today = new Date().toISOString().slice(0, 10);
  return `我的 FIRE 计划 (${today})`;
}

function defaultParameters(
  totalAssets: number,
  annualSpending: number,
  annualSavings: number,
  scenarioId: string,
  ages: { current: number; retirement: number; end: number },
): PlanParameters {
  return {
    plan_id: "",
    current_age: ages.current,
    retirement_age: ages.retirement,
    end_age: ages.end,
    total_assets_minor: totalAssets,
    annual_savings_minor: annualSavings,
    annual_savings_growth_rate: 0,
    annual_spending_minor: annualSpending,
    terminal_wealth_floor_minor: 0,
    selected_scenario_id: scenarioId,
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
    rebalance_frequency: "annual",
    rebalance_threshold: 0.03,
    transaction_cost_rate: 0,
    simulation_runs: DEFAULT_RUNS,
    student_t_df: 7,
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
  const [selectedInstruments, setSelectedInstruments] = useState<WizardHoldingSelection[]>([]);
  const [runSimulation, setRunSimulation] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [holdingTab, setHoldingTab] = useState<string>("equity");

  const scenariosQ = useQuery({ queryKey: ["scenarios"], queryFn: listScenarios });

  const endAge = retirementAge + fireDurationYears;

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
        parameters: defaultParameters(totalAssets, annualSpending, annualSavings, scenarioId, {
          current: currentAge,
          retirement: retirementAge,
          end: endAge,
        }),
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

  return (
    <div className="mx-auto w-full max-w-[96rem]">
      <Link href="/" className="text-sm underline">
        ← 计划列表
      </Link>
      <h1 className="mt-4 text-2xl font-semibold">新建计划向导</h1>
      <ol className="mt-4 flex gap-2 text-sm">
        {STEPS.map((label, i) => (
          <li
            key={label}
            className={`rounded-full px-3 py-1 ${
              i === step ? "bg-brand text-white" : "bg-surface-muted text-ink-muted"
            }`}
          >
            {i + 1}. {label}
          </li>
        ))}
      </ol>

      <div
        data-testid="wizard-step-card"
        className="mt-8 w-full space-y-4 rounded-lg border p-6"
      >
        {step === 0 && (
          <div className="max-w-6xl space-y-6">
            <label className="block text-sm">
              计划名称
              <input
                className="mt-1 w-full min-w-0 max-w-3xl rounded-md border px-3 py-2"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <div>
              <h2 className="flex items-center text-sm font-medium">
                FIRE 模拟参数
                <MetricHelp termKey="fire_params_for_simulation" />
              </h2>
              <div className="mt-2 grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
                <label className="text-sm">
                  当前年龄
                  <input
                    type="number"
                    className="mt-1 w-full min-w-0 rounded-md border px-3 py-2"
                    value={currentAge}
                    onChange={(e) => setCurrentAge(Number(e.target.value))}
                  />
                </label>
                <label className="text-sm">
                  退休年龄
                  <input
                    type="number"
                    className="mt-1 w-full min-w-0 rounded-md border px-3 py-2"
                    value={retirementAge}
                    onChange={(e) => setRetirementAge(Number(e.target.value))}
                  />
                </label>
                <label className="text-sm">
                  预计 FIRE 时长
                  <div className="mt-1 grid grid-cols-2 gap-2">
                    <select
                      className="w-full min-w-0 rounded-md border px-3 py-2"
                      value={
                        FIRE_DURATION_PRESETS.includes(fireDurationYears as (typeof FIRE_DURATION_PRESETS)[number])
                          ? String(fireDurationYears)
                          : "custom"
                      }
                      onChange={(e) => {
                        const value = e.target.value;
                        if (value !== "custom") setFireDurationYears(Number(value));
                      }}
                    >
                      {FIRE_DURATION_PRESETS.map((years) => (
                        <option key={years} value={years}>
                          {years} 年
                        </option>
                      ))}
                      <option value="custom">其他年限</option>
                    </select>
                    <input
                      type="number"
                      min={1}
                      className="w-full min-w-0 rounded-md border px-3 py-2"
                      value={fireDurationYears}
                      onChange={(e) => setFireDurationYears(Number(e.target.value))}
                      aria-label="预计 FIRE 时长（年）"
                    />
                  </div>
                </label>
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
              <MoneyInput label="当前总资产" valueMinor={totalAssets} onChange={setTotalAssets} plain />
              <MoneyInput label="当前年支出" valueMinor={annualSpending} onChange={setAnnualSpending} plain />
              <label className="block text-sm">
                <span className="mb-1 flex items-center gap-1">
                  年储蓄
                  <MetricHelp termKey="annual_savings_wizard" />
                </span>
                <MoneyInput valueMinor={annualSavings} onChange={setAnnualSavings} plain />
              </label>
            </div>
          </div>
        )}

        {step === 1 && (
          <div className="max-w-6xl space-y-4">
            <label className="block max-w-3xl text-sm">
              选择场景
              <select
                className="mt-1 w-full min-w-0 rounded-md border px-3 py-2"
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
              权益与债券的国内/国外比例在此设定，将写入计划目标；创建后仍可在「参数」页修改。
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
          </div>
        )}

        {step === 2 && (
          <div className="space-y-4">
            <div className="grid gap-4 md:grid-cols-[2fr_1fr]">
              <div className="space-y-2">
                <p className="text-sm text-ink-muted">
                  按大类分标签页搜索并添加标的；组内占比将自动均分，手动调整后其余标的自动补齐。未配置资金默认计入
                  现金/其他。预期资金 = 总资产 × 大类权重 × 地区权重 × 组内占比。
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
                  总资产 {(totalAssets / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元
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
                      setSelectedInstruments([...other, ...next]);
                    };

                    const rt =
                      regionTargets[assetClass as WizardRegionEditableClass] ??
                      defaultWizardRegionTargets()[assetClass as WizardRegionEditableClass];
                    const splitForeign = rt.foreign > 0.0001;

                    return (
                      <section
                        key={assetClass}
                        className="mt-4 rounded-lg border border-line p-4"
                        role="tabpanel"
                        aria-label={`${assetClassLabel(assetClass)}选标`}
                      >
                        {!splitForeign ? (
                          <AssetClassHoldingPicker
                            assetClass={assetClass}
                            classWeight={classWeight}
                            regionWeight={rt.domestic}
                            region="domestic"
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

        {step === 3 && (
          <>
            <ul className="list-disc pl-5 text-sm text-ink">
              <li>组内权重：{groupWeightChecks.every((g) => g.passed) ? "通过" : "未通过"}</li>
              <li>全组合目标权重：{portfolioReview?.passed ? "通过" : "未通过"}</li>
              <li>已选标的：{selectedInstruments.length} 个</li>
            </ul>

            {selectedScenario && (
              <>
                <p className="mt-3 text-sm text-ink-muted">
                  场景「{selectedScenario.name}」目标：
                  {selectedScenario.weights
                    .map((w) => `${assetClassLabel(w.asset_class)} ${formatPercent(w.weight)}`)
                    .join(" / ")}
                </p>
                <p className="text-sm text-ink-muted">
                  地区目标：{formatRegionTargetsSummary(selectedScenario.weights, regionTargets)}
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
                <div className="overflow-x-auto rounded-lg border">
                  <table className="min-w-full text-sm">
                    <thead className="bg-surface-muted text-left">
                      <tr>
                        <th className="px-3 py-2 font-medium">方向</th>
                        <th className="px-3 py-2 font-medium">资产名称</th>
                        <th className="px-3 py-2 font-medium">编号</th>
                        <th className="px-3 py-2 font-medium text-right">组内占比</th>
                        <th className="px-3 py-2 font-medium text-right">全组合目标</th>
                        <th className="px-3 py-2 font-medium">国别</th>
                        <th className="px-3 py-2 font-medium text-right">已投入</th>
                        <th className="px-3 py-2 font-medium text-right">待投入/减配</th>
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
                    建议：返回「选择标的」补充
                    {portfolioReview.missingClasses.map((m) => m.label).join("、")}
                    类资产；若暂时无法配置，可先调整场景或稍后在计划内完善持仓。
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
              <p className="text-sm text-danger">持仓合计超过总资产，请返回上一步调整。</p>
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
            {error && <p className="text-sm text-danger">{error}</p>}
          </>
        )}
      </div>

      {error && step < 3 && (
        <p className="mt-4 text-sm text-danger" role="alert">
          {error}
        </p>
      )}

      <div className="mt-6 flex w-full justify-between">
        <button
          type="button"
          className="rounded-md border px-4 py-2 text-sm"
          disabled={step === 0}
          onClick={() => setStep((s) => s - 1)}
        >
          上一步
        </button>
        {step < 3 ? (
          <button
            type="button"
            className="rounded-md bg-brand px-4 py-2 text-sm text-white"
            onClick={() => {
              setError(null);
              if (step === 1 && !scenarioId) {
                setError("请选择资产配置场景。");
                return;
              }
              if (step === 1) {
                if (!regionTargetChecks.every((c) => c.passed)) {
                  setError("各「大类」国内与国外配比须合计 100%。");
                  return;
                }
                if (selectedScenario) {
                  setSelectedInstruments((prev) =>
                    pruneSelectedByRegionTargets(
                      pruneSelectedByScenario(prev, selectedScenario.weights),
                      regionTargets,
                    ),
                  );
                }
              }
              if (step === 2) {
                if (selectedInstruments.length === 0) {
                  setError("请至少选择一个标的。");
                  return;
                }
                if (!groupWeightChecks.every((g) => g.passed)) {
                  setError("各「大类 × 地区」组内权重须合计 100%。");
                  return;
                }
                const unavailable = selectedInstruments.filter(
                  (s) => s.inst.quality_status === "insufficient_history",
                );
                if (unavailable.length > 0) {
                  setError(
                    `以下标的历史不足，不能用于模拟：${unavailable.map((s) => s.inst.code).join("、")}`,
                  );
                  return;
                }
                const sum = selectedInstruments.reduce((a, s) => a + s.amount, 0);
                if (sum > totalAssets + 100) {
                  setError("持仓合计不能超过总资产，请调整金额。");
                  return;
                }
              }
              setStep((s) => s + 1);
            }}
          >
            下一步
          </button>
        ) : (
          <button
            type="button"
            className="rounded-md bg-brand px-4 py-2 text-sm text-white disabled:opacity-50"
            disabled={
              !groupWeightChecks.every((g) => g.passed) ||
              !portfolioReview?.passed ||
              selectedInstruments.length === 0 ||
              !scenarioId ||
              assetGap < -100 ||
              finishMut.isPending
            }
            title={
              portfolioReview && !portfolioReview.passed
                ? portfolioReview.message
                : undefined
            }
            onClick={() => finishMut.mutate()}
          >
            {runSimulation ? "创建并运行模拟" : "创建计划"}
          </button>
        )}
      </div>
    </div>
  );
}
