"use client";

import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { ReturnOverridesCard } from "@/components/parameters/ReturnOverridesCard";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { SaveBar } from "@/components/ui/SaveBar";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { usePlanResultStale } from "@/hooks/usePlanResultStale";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { getPlan, updatePlanSettings } from "@/lib/api/plans";
import { getParameters } from "@/lib/api/plans";
import { getHoldings } from "@/lib/api/holdings";
import { getAllocation, listScenarios } from "@/lib/api/allocation";
import { listAssumptionProfiles } from "@/lib/api/assumptions";
import { ApiError } from "@/lib/api/client";
import { ErrorState } from "@/components/ui/ErrorState";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { queryErrorMessage } from "@/lib/query-error";
import {
  assetClassLabel,
  formatDateTimeFromMs,
  formatMoney,
  historyDepthLabel,
  regionLabel,
} from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import { validateAges, validatePositiveMoneyFields } from "@/lib/plan-validation";
import {
  buildParametersFormSnapshot,
  isParametersFormDirty,
} from "@/lib/form-snapshot";
import type { AssetClassTarget, PlanParameters, RegionTarget } from "@/types/api";

const ASSET_CLASSES = ["equity", "bond", "cash"] as const;
const MAX_SEED = "9223372036854775807";
const SEED_PATTERN = /^\d*$/;

const RETURN_MODE_OPTIONS: { value: string; label: string }[] = [
  { value: "blended_prior", label: "前瞻收益（历史向长期先验收缩，推荐）" },
  { value: "custom", label: "自定义前瞻收益" },
  { value: "historical_cagr", label: "历史 CAGR（旧模式，不建议用于长期 FIRE）" },
];

