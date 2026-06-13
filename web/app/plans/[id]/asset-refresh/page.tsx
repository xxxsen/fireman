"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { getHoldings, getTargets, updateHoldings } from "@/lib/api/holdings";
import { submitAssetRefresh } from "@/lib/api/asset-refresh";
import { listInstruments } from "@/lib/api/instruments";
import { getPlan, listPlans, getParameters } from "@/lib/api/plans";
import { listScenarios } from "@/lib/api/allocation";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
  regionLabel,
} from "@/lib/format";
import { assetClassSortIndex, regionSortIndex } from "@/lib/asset-class-order";
import {
  buildAssetRefreshBody,
  buildHoldingsUpdateItems,
  hasAssetRefreshStructureChange,
  holdingFromPlan,
  redistributeEnabledWeightsInGroup,
  sumHoldingsMinor,
  validateAssetRefreshTotal,
  type AssetRefreshHolding,
} from "@/lib/asset-refresh";
import { ApiError } from "@/lib/api/client";
import type { Instrument, PlanHolding } from "@/types/api";

const STEPS = ["说明", "配置确认", "录入当前资产", "确认提交"] as const;

function isSelectableInstrument(inst: Instrument): boolean {
  return (
    !inst.is_system &&
    inst.status === "active" &&
    (inst.quality_status ?? "available") === "available"
  );
}

