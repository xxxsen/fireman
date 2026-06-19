"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { MetricHelp } from "@/components/ui/MetricHelp";
import { Dialog } from "@/components/ui/Dialog";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import {
  deleteInstrument,
  getFetchStatus,
  getInstrumentDetail,
  getReturnSeries,
  refreshInstrument,
  retryFetch,
  updateInstrumentClassification,
  type ReturnSeriesRange,
} from "@/lib/api/instruments";
import { ReturnSeriesChart } from "@/components/charts/ReturnSeriesChart";
import { ApiError } from "@/lib/api/client";
import { useJobStatus } from "@/hooks/useJobStatus";
import { queryErrorMessage } from "@/lib/query-error";
import {
  annualCompletenessLabel,
  assetClassLabel,
  compressYears,
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

const RETURN_SERIES_RANGES: { key: ReturnSeriesRange; label: string }[] = [
  { key: "3d", label: "近3天" },
  { key: "1w", label: "近1周" },
  { key: "1m", label: "近1月" },
  { key: "3m", label: "近3月" },
  { key: "6m", label: "近6月" },
  { key: "1y", label: "近1年" },
  { key: "3y", label: "近3年" },
  { key: "5y", label: "近5年" },
  { key: "all", label: "全部" },
];

function refreshFeedbackMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
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

const ASSET_CLASS_OPTIONS = [
  { value: "equity", label: "权益" },
  { value: "bond", label: "债券" },
  { value: "cash", label: "现金/其他" },
];

const REGION_OPTIONS = [
  { value: "domestic", label: "国内" },
  { value: "foreign", label: "国外" },
];

function classificationFeedbackMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case "instrument_version_conflict":
        return "资产资料已被更新，请刷新后确认分类再保存。";
      case "instrument_not_editable":
        return "系统资产不可编辑分类。";
      case "instrument_classification_unsupported":
        return "分类取值不合法。";
      default:
        return err.message;
    }
  }
  return err instanceof Error ? err.message : "保存失败，请稍后重试。";
}

