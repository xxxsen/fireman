"use client";

import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { SaveBar } from "@/components/ui/SaveBar";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { CashFlowEditor } from "@/components/plans/CashFlowEditor";
import { usePlanResultStale } from "@/hooks/usePlanResultStale";
import { usePlanEdit } from "../layout";
import { getPlan } from "@/lib/api/plans";
import { getParameters, updateParameters } from "@/lib/api/plans";
import { getHoldings } from "@/lib/api/holdings";
import { getAllocation, listScenarios, updateAllocation } from "@/lib/api/allocation";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, formatMoney, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import type { AssetClassTarget, PlanCashFlow, PlanParameters, RegionTarget } from "@/types/api";

const ASSET_CLASSES = ["equity", "bond", "cash"] as const;
const MAX_SEED = "9223372036854775807";
const SEED_PATTERN = /^\d*$/;

function normalizeSeedInput(raw: string): string | null {
  if (raw === "") return null;
  if (!SEED_PATTERN.test(raw)) return null;
  if (raw.length > MAX_SEED.length || (raw.length === MAX_SEED.length && raw > MAX_SEED)) {
    return null;
  }
  return raw;
}

export function ParametersContent({
  showAllocation = true,
  showStale = true,
}: {
  showAllocation?: boolean;
  showStale?: boolean;
}) {
  const planId = useParams().id as string;
  const { dirty, markDirty, markClean } = usePlanEdit();
  const qc = useQueryClient();
  const [params, setParams] = useState<PlanParameters | null>(null);
  const [cashFlows, setCashFlows] = useState<PlanCashFlow[]>([]);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [gapAction, setGapAction] = useState<"cash" | "">("");
  const [highInflationConfirmed, setHighInflationConfirmed] = useState(false);
  const [assetTargets, setAssetTargets] = useState<AssetClassTarget[]>([]);
  const [regionTargets, setRegionTargets] = useState<RegionTarget[]>([]);
  const [allocationDirty, setAllocationDirty] = useState(false);

  const { stale } = usePlanResultStale(planId);

  const planQ = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const paramsQ = useQuery({
    queryKey: ["parameters", planId],
    queryFn: () => getParameters(planId),
  });
  const allocationQ = useQuery({
    queryKey: ["allocation", planId],
    queryFn: () => getAllocation(planId),
  });
  const holdingsQ = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });
  const scenariosQ = useQuery({
    queryKey: ["scenarios"],
    queryFn: listScenarios,
  });

  useEffect(() => {
    if (paramsQ.data) {
      setParams(paramsQ.data.parameters);
      setCashFlows(paramsQ.data.cash_flows ?? []);
      markClean();
      setAllocationDirty(false);
    }
  }, [paramsQ.data, markClean]);

  useEffect(() => {
    if (allocationQ.data) {
      setAssetTargets(allocationQ.data.asset_class_targets);
      setRegionTargets(allocationQ.data.region_targets);
    }
  }, [allocationQ.data]);

  const holdingsSum =
    holdingsQ.data?.holdings
      .filter((h) => h.enabled)
      .reduce((s, h) => s + h.current_amount_minor, 0) ?? 0;

  const gap = params ? params.total_assets_minor - holdingsSum : 0;

  const saveMut = useMutation({
    mutationFn: async () => {
      if (!params || !planQ.data) throw new Error("未加载");
      let version = planQ.data.config_version;
      if (showAllocation && allocationDirty) {
        await updateAllocation(planId, {
          config_version: version,
          asset_class_targets: assetTargets,
          region_targets: regionTargets,
        });
        version += 1;
      }
      return updateParameters(planId, {
        config_version: version,
        parameters: params,
        cash_flows: cashFlows,
        apply_unallocated_to_cash: gap > 100 && gapAction === "cash",
      });
    },
    onSuccess: () => {
      markClean();
      setAllocationDirty(false);
      setGapAction("");
      setSaveError(null);
      void qc.invalidateQueries({ queryKey: ["plan", planId] });
      void qc.invalidateQueries({ queryKey: ["parameters", planId] });
      void qc.invalidateQueries({ queryKey: ["allocation", planId] });
      void qc.invalidateQueries({ queryKey: ["holdings", planId] });
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
    },
    onError: (e) => {
      setSaveError(e instanceof ApiError ? e.message : "保存失败");
    },
  });

  const update = <K extends keyof PlanParameters>(key: K, value: PlanParameters[K]) => {
    if (!params) return;
    setParams({ ...params, [key]: value });
    markDirty();
  };

  if (!params || planQ.isLoading) return <p className="text-slate-600">加载参数…</p>;

  const gapBlocking = Math.abs(gap) > 100 && gap < 0;
  const gapNeedsCash = gap > 100 && gapAction !== "cash";
  const acCheck = validatePercentSum(
    assetTargets.map((t) => ({ label: assetClassLabel(t.asset_class), value: t.weight })),
  );
  const regionChecks = ASSET_CLASSES.map((ac) => {
    const items = regionTargets
      .filter((r) => r.asset_class === ac)
      .map((r) => ({ label: regionLabel(r.region), value: r.weight_within_class }));
    return { ac, ...validatePercentSum(items) };
  });

  return (
    <div className="space-y-6 pb-20">
      {showStale && stale && <StaleBanner />}
      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">计划信息</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <label className="block text-sm">
            计划名称
            <input
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={planQ.data?.name ?? ""}
              disabled
            />
            <span className="text-xs text-slate-500">在计划设置中修改</span>
          </label>
          <label className="block text-sm">
            估值日期
            <input className="mt-1 w-full rounded-md border px-3 py-2" value={planQ.data?.valuation_date ?? ""} disabled />
          </label>
          <label className="block text-sm">
            当前年龄
            <input
              type="number"
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.current_age}
              onChange={(e) => update("current_age", Number(e.target.value))}
            />
          </label>
          <label className="block text-sm">
            计划退休年龄
            <input
              type="number"
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.retirement_age}
              onChange={(e) => update("retirement_age", Number(e.target.value))}
            />
          </label>
          <label className="block text-sm">
            规划终止年龄
            <input
              type="number"
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.end_age}
              onChange={(e) => update("end_age", Number(e.target.value))}
            />
          </label>
        </div>
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">资金与现金流</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <MoneyInput
            label="总资产"
            valueMinor={params.total_assets_minor}
            currency={planQ.data?.base_currency}
            onChange={(v) => update("total_assets_minor", v)}
          />
          <MoneyInput
            label="当前年储蓄"
            valueMinor={params.annual_savings_minor}
            onChange={(v) => update("annual_savings_minor", v)}
          />
          <PercentInput
            label="年储蓄增长率"
            value={params.annual_savings_growth_rate}
            onChange={(v) => update("annual_savings_growth_rate", v)}
          />
          <MoneyInput
            label="退休后首年支出"
            valueMinor={params.annual_spending_minor}
            onChange={(v) => update("annual_spending_minor", v)}
          />
          <MoneyInput
            label="期末最低资产目标"
            valueMinor={params.terminal_wealth_floor_minor}
            onChange={(v) => update("terminal_wealth_floor_minor", v)}
          />
        </div>
        {Math.abs(gap) > 100 && (
          <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm">
            <p>
              未分配差额 = {formatMoney(gap, planQ.data?.base_currency)}
              <MetricHelp termKey="unallocated_gap" />
            </p>
            {gap > 0 ? (
              <label className="mt-2 flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={gapAction === "cash"}
                  onChange={(e) => setGapAction(e.target.checked ? "cash" : "")}
                />
                计入现金/其他（保存前请补充持仓或调整总资产）
              </label>
            ) : (
              <p className="mt-1 text-red-700">持仓金额超过总资产，无法保存。</p>
            )}
          </div>
        )}
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">额外现金流事件</h2>
        <div className="mt-4">
          <CashFlowEditor
            planId={planId}
            flows={cashFlows}
            onChange={(next) => {
              setCashFlows(next);
              markDirty();
            }}
          />
        </div>
      </section>

      {showAllocation && <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">资产配置</h2>
        <label className="mt-4 block text-sm">
          场景选择
          <select
            className="mt-1 w-full rounded-md border px-3 py-2"
            value={params.selected_scenario_id ?? ""}
            onChange={(e) => update("selected_scenario_id", e.target.value || null)}
          >
            <option value="">—</option>
            {scenariosQ.data?.scenarios.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <div className="mt-4 space-y-4">
          <div>
            <h3 className="text-sm font-medium">大类目标权重</h3>
            <div className="mt-2 grid gap-3 sm:grid-cols-3">
              {assetTargets.map((t, i) => (
                <PercentInput
                  key={t.asset_class}
                  label={assetClassLabel(t.asset_class)}
                  value={t.weight}
                  onChange={(v) => {
                    const next = [...assetTargets];
                    next[i] = { ...t, weight: v };
                    setAssetTargets(next);
                    setAllocationDirty(true);
                    markDirty();
                  }}
                />
              ))}
            </div>
            {!acCheck.passed && (
              <p className="mt-1 text-sm text-red-600">{acCheck.message}</p>
            )}
          </div>
          {ASSET_CLASSES.map((ac) => (
            <div key={ac}>
              <h3 className="text-sm font-medium">{assetClassLabel(ac)} · 地区组内权重</h3>
              <div className="mt-2 grid gap-3 sm:grid-cols-2">
                {regionTargets
                  .filter((r) => r.asset_class === ac)
                  .map((r) => {
                    const idx = regionTargets.indexOf(r);
                    return (
                      <PercentInput
                        key={`${r.asset_class}:${r.region}`}
                        label={regionLabel(r.region)}
                        value={r.weight_within_class}
                        onChange={(v) => {
                          const next = [...regionTargets];
                          next[idx] = { ...r, weight_within_class: v };
                          setRegionTargets(next);
                          setAllocationDirty(true);
                          markDirty();
                        }}
                      />
                    );
                  })}
              </div>
              {regionChecks.find((c) => c.ac === ac && !c.passed) && (
                <p className="mt-1 text-sm text-red-600">
                  {regionChecks.find((c) => c.ac === ac)?.message}
                </p>
              )}
            </div>
          ))}
        </div>
      </section>}

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">提取与通胀</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <label className="block text-sm">
            提取策略
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.withdrawal_type}
              onChange={(e) => update("withdrawal_type", e.target.value)}
            >
              <option value="fixed_real">固定实际支出</option>
              <option value="fixed_portfolio">组合百分比</option>
              <option value="guardrail">护栏策略</option>
            </select>
          </label>
          <label className="block text-sm">
            通胀模式
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.inflation_mode}
              onChange={(e) => update("inflation_mode", e.target.value)}
            >
              <option value="fixed_real">固定通胀率</option>
              <option value="random_ar1">随机通胀</option>
            </select>
          </label>
          <PercentInput
            label="固定通胀率"
            value={params.fixed_inflation_rate}
            onChange={(v) => update("fixed_inflation_rate", v)}
          />
          {params.inflation_mode === "random_ar1" && (
            <>
              <PercentInput
                label="通胀均值 μ"
                value={params.inflation_mu}
                onChange={(v) => update("inflation_mu", v)}
              />
              <PercentInput
                label="通胀波动 σ"
                value={params.inflation_sigma}
                onChange={(v) => update("inflation_sigma", v)}
              />
              <label className="block text-sm">
                通胀自回归 φ
                <input
                  type="number"
                  step={0.01}
                  min={0}
                  max={1}
                  className="mt-1 w-full rounded-md border px-3 py-2"
                  value={params.inflation_phi}
                  onChange={(e) => update("inflation_phi", Number(e.target.value))}
                />
              </label>
            </>
          )}
          {params.fixed_inflation_rate > 0.15 && (
            <label className="flex items-center gap-2 text-sm sm:col-span-2">
              <input
                type="checkbox"
                checked={highInflationConfirmed}
                onChange={(e) => setHighInflationConfirmed(e.target.checked)}
              />
              确认固定通胀率超过 15%（非常规假设）
            </label>
          )}
          {(params.withdrawal_type === "fixed_portfolio" ||
            params.withdrawal_type === "guardrail") && (
            <PercentInput
              label="提取率"
              value={params.withdrawal_rate}
              onChange={(v) => update("withdrawal_rate", v)}
            />
          )}
          {params.withdrawal_type === "guardrail" && (
            <>
              <PercentInput
                label="护栏下限比例"
                value={params.withdrawal_floor_ratio}
                onChange={(v) => update("withdrawal_floor_ratio", v)}
              />
              <PercentInput
                label="护栏上限比例"
                value={params.withdrawal_ceiling_ratio}
                onChange={(v) => update("withdrawal_ceiling_ratio", v)}
              />
            </>
          )}
          <PercentInput
            label="有效提取税率"
            value={params.withdrawal_tax_rate}
            onChange={(v) => update("withdrawal_tax_rate", v)}
          />
          <PercentInput
            label="应税提取比例"
            value={params.taxable_withdrawal_ratio}
            onChange={(v) => update("taxable_withdrawal_ratio", v)}
          />
        </div>
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">调仓</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-3">
          <label className="block text-sm">
            检查频率
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.rebalance_frequency}
              onChange={(e) => update("rebalance_frequency", e.target.value)}
            >
              <option value="monthly">月度</option>
              <option value="quarterly">季度</option>
              <option value="annual">年度</option>
            </select>
          </label>
          <div>
            <div className="mb-1 flex items-center text-sm text-slate-600">
              调仓阈值
              <MetricHelp termKey="rebalance_threshold" />
            </div>
            <PercentInput value={params.rebalance_threshold} onChange={(v) => update("rebalance_threshold", v)} />
          </div>
          <PercentInput
            label="交易成本率"
            value={params.transaction_cost_rate}
            onChange={(v) => update("transaction_cost_rate", v)}
          />
        </div>
      </section>

      <section className="rounded-lg border border-slate-200 p-4">
        <h2 className="text-lg font-medium">模拟设置</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <label className="block text-sm">
            <span className="flex items-center">
              模拟次数
              <MetricHelp termKey="simulation_runs" />
            </span>
            <input
              type="number"
              min={1000}
              max={100000}
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.simulation_runs}
              onChange={(e) => update("simulation_runs", Number(e.target.value))}
            />
          </label>
          <label className="block text-sm">
            <span className="flex items-center">
              Student-t 自由度
              <MetricHelp termKey="student_t_df" />
            </span>
            <input
              type="number"
              min={5}
              max={30}
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={params.student_t_df}
              onChange={(e) => update("student_t_df", Number(e.target.value))}
            />
          </label>
          <label className="block text-sm">
            随机种子（可选）
            <input
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              className="mt-1 w-full rounded-md border px-3 py-2 font-mono"
              value={params.seed ?? ""}
              onChange={(e) => {
                const next = normalizeSeedInput(e.target.value);
                if (next === null && e.target.value !== "") return;
                update("seed", next);
              }}
            />
            <span className="text-xs text-slate-500">0–{MAX_SEED}</span>
          </label>
        </div>
      </section>

      <SaveBar
        dirty={dirty || (showAllocation && allocationDirty)}
        saving={saveMut.isPending}
        error={saveError}
        onSave={() => {
          if (gapBlocking) {
            setSaveError("持仓金额超过总资产，请先调整标的当前金额或总资产。");
            return;
          }
          if (gapNeedsCash) {
            setSaveError("存在未分配差额，请勾选「计入现金/其他」或补充持仓。");
            return;
          }
          if (
            showAllocation &&
            (!acCheck.passed || regionChecks.some((c) => !c.passed))
          ) {
            setSaveError("资产配置权重未通过校验。");
            return;
          }
          if (params.fixed_inflation_rate > 0.15 && !highInflationConfirmed) {
            setSaveError("固定通胀率超过 15%，请勾选确认。");
            return;
          }
          saveMut.mutate();
        }}
      />
    </div>
  );
}

export default function ParametersPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  useEffect(() => {
    router.replace(`/plans/${planId}/settings?section=fire-params`);
  }, [planId, router]);
  return <p className="text-slate-600">正在前往计划设置…</p>;
}
