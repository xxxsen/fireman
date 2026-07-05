"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/Button";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { Dialog } from "@/components/ui/Dialog";
import { Stepper } from "@/components/ui/Stepper";
import { PlanPageHeader } from "@/components/layout/PlanPageHeader";
import { getHoldings, getTargets } from "@/lib/api/holdings";
import { getActiveRebalanceExecution } from "@/lib/api/rebalance-executions";
import { submitAssetRefresh } from "@/lib/api/asset-refresh";
import { listMarketAssets, type MarketAsset } from "@/lib/api/market-assets";
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
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { queryErrorMessage } from "@/lib/query-error";

const STEPS = ["确认范围", "录入当前资产", "确认提交"] as const;
const ASSET_CLASSES = ["equity", "bond", "cash"] as const;

/** System cash holdings reference built-in `SYS|…` market assets. */
function isSystemAssetKey(assetKey: string): boolean {
  return assetKey.startsWith("SYS|");
}

/** A query that is only letters/digits/dots is treated as a symbol search. */
function looksLikeSymbolQuery(q: string): boolean {
  return /^[A-Za-z0-9.]+$/.test(q);
}

export default function AssetRefreshPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const queryClient = useQueryClient();
  const [step, setStep] = useState(0);
  const [holdingsDraft, setHoldingsDraft] = useState<AssetRefreshHolding[] | null>(null);
  const [totalOverride, setTotalOverride] = useState<number | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [addAssetClass, setAddAssetClass] = useState("equity");
  const [addRegion, setAddRegion] = useState("domestic");
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
  const activeExecution = useQuery({
    queryKey: ["rebalance-execution-active", planId],
    queryFn: () => getActiveRebalanceExecution(planId),
  });

  const trimmedFilter = filter.trim();
  const assetSearch = useQuery({
    queryKey: ["asset-refresh-market-assets", trimmedFilter],
    queryFn: () =>
      listMarketAssets({
        symbolQ: looksLikeSymbolQuery(trimmedFilter) ? trimmedFilter : undefined,
        nameQ: looksLikeSymbolQuery(trimmedFilter) ? undefined : trimmedFilter,
        limit: 20,
      }),
    enabled: dialogOpen && trimmedFilter.length > 0,
  });

  const defaultHoldings = useMemo(
    () =>
      (holdings.data?.holdings ?? []).map((holding) =>
        holdingFromPlan(holding, isSystemAssetKey(holding.asset_key)),
      ),
    [holdings.data],
  );

  const draftHoldings = holdingsDraft ?? defaultHoldings;
  const defaultTotal = useMemo(
    () => draftHoldings.reduce((sum, holding) => sum + holding.current_amount_minor, 0),
    [draftHoldings],
  );
  const totalAssets = totalOverride ?? defaultTotal;
  const sumMinor = useMemo(
    () => sumHoldingsMinor(draftHoldings.map((row) => ({
      asset_key: row.asset_key,
      current_amount_minor: row.current_amount_minor,
    }))),
    [draftHoldings],
  );
  const validation = useMemo(
    () => validateAssetRefreshTotal(
      draftHoldings.map((row) => ({
        asset_key: row.asset_key,
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

  const hasChanges = useMemo(
    () =>
      holdings.data
        ? hasAssetRefreshDraftChanges(holdings.data.holdings, draftHoldings, totalAssets)
        : false,
    [holdings.data, draftHoldings, totalAssets],
  );

  // The wizard shows the plan's config template read-only; changing the
  // template is a plan-settings responsibility, not part of holdings entry.
  const selectedScenario = useMemo(() => {
    if (!initialScenarioId) return undefined;
    return scenarios.data?.scenarios.find((scenario) => scenario.id === initialScenarioId);
  }, [initialScenarioId, scenarios.data]);

  const previewAssetTargets = targets.data?.asset_class_targets ?? [];
  const previewRegionTargets = targets.data?.region_targets ?? [];

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

  const selectedAssetKeys = useMemo(
    () => new Set(draftHoldings.map((holding) => holding.asset_key)),
    [draftHoldings],
  );

  const candidateAssets = useMemo(
    () =>
      (assetSearch.data?.assets ?? []).filter(
        (asset) => !selectedAssetKeys.has(asset.asset_key),
      ),
    [assetSearch.data, selectedAssetKeys],
  );

  const updateDraft = (next: AssetRefreshHolding[]) => {
    setHoldingsDraft(next);
  };

  const updateHolding = (assetKey: string, patch: Partial<AssetRefreshHolding>) => {
    updateDraft(
      draftHoldings.map((holding) =>
        holding.asset_key === assetKey ? { ...holding, ...patch } : holding,
      ),
    );
  };

  const removeHolding = (holding: AssetRefreshHolding) => {
    if (holding.is_system) return;
    updateDraft(draftHoldings.filter((item) => item.asset_key !== holding.asset_key));
  };

  const addAsset = (asset: MarketAsset) => {
    if (selectedAssetKeys.has(asset.asset_key)) return;
    const defaultWeight = defaultWeightWithinGroup(draftHoldings, addAssetClass, addRegion);
    updateDraft([
      ...draftHoldings,
      {
        id: `draft_${asset.asset_key}`,
        asset_key: asset.asset_key,
        label: asset.name,
        code: asset.symbol,
        asset_class: addAssetClass,
        region: addRegion,
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

      return submitAssetRefresh(
        planId,
        buildAssetRefreshBody(
          plan.data.config_version,
          draftHoldings,
          totalAssets,
          true,
          structureChanged,
          null,
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

  if (
    (plan.isError ||
      holdings.isError ||
      targets.isError ||
      activeExecution.isError) &&
    (!plan.data ||
      !holdings.data ||
      !targets.data ||
      !activeExecution.data)
  ) {
    return (
      <ErrorState
        message="无法加载持仓校正数据。请确认后端服务可用后重试。"
        onRetry={() => {
          if (plan.isError) void plan.refetch();
          if (holdings.isError) void holdings.refetch();
          if (targets.isError) void targets.refetch();
          if (activeExecution.isError) void activeExecution.refetch();
        }}
        backHref={`/plans/${planId}/overview`}
        backLabel="返回总览"
        technicalDetail={queryErrorMessage(
          plan.error ??
            holdings.error ??
            targets.error ??
            activeExecution.error,
        )}
      />
    );
  }

  if (
    plan.isLoading ||
    holdings.isLoading ||
    activeExecution.isLoading ||
    targets.isLoading ||
    !plan.data ||
    !holdings.data ||
    !targets.data
  ) {
    return <LoadingState label="加载持仓校正…" />;
  }

  if (activeExecution.data?.execution) {
    const executionId = activeExecution.data.execution.id;
    return (
      <div className="content-enter space-y-4">
        <h1 className="text-xl font-semibold">持仓校正</h1>
        <div
          className="rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-sm text-warning"
          data-testid="asset-refresh-blocked"
        >
          调仓执行进行中，完成或放弃后才可进行持仓校正。
          <Link
            href={`/plans/${planId}/rebalance/executions/${executionId}`}
            className="ml-2 font-medium underline"
          >
            继续调仓执行
          </Link>
        </div>
        <Link href={`/plans/${planId}/rebalance`} className="text-sm underline">
          返回调仓工作台
        </Link>
      </div>
    );
  }

  const beforeTotal = sumHoldingsMinor(
    holdings.data.holdings.map((holding) => ({
      asset_key: holding.asset_key,
      current_amount_minor: holding.current_amount_minor,
    })),
  );
  const structureOnly = hasChanges && beforeTotal === totalAssets && changeCount > 0;
  const scenarioName = selectedScenario?.name ?? "—";

  return (
    <div className="content-enter mx-auto max-w-3xl space-y-6 pb-16">
      <PlanPageHeader
        title="持仓校正"
        description="按券商账户实际情况更新真实持仓结构与金额，提交后更新持仓事实并同步计划基准规模。"
      />

      <Stepper steps={STEPS} current={step} />

      {error && (
        <div className="rounded-md border border-danger/30 bg-danger/5 px-4 py-3 text-sm text-danger" role="alert">
          {error}
        </div>
      )}

      {step === 0 && targets.data && (
        <section className="space-y-4 rounded-lg border border-line p-6">
          <h2 className="font-medium">确认范围</h2>
          <p className="text-sm text-ink">
            此流程用于维护当前计划下的真实持仓：新增或移除标的、更新各资产当前金额与组内配比；提交后覆盖持仓事实，计划基准规模同步为最新持仓合计。
          </p>
          <dl className="grid gap-2 text-sm text-ink sm:grid-cols-2">
            <div>
              <dt className="text-ink-muted">当前计划</dt>
              <dd className="font-medium">{plan.data.name}</dd>
            </div>
            <div>
              <dt className="text-ink-muted">配置模板（只读）</dt>
              <dd className="font-medium" data-testid="asset-refresh-scenario-name">
                {scenarioName}
                <Link
                  href={`/plans/${planId}/settings?section=plan-targets`}
                  className="ml-2 text-sm font-normal text-brand underline underline-offset-2"
                >
                  前往计划设置修改
                </Link>
              </dd>
            </div>
            <div>
              <dt className="text-ink-muted">当前标的</dt>
              <dd className="font-medium">{draftHoldings.length} 个</dd>
            </div>
          </dl>
          <div>
            <h3 className="text-sm font-medium">大类目标（只读）</h3>
            <ul className="mt-2 text-sm text-ink">
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
                <ul className="mt-2 text-sm text-ink">
                  {regions.map((target) => (
                    <li key={`${target.asset_class}:${target.region}`}>
                      {regionLabel(target.region)} {formatPercent(target.weight_within_class)}
                    </li>
                  ))}
                </ul>
              </div>
            );
          })}
          <div className="flex flex-wrap gap-3">
            <Button size="lg" onClick={() => setStep(1)}>
              下一步
            </Button>
            <Button
              variant="secondary"
              size="lg"
              href={`/plans/${planId}/rebalance`}
            >
              返回调仓工作台
            </Button>
          </div>
        </section>
      )}

      {step === 1 && (
        <section className="space-y-4 rounded-lg border border-line p-6">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-medium">录入当前资产</h2>
            <Button
              variant="secondary"
              size="lg"
              data-testid="asset-refresh-add-instrument"
              onClick={() => setDialogOpen(true)}
            >
              添加标的
            </Button>
          </div>
          {groupedHoldings.map(({ assetClass, regions }) => (
            <div key={assetClass} className="rounded-md border border-line">
              <h3 className="border-b border-line bg-surface-muted px-3 py-2 text-sm font-medium">
                {assetClassLabel(assetClass)}
              </h3>
              {regions.map(({ region, rows: regionRows }) => (
                <div key={`${assetClass}:${region}`} className="border-t border-line">
                  <h4 className="bg-surface-muted/80 px-3 py-1.5 text-xs font-medium text-ink-muted">
                    {regionLabel(region)}
                  </h4>
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-sm">
                      <thead>
                        <tr className="text-left text-ink-muted">
                          <th scope="col" className="px-3 py-2">标的</th>
                          <th scope="col" className="px-3 py-2">分类</th>
                          <th scope="col" className="px-3 py-2">国别</th>
                          <th scope="col" className="px-3 py-2">组内配比</th>
                          <th scope="col" className="px-3 py-2">当前金额</th>
                          <th scope="col" className="px-3 py-2">操作</th>
                        </tr>
                      </thead>
                      <tbody>
                        {regionRows.map((row) => (
                          <tr key={row.asset_key} className="border-t border-line">
                            <td className="px-3 py-2">
                              <span className="font-medium">{row.label}</span>
                              <span className="block text-xs text-ink-muted">{row.code}</span>
                            </td>
                            <td className="px-3 py-2">{assetClassLabel(row.asset_class)}</td>
                            <td className="px-3 py-2">{regionLabel(row.region)}</td>
                            <td className="px-3 py-2">
                              <PercentInput
                                value={row.weight_within_group}
                                onChange={(value) =>
                                  updateHolding(row.asset_key, { weight_within_group: value })
                                }
                              />
                            </td>
                            <td className="px-3 py-2">
                              <MoneyInput
                                plain
                                valueMinor={row.current_amount_minor}
                                onChange={(value) =>
                                  updateHolding(row.asset_key, { current_amount_minor: value })
                                }
                              />
                            </td>
                            <td className="px-3 py-2">
                              {!row.is_system ? (
                                <Button
                                  variant="ghost"
                                  className="px-2 py-1 text-xs text-danger"
                                  onClick={() => removeHolding(row)}
                                >
                                  移除
                                </Button>
                              ) : (
                                <span className="text-xs text-ink-muted">—</span>
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
            <span className="mb-1 block text-sm text-ink-muted">计划基准规模（提交后同步）</span>
            <div className="flex items-center gap-3">
              <MoneyInput
                plain
                valueMinor={totalAssets}
                currency={plan.data.base_currency}
                onChange={setTotalOverride}
              />
              <Button
                variant="secondary"
                size="lg"
                className="shrink-0"
                onClick={() => setTotalOverride(sumMinor)}
              >
                使用分项合计 {formatMoney(sumMinor, plan.data.base_currency)}
              </Button>
            </div>
          </div>
          {sumMinor === totalAssets && (
            <p className="text-sm text-ink-muted">分项合计与基准规模一致。</p>
          )}
          <div role="alert" className="space-y-1">
            {!validation.ok && (
              <p className="text-sm text-danger">{validation.message}</p>
            )}
            {!groupWeightValidation.ok && (
              <p className="text-sm text-danger" data-testid="asset-refresh-group-weight-error">
                {groupWeightValidation.message}
              </p>
            )}
          </div>
          <div className="flex flex-wrap gap-3">
            <Button variant="secondary" size="lg" onClick={() => setStep(0)}>
              上一步
            </Button>
            <Button size="lg" disabled={!canProceedFromEntry} onClick={() => setStep(2)}>
              下一步
            </Button>
          </div>
        </section>
      )}

      {step === 2 && (
        <section className="space-y-4 rounded-lg border border-line p-6">
          <h2 className="font-medium">确认提交</h2>
          <dl className="grid gap-2 text-sm text-ink sm:grid-cols-2">
            <div>
              <dt className="text-ink-muted">影响计划</dt>
              <dd className="font-medium">{plan.data.name}</dd>
            </div>
            <div>
              <dt className="text-ink-muted">影响资产数量</dt>
              <dd className="font-medium" data-testid="asset-refresh-change-count">
                {changeCount === 0 ? "0" : `${changeCount} 项`}
              </dd>
            </div>
          </dl>
          {changeCount === 0 && !hasChanges && (
            <p className="text-sm text-warning" data-testid="asset-refresh-no-changes">
              本次未修改任何资产，无需提交。
            </p>
          )}
          {structureOnly ? (
            <p className="text-sm text-ink">
              变更前合计 {formatMoney(beforeTotal, plan.data.base_currency)}，变更后合计{" "}
              {formatMoney(totalAssets, plan.data.base_currency)}。
              本次变更未改变基准规模，仅更新了持仓结构或资产分配。
            </p>
          ) : (
            <p className="text-sm text-ink">
              变更前合计 {formatMoney(beforeTotal, plan.data.base_currency)} → 变更后合计{" "}
              {formatMoney(totalAssets, plan.data.base_currency)}
            </p>
          )}
          {structureChanged && (
            <p className="text-sm text-ink-muted">
              本次提交包含持仓配置变更（新增、移除或组内配比调整）。
            </p>
          )}
          <p className="text-sm text-ink-muted">
            提交后，当前计划基准规模将同步更新为最新持仓合计。若新增标的尚未同步历史数据，
            创建模拟前可在计划的模拟入口一键同步缺失历史。
          </p>
          <div className="flex flex-wrap gap-3">
            <Button variant="secondary" size="lg" onClick={() => setStep(1)}>
              上一步
            </Button>
            <Button
              size="lg"
              pending={submit.isPending}
              disabled={!hasChanges}
              onClick={() => submit.mutate()}
            >
              提交持仓校正
            </Button>
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
          className="input-base text-sm"
          placeholder="按代码或名称搜索市场资产目录"
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          data-testid="asset-refresh-instrument-filter"
        />
        <div className="mt-2 flex gap-2">
          <label className="flex flex-1 flex-col gap-1 text-xs text-ink-muted">
            资产大类
            <select
              className="input-base text-sm"
              value={addAssetClass}
              onChange={(event) => setAddAssetClass(event.target.value)}
              data-testid="asset-refresh-add-asset-class"
            >
              {ASSET_CLASSES.map((assetClass) => (
                <option key={assetClass} value={assetClass}>
                  {assetClassLabel(assetClass)}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-1 flex-col gap-1 text-xs text-ink-muted">
            国内 / 国外
            <select
              className="input-base text-sm"
              value={addRegion}
              onChange={(event) => setAddRegion(event.target.value)}
              data-testid="asset-refresh-add-region"
            >
              <option value="domestic">{regionLabel("domestic")}</option>
              <option value="foreign">{regionLabel("foreign")}</option>
            </select>
          </label>
        </div>
        <Link href="/assets" className="mt-2 block text-sm underline">
          目录中不存在？前往资产页同步资产列表
        </Link>
        {assetSearch.isFetching && (
          <p className="mt-2 text-xs text-ink-muted" role="status">
            搜索中…
          </p>
        )}
        <ul className="mt-4 divide-y divide-line" data-testid="asset-refresh-instrument-results">
          {candidateAssets.map((asset) => (
            <li key={asset.asset_key}>
              <button
                type="button"
                className="w-full px-1 py-3 text-left hover:bg-surface-muted"
                onClick={() => addAsset(asset)}
              >
                <div className="font-medium">{asset.name}</div>
                <div className="text-xs text-ink-muted">
                  {asset.symbol} · {asset.market}
                  {asset.has_history
                    ? ` · 数据截至 ${asset.history_data_as_of || "—"}`
                    : " · 未同步历史，模拟前需要同步"}
                </div>
              </button>
            </li>
          ))}
        </ul>
      </Dialog>
    </div>
  );
}
