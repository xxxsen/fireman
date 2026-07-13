"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  isTaskActive,
  listMarketAssets,
  syncFXRates,
  syncMarketAssets,
  type DirectoryScopeStatus,
  type DirectoryScopeSyncView,
  type DirectorySyncScope,
  type DirectorySyncUnitView,
  type MarketAssetSyncView,
  type WorkerTask,
} from "@/lib/api/market-assets";
import { ApiError } from "@/lib/api/client";
import {
  dataSourceLabel,
  formatDateTimeFromMs,
  instrumentTypeLabel,
} from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import { PageHeader } from "@/components/ui/PageHeader";
import { Badge, type BadgeVariant } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Skeleton } from "@/components/ui/Skeleton";
import { SplitButton } from "@/components/ui/SplitButton";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { TaskErrorInline } from "@/components/ui/TaskErrorInline";
import { RefreshTaskButton } from "@/components/ui/RefreshTaskButton";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { HelpLabel } from "@/components/ui/HelpLabel";

const PAGE_SIZE = 50;
const STALE_AFTER_MS = 7 * 24 * 60 * 60 * 1000;

const SCOPE_STATUS_LABELS: Record<DirectoryScopeStatus, string> = {
  running: "同步中",
  complete: "已同步",
  partial: "部分未同步",
  failed: "同步失败",
  never: "从未同步",
};

const SCOPE_STATUS_VARIANTS: Record<DirectoryScopeStatus, BadgeVariant> = {
  running: "info",
  complete: "positive",
  partial: "warning",
  failed: "danger",
  never: "neutral",
};

const MARKET_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "全部市场" },
  { value: "CN", label: "中国市场" },
  { value: "HK", label: "香港市场" },
  { value: "US", label: "美国市场" },
  { value: "SYS", label: "系统内置" },
];

const TYPE_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "全部类型" },
  { value: "cn_exchange_stock", label: "A 股" },
  { value: "cn_exchange_fund", label: "场内 ETF / LOF" },
  { value: "cn_mutual_fund", label: "公募基金" },
  { value: "hk_stock", label: "港股" },
  { value: "hk_etf", label: "香港 ETF" },
  { value: "us_stock", label: "美国股票" },
  { value: "us_etf", label: "美国 ETF" },
  { value: "cash", label: "现金" },
];

const DIRECTORY_TASK_LABELS = { complete: "最近同步成功" } as const;

function marketAssetDetailHref(assetKey: string): string {
  return `/assets/market/${encodeURIComponent(assetKey)}`;
}

/**
 * One directory sync unit row: polls its latest active task and reports
 * terminal transitions so the parent can refetch the scope aggregation.
 */
function DirectoryUnitRow({
  unit,
  onChanged,
}: {
  unit: DirectorySyncUnitView;
  onChanged: () => void;
}) {
  const serverTask = unit.task ?? null;
  const activeTaskId =
    serverTask && isTaskActive(serverTask.status) ? serverTask.id : null;

  const { task: polledTask, pollError } = useTaskStatus(activeTaskId, {
    initialTask:
      serverTask && serverTask.id === activeTaskId ? serverTask : undefined,
    onComplete: onChanged,
    onFailed: onChanged,
    onCanceled: onChanged,
  });

  const task = polledTask ?? serverTask;
  const active = isTaskActive(task?.status);

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-1 py-1.5 pl-6"
      data-testid={`directory-sync-unit-${unit.sync_key}`}
    >
      <span className="w-40 shrink-0 text-xs text-ink">{unit.label}</span>
      <span className="flex min-w-0 items-center gap-2 text-xs text-ink-muted">
        {task ? (
          <TaskStatusBadge
            status={task.status}
            labels={DIRECTORY_TASK_LABELS}
          />
        ) : (
          <span>从未同步</span>
        )}
        {active && <LoadingState label="同步进行中…" className="text-xs" />}
        <TaskCancelButton
          task={task}
          shared
          className="min-h-7 px-2 py-0.5 text-xs"
          onCanceled={onChanged}
        />
        {task?.status === "failed" && (
          <TaskErrorInline
            errorCode={task.error_code}
            errorMessage={task.error_message}
          />
        )}
        {pollError && (
          <span className="text-danger">任务状态查询失败：{pollError}</span>
        )}
      </span>
      <span className="ml-auto text-xs text-ink-muted">
        最近成功：
        <span className="font-mono-numeric text-ink">
          {formatDateTimeFromMs(unit.last_success_at)}
        </span>
      </span>
    </div>
  );
}