export default function AssetDetailPage() {
  const id = useParams().id as string;
  const router = useRouter();
  const qc = useQueryClient();
  const [refreshNotice, setRefreshNotice] = useState<{ kind: "success" | "error"; text: string } | null>(
    null,
  );
  const [showFetchDialog, setShowFetchDialog] = useState(false);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [fetchStatusError, setFetchStatusError] = useState<string | null>(null);
  const [fetchErrorCode, setFetchErrorCode] = useState<string | null>(null);

  const [editingClass, setEditingClass] = useState(false);
  const [classDraft, setClassDraft] = useState<{ asset_class: string; region: string } | null>(null);
  const [classNotice, setClassNotice] = useState<
    { kind: "success" | "error" | "conflict"; text: string } | null
  >(null);

  const [seriesRange, setSeriesRange] = useState<ReturnSeriesRange>("3m");

  const { data, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["instrument-detail", id],
    queryFn: () => getInstrumentDetail(id),
  });

  const seriesQ = useQuery({
    queryKey: ["return-series", id, seriesRange],
    queryFn: () => getReturnSeries(id, seriesRange),
    enabled: data?.instrument.status === "active",
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
    mutationFn: () => refreshInstrument(id),
    onMutate: () => {
      setRefreshNotice(null);
    },
    onSuccess: (inst) => {
      void qc.invalidateQueries({ queryKey: ["instrument-detail", id] });
      void qc.invalidateQueries({ queryKey: ["instruments"] });
      const asOf = inst.data_as_of ? `，截至 ${inst.data_as_of}` : "";
      const src = inst.data_source_name
        ? `，来源 ${dataSourceLabel(inst.data_source_name)}`
        : "";
      setRefreshNotice({ kind: "success", text: `数据已刷新${asOf}${src}。` });
    },
    onError: (err) => {
      setRefreshNotice({ kind: "error", text: refreshFeedbackMessage(err) });
    },
  });

  const classMut = useMutation({
    mutationFn: (body: { asset_class: string; region: string; expected_updated_at: number }) =>
      updateInstrumentClassification(id, body),
    onSuccess: (result) => {
      setEditingClass(false);
      setClassDraft(null);
      const refs = result.referencing_plan_count;
      const suffix = refs > 0 ? `已关联 ${refs} 个计划保持原配置，` : "";
      setClassNotice({
        kind: "success",
        text: `分类已更新；${suffix}后续新建或新增资产将使用新分类。`,
      });
      void qc.invalidateQueries({ queryKey: ["instrument-detail", id] });
      void qc.invalidateQueries({ queryKey: ["instruments"] });
      void qc.invalidateQueries({ queryKey: ["instrument-picker"] });
    },
    onError: (err) => {
      if (err instanceof ApiError && err.code === "instrument_version_conflict") {
        setClassNotice({ kind: "conflict", text: classificationFeedbackMessage(err) });
        return;
      }
      setClassNotice({ kind: "error", text: classificationFeedbackMessage(err) });
    },
  });

  const deleteMut = useMutation({
    mutationFn: () => deleteInstrument(id),
    onSuccess: async () => {
      qc.removeQueries({ queryKey: ["instrument-detail", id] });
      await qc.invalidateQueries({ queryKey: ["instruments"] });
      router.push("/assets");
    },
    onError: (err) => {
      setDeleteError(queryErrorMessage(err, "删除失败"));
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

  if (isLoading && !data) {
    return <LoadingState label="加载资产详情…" />;
  }
  if (isError && !data) {
    return (
      <ErrorState
        message="无法加载资产详情。请确认后端服务可用后重试。"
        onRetry={() => void refetch()}
        backHref="/assets"
        backLabel="返回资料库"
        technicalDetail={queryErrorMessage(error)}
      />
    );
  }
  if (!data) return null;

  const inst = data.instrument;
  const isPending = inst.status === "pending_fetch";
  const isFailed = inst.status === "fetch_failed";
  const isActive = inst.status === "active";
  const win = data.simulation_window;
  const annualReturns = [...(data.annual_returns ?? [])].sort((a, b) => b.year - a.year);
  const historicalSnapshots = data.historical_snapshots ?? [];
  const referencingPlans = data.referencing_plans ?? [];

  return (
    <div className="max-w-6xl">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <Link
            href="/assets"
            className="text-sm text-ink-muted underline-offset-2 hover:text-ink hover:underline"
          >
            ← 资料库
          </Link>
          <h1 className="mt-4 text-2xl font-semibold text-ink">
            {inst.name}{" "}
            <span className="font-mono-numeric text-lg text-ink-muted">({inst.code})</span>
          </h1>
          <p className="mt-1 text-sm text-ink-muted">
            {assetClassLabel(inst.asset_class)} / {regionLabel(inst.region)} · {inst.market} /{" "}
            {inst.instrument_type}
          </p>
        </div>
        {isActive && (
          <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
            <Button
              disabled={refreshMut.isPending || inst.is_system}
              onClick={() => refreshMut.mutate()}
            >
              {refreshMut.isPending ? "刷新中（可能需要数分钟）…" : "刷新"}
            </Button>
            {!inst.is_system && (
              <Button
                variant="danger"
                disabled={deleteMut.isPending || referencingPlans.length > 0}
                title={referencingPlans.length > 0 ? "被计划引用时不可删除" : undefined}
                onClick={() => {
                  setDeleteError(null);
                  setShowDeleteConfirm(true);
                }}
              >
                {deleteMut.isPending ? "删除中…" : "删除"}
              </Button>
            )}
          </div>
        )}
      </div>

      {refreshNotice && (
        <p
          className={`mt-3 text-sm ${refreshNotice.kind === "success" ? "text-positive" : "text-danger"}`}
          role="status"
        >
          {refreshNotice.text}
        </p>
      )}

      {fetchStatusError && (
        <Alert variant="warning" title="抓取状态查询失败" className="mt-4">
          {fetchStatusError}
        </Alert>
      )}

      {isPending && (
        <Alert variant="info" title="历史数据抓取中" className="mt-4">
          <p>后台任务正在拉取全量历史，完成后将自动刷新本页。</p>
          <Button
            variant="ghost"
            className="mt-2 px-2 py-1"
            onClick={() => setShowFetchDialog(true)}
          >
            查看抓取状态
          </Button>
        </Alert>
      )}

      {isFailed && fetchErrorCode === "fetch_canceled" && (
        <Alert variant="warning" title="历史数据抓取已取消" className="mt-4">
          <p>抓取任务已取消，可点击下方按钮重试。</p>
          <Button
            className="mt-2"
            disabled={retryMut.isPending}
            onClick={() => retryMut.mutate()}
          >
            {retryMut.isPending ? "提交中…" : "重试抓取"}
          </Button>
        </Alert>
      )}

      {isFailed && fetchErrorCode !== "fetch_canceled" && (
        <Alert variant="danger" title="历史数据抓取失败" className="mt-4">
          <p>{jobState.error ?? "请重试抓取或检查数据源。"}</p>
          <Button
            className="mt-2"
            disabled={retryMut.isPending}
            onClick={() => retryMut.mutate()}
          >
            {retryMut.isPending ? "提交中…" : "重试抓取"}
          </Button>
        </Alert>
      )}

      <section className="mt-6 rounded-lg border border-line bg-surface p-4">
      <h2 className="mb-3 font-medium text-ink">基础信息</h2>
      <dl className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 text-sm">
        <div>
          <dt className="text-ink-muted">市场 / 类型</dt>
          <dd className="text-ink">
            {inst.market} / {inst.instrument_type}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">大类 / 地区</dt>
          {editingClass && classDraft ? (
            <dd className="mt-1 space-y-2" data-testid="classification-editor">
              <div className="flex flex-wrap gap-2">
                <select
                  aria-label="资产大类"
                  className="rounded border border-line bg-surface px-2 py-1 text-sm text-ink"
                  value={classDraft.asset_class}
                  onChange={(e) =>
                    setClassDraft({ ...classDraft, asset_class: e.target.value })
                  }
                >
                  {ASSET_CLASS_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
                <select
                  aria-label="资产地区"
                  className="rounded border border-line bg-surface px-2 py-1 text-sm text-ink"
                  value={classDraft.region}
                  onChange={(e) => setClassDraft({ ...classDraft, region: e.target.value })}
                >
                  {REGION_OPTIONS.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </div>
              <p className="text-xs text-ink-muted">
                仅影响资料库和后续新建/新增资产；已关联计划保持原配置。
                {referencingPlans.length > 0 && (
                  <span className="ml-1 text-warning">当前被 {referencingPlans.length} 个计划引用。</span>
                )}
              </p>
              <div className="flex gap-2">
                <Button
                  className="px-2 py-1"
                  disabled={classMut.isPending}
                  onClick={() =>
                    classMut.mutate({
                      asset_class: classDraft.asset_class,
                      region: classDraft.region,
                      expected_updated_at: inst.updated_at,
                    })
                  }
                >
                  {classMut.isPending ? "保存中…" : "保存"}
                </Button>
                <Button
                  variant="secondary"
                  className="px-2 py-1"
                  disabled={classMut.isPending}
                  onClick={() => {
                    setEditingClass(false);
                    setClassDraft(null);
                    setClassNotice(null);
                  }}
                >
                  取消
                </Button>
              </div>
            </dd>
          ) : (
            <dd className="text-ink">
              {assetClassLabel(inst.asset_class)} / {regionLabel(inst.region)}
              {!inst.is_system && (
                <Button
                  variant="ghost"
                  className="ml-2 px-2 py-0.5"
                  onClick={() => {
                    setClassNotice(null);
                    setClassDraft({ asset_class: inst.asset_class, region: inst.region });
                    setEditingClass(true);
                  }}
                >
                  编辑分类
                </Button>
              )}
            </dd>
          )}
          {classNotice && (
            <p
              className={`mt-1 text-xs ${
                classNotice.kind === "success" ? "text-positive" : "text-danger"
              }`}
              role="status"
            >
              {classNotice.text}
              {classNotice.kind === "conflict" && (
                <Button
                  variant="ghost"
                  className="ml-2 px-2 py-0.5"
                  onClick={() => {
                    setClassNotice(null);
                    setEditingClass(false);
                    setClassDraft(null);
                    void refetch();
                  }}
                >
                  重新加载
                </Button>
              )}
            </p>
          )}
        </div>
        <div>
          <dt className="text-ink-muted">数据状态</dt>
          <dd className="text-ink">
            {isPending || isFailed
              ? instrumentStatusLabel(inst.status)
              : qualityStatusLabel(inst.quality_status ?? inst.status)}
          </dd>
        </div>
        <div>
          <dt className="text-ink-muted">数据截止</dt>
          <dd className="text-ink">{inst.data_as_of || "—"}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">抓取数据源</dt>
          <dd className="text-ink">{dataSourceLabel(inst.data_source_name)}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">价格类型</dt>
          <dd className="text-ink">{pointTypeLabel(inst.point_type)}</dd>
        </div>
        <div>
          <dt className="text-ink-muted">费率处理</dt>
          <dd className="text-ink">
            {inst.fee_treatment}
            {inst.expense_ratio_status === "unavailable" && (
              <span className="ml-2 text-warning">（历史净值已含费率，不阻止模拟）</span>
            )}
            <MetricHelp termKey="fee_included" />
          </dd>
        </div>
      </dl>
      </section>

      {isActive && (
      <section className="mt-6 rounded-lg border border-line bg-surface p-4 text-sm">
        {win.history_depth === "one_year" && (
          <Alert variant="warning" className="mb-3">
            历史样本有限：当前指标仅基于 {win.complete_year_count} 个完整自然年度，可参与模拟但长期估计不确定性较高。
          </Alert>
        )}
        <h2 className="font-medium text-ink">模拟窗口预览（完整自然年度）</h2>
        <dl className="mt-3 grid gap-2 sm:grid-cols-2">
          <div>
            <dt className="text-ink-muted">入选年份</dt>
            <dd className="text-ink">{compressYears(win.selected_years ?? [])}</dd>
          </div>
          <div>
            <dt className="text-ink-muted">排除年份</dt>
            <dd className="text-ink">
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
            <dt className="text-ink-muted">CAGR</dt>
            <dd className="text-ink">
              {formatNullablePercent(win.historical_cagr)}
              {win.historical_cagr == null && win.cagr_status && (
                <span className="ml-1 text-xs text-ink-muted">{metricStatusLabel(win.cagr_status)}</span>
              )}
              {win.complete_year_count != null && (
                <span className="ml-1 text-xs text-ink-muted">{win.complete_year_count} 个完整自然年度</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">年化波动率</dt>
            <dd className="text-ink">
              {formatNullablePercent(win.annual_volatility)}
              {win.annual_volatility == null && win.volatility_status && (
                <span className="ml-1 text-xs text-ink-muted">{metricStatusLabel(win.volatility_status)}</span>
              )}
              {win.monthly_return_count != null && (
                <span className="ml-1 text-xs text-ink-muted">{win.monthly_return_count} 个月度收益样本</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">最大回撤</dt>
            <dd className="text-ink">{formatNullablePercent(win.max_drawdown)}
              {win.max_drawdown == null && win.drawdown_status && (
                <span className="ml-1 text-xs text-ink-muted">{metricStatusLabel(win.drawdown_status)}</span>
              )}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">数据覆盖</dt>
            <dd className="text-ink">
              日度 {win.daily_observation_count ?? "—"} · 月度 {win.monthly_return_count ?? "—"}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">历史深度</dt>
            <dd className="text-ink">{historyDepthLabel(win.history_depth)}</dd>
          </div>
        </dl>
      </section>
      )}

      {isActive && data.trailing_returns && (
        <section className="mt-6 rounded-lg border border-line bg-surface p-4 text-sm">
          <h2 className="font-medium text-ink">
            区间收益
            <span className="ml-2 text-xs text-ink-muted">截至 {data.trailing_returns.as_of_date}</span>
          </h2>
          <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
            {(["1m", "3m", "6m", "1y", "3y", "5y"] as const).map((key) => {
              const p = data.trailing_returns?.periods[key];
              const label = { "1m": "近 1 月", "3m": "近 3 月", "6m": "近 6 月", "1y": "近 1 年", "3y": "近 3 年", "5y": "近 5 年" }[key];
              const available = p?.status === "available" && p.cumulative_return != null;
              return (
                <div key={key} className="rounded border border-line px-3 py-2">
                  <div className="text-xs text-ink-muted">{label}</div>
                  <div className="text-lg font-medium text-ink">
                    {available && p ? formatPercent(p.cumulative_return!) : "—"}
                  </div>
                  {!available && (
                    <div className="text-xs text-ink-muted">
                      {p?.status === "insufficient_history" ? "历史不足" : p?.status === "start_point_too_stale" ? "起点过旧" : "不可用"}
                    </div>
                  )}
                  {available && p && p.annualized_return != null && (key === "3y" || key === "5y") && (
                    <div className="text-xs text-ink-muted">年化 {formatPercent(p.annualized_return)}</div>
                  )}
                </div>
              );
            })}
          </div>
        </section>
      )}

      {isActive && (
        <section className="mt-6 rounded-lg border border-line bg-surface p-4 text-sm">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h2 className="font-medium text-ink">收益曲线</h2>
            <div className="flex flex-wrap gap-1" role="tablist" aria-label="收益曲线区间">
              {RETURN_SERIES_RANGES.map(({ key, label }) => (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={seriesRange === key}
                  className={`rounded-full px-3 py-1 text-xs transition-colors ${
                    seriesRange === key
                      ? "bg-brand text-white"
                      : "bg-surface-muted text-ink-muted hover:text-ink"
                  }`}
                  onClick={() => setSeriesRange(key)}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
          <div className="mt-3" data-testid="return-series-panel">
            {seriesQ.isError ? (
              <p className="py-8 text-center text-sm text-danger" role="alert">
                收益曲线加载失败，请稍后重试。
              </p>
            ) : seriesQ.isLoading ? (
              <LoadingState label="加载收益曲线…" />
            ) : (seriesQ.data?.points.length ?? 0) > 0 ? (
              <ReturnSeriesChart
                points={seriesQ.data!.points}
                pointType={seriesQ.data!.point_type}
              />
            ) : (
              <p className="py-8 text-center text-sm text-ink-muted">
                该区间历史数据不足，暂无法绘制收益曲线。
              </p>
            )}
          </div>
        </section>
      )}

      {isActive && (
        <>
          <h2 className="mt-8 font-medium text-ink">年度收益</h2>
          <div className="mt-2 max-h-96 overflow-auto rounded-lg border border-line">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-surface-muted">
                <tr>
                  <th className="px-3 py-2 text-left font-medium text-ink-muted">年份</th>
                  <th className="px-3 py-2 text-right font-medium text-ink-muted">年化收益</th>
                  <th className="px-3 py-2 text-left font-medium text-ink-muted">完整性</th>
                  <th className="px-3 py-2 text-left font-medium text-ink-muted">统计区间</th>
                  <th className="px-3 py-2 text-left font-medium text-ink-muted">预览窗口</th>
                </tr>
              </thead>
              <tbody>
                {annualReturns.map((r) => (
                  <tr key={r.year} className="border-t border-line">
                    <td className="px-3 py-2 text-ink">{r.year}</td>
                    <td className="px-3 py-2 text-right font-mono-numeric text-ink">{formatPercent(r.annual_return)}</td>
                    <td className="px-3 py-2 text-ink">{annualCompletenessLabel(r)}</td>
                    <td className="px-3 py-2 font-mono-numeric text-xs text-ink-muted">
                      {formatAnnualPeriod(r.start_date, r.end_date)}
                    </td>
                    <td className="px-3 py-2 text-ink">{r.in_simulation ? "参与" : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {historicalSnapshots.length > 0 && (
            <>
              <h2 className="mt-8 font-medium text-ink">历史计划快照</h2>
              <ul className="mt-2 space-y-1 text-sm">
                {historicalSnapshots.map((s) => (
                  <li key={s.id} className="rounded border border-line px-3 py-2">
                    <div className="text-ink">
                      {s.inclusion_date} · {historyDepthLabel(s.history_depth)} ·{" "}
                      {s.complete_year_count} 完整年 · {s.monthly_return_count} 月收益观测
                    </div>
                    <div className="mt-1 text-xs text-ink-muted">
                      指标版本 {s.metrics_version} · 创建于{" "}
                      {new Date(s.created_at).toLocaleString("zh-CN")}
                    </div>
                    {(s.warnings ?? []).length > 0 && (
                      <ul className="mt-1 list-disc pl-5 text-xs text-warning">
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
              <h2 className="mt-8 font-medium text-ink">引用计划</h2>
              <ul className="mt-2 space-y-1 text-sm">
                {referencingPlans.map((p) => (
                  <li key={p.plan_id}>
                    <Link
                      href={`/plans/${p.plan_id}/overview`}
                      className="text-brand underline-offset-2 hover:underline"
                    >
                      {p.plan_name}
                    </Link>
                    <span className="text-ink-muted"> · 快照纳入 {p.snapshot_inclusion_date || "—"}</span>
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
          <Button variant="secondary" onClick={() => setShowFetchDialog(false)}>
            关闭
          </Button>
        }
      >
        <dl className="space-y-2 text-sm">
          <div>
            <dt className="text-ink-muted">任务状态</dt>
            <dd className="text-ink" data-testid="fetch-status-job-status">{jobState.job?.status ?? "—"}</dd>
          </div>
          <div>
            <dt className="text-ink-muted">阶段</dt>
            <dd className="text-ink">{jobState.job?.phase ?? "—"}</dd>
          </div>
          <div>
            <dt className="text-ink-muted">进度</dt>
            <dd className="text-ink">{Math.round(jobState.progress * 100)}%</dd>
          </div>
          {jobState.error && (
            <div>
              <dt className="text-ink-muted">错误</dt>
              <dd className="text-danger">{jobState.error}</dd>
            </div>
          )}
        </dl>
      </Dialog>

      <ConfirmDialog
        open={showDeleteConfirm}
        title="删除标的"
        description={`确定删除标的「${inst.name}」？此操作不可撤销。`}
        confirmLabel="删除标的"
        variant="danger"
        pending={deleteMut.isPending}
        error={deleteError}
        onConfirm={() => deleteMut.mutate()}
        onClose={() => {
          setShowDeleteConfirm(false);
          setDeleteError(null);
        }}
      />
    </div>
  );
}
