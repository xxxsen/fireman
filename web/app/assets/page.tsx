"use client";

import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  isTaskActive,
  listMarketAssets,
  syncFXRates,
  syncMarketAssets,
  type DirectorySyncScope,
  type MarketAssetSyncView,
  type WorkerTask,
} from "@/lib/api/market-assets";
import { dataSourceLabel, formatDateTimeFromMs, instrumentTypeLabel } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import { useWorkerTaskPolling } from "@/hooks/useWorkerTaskPolling";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Skeleton } from "@/components/ui/Skeleton";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { TaskErrorInline } from "@/components/ui/TaskErrorInline";
import { RefreshTaskButton } from "@/components/ui/RefreshTaskButton";

const PAGE_SIZE = 50;
const STALE_AFTER_MS = 7 * 24 * 60 * 60 * 1000;

const SCOPE_LABELS: Record<string, string> = {
  cn_all: "A 股 / 场内基金",
  hk_all: "港股 / 港股 ETF",
  us_all: "美股 / 美股 ETF",
};

const MARKET_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "全部市场" },
  { value: "CN", label: "中国市场" },
  { value: "HK", label: "香港市场" },
  { value: "US", label: "美国市场" },
];

const DIRECTORY_TASK_LABELS = { complete: "最近同步成功" } as const;

function marketAssetDetailHref(assetKey: string): string {
  return `/assets/market/${encodeURIComponent(assetKey)}`;
}

