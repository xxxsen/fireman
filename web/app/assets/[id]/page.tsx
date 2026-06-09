"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import {
  deleteInstrument,
  getInstrumentDetail,
  refreshInstrument,
} from "@/lib/api/instruments";
import { ApiError } from "@/lib/api/client";
import {
  annualCompletenessLabel,
  assetClassLabel,
  dataSourceLabel,
  formatAnnualPeriod,
  formatPercent,
  pointTypeLabel,
  qualityStatusLabel,
  regionLabel,
} from "@/lib/format";

function refreshFeedbackMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case "instrument_refresh_throttled":
        return "24 小时内已刷新过。如需立即验证数据，请使用「强制刷新」。";
      case "market_provider_unavailable":
        return `数据源暂时不可用：${err.message}`;
      case "provider_data_anomaly":
        return "刷新被拒绝：检测到异常日收益率。";
      case "instrument_not_refreshable":
        return "该标的不可刷新。";
      default:
        return err.message;
    }
  }
  return err instanceof Error ? err.message : "刷新失败，请稍后重试。";
}

export default function AssetDetailPage() {
  const id = useParams().id as string;
  const router = useRouter();
  const qc = useQueryClient();
  const [refreshNotice, setRefreshNotice] = useState<{ kind: "success" | "error"; text: string } | null>(
    null,
  );

  const { data, isLoading, error } = useQuery({
    queryKey: ["instrument-detail", id],
    queryFn: () => getInstrumentDetail(id),
  });

  const refreshMut = useMutation({
    mutationFn: (force: boolean) => refreshInstrument(id, { force }),
    onMutate: () => {
      setRefreshNotice(null);
    },
    onSuccess: (inst, force) => {
      void qc.invalidateQueries({ queryKey: ["instrument-detail", id] });
      void qc.invalidateQueries({ queryKey: ["instruments"] });
      const asOf = inst.data_as_of ? `，数据截至 ${inst.data_as_of}` : "";
      const src = inst.data_source_name
        ? `，来源 ${dataSourceLabel(inst.data_source_name)}`
        : "";
      const prefix = force ? "已强制刷新 AKShare 数据" : "AKShare 数据已刷新";
      setRefreshNotice({ kind: "success", text: `${prefix}${asOf}${src}。` });
    },
    onError: (err) => {
      setRefreshNotice({ kind: "error", text: refreshFeedbackMessage(err) });
    },
  });

  const handleForceRefresh = () => {
    if (
      !window.confirm(
        "强制刷新将立即重新抓取远端数据，跳过 24 小时限制，可能增加被数据源限流的风险。确定继续？",
      )
    ) {
      return;
    }
    refreshMut.mutate(true);
  };

  const deleteMut = useMutation({
    mutationFn: () => deleteInstrument(id),
    onSuccess: () => router.push("/assets"),
  });

  if (isLoading) return <p>加载资产详情…</p>;
  if (error) {
    return (
      <p className="text-red-600">
        加载失败：{error instanceof Error ? error.message : "未知错误"}
      </p>
    );
  }
  if (!data) return <p>加载资产详情…</p>;

  const inst = data.instrument;
  const win = data.simulation_window;
  const annualReturns = data.annual_returns ?? [];
  const historicalSnapshots = data.historical_snapshots ?? [];
  const referencingPlans = data.referencing_plans ?? [];

  return (
    <div className="max-w-4xl">
      <Link href="/assets" className="text-sm underline">
        ← 资料库
      </Link>
      <h1 className="mt-4 text-2xl font-semibold">
        {inst.name} <span className="font-mono text-lg text-slate-500">({inst.code})</span>
      </h1>

      <dl className="mt-6 grid gap-3 sm:grid-cols-2 text-sm">
        <div>
          <dt className="text-slate-500">市场 / 类型</dt>
          <dd>
            {inst.market} / {inst.instrument_type}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">大类 / 地区</dt>
          <dd>
            {assetClassLabel(inst.asset_class)} / {regionLabel(inst.region)}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">数据状态</dt>
          <dd>{qualityStatusLabel(inst.quality_status ?? inst.status)}</dd>
        </div>
        <div>
          <dt className="text-slate-500">数据截止</dt>
          <dd>{inst.data_as_of || "—"}</dd>
        </div>
        <div>
          <dt className="text-slate-500">抓取数据源</dt>
          <dd>
            {dataSourceLabel(inst.data_source_name)}
            {inst.data_source_name && (
              <span className="ml-1 font-mono text-xs text-slate-400">
                ({inst.data_source_name})
              </span>
            )}
          </dd>
        </div>
        <div>
          <dt className="text-slate-500">价格类型</dt>
          <dd>{pointTypeLabel(inst.point_type)}</dd>
        </div>
        <div>
          <dt className="text-slate-500">费率处理</dt>
          <dd>
            {inst.fee_treatment}
            {inst.expense_ratio_status === "unavailable" && (
              <span className="ml-2 text-amber-700">（历史净值已含费率，不阻止模拟）</span>
            )}
            <MetricHelp termKey="fee_included" />
          </dd>
        </div>
      </dl>

      <section className="mt-6 rounded-lg border p-4 text-sm">
        <h2 className="font-medium">模拟窗口预览（最近完整年度）</h2>
        <dl className="mt-3 grid gap-2 sm:grid-cols-2">
          <div>
            <dt className="text-slate-500">入选年份</dt>
            <dd>{(win.selected_years ?? []).join("、") || "—"}</dd>
          </div>
          <div>
            <dt className="text-slate-500">排除年份</dt>
            <dd>{(win.excluded_years ?? []).join("、") || "无"}</dd>
          </div>
          <div>
            <dt className="text-slate-500">CAGR</dt>
            <dd>{formatPercent(win.historical_cagr)}</dd>
          </div>
          <div>
            <dt className="text-slate-500">波动率</dt>
            <dd>{formatPercent(win.annual_volatility)}</dd>
          </div>
          <div>
            <dt className="text-slate-500">最大回撤</dt>
            <dd>{formatPercent(win.max_drawdown)}</dd>
          </div>
          <div>
            <dt className="text-slate-500">观测数</dt>
            <dd>{win.observation_count}</dd>
          </div>
        </dl>
      </section>

      <div className="mt-6 space-y-2">
        <div className="flex flex-wrap items-center gap-3">
          <button
            type="button"
            className="rounded-md bg-slate-900 px-3 py-2 text-sm text-white disabled:cursor-not-allowed disabled:opacity-60"
            disabled={refreshMut.isPending || inst.is_system}
            onClick={() => refreshMut.mutate(false)}
          >
            {refreshMut.isPending && !refreshMut.variables ? "刷新中…" : "刷新 AKShare 数据"}
          </button>
          {!inst.is_system && (
            <button
              type="button"
              className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 disabled:cursor-not-allowed disabled:opacity-60"
              disabled={refreshMut.isPending}
              onClick={handleForceRefresh}
            >
              {refreshMut.isPending && refreshMut.variables ? "强制刷新中…" : "强制刷新"}
            </button>
          )}
          {!inst.is_system && (
            <button
              type="button"
              className="rounded-md border border-red-200 px-3 py-2 text-sm text-red-700 disabled:opacity-50"
              disabled={deleteMut.isPending || referencingPlans.length > 0}
              title={referencingPlans.length > 0 ? "被计划引用时不可删除" : undefined}
              onClick={() => {
                if (window.confirm("确定删除此标的？")) deleteMut.mutate();
              }}
            >
              {deleteMut.isPending ? "删除中…" : "删除"}
            </button>
          )}
        </div>
        {!inst.is_system && (
          <p className="text-xs text-slate-500">
            常规刷新 24 小时内限一次；强制刷新跳过该限制，适合验证数据源或排查数据问题。
          </p>
        )}
      </div>
      {refreshNotice && (
        <p
          className={`mt-3 text-sm ${refreshNotice.kind === "success" ? "text-emerald-700" : "text-red-600"}`}
          role="status"
        >
          {refreshNotice.text}
          {refreshNotice.kind === "error" &&
            refreshNotice.text.includes("24 小时内") && (
              <button
                type="button"
                className="ml-2 underline"
                disabled={refreshMut.isPending}
                onClick={handleForceRefresh}
              >
                立即强制刷新
              </button>
            )}
        </p>
      )}

      <h2 className="mt-8 font-medium">年度收益</h2>
      <div className="mt-2 max-h-96 overflow-auto rounded-lg border">
        <table className="w-full text-sm">
          <thead className="sticky top-0 bg-slate-50">
            <tr>
              <th className="px-3 py-2 text-left">年份</th>
              <th className="px-3 py-2 text-right">年化收益</th>
              <th className="px-3 py-2 text-left">完整性</th>
              <th className="px-3 py-2 text-left">统计区间</th>
              <th className="px-3 py-2 text-left">预览窗口</th>
            </tr>
          </thead>
          <tbody>
            {annualReturns.map((r) => (
              <tr key={r.year} className="border-t">
                <td className="px-3 py-2">{r.year}</td>
                <td className="px-3 py-2 text-right">{formatPercent(r.annual_return)}</td>
                <td className="px-3 py-2">{annualCompletenessLabel(r)}</td>
                <td className="px-3 py-2 font-mono text-xs text-slate-600">
                  {formatAnnualPeriod(r.start_date, r.end_date)}
                </td>
                <td className="px-3 py-2">{r.in_simulation ? "参与" : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {historicalSnapshots.length > 0 && (
        <>
          <h2 className="mt-8 font-medium">历史计划快照</h2>
          <ul className="mt-2 space-y-1 text-sm">
            {historicalSnapshots.map((s) => (
              <li key={s.id} className="rounded border px-3 py-2">
                {s.inclusion_date} · {s.complete_year_count} 完整年 ·{" "}
                {new Date(s.created_at).toLocaleString("zh-CN")}
              </li>
            ))}
          </ul>
        </>
      )}

      {referencingPlans.length > 0 && (
        <>
          <h2 className="mt-8 font-medium">引用计划</h2>
          <ul className="mt-2 space-y-1 text-sm">
            {referencingPlans.map((p) => (
              <li key={p.plan_id}>
                <Link href={`/plans/${p.plan_id}/dashboard`} className="underline">
                  {p.plan_name}
                </Link>
                <span className="text-slate-500"> · 快照纳入 {p.snapshot_inclusion_date || "—"}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
