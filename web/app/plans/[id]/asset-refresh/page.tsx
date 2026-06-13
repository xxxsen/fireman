"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { Dialog } from "@/components/ui/Dialog";
import { getHoldings, getTargets } from "@/lib/api/holdings";
import { submitAssetRefresh } from "@/lib/api/asset-refresh";
import { listInstruments } from "@/lib/api/instruments";
import { getPlan, getParameters } from "@/lib/api/plans";
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
  countAssetRefreshChanges,
  defaultWeightWithinGroup,
  hasAssetRefreshDraftChanges,
  hasAssetRefreshStructureChange,
  holdingFromPlan,
  sumHoldingsMinor,
  validateAssetRefreshGroupWeights,
  validateAssetRefreshTotal,
  type AssetRefreshHolding,
} from "@/lib/asset-refresh";
import { ApiError } from "@/lib/api/client";
import type { Instrument } from "@/types/api";

const STEPS = ["说明", "配置确认", "录入当前资产", "确认提交"] as const;
const ASSET_CLASSES = ["equity", "bond", "cash"] as const;

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
  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedScenarioId, setSelectedScenarioId] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [error, setError] = useState<string | null>(null);

  const plan = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
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
  const defaultTotal = useMemo(
    () => draftHoldings.reduce((sum, holding) => sum + holding.current_amount_minor, 0),
    [draftHoldings],
  );
  const totalAssets = totalOverride ?? defaultTotal;
  const sumMinor = useMemo(
    () => sumHoldingsMinor(draftHoldings.map((row) => ({
      instrument_id: row.instrument_id,
      current_amount_minor: row.current_amount_minor,
    }))),
    [draftHoldings],
  );
  const validation = useMemo(
    () => validateAssetRefreshTotal(
      draftHoldings.map((row) => ({
        instrument_id: row.instrument_id,
        current_amount_minor: row.current_amount_minor,
      })),
      totalAssets,
    ),
    [draftHoldings, totalAssets],
  );
  const groupWeightValidation = useMemo(
    () => validateAssetRefreshGroupWeights(draftHoldings),
    [draftHoldings],
  );
  const canProceedFromEntry =
    validation.ok && groupWeightValidation.ok && draftHoldings.length > 0;
  const structureChanged = useMemo(
    () =>
      holdings.data
        ? hasAssetRefreshStructureChange(holdings.data.holdings, draftHoldings)
        : false,
    [holdings.data, draftHoldings],
  );
  const changeCount = useMemo(
    () =>
      holdings.data
        ? countAssetRefreshChanges(holdings.data.holdings, draftHoldings)
        : 0,
    [holdings.data, draftHoldings],
  );

  const initialScenarioId = parameters.data?.parameters.selected_scenario_id ?? "";
  const currentScenarioId = selectedScenarioId ?? initialScenarioId;

  const hasChanges = useMemo(() => {
    const scenarioChanged =
      !!currentScenarioId && currentScenarioId !== initialScenarioId;
    const holdingsChanged = holdings.data
      ? hasAssetRefreshDraftChanges(holdings.data.holdings, draftHoldings, totalAssets)
      : false;
    return scenarioChanged || holdingsChanged;
  }, [currentScenarioId, initialScenarioId, holdings.data, draftHoldings, totalAssets]);

  const previewScenario = useMemo(() => {
    if (!currentScenarioId) return undefined;
    return scenarios.data?.scenarios.find((scenario) => scenario.id === currentScenarioId);
  }, [currentScenarioId, scenarios.data]);

  const selectedScenario = useMemo(() => {
    if (!initialScenarioId) return undefined;
    return scenarios.data?.scenarios.find((scenario) => scenario.id === initialScenarioId);
  }, [initialScenarioId, scenarios.data]);

  const previewAssetTargets =
    previewScenario?.weights ?? targets.data?.asset_class_targets ?? [];
  const previewRegionTargets =
    previewScenario?.region_targets ?? targets.data?.region_targets ?? [];

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

  const switchScenario = (nextScenarioId: string) => {
    setSelectedScenarioId(nextScenarioId);
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

  const removeHolding = (holding: AssetRefreshHolding) => {
    if (holding.is_system) return;
    updateDraft(draftHoldings.filter((item) => item.instrument_id !== holding.instrument_id));
  };

  const addInstrument = (instrument: Instrument) => {
    if (selectedInstrumentIds.has(instrument.id)) return;
    const defaultWeight = defaultWeightWithinGroup(
      draftHoldings,
      instrument.asset_class,
      instrument.region,
    );
    updateDraft([
      ...draftHoldings,
      {
        id: `draft_${instrument.id}`,
        instrument_id: instrument.id,
        label: instrument.name,
        code: instrument.code,
        asset_class: instrument.asset_class,
        region: instrument.region,
        current_amount_minor: 0,
        weight_within_group: defaultWeight,
        sort_order: draftHoldings.length * 10,
        is_system: false,
      },
    ]);
    setFilter("");
    setDialogOpen(false);
  };

  const submit = useMutation({
    mutationFn: async () => {
      if (!plan.data) throw new Error("计划尚未加载");
      if (!validation.ok) throw new Error(validation.message ?? "校验失败");
      if (!groupWeightValidation.ok) {
        throw new Error(groupWeightValidation.message ?? "组内配比校验失败");
      }

      const scenarioChanged =
        !!currentScenarioId && currentScenarioId !== initialScenarioId;
      const configChanged = structureChanged || scenarioChanged;

      return submitAssetRefresh(
        planId,
        buildAssetRefreshBody(
          plan.data.config_version,
          draftHoldings,
          totalAssets,
          true,
          configChanged,
          scenarioChanged ? currentScenarioId : null,
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
    holdings.data.holdings.map((holding) => ({
      instrument_id: holding.instrument_id,
      current_amount_minor: holding.current_amount_minor,
    })),
  );
  const structureOnly = hasChanges && beforeTotal === totalAssets && changeCount > 0;
  const scenarioName = previewScenario?.name ?? selectedScenario?.name ?? "—";

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
              <li>新增或移除资产标的</li>
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
          <p className="text-sm text-slate-600">
            当前计划：<strong>{plan.data.name}</strong>
          </p>
          <label className="block text-sm">
            FIRE 方案
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={currentScenarioId}
              onChange={(e) => switchScenario(e.target.value)}
              data-testid="asset-refresh-scenario-select"
            >
              <option value="">—</option>
              {(scenarios.data?.scenarios ?? []).map((scenario) => (
                <option key={scenario.id} value={scenario.id}>
                  {scenario.name}
                </option>
              ))}
            </select>
          </label>
          <p className="text-sm text-slate-600">
            当前选择的 FIRE 方案 / 场景模板：<strong>{scenarioName}</strong>
          </p>
          <div>
            <h3 className="text-sm font-medium">大类目标（只读）</h3>
            <ul className="mt-2 text-sm text-slate-700">
              {previewAssetTargets.map((target) => (
                <li key={target.asset_class}>
                  {assetClassLabel(target.asset_class)} {formatPercent(target.weight)}
                </li>
              ))}
            </ul>
          </div>
          {ASSET_CLASSES.map((assetClass) => {
            const regions = previewRegionTargets.filter(
              (target) => target.asset_class === assetClass,
            );
            if (regions.length === 0) return null;
            return (
              <div key={assetClass}>
                <h3 className="text-sm font-medium">
                  {assetClassLabel(assetClass)} · 地区组内目标（只读）
                </h3>
                <ul className="mt-2 text-sm text-slate-700">
                  {regions.map((target) => (
                    <li key={`${target.asset_class}:${target.region}`}>
                      {regionLabel(target.region)} {formatPercent(target.weight_within_class)}
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
          <p className="text-sm text-slate-600">
            当前标的 {draftHoldings.length} 个
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
              onClick={() => setDialogOpen(true)}
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
                          <th className="px-3 py-2">标的</th>
                          <th className="px-3 py-2">分类</th>
                          <th className="px-3 py-2">国别</th>
                          <th className="px-3 py-2">组内配比</th>
                          <th className="px-3 py-2">当前金额</th>
                          <th className="px-3 py-2">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {regionRows.map((row) => (
                          <tr key={row.instrument_id} className="border-t">
                            <td className="px-3 py-2">
                              <span className="font-medium">{row.label}</span>
                              <span className="block text-xs text-slate-500">{row.code}</span>
                            </td>
                            <td className="px-3 py-2">{assetClassLabel(row.asset_class)}</td>
                            <td className="px-3 py-2">{regionLabel(row.region)}</td>
                            <td className="px-3 py-2">
                              <PercentInput
                                value={row.weight_within_group}
                                onChange={(value) =>
                                  updateHolding(row.instrument_id, { weight_within_group: value })
                                }
                              />
                            </td>
                            <td className="px-3 py-2">
                              <MoneyInput
                                plain
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
          <div>
            <span className="mb-1 block text-sm text-ink-muted">资产总值</span>
            <div className="flex items-center gap-3">
              <MoneyInput
                plain
                valueMinor={totalAssets}
                currency={plan.data.base_currency}
                onChange={setTotalOverride}
              />
              <button
                type="button"
                className="min-h-11 shrink-0 rounded-md border border-slate-300 px-4 text-sm text-slate-700"
                onClick={() => setTotalOverride(sumMinor)}
              >
                使用分项合计 {formatMoney(sumMinor, plan.data.base_currency)}
              </button>
            </div>
          </div>
          {sumMinor === totalAssets && (
            <p className="text-sm text-slate-600">分项合计与资产总值一致。</p>
          )}
          {!validation.ok && (
            <p className="text-sm text-red-700">{validation.message}</p>
          )}
          {!groupWeightValidation.ok && (
            <p className="text-sm text-red-700" data-testid="asset-refresh-group-weight-error">
              {groupWeightValidation.message}
            </p>
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
              disabled={!canProceedFromEntry}
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
              <dd className="font-medium" data-testid="asset-refresh-change-count">
                {changeCount === 0 ? "0" : `${changeCount} 项`}
              </dd>
            </div>
          </dl>
          {changeCount === 0 && !hasChanges && (
            <p className="text-sm text-amber-800" data-testid="asset-refresh-no-changes">
              本次未修改任何资产，无需提交。
            </p>
          )}
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
            <p className="text-sm text-slate-600">
              本次提交包含持仓配置变更（新增、移除或组内配比调整）。
            </p>
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
              disabled={submit.isPending || !hasChanges}
              onClick={() => submit.mutate()}
            >
              提交资产变更
            </button>
          </div>
        </section>
      )}

      <Dialog
        open={dialogOpen}
        onClose={() => setDialogOpen(false)}
        title="选择标的"
        className="max-w-md"
      >
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
        <ul className="mt-4 divide-y" data-testid="asset-refresh-instrument-results">
          {filteredInstruments.map((instrument) => (
            <li key={instrument.id}>
              <button
                type="button"
                className="w-full px-1 py-3 text-left hover:bg-slate-50"
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
      </Dialog>
    </div>
  );
}