/** One sync scope row: status badge, last success time, sync button, error. */
function DirectorySyncRow({
  view,
  onChanged,
}: {
  view: MarketAssetSyncView;
  onChanged: () => void;
}) {
  const serverTask = view.task ?? null;
  const [manualTaskId, setManualTaskId] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  // Prefer an active task from the server snapshot (authoritative after
  // refetch), otherwise the task we just created locally.
  const serverActiveId = serverTask && isTaskActive(serverTask.status) ? serverTask.id : null;
  const trackedTaskId = serverActiveId ?? manualTaskId;

  const { task: polledTask, pollError } = useWorkerTaskPolling(trackedTaskId, {
    initialTask: serverTask && serverTask.id === trackedTaskId ? serverTask : undefined,
    onComplete: onChanged,
    onFailed: onChanged,
  });

  const task = polledTask ?? serverTask;
  const active = isTaskActive(task?.status);

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-2 py-2"
      data-testid={`directory-sync-${view.scope}`}
    >
      <span className="w-32 shrink-0 text-sm font-medium text-ink">
        {SCOPE_LABELS[view.scope] ?? view.scope}
      </span>
      <span className="flex items-center gap-2 text-xs text-ink-muted">
        {task ? (
          <TaskStatusBadge status={task.status} labels={DIRECTORY_TASK_LABELS} />
        ) : (
          <span>从未同步</span>
        )}
        {active && <LoadingState label="同步进行中…" className="text-xs" />}
        {task?.status === "failed" && (
          <TaskErrorInline errorCode={task.error_code} errorMessage={task.error_message} />
        )}
        {pollError && <span className="text-danger">任务状态查询失败：{pollError}</span>}
      </span>
      <span className="text-xs text-ink-muted">
        最近成功：
        <span className="font-mono-numeric text-ink">
          {formatDateTimeFromMs(view.last_success_at)}
        </span>
      </span>
      <span className="ml-auto flex items-center gap-2">
        {createError && <span className="text-xs text-danger">{createError}</span>}
        <RefreshTaskButton
          variant="secondary"
          className="min-h-8 px-3 py-1 text-xs"
          data-testid={`sync-button-${view.scope}`}
          createTask={() => {
            setCreateError(null);
            return syncMarketAssets({ scope: view.scope as DirectorySyncScope });
          }}
          onTask={(t: WorkerTask) => setManualTaskId(t.id)}
          onError={setCreateError}
          activeTask={task}
        >
          同步资产列表
        </RefreshTaskButton>
      </span>
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
  const [manualTaskId, setManualTaskId] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);

  const serverActiveId = serverTask && isTaskActive(serverTask.status) ? serverTask.id : null;
  const trackedTaskId = serverActiveId ?? manualTaskId;

  const { task: polledTask, pollError } = useWorkerTaskPolling(trackedTaskId, {
    initialTask: serverTask && serverTask.id === trackedTaskId ? serverTask : undefined,
    onComplete: onChanged,
    onFailed: onChanged,
  });
  const task = polledTask ?? serverTask;
  const active = isTaskActive(task?.status);

  return (
    <div
      className="flex flex-wrap items-center gap-x-4 gap-y-2 py-2"
      data-testid="fx-sync-row"
    >
      <span className="w-32 shrink-0 text-sm font-medium text-ink">汇率（USD/HKD）</span>
      <span className="flex items-center gap-2 text-xs text-ink-muted">
        {task && <TaskStatusBadge status={task.status} labels={DIRECTORY_TASK_LABELS} />}
        {active && <LoadingState label="同步进行中…" className="text-xs" />}
        {task?.status === "failed" && (
          <TaskErrorInline errorCode={task.error_code} errorMessage={task.error_message} />
        )}
        {pollError && <span className="text-danger">任务状态查询失败：{pollError}</span>}
      </span>
      <span className="text-xs text-ink-muted">
        最近成功：
        <span className="font-mono-numeric text-ink">
          {formatDateTimeFromMs(view?.last_success_at)}
        </span>
      </span>
      <span className="ml-auto flex items-center gap-2">
        {createError && <span className="text-xs text-danger">{createError}</span>}
        <RefreshTaskButton
          variant="secondary"
          className="min-h-8 px-3 py-1 text-xs"
          data-testid="fx-sync-button"
          createTask={() => {
            setCreateError(null);
            return syncFXRates();
          }}
          onTask={(t: WorkerTask) => setManualTaskId(t.id)}
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
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [market, setMarket] = useState("");
  const [includeInactive, setIncludeInactive] = useState(false);
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(searchInput.trim());
      setOffset(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchInput]);

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ["market-assets", market, search, includeInactive, offset],
    // fetchedAt is captured at request time so staleness checks stay pure in render.
    queryFn: async () => {
      const result = await listMarketAssets({
        market: market || undefined,
        q: search || undefined,
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
  const everSynced = syncs.some((s) => s.last_success_at);
  const fetchedAt = data?.fetchedAt ?? 0;
  const staleScopes = syncs.filter(
    (s) => s.last_success_at && fetchedAt - s.last_success_at > STALE_AFTER_MS,
  );
  const invalidateDirectory = () => {
    void qc.invalidateQueries({ queryKey: ["market-assets"] });
  };

  const syncPanel = (
    <section
      className="mb-4 divide-y divide-line rounded-lg border border-line bg-surface px-4 py-1"
      aria-label="资产目录同步状态"
      data-testid="directory-sync-panel"
    >
      {syncs.map((view) => (
        <DirectorySyncRow key={view.scope} view={view} onChanged={invalidateDirectory} />
      ))}
      <FXSyncRow view={data?.fx_sync} onChanged={invalidateDirectory} />
    </section>
  );

  const toolbar = (
    <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center">
        <input
          type="search"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
          placeholder="搜索代码或名称（本地目录）…"
          className="input-base max-w-md"
          aria-label="搜索市场资产"
          data-testid="market-assets-search"
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
      {isFetching && !isLoading && data && <LoadingState label="刷新中…" className="text-xs" />}
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
        description="全市场资产基础信息由后台任务同步，搜索仅查询本地目录，不触发外部请求。"
        secondaryActions={
          <Button variant="secondary" href="/assets/library" data-testid="my-library-link">
            我的资产库
          </Button>
        }
        primaryAction={{ label: "录入资产", href: "/assets/import" }}
      />

      {syncPanel}

      {staleScopes.length > 0 && (
        <div
          role="alert"
          data-testid="directory-stale-banner"
          className="mb-4 rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-sm text-warning"
        >
          {staleScopes.map((s) => SCOPE_LABELS[s.scope] ?? s.scope).join("、")}
          目录已超过 7 天未同步，搜索结果基于旧数据，建议重新同步资产列表。
        </div>
      )}

      {toolbar}

      {!assets.length && !search && !everSynced ? (
        <EmptyState
          title="当前没有资产基础信息"
          description="请先同步资产列表，同步完成后即可搜索并录入资产。"
        />
      ) : !assets.length ? (
        <EmptyState
          title="未在本地资产目录中找到匹配资产"
          description="搜索仅查询本地目录。若目标资产为新上市或目录较旧，可先手动同步资产列表后重试。"
          action={{
            label: "清除筛选",
            onClick: () => {
              setSearchInput("");
              setMarket("");
              setOffset(0);
            },
          }}
        />
      ) : (
        <>
          <div className="overflow-x-auto rounded-lg border border-line bg-surface">
            <table className="min-w-full text-sm" data-testid="market-assets-table">
              <thead className="border-b border-line bg-surface-muted/60 text-left text-ink-muted">
                <tr>
                  <th className="px-3 py-2.5 font-medium">代码</th>
                  <th className="px-3 py-2.5 font-medium">名称</th>
                  <th className="px-3 py-2.5 font-medium">市场</th>
                  <th className="px-3 py-2.5 font-medium">类型</th>
                  <th className="px-3 py-2.5 font-medium">交易所</th>
                  <th className="px-3 py-2.5 font-medium">资产 kind</th>
                  <th className="px-3 py-2.5 font-medium">币种</th>
                  <th className="px-3 py-2.5 font-medium">来源</th>
                  <th className="px-3 py-2.5 font-medium">基础信息刷新时间</th>
                  <th className="px-3 py-2.5 font-medium">操作</th>
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
                        <span className="ml-2 text-xs text-warning">已退市/未在目录</span>
                      )}
                    </td>
                    <td className="px-3 py-2.5">{asset.market}</td>
                    <td className="px-3 py-2.5">{instrumentTypeLabel(asset.instrument_type)}</td>
                    <td className="px-3 py-2.5">{asset.exchange || "—"}</td>
                    <td className="px-3 py-2.5">{asset.instrument_kind || "—"}</td>
                    <td className="px-3 py-2.5">{asset.currency || "—"}</td>
                    <td className="px-3 py-2.5 text-xs text-ink-muted">
                      {dataSourceLabel(asset.source_name)}
                    </td>
                    <td className="px-3 py-2.5 font-mono-numeric text-xs text-ink-muted">
                      {formatDateTimeFromMs(asset.refreshed_at)}
                    </td>
                    <td className="px-3 py-2.5">
                      <Link
                        href={`/assets/import?asset_key=${encodeURIComponent(asset.asset_key)}`}
                        className="text-xs text-brand underline-offset-2 hover:underline"
                      >
                        录入
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-3 flex items-center justify-between text-sm text-ink-muted">
            <span>
              第 {Math.floor(offset / PAGE_SIZE) + 1} 页 · 本页 {assets.length} 条
            </span>
            <span className="flex gap-2">
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
                disabled={assets.length < PAGE_SIZE}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                下一页
              </Button>
            </span>
          </div>
        </>
      )}
    </div>
  );
}
