"use client";

import Link from "next/link";
import { useParams, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { SaveBar } from "@/components/ui/SaveBar";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import {
  getHoldings,
  getTargets,
  syncHoldingSnapshot,
  updateHoldings,
} from "@/lib/api/holdings";
import { listInstruments } from "@/lib/api/instruments";
import { getPlan } from "@/lib/api/plans";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
  qualityStatusLabel,
  regionLabel,
} from "@/lib/format";
import { assetClassSortIndex, regionSortIndex } from "@/lib/asset-class-order";
import { validatePercentSum } from "@/lib/percent";
import type { Instrument, PlanHolding } from "@/types/api";
import { ApiError } from "@/lib/api/client";

export default function HoldingsPage() {
  const planId = useParams().id as string;
  const highlight = useSearchParams().get("highlight");
  const assetRefreshed = useSearchParams().get("asset_refreshed") === "1";
  const queryClient = useQueryClient();
  const { dirty, markDirty, markClean } = usePlanEdit();
  const [editedRows, setEditedRows] = useState<PlanHolding[] | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [saveError, setSaveError] = useState<string | null>(null);
  const [savedMessage, setSavedMessage] = useState<string | null>(null);
  const [highlightedId, setHighlightedId] = useState<string | null>(null);
  const rowRefs = useRef(new Map<string, HTMLTableRowElement>());

  const plan = useQuery({
    queryKey: ["plan", planId],
    queryFn: () => getPlan(planId),
  });
  const holdings = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });
  const instruments = useQuery({
    queryKey: ["instruments", plan.data?.valuation_date],
    queryFn: () =>
      listInstruments(
        plan.data?.valuation_date
          ? { valuationDate: plan.data.valuation_date }
          : undefined,
      ),
    enabled: !!plan.data,
  });
  const targets = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });

  const serverRows = useMemo(
    () => holdings.data?.holdings.map((holding) => ({ ...holding })) ?? [],
    [holdings.data],
  );
  const rows = editedRows ?? serverRows;
  const displayRows = useMemo(() => rows, [rows]);

  useEffect(() => {
    if (!highlight || displayRows.length === 0) return;
    const element = rowRefs.current.get(highlight);
    if (!element) return;
    element.scrollIntoView({ behavior: "smooth", block: "center" });
    setHighlightedId(highlight);
    const timer = window.setTimeout(() => setHighlightedId(null), 2000);
    return () => window.clearTimeout(timer);
  }, [highlight, displayRows.length]);

  const instrumentById = useMemo(() => {
    const map = new Map<string, Instrument>();
    for (const instrument of instruments.data?.instruments ?? []) {
      map.set(instrument.id, instrument);
    }
    return map;
  }, [instruments.data]);
  const classGroups = useMemo(() => {
    const byClass = new Map<string, Map<string, PlanHolding[]>>();
    for (const holding of displayRows) {
      const regions = byClass.get(holding.asset_class) ?? new Map<string, PlanHolding[]>();
      const regionRows = regions.get(holding.region) ?? [];
      regionRows.push(holding);
      regions.set(holding.region, regionRows);
      byClass.set(holding.asset_class, regions);
    }

    const orderedClasses = [...byClass.keys()].sort(
      (left, right) => assetClassSortIndex(left) - assetClassSortIndex(right),
    );

    return orderedClasses.map((assetClass) => {
      const regions = byClass.get(assetClass)!;
      const orderedRegions = [...regions.keys()].sort(
        (left, right) => regionSortIndex(left) - regionSortIndex(right),
      );
      return {
        assetClass,
        classTarget:
          targets.data?.asset_class_targets?.find(
            (target) => target.asset_class === assetClass,
          )?.weight ?? 0,
        regions: orderedRegions.map((region) => ({
          region,
          rows: regions.get(region) ?? [],
        })),
      };
    });
  }, [displayRows, targets.data?.asset_class_targets]);

  const renderHoldingRows = (groupRows: PlanHolding[]) =>
    groupRows.map((holding) => {
      const instrument = instrumentById.get(holding.instrument_id);
      return (
        <tr
          key={holding.id}
          ref={(element) => {
            if (element) rowRefs.current.set(holding.id, element);
            else rowRefs.current.delete(holding.id);
          }}
          className={`border-t transition-colors ${
            highlightedId === holding.id ? "bg-amber-100" : ""
          }`}
        >
          <td className="px-3 py-2">
            <input
              type="checkbox"
              checked={holding.enabled}
              onChange={(event) =>
                updateRow(holding.id, { enabled: event.target.checked })
              }
            />
          </td>
          <td className="px-3 py-2">
            <span className="font-medium">
              {holding.instrument_name ??
                holding.instrument_code ??
                holding.instrument_id}
            </span>
            <span className="block text-xs text-slate-500">
              {holding.instrument_code} ·{" "}
              {qualityStatusLabel(
                instrument?.quality_status ?? instrument?.status ?? "—",
              )}
            </span>
          </td>
          <td className="px-3 py-2">
            <span className="tabular-nums">{formatPercent(holding.weight_within_group)}</span>
            <Link
              href={`/plans/${planId}/asset-refresh`}
              className="mt-1 block text-xs text-slate-600 underline"
            >
              在更新账户资产中调整
            </Link>
          </td>
          <td className="px-3 py-2">
            <span className="tabular-nums">
              {formatMoney(holding.current_amount_minor, plan.data?.base_currency)}
            </span>
            <Link
              href={`/plans/${planId}/asset-refresh`}
              className="mt-1 block text-xs text-slate-600 underline"
            >
              在更新账户资产中调整
            </Link>
          </td>
          <td className="px-3 py-2">
            <Link href={`/assets/${holding.instrument_id}`} className="block underline">
              查看资产
            </Link>
            {!holding.id.startsWith("draft_") && (
              <button
                type="button"
                className="mt-1 flex items-center gap-1 text-left text-xs text-slate-600 underline disabled:opacity-50"
                disabled={sync.isPending}
                onClick={() => sync.mutate(holding.id)}
              >
                刷新模拟历史数据
                <MetricHelp termKey="simulation_snapshot_sync" />
              </button>
            )}
          </td>
        </tr>
      );
    });

  const updateRow = (holdingId: string, patch: Partial<PlanHolding>) => {
    setEditedRows(
      displayRows.map((holding) =>
        holding.id === holdingId ? { ...holding, ...patch } : holding,
      ),
    );
    setSavedMessage(null);
    markDirty();
  };

  const addInstrument = (instrument: Instrument) => {
    if (displayRows.some((holding) => holding.instrument_id === instrument.id)) return;
    setEditedRows([
      ...displayRows,
      {
        id: `draft_${instrument.id}`,
        plan_id: planId,
        instrument_id: instrument.id,
        enabled: true,
        asset_class: instrument.asset_class,
        region: instrument.region,
        weight_within_group: 0,
        current_amount_minor: 0,
        simulation_snapshot_id: "",
        sort_order: displayRows.length * 10,
        instrument_code: instrument.code,
        instrument_name: instrument.name,
      },
    ]);
    markDirty();
    setSavedMessage(null);
    setDrawerOpen(false);
  };

  const save = useMutation({
    mutationFn: () => {
      if (!plan.data) throw new Error("计划尚未加载");
      return updateHoldings(planId, {
        config_version: plan.data.config_version,
        holdings: displayRows.map((holding, index) => ({
          instrument_id: holding.instrument_id,
          enabled: holding.enabled,
          weight_within_group: holding.weight_within_group,
          current_amount_minor: holding.current_amount_minor,
          sort_order: holding.sort_order ?? index * 10,
        })),
      });
    },
    onSuccess: () => {
      markClean();
      setEditedRows(null);
      setSaveError(null);
      setSavedMessage("持仓已更新，可返回调仓工作台查看建议。");
      for (const key of ["holdings", "targets", "rebalance", "dashboard", "plan"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
    },
    onError: (error) =>
      setSaveError(error instanceof ApiError ? error.message : "保存失败"),
  });

  const sync = useMutation({
    mutationFn: (holdingId: string) => {
      if (!plan.data) throw new Error("计划尚未加载");
      return syncHoldingSnapshot(planId, holdingId, plan.data.config_version);
    },
    onSuccess: () => {
      for (const key of ["holdings", "targets", "dashboard", "plan"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
    },
  });

  const filteredInstruments =
    instruments.data?.instruments.filter((instrument) => {
      if (
        instrument.is_system ||
        instrument.status !== "active" ||
        (instrument.quality_status ?? "available") !== "available"
      ) {
        return false;
      }
      const query = filter.toLowerCase();
      return (
        instrument.code.toLowerCase().includes(query) ||
        instrument.name.toLowerCase().includes(query) ||
        assetClassLabel(instrument.asset_class).includes(query) ||
        regionLabel(instrument.region).includes(query)
      );
    }) ?? [];

  const totalCurrent = displayRows
    .filter((holding) => holding.enabled)
    .reduce((sum, holding) => sum + holding.current_amount_minor, 0);

  return (
    <div className="space-y-6 pb-20">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="flex items-center text-xl font-semibold">
            持仓管理
            <MetricHelp termKey="current_amount_vs_target" />
          </h1>
          <p className="mt-1 text-sm text-slate-600">
            持仓合计 {formatMoney(totalCurrent, plan.data?.base_currency)}。
            在此管理标的启用与增删；修改当前金额或组内占比请走「更新账户资产」。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Link
            href={`/plans/${planId}/asset-refresh`}
            className="inline-flex min-h-11 items-center rounded-md bg-slate-900 px-4 text-sm text-white"
          >
            更新账户资产
          </Link>
          <Link
            href={`/plans/${planId}/rebalance`}
            className="inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm font-medium"
          >
            查看调仓工作台 →
          </Link>
          <button
            type="button"
            className="min-h-11 rounded-md border border-slate-300 px-4 text-sm"
            onClick={() => setDrawerOpen(true)}
          >
            添加标的
          </button>
        </div>
      </div>

      {assetRefreshed && (
        <div className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800">
          账户资产已更新。
          <Link href={`/plans/${planId}/rebalance`} className="ml-2 font-medium underline">
            查看调仓工作台
          </Link>
        </div>
      )}

      {savedMessage && (
        <div className="rounded-md border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800">
          {savedMessage}
          <Link href={`/plans/${planId}/rebalance`} className="ml-2 font-medium underline">
            返回调仓工作台
          </Link>
        </div>
      )}

      {classGroups.map(({ assetClass, classTarget, regions }) => (
        <section key={assetClass} className="rounded-lg border border-slate-200">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b bg-slate-100 px-4 py-3">
            <h2 className="text-base font-semibold">{assetClassLabel(assetClass)}</h2>
            <span className="text-sm text-slate-600">
              场景大类目标 {formatPercent(classTarget)}
            </span>
          </div>
          <div className="divide-y divide-slate-200">
            {regions.map(({ region, rows: groupRows }) => {
              const enabledRows = groupRows.filter((holding) => holding.enabled);
              const check = validatePercentSum(
                enabledRows.map((holding) => ({
                  label:
                    holding.instrument_name ??
                    holding.instrument_code ??
                    holding.instrument_id,
                  value: holding.weight_within_group,
                })),
              );
              const regionTarget = (targets.data?.holdings ?? [])
                .filter(
                  (line) =>
                    line.enabled &&
                    line.asset_class === assetClass &&
                    line.region === region,
                )
                .reduce((sum, line) => sum + line.portfolio_target_weight, 0);

              return (
                <div key={`${assetClass}:${region}`} className="bg-white">
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 bg-slate-50 px-4 py-2">
                    <h3 className="text-sm font-medium text-slate-800">
                      {regionLabel(region)}
                    </h3>
                    <span className="text-xs text-slate-600">
                      全组合目标 {formatPercent(regionTarget)}
                    </span>
                  </div>
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-sm">
                      <thead>
                        <tr className="text-left text-slate-500">
                          <th className="px-3 py-2">启用</th>
                          <th className="px-3 py-2">标的</th>
                          <th className="px-3 py-2">
                            组内占比
                            <MetricHelp termKey="weight_within_group" />
                          </th>
                          <th className="px-3 py-2">当前金额</th>
                          <th className="px-3 py-2">操作</th>
                        </tr>
                      </thead>
                      <tbody>{renderHoldingRows(groupRows)}</tbody>
                    </table>
                  </div>
                  <p
                    className={`border-t px-4 py-2 text-sm ${
                      check.passed ? "text-emerald-700" : "text-red-700"
                    }`}
                  >
                    {regionLabel(region)} 组内权重合计 {formatPercent(check.total)}
                    {check.passed ? "，通过" : `，${check.message}`}
                    {!check.passed && (
                      <>
                        {" "}
                        <Link
                          href={`/plans/${planId}/asset-refresh`}
                          className="font-medium underline"
                        >
                          前往更新账户资产调整组内占比
                        </Link>
                      </>
                    )}
                  </p>
                </div>
              );
            })}
          </div>
        </section>
      ))}

      <SaveBar
        dirty={dirty}
        saving={save.isPending}
        error={saveError}
        onSave={() => save.mutate()}
      />

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
              />
              <Link href="/assets/import" className="mt-2 block text-sm underline">
                资料库中不存在？从 AKShare 录入
              </Link>
            </div>
            <ul className="flex-1 divide-y overflow-y-auto">
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