/**
 * One directory scope block: aggregated status computed by the backend, a
 * split button (sync all units / sync one unit) and the per-unit rows.
 */
function DirectoryScopeRow({
  view,
  onChanged,
}: {
  view: DirectoryScopeSyncView;
  onChanged: () => void;
}) {
  const [submitting, setSubmitting] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [localTasks, setLocalTasks] = useState<Record<string, WorkerTask>>({});

  const submit = async (body: Parameters<typeof syncMarketAssets>[0]) => {
    if (submitting) return;
    setSubmitting(true);
    setCreateError(null);
    try {
      const result = await syncMarketAssets(body);
      setLocalTasks((current) => {
        const next = { ...current };
        for (const item of result.tasks) {
          if (isTaskActive(item.task.status)) next[item.sync_key] = item.task;
        }
        return next;
      });
      // The backend recomputes the aggregation; refetch instead of guessing.
      onChanged();
    } catch (err) {
      if (err instanceof ApiError && err.code === "task_already_active") {
        setCreateError("已有同步任务正在执行，已继续跟踪该任务。");
        onChanged();
        return;
      }
      const message =
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "创建任务失败";
      setCreateError(message);
    } finally {
      setSubmitting(false);
    }
  };

  const anySuccess = view.units.some((u) => u.last_success_at);
  const visibleUnits = view.units.map((unit) => ({
    ...unit,
    task: isTaskActive(localTasks[unit.sync_key]?.status)
      ? localTasks[unit.sync_key]
      : unit.task,
  }));
  const visibleScopeStatus = visibleUnits.some((unit) =>
    isTaskActive(unit.task?.status),
  )
    ? "running"
    : view.status;
  const unitChanged = (syncKey: string) => {
    setLocalTasks((current) => {
      if (!current[syncKey]) return current;
      const next = { ...current };
      delete next[syncKey];
      return next;
    });
    onChanged();
  };
  const lastFullSuccess = view.last_success_at
    ? formatDateTimeFromMs(view.last_success_at)
    : anySuccess
      ? "部分未同步"
      : "—";

  return (
    <div className="py-2" data-testid={`directory-sync-${view.scope}`}>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="w-32 shrink-0 text-sm font-medium text-ink">
          {view.label}
        </span>
        <span className="flex items-center gap-2 text-xs text-ink-muted">
          <Badge variant={SCOPE_STATUS_VARIANTS[visibleScopeStatus] ?? "neutral"}>
            <span
              data-testid={`scope-status-${view.scope}`}
              data-status={visibleScopeStatus}
            >
              {SCOPE_STATUS_LABELS[visibleScopeStatus] ?? visibleScopeStatus}
            </span>
          </Badge>
          {visibleScopeStatus === "running" && (
            <LoadingState label="同步进行中…" className="text-xs" />
          )}
        </span>
        <span className="text-xs text-ink-muted">
          最近全量成功：
          <span className="font-mono-numeric text-ink">{lastFullSuccess}</span>
        </span>
        <span className="ml-auto flex items-center gap-2">
          {createError && (
            <span className="text-xs text-danger">{createError}</span>
          )}
          <SplitButton
            data-testid={`sync-button-${view.scope}`}
            pending={submitting}
            onMain={() =>
              void submit({ scope: view.scope as DirectorySyncScope })
            }
            items={visibleUnits.map((unit) => {
              const active = isTaskActive(unit.task?.status);
              return {
                key: unit.sync_key,
                label: `同步 ${unit.label}`,
                disabled: active,
                note: active ? "同步中" : undefined,
              };
            })}
            onItem={(syncKey) => void submit({ sync_key: syncKey })}
          >
            同步全部
          </SplitButton>
        </span>
      </div>
      <div className="mt-1 divide-y divide-line/60">
        {visibleUnits.map((unit) => (
          <DirectoryUnitRow
            key={unit.sync_key}
            unit={unit}
            onChanged={() => unitChanged(unit.sync_key)}
          />
        ))}
      </div>
    </div>
  );
}

