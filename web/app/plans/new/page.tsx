"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMutation, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { createPlanWizard } from "@/lib/api/plans";
import { listScenarios } from "@/lib/api/allocation";
import { listInstruments } from "@/lib/api/instruments";
import { createSimulation } from "@/lib/api/simulations";
import { assetClassLabel, formatMoney, formatPercent, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import {
  buildWizardPortfolioReview,
  formatPendingAmount,
} from "@/lib/wizard-allocation";
import type { Instrument, PlanParameters } from "@/types/api";
import { ApiError } from "@/lib/api/client";
import { useJobStatus } from "@/hooks/useJobStatus";

const STEPS = ["FIRE 基础信息", "目标配置", "选择标的", "检查并模拟"] as const;
const DEFAULT_RUNS = 10000;

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
  const [name, setName] = useState("我的 FIRE 计划");
  const [valuationDate] = useState(new Date().toISOString().slice(0, 10));
  const [currentAge, setCurrentAge] = useState(30);
  const [retirementAge, setRetirementAge] = useState(55);
  const [endAge, setEndAge] = useState(90);
  const [totalAssets, setTotalAssets] = useState(1_000_000_00);
  const [annualSpending, setAnnualSpending] = useState(400_000_00);
  const [annualSavings, setAnnualSavings] = useState(200_000_00);
  const [scenarioId, setScenarioId] = useState("");
  const [selectedInstruments, setSelectedInstruments] = useState<
    { inst: Instrument; weight: number; amount: number }[]
  >([]);
  const [jobId, setJobId] = useState<string | null>(null);
  const [createdPlanId, setCreatedPlanId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [gapToCash, setGapToCash] = useState(false);

  const scenariosQ = useQuery({ queryKey: ["scenarios"], queryFn: listScenarios });
  const instrumentsQ = useQuery({ queryKey: ["instruments"], queryFn: listInstruments });

  const [simFailed, setSimFailed] = useState(false);

  const jobState = useJobStatus(jobId, {
    onComplete: () => {
      if (createdPlanId) router.push(`/plans/${createdPlanId}/dashboard`);
    },
    onFailed: (msg) => {
      setJobId(null);
      setSimFailed(true);
      setError(msg);
    },
    onCanceled: () => {
      setJobId(null);
      setSimFailed(true);
      setError("模拟已取消");
    },
  });

  const retrySimMut = useMutation({
    mutationFn: async () => {
      if (!createdPlanId) {
        throw new Error("计划尚未创建");
      }
      return createSimulation(createdPlanId, { runs: DEFAULT_RUNS });
    },
    onSuccess: (sim) => {
      setSimFailed(false);
      setJobId(sim.job_id);
      setError(null);
    },
    onError: (e) => {
      setError(e instanceof ApiError ? e.message : e instanceof Error ? e.message : "重试失败");
    },
  });

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
        apply_unallocated_to_cash: gapToCash && assetGap > 100,
      });
      const sim = await createSimulation(plan.id, { runs: DEFAULT_RUNS });
      return { plan, sim };
    },
    onSuccess: ({ plan, sim }) => {
      setCreatedPlanId(plan.id);
      setJobId(sim.job_id);
      setError(null);
    },
    onError: (e) => {
      if (e instanceof ApiError) {
        setError(e.message);
        return;
      }
      setError(e instanceof Error ? e.message : "创建失败");
    },
  });

  const groupWeightChecks = useMemo(() => {
    const groups = new Map<string, { label: string; items: { label: string; value: number }[] }>();
    for (const s of selectedInstruments) {
      const key = `${s.inst.asset_class}:${s.inst.region}`;
      const label = `${regionLabel(s.inst.region)}${assetClassLabel(s.inst.asset_class)}`;
      const g = groups.get(key) ?? { label, items: [] };
      g.items.push({ label: s.inst.code, value: s.weight });
      groups.set(key, g);
    }
    return [...groups.values()].map((g) => ({
      label: g.label,
      ...validatePercentSum(g.items),
    }));
  }, [selectedInstruments]);

  const holdingsSum = useMemo(
    () => selectedInstruments.reduce((a, s) => a + s.amount, 0),
    [selectedInstruments],
  );
  const assetGap = totalAssets - holdingsSum;

  const selectedScenario = useMemo(
    () => scenariosQ.data?.scenarios.find((s) => s.id === scenarioId),
    [scenariosQ.data?.scenarios, scenarioId],
  );

  const portfolioReview = useMemo(() => {
    if (!selectedScenario) return null;
    return buildWizardPortfolioReview({
      scenarioWeights: selectedScenario.weights,
      selectedInstruments,
      totalAssetsMinor: totalAssets,
      gapToCash,
      assetGapMinor: assetGap,
    });
  }, [selectedScenario, selectedInstruments, totalAssets, gapToCash, assetGap]);

  return (
    <div className="mx-auto max-w-2xl">
      <Link href="/" className="text-sm underline">
        ← 计划列表
      </Link>
      <h1 className="mt-4 text-2xl font-semibold">新建计划向导</h1>
      <ol className="mt-4 flex gap-2 text-sm">
        {STEPS.map((label, i) => (
          <li
            key={label}
            className={`rounded-full px-3 py-1 ${
              i === step ? "bg-slate-900 text-white" : "bg-slate-100 text-slate-600"
            }`}
          >
            {i + 1}. {label}
          </li>
        ))}
      </ol>

      <div className="mt-8 space-y-4 rounded-lg border p-6">
        {step === 0 && (
          <>
            <label className="block text-sm">
              计划名称
              <input
                className="mt-1 w-full rounded-md border px-3 py-2"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </label>
            <div className="grid gap-4 sm:grid-cols-3">
              <label className="text-sm">
                当前年龄
                <input
                  type="number"
                  className="mt-1 w-full rounded-md border px-3 py-2"
                  value={currentAge}
                  onChange={(e) => setCurrentAge(Number(e.target.value))}
                />
              </label>
              <label className="text-sm">
                退休年龄
                <input
                  type="number"
                  className="mt-1 w-full rounded-md border px-3 py-2"
                  value={retirementAge}
                  onChange={(e) => setRetirementAge(Number(e.target.value))}
                />
              </label>
              <label className="text-sm">
                终止年龄
                <input
                  type="number"
                  className="mt-1 w-full rounded-md border px-3 py-2"
                  value={endAge}
                  onChange={(e) => setEndAge(Number(e.target.value))}
                />
              </label>
            </div>
            <MoneyInput label="当前总资产" valueMinor={totalAssets} onChange={setTotalAssets} />
            <MoneyInput label="当前年支出" valueMinor={annualSpending} onChange={setAnnualSpending} />
            <MoneyInput label="年储蓄" valueMinor={annualSavings} onChange={setAnnualSavings} />
          </>
        )}

        {step === 1 && (
          <>
            <label className="block text-sm">
              选择场景
              <select
                className="mt-1 w-full rounded-md border px-3 py-2"
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
            <p className="text-sm text-slate-600">
              场景只定义大类权重；地区权重使用计划默认值，创建后可在「参数」页调整。
            </p>
          </>
        )}

        {step === 2 && (
          <>
            <p className="text-sm text-slate-600">从资料库选择标的并填写组内占比与当前金额。</p>
            <Link href="/assets/import" className="text-sm underline">
              需要新标的？从 AKShare 录入
            </Link>
            <ul className="mt-4 max-h-64 space-y-2 overflow-y-auto">
              {instrumentsQ.data?.instruments
                .filter(
                  (i) =>
                    !i.is_system &&
                    i.status === "active" &&
                    (i.quality_status ?? "available") === "available",
                )
                .map((inst) => {
                  const sel = selectedInstruments.find((s) => s.inst.id === inst.id);
                  return (
                    <li key={inst.id} className="flex items-center gap-2 rounded border p-2 text-sm">
                      <input
                        type="checkbox"
                        checked={!!sel}
                        onChange={(e) => {
                          if (e.target.checked) {
                            setSelectedInstruments((prev) => [
                              ...prev,
                              { inst, weight: 0, amount: 0 },
                            ]);
                          } else {
                            setSelectedInstruments((prev) =>
                              prev.filter((s) => s.inst.id !== inst.id),
                            );
                          }
                        }}
                      />
                      <span className="flex-1">
                        {inst.name} ({inst.code})
                      </span>
                      {sel && (
                        <>
                          <PercentInput
                            value={sel.weight}
                            onChange={(w) =>
                              setSelectedInstruments((prev) =>
                                prev.map((s) =>
                                  s.inst.id === inst.id ? { ...s, weight: w } : s,
                                ),
                              )
                            }
                          />
                          <MoneyInput
                            valueMinor={sel.amount}
                            onChange={(a) =>
                              setSelectedInstruments((prev) =>
                                prev.map((s) =>
                                  s.inst.id === inst.id ? { ...s, amount: a } : s,
                                ),
                              )
                            }
                          />
                        </>
                      )}
                    </li>
                  );
                })}
            </ul>
            {groupWeightChecks.map((g) => (
              <p
                key={g.label}
                className={`text-sm ${g.passed ? "text-emerald-700" : "text-red-700"}`}
              >
                {g.label} 组内权重：{g.message}
              </p>
            ))}
            <p className="text-sm text-slate-600">
              持仓合计：{(holdingsSum / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })}{" "}
              元 / 总资产 {(totalAssets / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元
              {assetGap > 100 && "（存在未分配差额，下一步可计入现金/其他）"}
            </p>
          </>
        )}

        {step === 3 && (
          <>
            <ul className="list-disc pl-5 text-sm text-slate-700">
              <li>组内权重：{groupWeightChecks.every((g) => g.passed) ? "通过" : "未通过"}</li>
              <li>全组合目标权重：{portfolioReview?.passed ? "通过" : "未通过"}</li>
              <li>已选标的：{selectedInstruments.length} 个</li>
              <li>预计模拟：{DEFAULT_RUNS.toLocaleString()} 次</li>
            </ul>

            {selectedScenario && (
              <p className="mt-3 text-sm text-slate-600">
                场景「{selectedScenario.name}」目标：
                {selectedScenario.weights
                  .map((w) => `${assetClassLabel(w.asset_class)} ${formatPercent(w.weight)}`)
                  .join(" / ")}
              </p>
            )}

            {portfolioReview && (
              <div className="mt-4 space-y-3">
                <p
                  className={`text-sm ${portfolioReview.passed ? "text-emerald-700" : "text-amber-800"}`}
                  role="status"
                >
                  {portfolioReview.message}
                </p>
                <div className="overflow-x-auto rounded-lg border">
                  <table className="min-w-full text-sm">
                    <thead className="bg-slate-50 text-left">
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
                    <tfoot className="border-t bg-slate-50">
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
                  <p className="text-sm text-amber-800">
                    建议：返回「选择标的」补充
                    {portfolioReview.missingClasses.map((m) => m.label).join("、")}
                    类资产；若暂时无法配置，可先调整场景或稍后在计划内完善持仓。
                  </p>
                )}
              </div>
            )}

            {assetGap > 100 && (
              <label className="mt-4 flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={gapToCash}
                  onChange={(e) => setGapToCash(e.target.checked)}
                />
                <span>
                  将未分配差额{" "}
                  {(assetGap / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })} 元计入
                  「现金/其他」
                </span>
              </label>
            )}
            {assetGap < -100 && (
              <p className="text-sm text-red-600">持仓合计超过总资产，请返回上一步调整。</p>
            )}
            {jobId && (
              <p className="text-sm">
                模拟进度：{jobState.job?.status} {Math.round(jobState.progress * 100)}%
              </p>
            )}
            {simFailed && createdPlanId && (
              <div className="mt-3 flex flex-wrap gap-3">
                <button
                  type="button"
                  className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
                  disabled={retrySimMut.isPending || !!jobId}
                  onClick={() => retrySimMut.mutate()}
                >
                  重新启动模拟
                </button>
                <Link
                  href={`/plans/${createdPlanId}/dashboard`}
                  className="rounded-md border px-4 py-2 text-sm"
                >
                  进入计划
                </Link>
              </div>
            )}
            {error && <p className="text-sm text-red-600">{error}</p>}
          </>
        )}
      </div>

      <div className="mt-6 flex justify-between">
        <button
          type="button"
          className="rounded-md border px-4 py-2 text-sm"
          disabled={step === 0 || !!jobId}
          onClick={() => setStep((s) => s - 1)}
        >
          上一步
        </button>
        {step < 3 ? (
          <button
            type="button"
            className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white"
            onClick={() => {
              setError(null);
              if (step === 1 && !scenarioId) {
                setError("请选择资产配置场景。");
                return;
              }
              if (step === 2) {
                if (selectedInstruments.length === 0) {
                  setError("请至少选择一个标的。");
                  return;
                }
                if (!groupWeightChecks.every((g) => g.passed)) {
                  setError("各「大类+地区」组内权重须合计 100%。");
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
            className="rounded-md bg-slate-900 px-4 py-2 text-sm text-white disabled:opacity-50"
            disabled={
              !groupWeightChecks.every((g) => g.passed) ||
              !portfolioReview?.passed ||
              selectedInstruments.length === 0 ||
              !scenarioId ||
              assetGap < -100 ||
              (assetGap > 100 && !gapToCash) ||
              !!jobId ||
              !!createdPlanId ||
              finishMut.isPending
            }
            title={
              portfolioReview && !portfolioReview.passed
                ? portfolioReview.message
                : undefined
            }
            onClick={() => finishMut.mutate()}
          >
            创建并启动模拟
          </button>
        )}
      </div>
    </div>
  );
}
