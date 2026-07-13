"use client";

import { useParams } from "next/navigation";
import Link from "next/link";
import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getMarketAssetDetail,
  isTaskActive,
  syncMarketAssetHistory,
  setMarketAssetHistoryAutoUpdate,
  type MarketAssetDetail,
  type WorkerTask,
} from "@/lib/api/market-assets";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import { queryErrorMessage } from "@/lib/query-error";
import {
  adjustPolicyLabel,
  dataSourceLabel,
  formatAnnualPeriod,
  formatDateTimeFromMs,
  formatPercent,
  instrumentTypeLabel,
  pointTypeLabel,
} from "@/lib/format";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { TaskErrorInline } from "@/components/ui/TaskErrorInline";
import { LastRefreshMeta } from "@/components/ui/LastRefreshMeta";
import { RefreshTaskButton } from "@/components/ui/RefreshTaskButton";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { ReturnSeriesChart } from "@/components/charts/ReturnSeriesChart";
import { HelpLabel } from "@/components/ui/HelpLabel";
import { MetricHelp } from "@/components/ui/MetricHelp";
import {
  defaultHistoryRange,
  filterHistoryPoints,
  HISTORY_RANGE_OPTIONS,
  historyRangeLabel,
  isHistoryRangeAvailable,
  toChartPoints,
  type HistoryRangeKey,
} from "@/lib/market-asset-history-range";

const HISTORY_TASK_LABELS = {
  pending: "等待刷新",
  running: "刷新中",
  pre_complete: "处理中",
  complete: "刷新成功",
  failed: "刷新失败",
} as const;