export default function AssetRefreshPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [step, setStep] = useState(0);
  const [holdingsDraft, setHoldingsDraft] = useState<AssetRefreshHolding[] | null>(null);
  const [totalOverride, setTotalOverride] = useState<number | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState<string | null>(null);

  const plan = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const plans = useQuery({ queryKey: ["plans"], queryFn: listPlans });
  const holdings = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });
  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });
  const parameters = useQuery({
    queryKey: ["parameters", planId],
    queryFn: () => getParameters(planId),
  });
  const scenarios = useQuery({
    queryKey: ["scenarios"],
    queryFn: listScenarios,
  });
  const instruments = useQuery({
    queryKey: ["instruments", plan.data?.valuation_date],
    queryFn: () =>
      listInstruments(
        plan.data?.valuation_date ? { valuationDate: plan.data.valuation_date } : undefined,
      ),
    enabled: !!plan.data,
  });

  const systemInstrumentIds = useMemo(
    () =>
      new Set(
        (instruments.data?.instruments ?? [])
          .filter((inst) => inst.is_system)
          .map((inst) => inst.id),
      ),
    [instruments.data],
  );

  const defaultHoldings = useMemo(
    () =>
      (holdings.data?.holdings ?? []).map((holding) =>
        holdingFromPlan(holding, systemInstrumentIds.has(holding.instrument_id)),
      ),
    [holdings.data, systemInstrumentIds],
  );

  const draftHoldings = holdingsDraft ?? defaultHoldings;
  const enabledRows = useMemo(
    () => draftHoldings.filter((holding) => holding.enabled),
    [draftHoldings],
  );
  const defaultTotal = useMemo(
    () => enabledRows.reduce((sum, holding) => sum + holding.current_amount_minor, 0),
    [enabledRows],
  );
  const totalAssets = totalOverride ?? defaultTotal;
  const sumMinor = useMemo(
    () => sumHoldingsMinor(enabledRows.map((row) => ({
      instrument_id: row.instrument_id,
      current_amount_minor: row.current_amount_minor,
    }))),
    [enabledRows],
  );
  const validation = useMemo(
    () => validateAssetRefreshTotal(
      enabledRows.map((row) => ({
        instrument_id: row.instrument_id,
        current_amount_minor: row.current_amount_minor,
      })),
      totalAssets,
    ),
    [enabledRows, totalAssets],
  );
  const structureChanged = useMemo(
    () =>
      holdings.data
        ? hasAssetRefreshStructureChange(holdings.data.holdings, draftHoldings)
        : false,
    [holdings.data, draftHoldings],
  );

  const selectedScenario = useMemo(() => {
    const scenarioId = parameters.data?.parameters.selected_scenario_id;
    if (!scenarioId) return undefined;
    return scenarios.data?.scenarios.find((scenario) => scenario.id === scenarioId);
  }, [parameters.data, scenarios.data]);

  const groupedHoldings = useMemo(() => {
    const byClass = new Map<string, Map<string, AssetRefreshHolding[]>>();
    for (const holding of draftHoldings) {
      const regions = byClass.get(holding.asset_class) ?? new Map<string, AssetRefreshHolding[]>();
      const bucket = regions.get(holding.region) ?? [];
      bucket.push(holding);
      regions.set(holding.region, bucket);
      byClass.set(holding.asset_class, regions);
    }
    return [...byClass.keys()]
      .sort((left, right) => assetClassSortIndex(left) - assetClassSortIndex(right))
      .map((assetClass) => ({
        assetClass,
        regions: [...(byClass.get(assetClass)?.keys() ?? [])]
          .sort((left, right) => regionSortIndex(left) - regionSortIndex(right))
          .map((region) => ({
            region,
            rows: byClass.get(assetClass)?.get(region) ?? [],
          })),
      }));
  }, [draftHoldings]);

  const selectedInstrumentIds = useMemo(
    () => new Set(draftHoldings.map((holding) => holding.instrument_id)),
    [draftHoldings],
  );

  const filteredInstruments = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return [];
    return (instruments.data?.instruments ?? [])
      .filter(isSelectableInstrument)
      .filter((inst) => !selectedInstrumentIds.has(inst.id))
      .filter(
        (inst) =>
          inst.code.toLowerCase().includes(query) ||
          inst.name.toLowerCase().includes(query) ||
          assetClassLabel(inst.asset_class).includes(query) ||
          regionLabel(inst.region).includes(query),
      )
      .slice(0, 20);
  }, [filter, instruments.data, selectedInstrumentIds]);

  const switchPlan = (nextPlanId: string) => {
    if (nextPlanId === planId) return;
    setHoldingsDraft(null);
    setTotalOverride(null);
    setStep(1);
    router.replace(`/plans/${nextPlanId}/asset-refresh`);
  };

  const updateDraft = (next: AssetRefreshHolding[]) => {
    setHoldingsDraft(next);
  };

  const updateHolding = (instrumentId: string, patch: Partial<AssetRefreshHolding>) => {
    updateDraft(
      draftHoldings.map((holding) =>
        holding.instrument_id === instrumentId ? { ...holding, ...patch } : holding,
      ),
    );
  };

  const toggleEnabled = (holding: AssetRefreshHolding, enabled: boolean) => {
    let next = updateDraftHoldings(
      draftHoldings,
      holding.instrument_id,
      { enabled },
    );
    next = redistributeEnabledWeightsInGroup(next, holding.asset_class, holding.region);
    updateDraft(next);
  };

  const removeHolding = (holding: AssetRefreshHolding) => {
    if (holding.is_system) return;
    let next = draftHoldings.filter((item) => item.instrument_id !== holding.instrument_id);
    next = redistributeEnabledWeightsInGroup(next, holding.asset_class, holding.region);
    updateDraft(next);
  };

  const addInstrument = (instrument: Instrument) => {
    if (selectedInstrumentIds.has(instrument.id)) return;
    let next: AssetRefreshHolding[] = [
      ...draftHoldings,
      {
        id: `draft_${instrument.id}`,
        instrument_id: instrument.id,
        label: instrument.name,
        code: instrument.code,
        asset_class: instrument.asset_class,
        region: instrument.region,
        enabled: true,
        current_amount_minor: 0,
        weight_within_group: 0,
        sort_order: draftHoldings.length * 10,
        is_system: false,
      },
    ];
    next = redistributeEnabledWeightsInGroup(next, instrument.asset_class, instrument.region);
    updateDraft(next);
    setFilter("");
    setDrawerOpen(false);
  };

  const submit = useMutation({
    mutationFn: async () => {
      if (!plan.data) throw new Error("计划尚未加载");
      if (!validation.ok) throw new Error(validation.message ?? "校验失败");

      let configVersion = plan.data.config_version;
      if (structureChanged) {
        await updateHoldings(planId, {
          config_version: configVersion,
          holdings: buildHoldingsUpdateItems(draftHoldings),
        });
        const updatedPlan = await getPlan(planId);
        configVersion = updatedPlan.config_version;
      }

      return submitAssetRefresh(
        planId,
        buildAssetRefreshBody(
          configVersion,
          enabledRows.map((row) => ({
            instrument_id: row.instrument_id,
            current_amount_minor: row.current_amount_minor,
          })),
          totalAssets,
          true,
          structureChanged,
        ),
      );
    },
    onSuccess: () => {
      for (const key of ["holdings", "targets", "rebalance", "dashboard", "plan", "parameters"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
      router.push(`/plans/${planId}/rebalance?asset_refreshed=1`);
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : err instanceof Error ? err.message : "提交失败"),
  });

  if (plan.isLoading || holdings.isLoading || !plan.data || !holdings.data) {
    return <p className="text-slate-600">加载资产变更向导…</p>;
  }

  const beforeTotal = sumHoldingsMinor(
    holdings.data.holdings
      .filter((holding: PlanHolding) => holding.enabled)
      .map((holding) => ({
        instrument_id: holding.instrument_id,
        current_amount_minor: holding.current_amount_minor,
      })),
  );
  const structureOnly = beforeTotal === totalAssets;
  const scenarioName = selectedScenario?.name ?? "—";

  return (
    <div className="mx-auto max-w-3xl space-y-6 pb-16">
      <div>
        <h1 className="text-xl font-semibold">资产变更</h1>
        <p className="mt-1 text-sm text-slate-600">
          录入当前计划下的真实持仓结构与金额，提交后更新持仓事实并同步计划总资产。
        </p>
      </div>

      <ol className="flex flex-wrap gap-2 text-sm" data-testid="asset-refresh-steps">
        {STEPS.map((label, index) => (
          <li
            key={label}
            className={`rounded-full px-3 py-1 ${
              index === step ? "bg-slate-900 text-white" : "bg-slate-100 text-slate-600"
            }`}
          >
            {index + 1}. {label}
          </li>
        ))}
      </ol>

      {error && (
        <div className="rounded-md border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
          {error}
        </div>
      )}

      {step === 0 && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <div className="space-y-2 text-sm text-slate-700">
            <p>此流程用于维护当前计划下的真实持仓结构，包括：</p>
            <ul className="list-disc space-y-1 pl-5">
              <li>新增、移除或启停资产标的</li>
              <li>修改各资产当前金额与组内配置</li>
              <li>提交后覆盖当前计划内的持仓事实</li>
              <li>提交后计划总资产将同步为最新持仓合计</li>
            </ul>
            <p>
              如需修改场景模板本身，请前往{" "}
              <Link href="/scenarios" className="underline">
                场景配置
              </Link>
              ，而不是在此流程中修改。
            </p>
          </div>
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white"
              onClick={() => setStep(1)}
            >
              下一步
            </button>
            <Link
              href={`/plans/${planId}/rebalance`}
              className="inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm"
            >
              返回持仓预览
            </Link>
          </div>
        </section>
      )}

      {step === 1 && targets.data && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <h2 className="font-medium">配置确认</h2>
          <label className="block text-sm">
            目标计划
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={planId}
              onChange={(e) => switchPlan(e.target.value)}
              data-testid="asset-refresh-plan-select"
            >
              {(plans.data ?? []).map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <p className="text-sm text-slate-600">
            当前计划：<strong>{plan.data.name}</strong>
          </p>
          <p className="text-sm text-slate-600">
            绑定场景模板：<strong>{scenarioName}</strong>
          </p>
          <div>
            <h3 className="text-sm font-medium">大类目标（只读）</h3>
            <ul className="mt-2 text-sm text-slate-700">
              {targets.data.asset_class_targets.map((target) => (
                <li key={target.asset_class}>
                  {assetClassLabel(target.asset_class)} {formatPercent(target.weight)}
                </li>
              ))}
            </ul>
          </div>
          <p className="text-sm text-slate-600">
            已启用标的 {enabledRows.length} 个
          </p>
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="min-h-11 rounded-md border border-slate-300 px-4 text-sm"
              onClick={() => setStep(0)}
            >
              上一步
            </button>
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white"
              onClick={() => setStep(2)}
            >
              下一步
            </button>
          </div>
        </section>
      )}

      {step === 2 && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-medium">录入当前资产</h2>
            <button
              type="button"
              className="min-h-11 rounded-md border border-slate-300 px-4 text-sm"
              data-testid="asset-refresh-add-instrument"
              onClick={() => setDrawerOpen(true)}
            >
              添加标的
            </button>
          </div>
          {groupedHoldings.map(({ assetClass, regions }) => (
            <div key={assetClass} className="rounded-md border border-slate-200">
              <h3 className="border-b bg-slate-50 px-3 py-2 text-sm font-medium">
                {assetClassLabel(assetClass)}
              </h3>
              {regions.map(({ region, rows: regionRows }) => (
                <div key={`${assetClass}:${region}`} className="border-t">
                  <h4 className="bg-slate-50/80 px-3 py-1.5 text-xs font-medium text-slate-600">
                    {regionLabel(region)}
                  </h4>
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-sm">
                      <thead>
                        <tr className="text-left text-slate-500">
                          <th className="px-3 py-2">启用</th>
                          <th className="px-3 py-2">标的</th>
                          <th className="px-3 py-2">分类</th>
                          <th className="px-3 py-2">国别</th>
                          <th className="px-3 py-2">当前金额</th>
                          <th className="px-3 py-2">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {regionRows.map((row) => (
                          <tr
                            key={row.instrument_id}
                            className={`border-t ${row.enabled ? "" : "bg-slate-50/60 text-slate-500"}`}
                          >
                            <td className="px-3 py-2">
                              <input
                                type="checkbox"
                                checked={row.enabled}
                                aria-label={`${row.label} 启用`}
                                onChange={(event) => toggleEnabled(row, event.target.checked)}
                              />
                            </td>
                            <td className="px-3 py-2">
                              <span className="font-medium">{row.label}</span>
                              <span className="block text-xs text-slate-500">{row.code}</span>
                            </td>
                            <td className="px-3 py-2">{assetClassLabel(row.asset_class)}</td>
                            <td className="px-3 py-2">{regionLabel(row.region)}</td>
                            <td className="px-3 py-2">
                              <MoneyInput
                                plain
                                disabled={!row.enabled}
                                valueMinor={row.current_amount_minor}
                                onChange={(value) =>
                                  updateHolding(row.instrument_id, { current_amount_minor: value })
                                }
                              />
                            </td>
                            <td className="px-3 py-2">
                              {!row.is_system ? (
                                <button
                                  type="button"
                                  className="text-xs text-red-700 underline"
                                  onClick={() => removeHolding(row)}
                                >
                                  移除
                                </button>
                              ) : (
                                <span className="text-xs text-slate-400">—</span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              ))}
            </div>
          ))}
          <div className="flex flex-wrap items-end gap-3">
            <MoneyInput
              label="资产总值"
              plain
              valueMinor={totalAssets}
              currency={plan.data.base_currency}
              onChange={setTotalOverride}
            />
            <button
              type="button"
              className="min-h-11 rounded-md border border-slate-300 px-4 text-sm text-slate-700"
              onClick={() => setTotalOverride(sumMinor)}
            >
              使用分项合计 {formatMoney(sumMinor, plan.data.base_currency)}
            </button>
          </div>
          {sumMinor === totalAssets && (
            <p className="text-sm text-slate-600">分项合计与资产总值一致。</p>
          )}
          {!validation.ok && (
            <p className="text-sm text-red-700">{validation.message}</p>
          )}
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="min-h-11 rounded-md border border-slate-300 px-4 text-sm"
              onClick={() => setStep(1)}
            >
              上一步
            </button>
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
              disabled={!validation.ok}
              onClick={() => setStep(3)}
            >
              下一步
            </button>
          </div>
        </section>
      )}

      {step === 3 && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <h2 className="font-medium">确认提交</h2>
          <dl className="grid gap-2 text-sm text-slate-700 sm:grid-cols-2">
            <div>
              <dt className="text-slate-500">影响计划</dt>
              <dd className="font-medium">{plan.data.name}</dd>
            </div>
            <div>
              <dt className="text-slate-500">影响资产数量</dt>
              <dd className="font-medium">{enabledRows.length} 个</dd>
            </div>
          </dl>
          {structureOnly ? (
            <p className="text-sm text-slate-700">
              变更前合计 {formatMoney(beforeTotal, plan.data.base_currency)}，变更后合计{" "}
              {formatMoney(totalAssets, plan.data.base_currency)}。
              本次变更未改变资产总值，仅更新了持仓结构或资产分配。
            </p>
          ) : (
            <p className="text-sm text-slate-700">
              变更前合计 {formatMoney(beforeTotal, plan.data.base_currency)} → 变更后合计{" "}
              {formatMoney(totalAssets, plan.data.base_currency)}
            </p>
          )}
          {structureChanged && (
            <p className="text-sm text-slate-600">本次提交包含标的结构变更（新增、移除或启停）。</p>
          )}
          <p className="text-sm text-slate-600">
            提交后，当前计划总资产将同步更新为最新持仓合计。
          </p>
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="min-h-11 rounded-md border border-slate-300 px-4 text-sm"
              onClick={() => setStep(2)}
            >
              上一步
            </button>
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
              disabled={submit.isPending}
              onClick={() => submit.mutate()}
            >
              提交资产变更
            </button>
          </div>
        </section>
      )}

      {drawerOpen && (
        <div className="fixed inset-0 z-50 flex justify-end bg-black/30">
          <div className="flex h-full w-full max-w-md flex-col bg-white shadow-xl">
            <div className="flex items-center justify-between border-b p-4">
              <h3 className="font-medium">选择标的</h3>
              <button type="button" onClick={() => setDrawerOpen(false)}>
                关闭
              </button>
            </div>
            <div className="p-4">
              <input
                className="w-full rounded-md border px-3 py-2 text-sm"
                placeholder="按代码、名称过滤"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                data-testid="asset-refresh-instrument-filter"
              />
              <Link href="/assets/import" className="mt-2 block text-sm underline">
                资料库中不存在？从 AKShare 录入
              </Link>
            </div>
            <ul className="flex-1 divide-y overflow-y-auto" data-testid="asset-refresh-instrument-results">
              {filteredInstruments.map((instrument) => (
                <li key={instrument.id}>
                  <button
                    type="button"
                    className="w-full px-4 py-3 text-left hover:bg-slate-50"
                    onClick={() => addInstrument(instrument)}
                  >
                    <div className="font-medium">{instrument.name}</div>
                    <div className="text-xs text-slate-500">
                      {instrument.code} · {assetClassLabel(instrument.asset_class)} ·{" "}
                      {regionLabel(instrument.region)}
                    </div>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}

function updateDraftHoldings(
  holdings: AssetRefreshHolding[],
  instrumentId: string,
  patch: Partial<AssetRefreshHolding>,
): AssetRefreshHolding[] {
  return holdings.map((holding) =>
    holding.instrument_id === instrumentId ? { ...holding, ...patch } : holding,
  );
}
