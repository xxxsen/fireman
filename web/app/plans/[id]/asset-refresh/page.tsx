"use client";

import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { getHoldings, getTargets } from "@/lib/api/holdings";
import { submitAssetRefresh } from "@/lib/api/asset-refresh";
import { getPlan } from "@/lib/api/plans";
import {
  assetClassLabel,
  formatMoney,
  formatPercent,
} from "@/lib/format";
import {
  buildAssetRefreshBody,
  sumHoldingsMinor,
  validateAssetRefreshTotal,
} from "@/lib/asset-refresh";
import { ApiError } from "@/lib/api/client";

const STEPS = ["说明", "配置确认", "录入当前资产", "确认提交"] as const;

type AssetRefreshRow = {
  instrument_id: string;
  label: string;
  current_amount_minor: number;
};

export default function AssetRefreshPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const searchParams = useSearchParams();
  const reason = searchParams.get("reason");
  const queryClient = useQueryClient();
  const [step, setStep] = useState(reason === "scale" ? 2 : 0);
  const [rowsOverride, setRowsOverride] = useState<AssetRefreshRow[] | null>(null);
  const [totalOverride, setTotalOverride] = useState<number | null>(null);
  const [syncScale, setSyncScale] = useState(reason === "scale");
  const [configChanged, setConfigChanged] = useState(false);
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

  const enabledHoldings = useMemo(
    () => holdings.data?.holdings.filter((h) => h.enabled) ?? [],
    [holdings.data],
  );
  const defaultRows = useMemo(
    () =>
      enabledHoldings.map((h) => ({
        instrument_id: h.instrument_id,
        label: h.instrument_name ?? h.instrument_code ?? h.instrument_id,
        current_amount_minor: h.current_amount_minor,
      })),
    [enabledHoldings],
  );
  const defaultTotal = useMemo(
    () => enabledHoldings.reduce((s, h) => s + h.current_amount_minor, 0),
    [enabledHoldings],
  );
  const rows = rowsOverride ?? defaultRows;
  const totalAssets = totalOverride ?? defaultTotal;

  const sumMinor = useMemo(() => sumHoldingsMinor(rows), [rows]);
  const validation = useMemo(
    () => validateAssetRefreshTotal(rows, totalAssets),
    [rows, totalAssets],
  );

  const submit = useMutation({
    mutationFn: () => {
      if (!plan.data) throw new Error("计划尚未加载");
      if (!validation.ok) throw new Error(validation.message ?? "校验失败");
      return submitAssetRefresh(
        planId,
        buildAssetRefreshBody(plan.data.config_version, rows, totalAssets, syncScale, configChanged),
      );
    },
    onSuccess: () => {
      for (const key of ["holdings", "targets", "rebalance", "dashboard", "plan", "parameters"]) {
        void queryClient.invalidateQueries({ queryKey: [key, planId] });
      }
      router.push(`/plans/${planId}/holdings?asset_refreshed=1`);
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : err instanceof Error ? err.message : "提交失败"),
  });

  if (plan.isLoading || holdings.isLoading || !plan.data || !holdings.data) {
    return <p className="text-slate-600">加载资产变更向导…</p>;
  }

  const beforeTotal = sumHoldingsMinor(
    holdings.data.holdings
      .filter((h) => h.enabled)
      .map((h) => ({ instrument_id: h.instrument_id, current_amount_minor: h.current_amount_minor })),
  );

  return (
    <div className="mx-auto max-w-3xl space-y-6 pb-16">
      <div>
        <h1 className="text-xl font-semibold">更新账户资产</h1>
        <p className="mt-1 text-sm text-slate-600">
          只是更新账户市值？
          <span className="font-medium"> 在此录入真实持仓</span>。要按建议买卖调整持仓？请使用
          <Link href={`/plans/${planId}/rebalance`} className="ml-1 underline">
            调仓计划
          </Link>
          。
          <MetricHelp termKey="asset_refresh_vs_rebalance_plan" />
        </p>
      </div>

      <ol className="flex flex-wrap gap-2 text-sm">
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
          <p className="text-sm text-slate-700">
            将录入当前真实持仓，提交后覆盖系统内当前金额。
            <MetricHelp termKey="asset_refresh" />
          </p>
          <button
            type="button"
            className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white"
            onClick={() => setStep(1)}
          >
            开始
          </button>
        </section>
      )}

      {step === 1 && targets.data && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <h2 className="font-medium">当前配置（只读）</h2>
          <ul className="text-sm text-slate-700">
            {targets.data.asset_class_targets.map((t) => (
              <li key={t.asset_class}>
                {assetClassLabel(t.asset_class)} 目标 {formatPercent(t.weight)}
              </li>
            ))}
          </ul>
          <p className="text-sm text-slate-600">
            已启用标的 {holdings.data.holdings.filter((h) => h.enabled).length} 个
          </p>
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white"
              onClick={() => setStep(2)}
            >
              配置不变，下一步
            </button>
            <Link
              href={`/plans/${planId}/settings?section=scenarios&return=asset-refresh`}
              className="inline-flex min-h-11 items-center rounded-md border border-slate-300 px-4 text-sm"
              onClick={() => setConfigChanged(true)}
            >
              比例 / 结构变更
            </Link>
          </div>
        </section>
      )}

      {step === 2 && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <h2 className="font-medium">录入当前资产</h2>
          <div className="overflow-x-auto">
            <table className="min-w-full text-sm">
              <thead>
                <tr className="text-left text-slate-500">
                  <th className="px-3 py-2">标的</th>
                  <th className="px-3 py-2">当前金额</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.instrument_id} className="border-t">
                    <td className="px-3 py-2">{row.label}</td>
                    <td className="px-3 py-2">
                      <MoneyInput
                        valueMinor={row.current_amount_minor}
                        onChange={(value) =>
                          setRowsOverride(
                            rows.map((r) =>
                              r.instrument_id === row.instrument_id
                                ? { ...r, current_amount_minor: value }
                                : r,
                            ),
                          )
                        }
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <label className="text-sm">
              资产总值
              <div className="mt-1">
                <MoneyInput valueMinor={totalAssets} onChange={setTotalOverride} />
              </div>
            </label>
            <button
              type="button"
              className="text-sm underline"
              onClick={() => setTotalOverride(sumMinor)}
            >
              使用分项合计 {formatMoney(sumMinor, plan.data.base_currency)}
            </button>
          </div>
          {!validation.ok && (
            <p className="text-sm text-red-700">{validation.message}</p>
          )}
          <button
            type="button"
            className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
            disabled={!validation.ok}
            onClick={() => setStep(3)}
          >
            下一步
          </button>
        </section>
      )}

      {step === 3 && (
        <section className="space-y-4 rounded-lg border border-slate-200 p-4">
          <h2 className="font-medium">确认提交</h2>
          <p className="text-sm text-slate-700">
            变更前合计 {formatMoney(beforeTotal, plan.data.base_currency)} → 变更后合计{" "}
            {formatMoney(totalAssets, plan.data.base_currency)}
          </p>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={syncScale}
              onChange={(e) => setSyncScale(e.target.checked)}
            />
            同步计划基准规模至新总值
          </label>
          <div className="flex flex-wrap gap-3">
            <button
              type="button"
              className="min-h-11 rounded-md bg-slate-900 px-4 text-sm text-white disabled:opacity-50"
              disabled={submit.isPending}
              onClick={() => submit.mutate()}
            >
              确认更新资产
            </button>
            <button
              type="button"
              className="min-h-11 rounded-md border px-4 text-sm"
              onClick={() => setStep(2)}
            >
              返回修改
            </button>
          </div>
        </section>
      )}
    </div>
  );
}