export default function MarketAssetDetailPage() {
  const rawKey = useParams().assetKey as string;
  const assetKey = decodeURIComponent(rawKey);
  const qc = useQueryClient();
  const [createError, setCreateError] = useState<string | null>(null);
  const [manualTask, setManualTask] = useState<WorkerTask | null>(null);
  const [autoUpdatePending, setAutoUpdatePending] = useState(false);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ["market-asset-detail", assetKey],
    queryFn: () => getMarketAssetDetail(assetKey),
  });

  const serverTask = data?.history.task ?? null;

  // Prefer an active task from the server snapshot (e.g. a sync started
  // elsewhere), otherwise the task we just created locally.
  const serverActiveId =
    serverTask && isTaskActive(serverTask.status) ? serverTask.id : null;
  const trackedTaskId = serverActiveId ?? manualTask?.id ?? null;

  const invalidateDetail = () => {
    setManualTask(null);
    void qc.invalidateQueries({ queryKey: ["market-asset-detail", assetKey] });
  };

  const { task: polledTask, pollError } = useTaskStatus(trackedTaskId, {
    initialTask:
      serverTask && serverTask.id === trackedTaskId
        ? serverTask
        : manualTask?.id === trackedTaskId
          ? manualTask
          : undefined,
    onComplete: invalidateDetail,
    onFailed: invalidateDetail,
    onCanceled: invalidateDetail,
  });

  const task = polledTask ?? serverTask ?? manualTask;
  const taskActive = isTaskActive(task?.status);

  const rawPoints = useMemo(() => data?.points ?? [], [data?.points]);

  // Availability is O(points) per range; memoize so task polling re-renders
  // don't refilter a multi-thousand-point series for every button.
  const availableRanges = useMemo(() => {
    const set = new Set<HistoryRangeKey>();
    for (const option of HISTORY_RANGE_OPTIONS) {
      if (isHistoryRangeAvailable(rawPoints, option.key)) set.add(option.key);
    }
    return set;
  }, [rawPoints]);

  // User-picked range, or null until the user touches the control. The
  // effective range derives from it so a data refresh that shrinks coverage
  // automatically falls back instead of leaving an empty chart.
  const [selectedRange, setSelectedRange] = useState<HistoryRangeKey | null>(
    null,
  );
  const historyRange: HistoryRangeKey = useMemo(() => {
    if (selectedRange) {
      return availableRanges.has(selectedRange) ? selectedRange : "all";
    }
    return defaultHistoryRange(rawPoints);
  }, [selectedRange, availableRanges, rawPoints]);
  // A picked range that lost coverage after a refresh renders as 全部 with a hint.
  const rangeFellBack =
    selectedRange !== null && selectedRange !== "all" && historyRange === "all";

  const filteredRawPoints = useMemo(
    () => filterHistoryPoints(rawPoints, historyRange),
    [rawPoints, historyRange],
  );
  const chartPoints = useMemo(
    () => toChartPoints(filteredRawPoints),
    [filteredRawPoints],
  );

  if (isLoading && !data) {
    return <LoadingState label="加载资产详情…" />;
  }
  if (isError && !data) {
    return (
      <ErrorState
        message="无法加载资产详情。请确认资产目录已同步且后端服务可用。"
        onRetry={() => void refetch()}
        backHref="/assets"
        backLabel="返回资产目录"
        technicalDetail={queryErrorMessage(error)}
      />
    );
  }
  if (!data) return null;

  const { asset, history } = data;
  const hasHistory = history.point_count > 0 || (data.points?.length ?? 0) > 0;
  const annualReturns = [...(data.annual_returns ?? [])].sort(
    (a, b) => b.year - a.year,
  );
  const trailing = data.trailing_returns;

  const createHistoryTask = (
    mode: "default_refresh" | "switch_source_full",
  ) => {
    setCreateError(null);
    return syncMarketAssetHistory({
      asset_key: assetKey,
      adjust_policy: history.adjust_policy || undefined,
      point_type: history.point_type || undefined,
      mode,
    });
  };
  const handleTask = (t: WorkerTask) => setManualTask(t);

  const toggleAutoUpdate = async () => {
    setAutoUpdatePending(true);
    setCreateError(null);
    try {
      const updated = await setMarketAssetHistoryAutoUpdate({
        asset_key: assetKey,
        adjust_policy: history.adjust_policy,
        point_type: history.point_type,
        enabled: !history.auto_update?.enabled,
      });
      qc.setQueryData<MarketAssetDetail>(
        ["market-asset-detail", assetKey],
        (current) =>
          current
            ? {
                ...current,
                history: { ...current.history, auto_update: updated },
              }
            : current,
      );
      await qc.invalidateQueries({
        queryKey: ["market-asset-detail", assetKey],
      });
    } catch (err) {
      setCreateError(queryErrorMessage(err));
    } finally {
      setAutoUpdatePending(false);
    }
  };

  const refreshControls = (
    <div className="flex flex-col items-end gap-2">
      <div className="flex flex-wrap items-center justify-end gap-2">
        {task && (
          <TaskStatusBadge status={task.status} labels={HISTORY_TASK_LABELS} />
        )}
        {task?.status === "failed" && (
          <TaskErrorInline
            errorCode={task.error_code}
            errorMessage={task.error_message}
          />
        )}
        <RefreshTaskButton
          data-testid="refresh-history-button"
          createTask={() => createHistoryTask("default_refresh")}
          onTask={handleTask}
          onError={setCreateError}
          activeTask={task}
        >
          刷新历史数据
        </RefreshTaskButton>
        <TaskCancelButton
          task={task}
          shared
          onCanceled={invalidateDetail}
        />
        <button
          type="button"
          aria-label={
            history.auto_update?.enabled
              ? `自动更新：每 ${history.auto_update.interval_hours >= 24 ? `${history.auto_update.interval_hours / 24} 天` : `${history.auto_update.interval_hours} 小时`}`
              : "启用每日自动更新"
          }
          disabled={autoUpdatePending}
          onClick={() => void toggleAutoUpdate()}
          className="rounded-md border border-line px-3 py-2 text-sm text-ink hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60"
        >
          {autoUpdatePending
            ? "更新中…"
            : history.auto_update?.enabled
              ? `自动更新：每 ${history.auto_update.interval_hours >= 24 ? `${history.auto_update.interval_hours / 24} 天` : `${history.auto_update.interval_hours} 小时`}`
              : "启用每日自动更新"}
        </button>
        {history.auto_update?.enabled && (
          <Link
            href={`/admin/auto-updates?q=${encodeURIComponent(assetKey)}`}
            className="text-sm text-brand hover:text-brand-strong"
          >
            管理自动更新
          </Link>
        )}
        {history.can_switch_source && (
          <RefreshTaskButton
            variant="secondary"
            data-testid="switch-source-button"
            createTask={() => createHistoryTask("switch_source_full")}
            onTask={handleTask}
            onError={setCreateError}
            activeTask={task}
          >
            切换数据源并全量刷新
          </RefreshTaskButton>
        )}
      </div>
      {createError && <p className="text-xs text-danger">{createError}</p>}
      {pollError && (
        <p className="text-xs text-danger">任务状态查询失败：{pollError}</p>
      )}
      <LastRefreshMeta
        lastSuccessAt={history.last_success_at}
        dataAsOf={history.data_as_of}
        sourceName={history.source_name}
      />
    </div>
  );

  return (
    <div className="max-w-6xl">
      <PageHeader
        backHref="/assets"
        backLabel="资产目录"
        title={asset.name || asset.symbol}
        eyebrow={asset.asset_key}
        description={`${asset.market} / ${instrumentTypeLabel(asset.instrument_type)} · ${asset.exchange || "—"}`}
        status={
          taskActive ? (
            <LoadingState label="历史数据同步中…" className="text-xs" />
          ) : undefined
        }
        secondaryActions={refreshControls}
      />

      <section className="rounded-lg border border-line bg-surface p-4">
        <h2 className="mb-3 font-medium text-ink">基础信息</h2>
        <dl className="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
          <div>
            <dt className="text-ink-muted">代码</dt>
            <dd className="font-mono-numeric text-ink">{asset.symbol}</dd>
          </div>
          <div>
            <dt className="text-ink-muted"><HelpLabel label="市场 / 资产类型" termKey="instrument_kind" /></dt>
            <dd className="text-ink">
              {asset.market} / {instrumentTypeLabel(asset.instrument_type)}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">交易所</dt>
            <dd className="text-ink">{asset.exchange || "—"}</dd>
          </div>
          <div>
            <dt className="text-ink-muted"><HelpLabel label="数据类别 / 币种" termKey="instrument_kind" /></dt>
            <dd className="text-ink">
              {asset.instrument_kind || "—"} / {asset.currency || "—"}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted"><HelpLabel label="收费模式" termKey="fee_included" /></dt>
            <dd className="text-ink" data-testid="fund-fee-mode">
              {asset.fee_mode === "front_end"
                ? "前端收费"
                : asset.fee_mode === "back_end"
                  ? "后端收费"
                  : asset.fee_mode === "standard"
                    ? "标准"
                    : "—"}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted"><HelpLabel label="净值主代码" termKey="canonical_fund_symbol" /></dt>
            <dd
              className="font-mono-numeric text-ink"
              data-testid="canonical-fund-symbol"
            >
              {asset.canonical_symbol || asset.symbol}
            </dd>
          </div>
        </dl>
        {(asset.fee_mode === "front_end" || asset.fee_mode === "back_end") && (
          <p
            className="mt-3 rounded-md border border-info/25 bg-info/5 px-3 py-2 text-xs text-ink"
            role="note"
            data-testid="fund-fee-history-note"
          >
            当前交易代码采用{asset.fee_mode === "front_end" ? "前端" : "后端"}
            收费，并共用 {asset.canonical_symbol || asset.symbol}{" "}
            的净值历史。历史回测仅使用净值序列，不包含申购、赎回或后端申购费用。
          </p>
        )}
      </section>

      <section
        className="mt-6 rounded-lg border border-line bg-surface p-4"
        data-testid="history-state-panel"
      >
        <h2 className="mb-3 font-medium text-ink">历史数据状态</h2>
        <dl className="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-3">
          <div>
            <dt className="text-ink-muted">最后成功刷新</dt>
            <dd
              className="font-mono-numeric text-ink"
              data-testid="history-last-success"
            >
              {formatDateTimeFromMs(history.last_success_at)}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted"><HelpLabel label="数据截至日期" termKey="data_as_of" /></dt>
            <dd
              className="font-mono-numeric text-ink"
              data-testid="history-data-as-of"
            >
              {history.data_as_of || "—"}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">数据源</dt>
            <dd className="text-ink" data-testid="history-source">
              {history.source_name ? dataSourceLabel(history.source_name) : "—"}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">数据点数</dt>
            <dd
              className="font-mono-numeric text-ink"
              data-testid="history-point-count"
            >
              {history.point_count || 0}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted"><HelpLabel label="复权 / 行情点类型" termKey="adjustment_policy" /><MetricHelp termKey="point_type" /></dt>
            <dd className="text-ink">
              {adjustPolicyLabel(history.adjust_policy)} /{" "}
              {pointTypeLabel(history.point_type)}
            </dd>
          </div>
          <div className="min-w-0">
            <dt className="text-ink-muted">当前任务</dt>
            <dd
              className="flex min-w-0 items-center gap-2 text-ink"
              data-testid="history-current-task"
            >
              {task ? (
                <span className="shrink-0">
                  <TaskStatusBadge
                    status={task.status}
                    labels={HISTORY_TASK_LABELS}
                  />
                </span>
              ) : (
                "无进行中任务"
              )}
              {task?.status === "failed" && (
                <TaskErrorInline
                  className="min-w-0 max-w-full overflow-hidden"
                  errorCode={task.error_code}
                  errorMessage={task.error_message}
                />
              )}
            </dd>
          </div>
        </dl>
      </section>

      {!hasHistory ? (
        <EmptyState
          className="mt-6"
          title="尚未同步历史数据"
          description="该资产还没有本地历史数据。点击右上角「刷新历史数据」创建同步任务，完成后即可查看曲线与年度收益。"
        />
      ) : (
        <>
          <section className="mt-6 rounded-lg border border-line bg-surface p-4 text-sm">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div>
                <h2 className="font-medium text-ink">历史曲线</h2>
                <p
                  className="mt-1 text-xs text-ink-muted"
                  data-testid="history-range-summary"
                >
                  {rangeFellBack
                    ? "当前区间历史数据不足，已显示全部区间。"
                    : `当前区间 ${historyRangeLabel(historyRange)} · ${chartPoints.length} / ${rawPoints.length} 个点`}
                </p>
              </div>
              <span className="text-xs text-ink-muted">
                {pointTypeLabel(history.point_type)} · 来源{" "}
                {dataSourceLabel(history.source_name)}
                {isFetching && <span className="ml-2">刷新中…</span>}
              </span>
            </div>
            <div className="mt-3 overflow-x-auto">
              <div
                className="inline-flex min-w-max gap-1 rounded-md border border-line bg-surface-muted/40 p-1"
                role="group"
                aria-label="历史曲线时间范围"
              >
                {HISTORY_RANGE_OPTIONS.map((option) => {
                  const active = option.key === historyRange;
                  const available = availableRanges.has(option.key);
                  return (
                    <button
                      key={option.key}
                      type="button"
                      data-testid={`history-range-${option.key}`}
                      disabled={!available}
                      aria-pressed={active}
                      onClick={() => setSelectedRange(option.key)}
                      className={
                        active
                          ? "rounded border border-brand bg-brand/10 px-2.5 py-1 text-xs font-medium text-brand-strong"
                          : available
                            ? "rounded border border-transparent px-2.5 py-1 text-xs text-ink-muted hover:bg-surface hover:text-ink"
                            : "cursor-not-allowed rounded border border-transparent px-2.5 py-1 text-xs text-ink-muted opacity-50"
                      }
                    >
                      {option.label}
                    </button>
                  );
                })}
              </div>
              <p className="mt-1 text-xs text-ink-muted">
                不可选区间表示该资产历史数据不足以覆盖对应时长。
              </p>
            </div>
            <div className="mt-3" data-testid="market-asset-chart">
              {chartPoints.length > 0 ? (
                <ReturnSeriesChart
                  points={chartPoints}
                  pointType={history.point_type}
                />
              ) : (
                <p className="py-8 text-center text-sm text-ink-muted">
                  历史数据不足，暂无法绘制曲线。
                </p>
              )}
            </div>
          </section>

          {trailing && (
            <section className="mt-6 rounded-lg border border-line bg-surface p-4 text-sm">
              <h2 className="font-medium text-ink">
                区间收益
                <span className="ml-2 text-xs text-ink-muted">
                  截至 {trailing.as_of_date}
                </span>
              </h2>
              <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
                {(["1m", "3m", "6m", "1y", "3y", "5y"] as const).map((key) => {
                  const p = trailing.periods?.[key];
                  const label = {
                    "1m": "近 1 月",
                    "3m": "近 3 月",
                    "6m": "近 6 月",
                    "1y": "近 1 年",
                    "3y": "近 3 年",
                    "5y": "近 5 年",
                  }[key];
                  const available =
                    p?.status === "available" && p.cumulative_return != null;
                  return (
                    <div
                      key={key}
                      className="rounded border border-line px-3 py-2"
                    >
                      <div className="text-xs text-ink-muted">{label}</div>
                      <div className="text-lg font-medium text-ink">
                        {available && p
                          ? formatPercent(p.cumulative_return!)
                          : "—"}
                      </div>
                      {!available && (
                        <div className="text-xs text-ink-muted">
                          {p?.status === "insufficient_history"
                            ? "历史不足"
                            : p?.status === "start_point_too_stale"
                              ? "起点过旧"
                              : "不可用"}
                        </div>
                      )}
                      {available &&
                        p &&
                        p.annualized_return != null &&
                        (key === "3y" || key === "5y") && (
                          <div className="text-xs text-ink-muted">
                            年化 {formatPercent(p.annualized_return)}
                          </div>
                        )}
                    </div>
                  );
                })}
              </div>
            </section>
          )}

          {annualReturns.length > 0 && (
            <>
              <h2 className="mt-8 font-medium text-ink">年度收益</h2>
              <div className="mt-2 max-h-96 overflow-auto rounded-lg border border-line">
                <table
                  className="w-full text-sm"
                  data-testid="annual-returns-table"
                >
                  <thead className="sticky top-0 bg-surface-muted">
                    <tr>
                      <th className="px-3 py-2 text-left font-medium text-ink-muted">
                        年份
                      </th>
                      <th className="px-3 py-2 text-right font-medium text-ink-muted">
                        <HelpLabel label="年化收益" termKey="annual_return" />
                      </th>
                      <th className="px-3 py-2 text-left font-medium text-ink-muted">
                        <HelpLabel label="完整性" termKey="complete_year" />
                      </th>
                      <th className="px-3 py-2 text-left font-medium text-ink-muted">
                        统计区间
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {annualReturns.map((r) => (
                      <tr key={r.year} className="border-t border-line">
                        <td className="px-3 py-2 text-ink">{r.year}</td>
                        <td className="px-3 py-2 text-right font-mono-numeric text-ink">
                          {formatPercent(r.annual_return)}
                        </td>
                        <td className="px-3 py-2 text-ink">
                          <span className="inline-flex items-center">{r.is_partial ? "部分年度" : "完整年度"}<MetricHelp termKey={r.is_partial ? "partial_year" : "complete_year"} /></span>
                        </td>
                        <td className="px-3 py-2 font-mono-numeric text-xs text-ink-muted">
                          {formatAnnualPeriod(r.start_date, r.end_date)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </>
      )}

      <p className="mt-8 text-xs text-ink-muted">
        全量市场资产与历史数据由系统统一同步维护；计划持仓直接引用该资产，无需单独录入。
      </p>
    </div>
  );
}
