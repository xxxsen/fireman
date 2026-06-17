"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { Dialog } from "@/components/ui/Dialog";
import {
  deleteInstrument,
  getFetchStatus,
  getInstrumentDetail,
  refreshInstrument,
  retryFetch,
} from "@/lib/api/instruments";
import { ApiError } from "@/lib/api/client";
import { useJobStatus } from "@/hooks/useJobStatus";
import {
  annualCompletenessLabel,
  assetClassLabel,
  dataSourceLabel,
  excludedYearReasonLabel,
  formatAnnualPeriod,
  formatNullablePercent,
  formatPercent,
  historyDepthLabel,
  instrumentStatusLabel,
  metricStatusLabel,
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
      case "market_provider_timeout":
        return "数据源响应超时，请重试";
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
  const [showFetchDialog, setShowFetchDialog] = useState(false);
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [fetchStatusError, setFetchStatusError] = useState<string | null>(null);
  const [fetchErrorCode, setFetchErrorCode] = useState<string | null>(null);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["instrument-detail", id],
    queryFn: () => getInstrumentDetail(id),
  });

  useEffect(() => {
    if (!data) return;
    if (data.instrument.status === "pending_fetch" || data.instrument.status === "fetch_failed") {
      void getFetchStatus(id)
        .then((s) => {
          setFetchStatusError(null);
          setFetchErrorCode(s.error_code ?? null);
          if (s.job_id) setActiveJobId(s.job_id);
        })
        .catch((err) => {
          setFetchStatusError(err instanceof Error ? err.message : "抓取状态查询失败");
        });
    }
  }, [data, id]);

  const handleJobTerminal = () => {
    setActiveJobId(null);
    void refetch();
    void qc.invalidateQueries({ queryKey: ["instruments"] });
  };

  const jobState = useJobStatus(activeJobId, {
    onComplete: handleJobTerminal,
    onFailed: () => handleJobTerminal(),
    onCanceled: () => {
      setFetchErrorCode("fetch_canceled");
      handleJobTerminal();
    },
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
    onSuccess: async () => {
      qc.removeQueries({ queryKey: ["instrument-detail", id] });
      await qc.invalidateQueries({ queryKey: ["instruments"] });
      router.push("/assets");
    },
  });

  const retryMut = useMutation({
    mutationFn: () => retryFetch(id),
    onSuccess: (result) => {
      setActiveJobId(result.job_id);
      void refetch();
      void qc.invalidateQueries({ queryKey: ["instruments"] });
    },
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
  const isPending = inst.status === "pending_fetch";
  const isFailed = inst.status === "fetch_failed";
  const isActive = inst.status === "active";
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

      {fetchStatusError && (
        <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          <p className="font-medium">抓取状态查询失败</p>
          <p className="mt-1">{fetchStatusError}</p>
        </div>
      )}

      {isPending && (
        <div className="mt-4 rounded-lg border border-blue-200 bg-blue-50 p-4 text-sm text-blue-900">
          <p className="font-medium">历史数据抓取中</p>
          <p className="mt-1">后台任务正在拉取全量历史，完成后将自动刷新本页。</p>
          <button
            type="button"
            className="mt-2 underline"
            onClick={() => setShowFetchDialog(true)}
          >
            查看抓取状态
          </button>
        </div>
      )}

      {isFailed && fetchErrorCode === "fetch_canceled" && (
        <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          <p className="font-medium">历史数据抓取已取消</p>
          <p className="mt-1">抓取任务已取消，可点击下方按钮重试。</p>
          <button
            type="button"
            className="mt-2 rounded-md bg-amber-900 px-3 py-1.5 text-white disabled:opacity-50"
            disabled={retryMut.isPending}
            onClick={() => retryMut.mutate()}
          >
            {retryMut.isPending ? "提交中…" : "重试抓取"}
          </button>
        </div>
      )}

      {isFailed && fetchErrorCode !== "fetch_canceled" && (
        <div className="mt-4 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-900">
          <p className="font-medium">历史数据抓取失败</p>
          <p className="mt-1">{jobState.error ?? "请重试抓取或检查数据源。"}</p>
          <button
            type="button"
            className="mt-2 rounded-md bg-red-800 px-3 py-1.5 text-white disabled:opacity-50"
            disabled={retryMut.isPending}
            onClick={() => retryMut.mutate()}
          >
            {retryMut.isPending ? "提交中…" : "重试抓取"}
          </button>
        </div>
      )}

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
          <dd>
            {isPending || isFailed
              ? instrumentStatusLabel(inst.status)
              : qualityStatusLabel(inst.quality_status ?? inst.status)}
          </dd>
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

      {isActive && (
      <section className="mt-6 rounded-lg border p-4 text-sm">
        {win.history_depth === "one_year" && (
          <p className="mb-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-amber-900" role="status">
            历史样本有限：当前指标仅基于 {win.complete_year_count} 个完整自然年度，可参与模拟但长期估计不确定性较高。
          </p>
        )}
        <h2 className="font-medium">模拟窗口预览（完整自然年度）</h2>
        <dl className="mt-3 grid gap-2 sm:grid-cols-2">
          <div>
            <dt className="text-slate-500">入选年份</dt>
            <dd>{(win.selected_years ?? []).join("、") || "—"}</dd>
          </div>
          <div>
            <dt className="text-slate-500">排除年份</dt>
            <dd>
              {(win.excluded_years ?? []).length === 0
                ? "无"
                : (win.excluded_years ?? []).map((y) =>
                    typeof y === "object" && y !== null && "year" in y
                      ? `${y.year}（${excludedYearReasonLabel(y.reason)}）`
                      : String(y),
                  ).join("；")}
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">CAGR</dt>
            <dd>
              {formatNullablePercent(win.historical_cagr)}
              {win.historical_cagr == null && win.cagr_status && (
                <span className="ml-1 text-xs text-slate-500">{metricStatusLabel(win.cagr_status)}</span>
              )}
              {win.complete_year_count != null && (
                <span className="ml-1 text-xs text-slate-500">{win.complete_year_count} 个完整自然年度</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">年化波动率</dt>
            <dd>
              {formatNullablePercent(win.annual_volatility)}
              {win.annual_volatility == null && win.volatility_status && (
                <span className="ml-1 text-xs text-slate-500">{metricStatusLabel(win.volatility_status)}</span>
              )}
              {win.monthly_return_count != null && (
                <span className="ml-1 text-xs text-slate-500">{win.monthly_return_count} 个月度收益样本</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">最大回撤</dt>
            <dd>{formatNullablePercent(win.max_drawdown)}
              {win.max_drawdown == null && win.drawdown_status && (
                <span className="ml-1 text-xs text-slate-500">{metricStatusLabel(win.drawdown_status)}</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">数据覆盖</dt>
            <dd>
              日度 {win.daily_observation_count ?? "—"} · 月度 {win.monthly_return_count ?? "—"}
            </dd>
          </div>
          <div>
            <dt className="text-slate-500">历史深度</dt>
            <dd>{historyDepthLabel(win.history_depth)}</dd>
          </div>
        </dl>
      </section>
      )}

      {isActive && data.trailing_returns && (
        <section className="mt-6 rounded-lg border p-4 text-sm">
          <h2 className="font-medium">
            区间收益
            <span className="ml-2 text-xs text-slate-500">截至 {data.trailing_returns.as_of_date}</span>
          </h2>
          <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
            {(["1m", "3m", "6m", "1y", "3y", "5y"] as const).map((key) => {
              const p = data.trailing_returns?.periods[key];
              const label = { "1m": "近 1 月", "3m": "近 3 月", "6m": "近 6 月", "1y": "近 1 年", "3y": "近 3 年", "5y": "近 5 年" }[key];
              const available = p?.status === "available" && p.cumulative_return != null;
              return (
                <div key={key} className="rounded border px-3 py-2">
                  <div className="text-xs text-slate-500">{label}</div>
                  <div className="text-lg font-medium">
                    {available && p ? formatPercent(p.cumulative_return!) : "—"}
                  </div>
                  {!available && (
                    <div className="text-xs text-slate-500">
                      {p?.status === "insufficient_history" ? "历史不足" : p?.status === "start_point_too_stale" ? "起点过旧" : "不可用"}
                    </div>
                  )}
                  {available && p && p.annualized_return != null && (key === "3y" || key === "5y") && (
                    <div className="text-xs text-slate-500">年化 {formatPercent(p.annualized_return)}</div>
                  )}
                </div>
              );
            })}
          </div>
        </section>
      )}

      {isActive && (
        <>
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
                  {refreshMut.isPending && refreshMut.variables
                    ? "强制刷新中（可能需要数分钟）…"
                    : "强制刷新"}
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
                    <div>
                      {s.inclusion_date} · {historyDepthLabel(s.history_depth)} ·{" "}
                      {s.complete_year_count} 完整年 · {s.monthly_return_count} 月收益观测
                    </div>
                    <div className="mt-1 text-xs text-slate-500">
                      指标版本 {s.metrics_version} · 创建于{" "}
                      {new Date(s.created_at).toLocaleString("zh-CN")}
                    </div>
                    {(s.warnings ?? []).length > 0 && (
                      <ul className="mt-1 list-disc pl-5 text-xs text-amber-800">
                        {s.warnings.map((w) => (
                          <li key={w}>{w}</li>
                        ))}
                      </ul>
                    )}
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
                    <Link href={`/plans/${p.plan_id}/overview`} className="underline">
                      {p.plan_name}
                    </Link>
                    <span className="text-slate-500"> · 快照纳入 {p.snapshot_inclusion_date || "—"}</span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </>
      )}

      <Dialog
        open={showFetchDialog}
        onClose={() => setShowFetchDialog(false)}
        title="抓取状态"
        footer={
          <button
            type="button"
            className="rounded-md border border-slate-300 px-4 py-2 text-sm"
            onClick={() => setShowFetchDialog(false)}
          >
            关闭
          </button>
        }
      >
        <dl className="space-y-2 text-sm">
          <div>
            <dt className="text-slate-500">任务状态</dt>
            <dd data-testid="fetch-status-job-status">{jobState.job?.status ?? "—"}</dd>
          </div>
          <div>
            <dt className="text-slate-500">阶段</dt>
            <dd>{jobState.job?.phase ?? "—"}</dd>
          </div>
          <div>
            <dt className="text-slate-500">进度</dt>
            <dd>{Math.round(jobState.progress * 100)}%</dd>
          </div>
          {jobState.error && (
            <div>
              <dt className="text-slate-500">错误</dt>
              <dd className="text-red-700">{jobState.error}</dd>
            </div>
          )}
        </dl>
      </Dialog>
    </div>
  );
}