/**
 * FX sync entry. FX pairs live outside market_assets so there is no sync-state
 * row to hydrate from; status comes from the created task's polling only.
 */
function FXSyncRow({
  view,
  onChanged,
}: {
  view?: MarketAssetSyncView | null;
  onChanged: () => void;
}) {
  const serverTask = view?.task ?? null;
  const [manualTask, setManualTask] = useState<WorkerTask | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  const serverActiveId =
    serverTask && isTaskActive(serverTask.status) ? serverTask.id : null;
  const trackedTaskId = serverActiveId ?? manualTask?.id ?? null;
  const taskSettled = () => {
    setManualTask(null);
    onChanged();
  };

  const { task: polledTask, pollError } = useTaskStatus(trackedTaskId, {
    initialTask:
      serverTask && serverTask.id === trackedTaskId
        ? serverTask
        : manualTask?.id === trackedTaskId
          ? manualTask
          : undefined,
    onComplete: taskSettled,
    onFailed: taskSettled,
    onCanceled: taskSettled,
  });
  const task = polledTask ?? serverTask ?? manualTask;
  const active = isTaskActive(task?.status);

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-2 py-2"
      data-testid="fx-sync-row"
    >
      <span className="w-32 shrink-0 text-sm font-medium text-ink">
        汇率（USD/HKD）
      </span>
      <span className="flex items-center gap-2 text-xs text-ink-muted">
        {task && (
          <TaskStatusBadge
            status={task.status}
            labels={DIRECTORY_TASK_LABELS}
          />
        )}
        {active && <LoadingState label="同步进行中…" className="text-xs" />}
        <TaskCancelButton
          task={task}
          shared
          className="min-h-7 px-2 py-0.5 text-xs"
          onCanceled={taskSettled}
        />
        {task?.status === "failed" && (
          <TaskErrorInline
            errorCode={task.error_code}
            errorMessage={task.error_message}
          />
        )}
        {pollError && (
          <span className="text-danger">任务状态查询失败：{pollError}</span>
        )}
      </span>
      <span className="text-xs text-ink-muted">
        最近成功：
        <span className="font-mono-numeric text-ink">
          {formatDateTimeFromMs(view?.last_success_at)}
        </span>
      </span>
      <span className="ml-auto flex items-center gap-2">
        {createError && (
          <span className="text-xs text-danger">{createError}</span>
        )}
        <RefreshTaskButton
          variant="secondary"
          className="min-h-8 px-3 py-1 text-xs"
          data-testid="fx-sync-button"
          createTask={() => {
            setCreateError(null);
            return syncFXRates();
          }}
          onTask={(t: WorkerTask) => setManualTask(t)}
          onError={setCreateError}
          activeTask={task}
        >
          同步汇率数据
        </RefreshTaskButton>
      </span>
    </div>
  );
}

