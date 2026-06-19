"use client";

import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { SaveBar } from "@/components/ui/SaveBar";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { CashFlowEditor } from "@/components/plans/CashFlowEditor";
import { usePlanResultStale } from "@/hooks/usePlanResultStale";
import { usePlanEdit } from "../layout";
import { getPlan, updatePlan } from "@/lib/api/plans";
import { getParameters, updateParameters } from "@/lib/api/plans";
import { getHoldings } from "@/lib/api/holdings";
import { getAllocation, listScenarios, updateAllocation } from "@/lib/api/allocation";
import { ApiError } from "@/lib/api/client";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { queryErrorMessage } from "@/lib/query-error";
import { assetClassLabel, formatMoney, historyDepthLabel, regionLabel } from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import {
  buildParametersFormSnapshot,
  isParametersFormDirty,
} from "@/lib/form-snapshot";
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
  const { markDirty, markClean } = usePlanEdit();
  const qc = useQueryClient();
  const [localParams, setLocalParams] = useState<PlanParameters | null>(null);
  const [localCashFlows, setLocalCashFlows] = useState<PlanCashFlow[] | null>(null);
  const [localPlanName, setLocalPlanName] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [gapAction, setGapAction] = useState<"" | "cash">("");
  const [highInflationConfirmed, setHighInflationConfirmed] = useState(false);
  const [localAssetTargets, setLocalAssetTargets] = useState<AssetClassTarget[] | null>(null);
  const [localRegionTargets, setLocalRegionTargets] = useState<RegionTarget[] | null>(null);
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

  const params = localParams ?? paramsQ.data?.parameters ?? null;
  const cashFlows = useMemo(
    () => localCashFlows ?? paramsQ.data?.cash_flows ?? [],
    [localCashFlows, paramsQ.data?.cash_flows],
  );
  const planName = localPlanName ?? planQ.data?.name ?? "";

  const initialSnapshot = useMemo(() => {
    if (!paramsQ.data || !planQ.data) return null;
    return buildParametersFormSnapshot(
      planQ.data.name,
      paramsQ.data.parameters,
      paramsQ.data.cash_flows,
      "",
    );
  }, [paramsQ.data, planQ.data]);

  const currentSnapshot = useMemo(() => {
    if (!params) return null;
    return buildParametersFormSnapshot(planName, params, cashFlows, gapAction);
  }, [planName, params, cashFlows, gapAction]);

  const formDirty = useMemo(
    () => (currentSnapshot ? isParametersFormDirty(initialSnapshot, currentSnapshot) : false),
    [initialSnapshot, currentSnapshot],
  );

  const allocationFormDirty = showAllocation && allocationDirty;

  useEffect(() => {
    if (formDirty || allocationFormDirty) markDirty();
    else markClean();
  }, [formDirty, allocationFormDirty, markDirty, markClean]);
  const assetTargets =
    allocationDirty && localAssetTargets
      ? localAssetTargets
      : (allocationQ.data?.asset_class_targets ?? []);
  const regionTargets =
    allocationDirty && localRegionTargets
      ? localRegionTargets
      : (allocationQ.data?.region_targets ?? []);

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
      if (planName !== planQ.data.name) {
        await updatePlan(planId, {
          config_version: version,
          name: planName,
          base_currency: planQ.data.base_currency,
          valuation_date: planQ.data.valuation_date,
          status: planQ.data.status,
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
      setLocalParams(null);
      setLocalCashFlows(null);
      setLocalPlanName(null);
      setLocalAssetTargets(null);
      setLocalRegionTargets(null);
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
    setLocalParams({ ...params, [key]: value });
  };

  if (
    (planQ.isError || paramsQ.isError || allocationQ.isError || holdingsQ.isError) &&
    (!params || !planQ.data || !allocationQ.data || !holdingsQ.data)
  ) {
    return (
      <ErrorState
        message="无法加载计划参数。请确认后端服务可用后重试。"
        onRetry={() => {
          if (planQ.isError) void planQ.refetch();
          if (paramsQ.isError) void paramsQ.refetch();
          if (allocationQ.isError) void allocationQ.refetch();
          if (holdingsQ.isError) void holdingsQ.refetch();
        }}
        backHref={`/plans/${planId}/overview`}
        backLabel="返回总览"
        technicalDetail={queryErrorMessage(
          planQ.error ?? paramsQ.error ?? allocationQ.error ?? holdingsQ.error,
        )}
      />
    );
  }

  if (
    !params ||
    planQ.isLoading ||
    allocationQ.isLoading ||
    holdingsQ.isLoading ||
    !allocationQ.data ||
    !holdingsQ.data
  ) {
    return <LoadingState label="加载参数…" />;
  }

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
      <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">计划信息</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <label className="block text-sm">
            计划名称
            <input
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={planName}
              onChange={(e) => setLocalPlanName(e.target.value)}
              data-testid="plan-name-input"
            />
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

      <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">资金与现金流</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <div>
            <MoneyInput
              label="计划基准规模"
              valueMinor={params.total_assets_minor}
              currency={planQ.data?.base_currency}
              onChange={(v) => update("total_assets_minor", v)}
            />
            <p className="mt-1 text-xs text-ink-muted">
              <MetricHelp termKey="configured_total_assets" />
            </p>
            {holdingsSum > params.total_assets_minor + 100 && (
              <p className="mt-1 text-xs text-warning">
                低于当前持仓 {formatMoney(holdingsSum - params.total_assets_minor, planQ.data?.base_currency)}
              </p>
            )}
            {params.total_assets_minor > holdingsSum + 100 && (
              <p className="mt-1 text-xs text-warning">
                高于当前持仓 {formatMoney(params.total_assets_minor - holdingsSum, planQ.data?.base_currency)}（规模缺口）
              </p>
            )}
          </div>
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
          <div className="mt-4 rounded-md border border-warning/30 bg-warning/5 p-3 text-sm">
            <p>
              {gap > 0 ? (
                <>
                  规模缺口 {formatMoney(gap, planQ.data?.base_currency)}
                  <MetricHelp termKey="unallocated_gap" />
                </>
              ) : (
                <>
                  规模超出 {formatMoney(Math.abs(gap), planQ.data?.base_currency)}
                  <MetricHelp termKey="scale_gap_over" />
                </>
              )}
            </p>
            {gap > 0 ? (
              <label className="mt-2 flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={gapAction === "cash"}
                  onChange={(e) => setGapAction(e.target.checked ? "cash" : "")}
                />
                计入现金/其他（保存前请补充持仓或下调计划基准规模）
              </label>
            ) : (
              <p className="mt-1 text-danger">持仓合计超过计划基准规模，无法保存。</p>
            )}
          </div>
        )}
      </section>

      <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">额外现金流事件</h2>
        <div className="mt-4">
          <CashFlowEditor
            planId={planId}
            flows={cashFlows}
            onChange={(next) => {
              setLocalCashFlows(next);
            }}
          />
        </div>
      </section>

      {showAllocation && <section className="rounded-lg border border-line p-4">
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
                    setLocalAssetTargets(next);
                    setAllocationDirty(true);
                  }}
                />
              ))}
            </div>
            {!acCheck.passed && (
              <p className="mt-1 text-sm text-danger">{acCheck.message}</p>
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
                          setLocalRegionTargets(next);
                          setAllocationDirty(true);
                        }}
                      />
                    );
                  })}
              </div>
              {regionChecks.find((c) => c.ac === ac && !c.passed) && (
                <p className="mt-1 text-sm text-danger">
                  {regionChecks.find((c) => c.ac === ac)?.message}
                </p>
              )}
            </div>
          ))}
        </div>
      </section>}

      <section className="rounded-lg border border-line p-4">
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

      <section className="rounded-lg border border-line p-4">
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
            <div className="mb-1 flex items-center text-sm text-ink-muted">
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

      <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">持仓模拟数据</h2>
        <div className="mt-3 overflow-x-auto text-sm">
          <table className="min-w-full text-left">
            <thead className="text-ink-muted">
              <tr>
                <th className="pr-4 py-1">标的</th>
                <th className="pr-4 py-1">历史深度</th>
                <th className="pr-4 py-1">完整年度数</th>
                <th className="pr-4 py-1">月度样本</th>
                <th className="pr-4 py-1">快照生成时间</th>
                <th className="pr-4 py-1">指标版本</th>
                <th className="pr-4 py-1">快照提示</th>
              </tr>
            </thead>
            <tbody>
              {(holdingsQ.data?.holdings ?? []).map((h) => (
                  <tr key={h.id} className="border-t">
                    <td className="py-1 pr-4">
                      {h.instrument_name ?? h.instrument_id}（{h.instrument_code ?? "—"}）
                    </td>
                    <td className="py-1 pr-4">{historyDepthLabel(h.snapshot_history_depth)}</td>
                    <td className="py-1 pr-4">{h.snapshot_complete_year_count ?? "—"}</td>
                    <td className="py-1 pr-4">{h.snapshot_monthly_return_count ?? "—"}</td>
                    <td className="py-1 pr-4">
                      {h.simulation_snapshot_created_at
                        ? new Date(h.simulation_snapshot_created_at).toLocaleString("zh-CN")
                        : "—"}
                    </td>
                    <td className="py-1 pr-4 font-mono text-xs">{h.snapshot_metrics_version ?? "—"}</td>
                    <td className="py-1 pr-4 text-xs text-warning">
                      {(h.snapshot_warnings ?? []).length > 0 ? (
                        <ul className="list-disc pl-4">
                          {h.snapshot_warnings!.map((w) => (
                            <li key={w}>{w}</li>
                          ))}
                        </ul>
                      ) : (
                        "—"
                      )}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="rounded-lg border border-line p-4">
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
            <span className="text-xs text-ink-muted">0–{MAX_SEED}</span>
          </label>
        </div>
      </section>

      <SaveBar
        dirty={formDirty || allocationFormDirty}
        saving={saveMut.isPending}
        error={saveError}
        onSave={() => {
          if (gapBlocking) {
            setSaveError("持仓合计超过计划基准规模，请先调整标的当前金额或计划基准规模。");
            return;
          }
          if (gapNeedsCash) {
            setSaveError("存在规模缺口，请勾选「计入现金/其他」、补充持仓或下调计划基准规模。");
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
  return <p className="text-ink-muted">正在前往计划设置…</p>;
}
