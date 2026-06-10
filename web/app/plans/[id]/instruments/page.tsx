"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { SaveBar } from "@/components/ui/SaveBar";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { getHoldings, getTargets, syncHoldingSnapshot, updateHoldings } from "@/lib/api/holdings";
import { listInstruments } from "@/lib/api/instruments";
import { getPlan } from "@/lib/api/plans";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
  qualityStatusLabel,
  regionLabel,
} from "@/lib/format";
import { validatePercentSum } from "@/lib/percent";
import type { Instrument, PlanHolding } from "@/types/api";
import { ApiError } from "@/lib/api/client";
import { StaleBanner } from "@/components/ui/StaleBanner";
import { usePlanResultStale } from "@/hooks/usePlanResultStale";

export default function InstrumentsPage() {
  const planId = useParams().id as string;
  const qc = useQueryClient();
  const { stale } = usePlanResultStale(planId);
  const { dirty, markDirty, markClean } = usePlanEdit();
  const [rows, setRows] = useState<PlanHolding[]>([]);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [saveError, setSaveError] = useState<string | null>(null);

  const planQ = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const holdingsQ = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
  });
  const instrumentsQ = useQuery({
    queryKey: ["instruments", planQ.data?.valuation_date],
    queryFn: () =>
      listInstruments(
        planQ.data?.valuation_date ? { valuationDate: planQ.data.valuation_date } : undefined,
      ),
    enabled: !!planQ.data,
  });
  const targetsQ = useQuery({
    queryKey: ["targets", planId],
    queryFn: () => getTargets(planId),
  });

  const instrumentById = useMemo(() => {
    const m = new Map<string, Instrument>();
    for (const i of instrumentsQ.data?.instruments ?? []) m.set(i.id, i);
    return m;
  }, [instrumentsQ.data]);

  const targetByHolding = useMemo(() => {
    const m = new Map<string, { current: number; target: number }>();
    for (const line of targetsQ.data?.holdings ?? []) {
      m.set(line.holding_id, {
        current: line.current_weight,
        target: line.portfolio_target_weight,
      });
    }
    return m;
  }, [targetsQ.data]);

  const displayRows = rows.length ? rows : holdingsQ.data?.holdings ?? [];

  const groupChecks = useMemo(() => {
    const groups = new Map<
      string,
      { label: string; items: { label: string; value: number }[] }
    >();
    for (const h of displayRows) {
      if (!h.enabled) continue;
      const key = `${h.asset_class}:${h.region}`;
      const label = `${regionLabel(h.region)}${assetClassLabel(h.asset_class)}`;
      const g = groups.get(key) ?? { label, items: [] };
      g.items.push({
        label: h.instrument_name ?? h.instrument_code ?? h.instrument_id,
        value: h.weight_within_group,
      });
      groups.set(key, g);
    }
    return [...groups.entries()].map(([key, g]) => ({
      key,
      label: g.label,
      ...validatePercentSum(g.items),
    }));
  }, [displayRows]);

  useEffect(() => {
    if (holdingsQ.data && !dirty) {
      setRows(holdingsQ.data.holdings.map((h) => ({ ...h })));
    }
  }, [holdingsQ.data, dirty]);

  const updateRow = (idx: number, patch: Partial<PlanHolding>) => {
    const next = [...(rows.length ? rows : holdingsQ.data?.holdings ?? [])];
    next[idx] = { ...next[idx], ...patch };
    setRows(next);
    markDirty();
  };

  const addInstrument = (inst: Instrument) => {
    const next = [...displayRows];
    if (next.some((h) => h.instrument_id === inst.id)) return;
    next.push({
      id: "draft_" + inst.id,
      plan_id: planId,
      instrument_id: inst.id,
      enabled: true,
      asset_class: inst.asset_class,
      region: inst.region,
      weight_within_group: 0,
      current_amount_minor: 0,
      simulation_snapshot_id: "",
      sort_order: next.length * 10,
      instrument_code: inst.code,
      instrument_name: inst.name,
    });
    setRows(next);
    markDirty();
    setDrawerOpen(false);
  };

  const saveMut = useMutation({
    mutationFn: () => {
      if (!planQ.data) throw new Error("plan");
      const payload = displayRows.map((h, i) => ({
        instrument_id: h.instrument_id,
        enabled: h.enabled,
        weight_within_group: h.weight_within_group,
        current_amount_minor: h.current_amount_minor,
        sort_order: h.sort_order ?? i * 10,
      }));
      return updateHoldings(planId, {
        config_version: planQ.data.config_version,
        holdings: payload,
      });
    },
    onSuccess: () => {
      markClean();
      setSaveError(null);
      setRows([]);
      void qc.invalidateQueries({ queryKey: ["holdings", planId] });
      void qc.invalidateQueries({ queryKey: ["plan", planId] });
    },
    onError: (e) => setSaveError(e instanceof ApiError ? e.message : "保存失败"),
  });

  const syncMut = useMutation({
    mutationFn: (holdingId: string) => {
      if (!planQ.data) throw new Error("plan");
      return syncHoldingSnapshot(planId, holdingId, planQ.data.config_version);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["holdings", planId] });
      void qc.invalidateQueries({ queryKey: ["plan", planId] });
      void qc.invalidateQueries({ queryKey: ["targets", planId] });
      void qc.invalidateQueries({ queryKey: ["dashboard", planId] });
    },
  });

  const filteredInstruments =
    instrumentsQ.data?.instruments.filter((i) => {
      if (i.is_system) return false;
      if (i.status !== "active") return false;
      if ((i.quality_status ?? "available") !== "available") return false;
      const q = filter.toLowerCase();
      return (
        i.code.toLowerCase().includes(q) ||
        i.name.toLowerCase().includes(q) ||
        i.asset_class.includes(q) ||
        i.region.includes(q) ||
        regionLabel(i.region).includes(q) ||
        i.market.toLowerCase().includes(q)
      );
    }) ?? [];

  const totalCurrent = displayRows
    .filter((h) => h.enabled)
    .reduce((s, h) => s + h.current_amount_minor, 0);

  return (
    <div className="space-y-4 pb-20">
      {stale && <StaleBanner />}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm text-slate-600">
          已启用标的当前金额合计：{formatMoney(totalCurrent, planQ.data?.base_currency)}
        </div>
        <button
          type="button"
          className="rounded-md bg-slate-900 px-3 py-2 text-sm text-white"
          onClick={() => setDrawerOpen(true)}
        >
          添加标的
        </button>
      </div>

      {groupChecks.map((g) => (
        <p
          key={g.key}
          className={`text-sm ${g.passed ? "text-emerald-700" : "text-red-700"}`}
        >
          {g.label}组内合计 {formatPercent(g.total)}
          {g.passed ? "，通过" : `，${g.message}`}
        </p>
      ))}

      <div className="overflow-x-auto rounded-lg border border-slate-200">
        <table className="min-w-full text-sm">
          <thead className="bg-slate-50 text-left text-slate-600">
            <tr>
              <th className="px-3 py-2">启用</th>
              <th className="px-3 py-2">大类</th>
              <th className="px-3 py-2">地区</th>
              <th className="px-3 py-2">标的</th>
              <th className="px-3 py-2">
                组内占比
                <MetricHelp termKey="weight_within_group" />
              </th>
              <th className="px-3 py-2">
                全组合占比
                <MetricHelp termKey="portfolio_weight" />
              </th>
              <th className="px-3 py-2">数据状态</th>
              <th className="px-3 py-2">当前金额</th>
              <th className="px-3 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {displayRows.map((h, idx) => {
              const inst = instrumentById.get(h.instrument_id);
              const weights = targetByHolding.get(h.id);
              return (
              <tr key={h.id} className="border-t border-slate-100">
                <td className="px-3 py-2">
                  <input
                    type="checkbox"
                    checked={h.enabled}
                    onChange={(e) => updateRow(idx, { enabled: e.target.checked })}
                  />
                </td>
                <td className="px-3 py-2">{assetClassLabel(h.asset_class)}</td>
                <td className="px-3 py-2">{regionLabel(h.region)}</td>
                <td className="px-3 py-2">
                  {h.instrument_name ?? h.instrument_code ?? h.instrument_id}
                  <span className="text-slate-500"> ({h.instrument_code})</span>
                </td>
                <td className="px-3 py-2">
                  <PercentInput
                    value={h.weight_within_group}
                    onChange={(v) => updateRow(idx, { weight_within_group: v })}
                  />
                </td>
                <td className="px-3 py-2">
                  {weights ? formatPercent(weights.current) : "—"}
                  {weights && (
                    <span className="block text-xs text-slate-500">
                      目标 {formatPercent(weights.target)}
                    </span>
                  )}
                </td>
                <td className="px-3 py-2 text-xs">
                  <div>{qualityStatusLabel(inst?.quality_status ?? inst?.status ?? "—")}</div>
                  {inst?.data_stale && (
                    <div className="text-amber-700">{inst.stale_warning ?? "数据可能过期"}</div>
                  )}
                  {h.simulation_snapshot_id && (
                    <div className="text-slate-500">快照已绑定</div>
                  )}
                </td>
                <td className="px-3 py-2">
                  <MoneyInput
                    valueMinor={h.current_amount_minor}
                    onChange={(v) => updateRow(idx, { current_amount_minor: v })}
                  />
                </td>
                <td className="px-3 py-2 space-y-1">
                  <Link href={`/assets/${h.instrument_id}`} className="block underline">
                    查看资产
                  </Link>
                  {!h.id.startsWith("draft_") && (
                    <button
                      type="button"
                      className="block text-left text-xs underline disabled:opacity-50"
                      disabled={syncMut.isPending}
                      onClick={() => syncMut.mutate(h.id)}
                    >
                      同步历史快照
                    </button>
                  )}
                </td>
              </tr>
            );})}
          </tbody>
        </table>
      </div>

      <SaveBar dirty={dirty} saving={saveMut.isPending} error={saveError} onSave={() => saveMut.mutate()} />

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
                onChange={(e) => setFilter(e.target.value)}
              />
              <Link href="/assets/import" className="mt-2 block text-sm underline">
                资料库中不存在？从 AKShare 录入
              </Link>
            </div>
            <ul className="flex-1 overflow-y-auto divide-y">
              {filteredInstruments.map((inst) => (
                <li key={inst.id}>
                  <button
                    type="button"
                    className="w-full px-4 py-3 text-left hover:bg-slate-50"
                    onClick={() => addInstrument(inst)}
                  >
                    <div className="font-medium">{inst.name}</div>
                    <div className="text-xs text-slate-500">
                      {inst.code} · {qualityStatusLabel(inst.quality_status ?? inst.status)}
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