export default function MarketAssetsPage() {
  const qc = useQueryClient();
  const [symbolInput, setSymbolInput] = useState("");
  const [nameInput, setNameInput] = useState("");
  const [symbolQ, setSymbolQ] = useState("");
  const [nameQ, setNameQ] = useState("");
  const [market, setMarket] = useState("");
  const [instrumentType, setInstrumentType] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const [offset, setOffset] = useState(0);
  const [pageInput, setPageInput] = useState("");
  // null = auto (derived from sync state); true/false = user's explicit choice
  // which is respected for the rest of the page lifetime.
  const [panelOpenOverride, setPanelOpenOverride] = useState<boolean | null>(
    null,
  );

  useEffect(() => {
    const timer = setTimeout(() => {
      setSymbolQ(symbolInput.trim());
      setNameQ(nameInput.trim());
      setOffset(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [symbolInput, nameInput]);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: [
      "market-assets",
      market,
      instrumentType,
      symbolQ,
      nameQ,
      includeInactive,
      offset,
    ],
    // fetchedAt is captured at request time so staleness checks stay pure in render.
    queryFn: async () => {
      const result = await listMarketAssets({
        market: market || undefined,
        instrumentTypes: instrumentType ? [instrumentType] : undefined,
        symbolQ: symbolQ || undefined,
        nameQ: nameQ || undefined,
        includeInactive: includeInactive || undefined,
        limit: PAGE_SIZE,
        offset,
      });
      return { ...result, fetchedAt: Date.now() };
    },
    placeholderData: keepPreviousData,
  });

  const syncs = useMemo(() => data?.syncs ?? [], [data]);
  const assets = data?.assets ?? [];
  const total = data?.total ?? 0;
  const everSynced = syncs.some((s) => s.units.some((u) => u.last_success_at));
  const fetchedAt = data?.fetchedAt ?? 0;
  const staleScopes = syncs.filter(
    (s) => s.last_success_at && fetchedAt - s.last_success_at > STALE_AFTER_MS,
  );
  const hasFilters = Boolean(symbolQ || nameQ || market || instrumentType);
  const invalidateDirectory = () => {
    void qc.invalidateQueries({ queryKey: ["market-assets"] });
  };

  // Panel default: collapsed only when every directory scope is complete;
  // any never/partial/failed/running scope keeps it expanded.
  const panelAutoOpen = useMemo(() => {
    if (!data) return true;
    if (!syncs.length) return true;
    return syncs.some((s) => s.status !== "complete");
  }, [data, syncs]);
  const panelOpen = panelOpenOverride ?? panelAutoOpen;

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;
  const rangeStart = total === 0 ? 0 : offset + 1;
  const rangeEnd = Math.min(offset + assets.length, offset + PAGE_SIZE);

  const jumpToPage = () => {
    const page = Number.parseInt(pageInput, 10);
    if (!Number.isFinite(page)) return;
    const clamped = Math.min(Math.max(page, 1), totalPages);
    setOffset((clamped - 1) * PAGE_SIZE);
    setPageInput("");
  };

  const syncPanel = (
    <section
      className="mb-4 rounded-lg border border-line bg-surface"
      aria-label="资产目录同步状态"
      data-testid="directory-sync-panel"
    >
      <button
        type="button"
        className="flex w-full items-center justify-between px-4 py-2.5 text-left"
        onClick={() => setPanelOpenOverride(!panelOpen)}
        aria-expanded={panelOpen}
        data-testid="directory-sync-toggle"
      >
        <span className="text-sm font-medium text-ink">资产目录同步状态</span>
        <span className="flex items-center gap-3 text-xs text-ink-muted">
          {!panelOpen && (
            <span data-testid="directory-sync-summary">
              {everSynced ? "目录已同步" : "尚未同步"}
            </span>
          )}
          <span aria-hidden>{panelOpen ? "收起 ▲" : "展开 ▼"}</span>
        </span>
      </button>
      {panelOpen && (
        <div className="divide-y divide-line border-t border-line px-4 py-1">
          {syncs.map((view) => (
            <DirectoryScopeRow
              key={view.scope}
              view={view}
              onChanged={invalidateDirectory}
            />
          ))}
          <FXSyncRow view={data?.fx_sync} onChanged={invalidateDirectory} />
        </div>
      )}
    </section>
  );

  const toolbar = (
    <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <input
          type="search"
          value={symbolInput}
          onChange={(e) => setSymbolInput(e.target.value)}
          placeholder="按代码搜索…"
          className="input-base max-w-48"
          aria-label="按代码搜索市场资产"
          data-testid="market-assets-symbol-search"
        />
        <input
          type="search"
          value={nameInput}
          onChange={(e) => setNameInput(e.target.value)}
          placeholder="按名称搜索…"
          className="input-base max-w-56"
          aria-label="按名称搜索市场资产"
          data-testid="market-assets-name-search"
        />
        <select
          value={market}
          onChange={(e) => {
            setMarket(e.target.value);
            setOffset(0);
          }}
          className="input-base max-w-xs"
          aria-label="按市场筛选"
          data-testid="market-assets-market-filter"
        >
          {MARKET_FILTERS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <select
          value={instrumentType}
          onChange={(e) => {
            setInstrumentType(e.target.value);
            setOffset(0);
          }}
          className="input-base max-w-xs"
          aria-label="按资产类型筛选"
          data-testid="market-assets-type-filter"
        >
          {TYPE_FILTERS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <label className="flex shrink-0 items-center gap-1.5 text-xs text-ink-muted">
          <input
            type="checkbox"
            checked={includeInactive}
            onChange={(e) => {
              setIncludeInactive(e.target.checked);
              setOffset(0);
            }}
            data-testid="market-assets-include-inactive"
          />
          含已退市
        </label>
      </div>
      {isFetching && !isLoading && data && (
        <LoadingState label="刷新中…" className="text-xs" />
      )}
    </div>
  );

  if (isLoading && !data) {
    return (
      <div className="content-enter">
        <PageHeader
          title="资产目录"
          description="全市场资产基础信息由后台任务同步，搜索仅查询本地目录。"
        />
        <div className="space-y-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      </div>
    );
  }

  if (isError && !data) {
    return (
      <div className="content-enter">
        <PageHeader title="资产目录" />
        <ErrorState
          message="无法加载资产目录。请确认后端服务可用后重试。"
          onRetry={() => void refetch()}
          backHref="/"
          technicalDetail={queryErrorMessage(error)}
        />
      </div>
    );
  }

  return (
    <div className="content-enter">
      <PageHeader
        title="资产目录"
        description="全市场资产基础信息由后台任务同步，搜索仅查询本地目录，不触发外部请求。计划持仓直接引用目录资产，无需单独录入。"
      />

      {syncPanel}

      {staleScopes.length > 0 && (
        <div
          role="alert"
          data-testid="directory-stale-banner"
          className="mb-4 rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-sm text-warning"
        >
          {staleScopes.map((s) => s.label || s.scope).join("、")}
          目录已超过 7 天未同步，搜索结果基于旧数据，建议重新同步资产列表。
        </div>
      )}

      {toolbar}

      {!assets.length && !hasFilters && !everSynced ? (
        <EmptyState
          title="当前没有资产基础信息"
          description="请先同步资产列表，同步完成后即可搜索资产。"
        />
      ) : !assets.length ? (
        <EmptyState
          title="未在本地资产目录中找到匹配资产"
          description="搜索仅查询本地目录。若目标资产为新上市或目录较旧，可先手动同步资产列表后重试。"
          action={{
            label: "清除筛选",
            onClick: () => {
              setSymbolInput("");
              setNameInput("");
              setMarket("");
              setInstrumentType("");
              setOffset(0);
            },
          }}
        />
      ) : (
        <>
          <div className="overflow-x-auto rounded-lg border border-line bg-surface">
            <table
              className="min-w-full text-sm"
              data-testid="market-assets-table"
            >
              <thead className="border-b border-line bg-surface-muted/60 text-left text-ink-muted">
                <tr>
                  <th className="px-3 py-2.5 font-medium">代码</th>
                  <th className="px-3 py-2.5 font-medium">名称</th>
                  <th className="px-3 py-2.5 font-medium">市场</th>
                  <th className="px-3 py-2.5 font-medium"><HelpLabel label="资产类型" termKey="instrument_kind" /></th>
                  <th className="px-3 py-2.5 font-medium">交易所</th>
                  <th className="px-3 py-2.5 font-medium">币种</th>
                  <th className="px-3 py-2.5 font-medium"><HelpLabel label="历史数据截至" termKey="data_as_of" /></th>
                  <th className="px-3 py-2.5 font-medium">来源</th>
                  <th className="px-3 py-2.5 font-medium">基础信息刷新时间</th>
                </tr>
              </thead>
              <tbody>
                {assets.map((asset) => (
                  <tr
                    key={asset.asset_key}
                    className="border-t border-line hover:bg-surface-muted/40"
                    data-testid="market-asset-row"
                  >
                    <td className="px-3 py-2.5">
                      <Link
                        href={marketAssetDetailHref(asset.asset_key)}
                        className="font-mono-numeric text-brand underline-offset-2 hover:underline"
                      >
                        {asset.symbol}
                      </Link>
                    </td>
                    <td className="max-w-[220px] px-3 py-2.5">
                      <Link
                        href={marketAssetDetailHref(asset.asset_key)}
                        className="line-clamp-2 text-ink underline-offset-2 hover:text-brand hover:underline"
                      >
                        {asset.name}
                      </Link>
                      {!asset.active && (
                        <span className="ml-2 text-xs text-warning">
                          已退市/未在目录
                        </span>
                      )}
                    </td>
                    <td className="px-3 py-2.5">{asset.market}</td>
                    <td className="px-3 py-2.5">
                      {instrumentTypeLabel(asset.instrument_type)}
                    </td>
                    <td className="px-3 py-2.5">{asset.exchange || "—"}</td>
                    <td className="px-3 py-2.5">{asset.currency || "—"}</td>
                    <td className="px-3 py-2.5 text-xs">
                      {asset.has_history ? (
                        <span className="text-ink-muted">
                          截至{" "}
                          <span className="font-mono-numeric">
                            {asset.history_data_as_of || "—"}
                          </span>
                        </span>
                      ) : (
                        <span className="text-warning">未同步</span>
                      )}
                    </td>
                    <td className="px-3 py-2.5 text-xs text-ink-muted">
                      {dataSourceLabel(asset.source_name)}
                    </td>
                    <td className="px-3 py-2.5 font-mono-numeric text-xs text-ink-muted">
                      {formatDateTimeFromMs(asset.refreshed_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div
            className="mt-3 flex flex-wrap items-center justify-between gap-3 text-sm text-ink-muted"
            data-testid="market-assets-pagination"
          >
            <span data-testid="market-assets-range">
              当前第 {rangeStart}-{rangeEnd} 条，共 {total} 条 · 第{" "}
              {currentPage} / {totalPages} 页
            </span>
            <span className="flex items-center gap-2">
              <Button
                variant="secondary"
                className="min-h-8 px-3 py-1 text-xs"
                disabled={offset === 0}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                上一页
              </Button>
              <Button
                variant="secondary"
                className="min-h-8 px-3 py-1 text-xs"
                disabled={currentPage >= totalPages}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                下一页
              </Button>
              <label className="flex items-center gap-1 text-xs">
                跳至
                <input
                  type="number"
                  min={1}
                  max={totalPages}
                  value={pageInput}
                  onChange={(e) => setPageInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") jumpToPage();
                  }}
                  className="input-base w-16 px-2 py-1 text-xs"
                  aria-label="跳转页码"
                  data-testid="market-assets-page-input"
                />
                页
              </label>
              <Button
                variant="secondary"
                className="min-h-8 px-3 py-1 text-xs"
                onClick={jumpToPage}
                data-testid="market-assets-page-jump"
              >
                跳转
              </Button>
            </span>
          </div>
        </>
      )}
    </div>
  );
}