const SCENARIO_OPTIONS: { value: string; label: string }[] = [
  { value: "conservative", label: "保守" },
  { value: "baseline", label: "基准" },
  { value: "optimistic", label: "乐观" },
];

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
  const [localPlanName, setLocalPlanName] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [gapAction, setGapAction] = useState<"" | "cash">("");
  const [highInflationConfirmed, setHighInflationConfirmed] = useState(false);
  const [assumptionModeConfirmed, setAssumptionModeConfirmed] = useState(false);
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
  const assumptionProfilesQ = useQuery({
    queryKey: ["assumption-profiles"],
    queryFn: listAssumptionProfiles,
  });

  const params = localParams ?? paramsQ.data?.parameters ?? null;
  const planName = localPlanName ?? planQ.data?.name ?? "";

  const initialSnapshot = useMemo(() => {
    if (!paramsQ.data || !planQ.data) return null;
    return buildParametersFormSnapshot(planQ.data.name, paramsQ.data.parameters, "");
  }, [paramsQ.data, planQ.data]);

  const currentSnapshot = useMemo(() => {
    if (!params) return null;
    return buildParametersFormSnapshot(planName, params, gapAction);
  }, [planName, params, gapAction]);

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

  const savedAssumptionMode = paramsQ.data?.parameters.return_assumption_mode ?? "";
  const assumptionModeChanged =
    !!params && savedAssumptionMode !== "" && params.return_assumption_mode !== savedAssumptionMode;

  const saveMut = useMutation({
    mutationFn: async () => {
      if (!params || !planQ.data) throw new Error("未加载");
      // One atomic request: the backend saves name/allocation/parameters in a
      // single transaction and bumps config_version exactly once.
      return updatePlanSettings(planId, {
        config_version: planQ.data.config_version,
        ...(planName !== planQ.data.name ? { plan: { name: planName } } : {}),
        ...(showAllocation && allocationDirty
          ? {
              allocation: {
                asset_class_targets: assetTargets,
                region_targets: regionTargets,
              },
            }
          : {}),
        parameters: params,
        apply_unallocated_to_cash: gap > 100 && gapAction === "cash",
      });
    },
    onSuccess: () => {
      markClean();
      setLocalParams(null);
      setLocalPlanName(null);
      setLocalAssetTargets(null);
      setLocalRegionTargets(null);
      setAllocationDirty(false);
      setGapAction("");
      setAssumptionModeConfirmed(false);
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
    return <PageSkeleton label="加载参数…" />;
  }

  const gapBlocking = Math.abs(gap) > 100 && gap < 0;
  const gapNeedsCash = gap > 100 && gapAction !== "cash";
  const transactionCostInvalid =
    !Number.isFinite(params.transaction_cost_rate) ||
    params.transaction_cost_rate < 0 ||
    params.transaction_cost_rate >= 1;
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
        <h2 className="text-lg font-medium">基本信息</h2>
        <p className="mt-1 text-sm text-ink-muted">
          计划名称与 FIRE 时间线；资产大类与地区的目标权重请在「目标配置」分区调整。
        </p>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <label className="block text-sm">
            计划名称
            <input
              className="input-base mt-1"
              value={planName}
              onChange={(e) => setLocalPlanName(e.target.value)}
              data-testid="plan-name-input"
            />
          </label>
          <label className="block text-sm">
            估值日期
            <input className="input-base mt-1" value={planQ.data?.valuation_date ?? ""} disabled />
          </label>
          <label className="block text-sm">
            当前年龄
            <input
              type="number"
              className="input-base mt-1"
              value={params.current_age}
              onChange={(e) => update("current_age", Number(e.target.value))}
            />
          </label>
          <label className="block text-sm">
            计划退休年龄
            <input
              type="number"
              className="input-base mt-1"
              value={params.retirement_age}
              onChange={(e) => update("retirement_age", Number(e.target.value))}
            />
          </label>
          <label className="block text-sm">
            规划终止年龄
            <input
              type="number"
              className="input-base mt-1"
              value={params.end_age}
              onChange={(e) => update("end_age", Number(e.target.value))}
            />
          </label>
        </div>
      </section>

      <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">资金与现金流</h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <MoneyInput
            label={
              <span className="flex items-center">
                计划基准规模
                <MetricHelp termKey="configured_total_assets" />
              </span>
            }
            valueMinor={params.total_assets_minor}
            currency={planQ.data?.base_currency}
            onChange={(v) => update("total_assets_minor", v)}
          />
          <MoneyInput
            label="当前年储蓄"
            valueMinor={params.annual_savings_minor}
            onChange={(v) => update("annual_savings_minor", v)}
          />
          <MoneyInput
            label="退休后首年支出"
            valueMinor={params.annual_spending_minor}
            onChange={(v) => update("annual_spending_minor", v)}
          />
		  <MoneyInput
			label="退休后稳定年收入"
			valueMinor={params.annual_retirement_income_minor}
			onChange={(v) => update("annual_retirement_income_minor", v)}
		  />
          <PercentInput
            label="年储蓄增长率"
            value={params.annual_savings_growth_rate}
            onChange={(v) => update("annual_savings_growth_rate", v)}
          />
		  <PercentInput
			label="稳定收入年增长率"
			value={params.annual_retirement_income_growth_rate}
			onChange={(v) => update("annual_retirement_income_growth_rate", v)}
		  />
          <MoneyInput
            label="期末最低资产目标"
            valueMinor={params.terminal_wealth_floor_minor}
            onChange={(v) => update("terminal_wealth_floor_minor", v)}
          />
        </div>
        {Math.abs(gap) > 100 && (
          <div className="mt-4 rounded-md border border-warning/30 bg-warning/5 p-3 text-sm">
            {gap > 0 ? (
              <label className="flex flex-wrap items-center gap-2">
                <input
                  type="checkbox"
                  checked={gapAction === "cash"}
                  onChange={(e) => setGapAction(e.target.checked ? "cash" : "")}
                />
                <span className="flex items-center">
                  规模缺口 {formatMoney(gap, planQ.data?.base_currency)}，可计入现金/其他
                  <MetricHelp termKey="unallocated_gap" />
                </span>
              </label>
            ) : (
              <p className="flex items-center text-danger">
                持仓合计超过计划基准规模 {formatMoney(Math.abs(gap), planQ.data?.base_currency)}，无法保存。
                <MetricHelp termKey="scale_gap_over" />
              </p>
            )}
          </div>
        )}
      </section>

      {showAllocation && <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">资产配置</h2>
        <label className="mt-4 block text-sm">
          配置模板
          <select
            className="input-base mt-1"
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
              className="input-base mt-1"
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
              className="input-base mt-1"
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
                  className="input-base mt-1"
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
              className="input-base mt-1"
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
            <PercentInput
              value={params.rebalance_threshold}
              onChange={(v) => update("rebalance_threshold", v)}
            />
          </div>
          <div>
            <PercentInput
              label="交易成本率"
              value={params.transaction_cost_rate}
              onChange={(v) => update("transaction_cost_rate", v)}
            />
            {transactionCostInvalid && (
              <p className="mt-1 text-xs text-danger">
                交易成本率必须大于等于 0% 且小于 100%。
              </p>
            )}
          </div>
        </div>
      </section>

      <section className="rounded-lg border border-line p-4">
        <h2 className="text-lg font-medium">持仓模拟数据</h2>
        <div className="mt-3 overflow-x-auto text-sm">
          <table className="min-w-full text-left">
            <caption className="sr-only">持仓模拟数据快照</caption>
            <thead className="text-ink-muted">
              <tr>
                <th scope="col" className="pr-4 py-1">标的</th>
                <th scope="col" className="pr-4 py-1">历史深度</th>
                <th scope="col" className="pr-4 py-1">完整年度数</th>
                <th scope="col" className="pr-4 py-1">月度样本</th>
                <th scope="col" className="pr-4 py-1">快照生成时间</th>
                <th scope="col" className="pr-4 py-1">指标版本</th>
                <th scope="col" className="pr-4 py-1">快照提示</th>
              </tr>
            </thead>
            <tbody>
              {(holdingsQ.data?.holdings ?? []).map((h) => (
                  <tr key={h.id} className="border-t">
                    <td className="py-1 pr-4">
                      {h.instrument_name ?? h.asset_key}（{h.instrument_code ?? "—"}）
                    </td>
                    <td className="py-1 pr-4">{historyDepthLabel(h.snapshot_history_depth)}</td>
                    <td className="py-1 pr-4">{h.snapshot_complete_year_count ?? "—"}</td>
                    <td className="py-1 pr-4">{h.snapshot_monthly_return_count ?? "—"}</td>
                    <td className="py-1 pr-4">
                      {formatDateTimeFromMs(h.simulation_snapshot_created_at)}
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
        <h2 className="text-lg font-medium">收益率假设</h2>
        <p className="mt-1 text-sm text-ink-muted">
          通用的收益先验、波动率边界与相关性在左侧「模拟假设」统一维护，此处只选择本计划如何使用它们。
        </p>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <label className="block text-sm">
            收益假设来源
            <select
              className="input-base mt-1"
              value={params.return_assumption_mode}
              onChange={(e) => update("return_assumption_mode", e.target.value)}
            >
              {RETURN_MODE_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
          {params.return_assumption_mode === "blended_prior" && (
            <label className="block text-sm">
              假设情景
              <select
                className="input-base mt-1"
                value={params.return_assumption_scenario}
                onChange={(e) => update("return_assumption_scenario", e.target.value)}
              >
                {SCENARIO_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>
          )}
          {params.return_assumption_mode === "blended_prior" && (
            <label className="block text-sm">
              假设集
              <select
                className="input-base mt-1"
                value={params.assumption_selection_mode}
                onChange={(e) => {
                  const mode = e.target.value;
                  if (mode === "follow_global") {
                    update("assumption_selection_mode", mode);
                    setLocalParams((p) =>
                      p
                        ? {
                            ...p,
                            assumption_selection_mode: mode,
                            return_assumption_set_id: "",
                            return_assumption_set_version: 0,
                          }
                        : p,
                    );
                  } else {
                    const first = assumptionProfilesQ.data?.profiles.find(
                      (pr) => pr.status === "active",
                    );
                    setLocalParams((p) =>
                      p
                        ? {
                            ...p,
                            assumption_selection_mode: mode,
                            return_assumption_set_id:
                              p.return_assumption_set_id ||
                              first?.id ||
                              assumptionProfilesQ.data?.preferences.default_profile_id ||
                              "",
                            return_assumption_set_version:
                              p.return_assumption_set_version ||
                              first?.version ||
                              assumptionProfilesQ.data?.preferences.default_profile_version ||
                              0,
                          }
                        : p,
                    );
                  }
                }}
              >
                <option value="follow_global">跟随全局默认</option>
                <option value="pinned_profile">固定指定假设集</option>
              </select>
            </label>
          )}
          {params.return_assumption_mode === "blended_prior" &&
            params.assumption_selection_mode === "pinned_profile" && (
              <label className="block text-sm">
                指定假设集版本
                <select
                  className="input-base mt-1"
                  value={`${params.return_assumption_set_id}@${params.return_assumption_set_version}`}
                  onChange={(e) => {
                    const [id, ver] = e.target.value.split("@");
                    setLocalParams((p) =>
                      p
                        ? {
                            ...p,
                            return_assumption_set_id: id,
                            return_assumption_set_version: Number(ver),
                          }
                        : p,
                    );
                  }}
                >
                  {(assumptionProfilesQ.data?.profiles ?? []).map((pr) => (
                    <option key={`${pr.id}@${pr.version}`} value={`${pr.id}@${pr.version}`}>
                      {pr.name}（{pr.id}@{pr.version}·{pr.status}）
                    </option>
                  ))}
                </select>
              </label>
            )}
        </div>
        {params.return_assumption_mode === "blended_prior" &&
          params.assumption_selection_mode === "follow_global" &&
          assumptionProfilesQ.data && (
            <p className="mt-2 text-xs text-ink-muted">
              当前全局默认：{assumptionProfilesQ.data.preferences.default_profile_id}@
              {assumptionProfilesQ.data.preferences.default_profile_version}（假设情景{" "}
              {assumptionProfilesQ.data.preferences.default_scenario}）。可在「模拟假设」修改。
            </p>
          )}
        {params.return_assumption_mode === "historical_cagr" && (
          <p className="mt-3 rounded-md border border-danger/30 bg-danger/5 p-3 text-sm text-danger">
            历史收益不代表未来收益。有限历史样本（尤其是高景气区间）不适合直接外推到数十年的 FIRE
            规划，可能严重高估期末资产与成功率。仅建议用于历史复盘或兼容旧计划。
          </p>
        )}
        {params.return_assumption_mode === "custom" && (
          <p className="mt-3 rounded-md border border-warning/30 bg-warning/5 p-3 text-sm text-warning">
            自定义前瞻收益由你自行负责其来源与合理性；请确保为「费用后、基准币种、名义几何年化」口径。
          </p>
        )}
        {assumptionModeChanged && (
          <label className="mt-3 flex flex-wrap items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={assumptionModeConfirmed}
              onChange={(e) => setAssumptionModeConfirmed(e.target.checked)}
            />
            我确认切换收益假设来源会改变后续模拟结果，且需重新运行模拟（旧结果将标记为过期）。
          </label>
        )}
      </section>

      <ReturnOverridesCard planId={planId} />

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
              className="input-base mt-1"
              value={params.simulation_runs}
              onChange={(e) => update("simulation_runs", Number(e.target.value))}
            />
          </label>
          <div className="block text-sm">
            <span className="flex items-center">
              Student-t 自由度
              <MetricHelp termKey="student_t_df" />
            </span>
            <p className="mt-1 rounded-md border border-dashed border-line bg-surface-muted px-3 py-2 text-ink-muted">
              厚尾自由度与收益截断现由全局「模拟假设」profile 统一管理并在每次运行时冻结，计划级不再单独配置。
            </p>
          </div>
          <label className="block text-sm">
            随机种子（可选）
            <input
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              className="input-base mt-1 font-mono"
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
          // The backend silently ignores an empty plan name (patch semantics),
          // so a cleared name would "save" without effect; reject it up front.
          if (planName.trim() === "") {
            setSaveError("计划名称不能为空。");
            return;
          }
          const ageCheck = validateAges({
            currentAge: params.current_age,
            retirementAge: params.retirement_age,
            endAge: params.end_age,
          });
          if (!ageCheck.ok) {
            setSaveError(ageCheck.message!);
            return;
          }
          const moneyCheck = validatePositiveMoneyFields({
            totalAssetsMinor: params.total_assets_minor,
            annualSpendingMinor: params.annual_spending_minor,
            annualSavingsMinor: params.annual_savings_minor,
			annualRetirementIncomeMinor: params.annual_retirement_income_minor,
          });
          if (!moneyCheck.ok) {
            setSaveError(moneyCheck.message!);
            return;
          }
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
          if (assumptionModeChanged && !assumptionModeConfirmed) {
            setSaveError("切换收益假设来源需先勾选确认。");
            return;
          }
          if (params.withdrawal_floor_ratio >= params.withdrawal_ceiling_ratio) {
            setSaveError("护栏下限比例需小于上限比例。");
            return;
          }
          if (transactionCostInvalid) {
            setSaveError("交易成本率必须大于等于 0% 且小于 100%。");
            return;
          }
          saveMut.mutate();
        }}
      />
    </div>
  );
}
