"use client";

import { useCallback, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  getCollectionSyncStatus,
  syncCollectionHistory,
  type ResearchReadiness,
  type ResearchSyncResult,
} from "@/lib/api/research";
import { isTaskActive, type WorkerTask } from "@/lib/api/market-assets";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import { queryErrorMessage } from "@/lib/query-error";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";

/** Readable retry suggestion for a failed sync task error code. */
export function syncFailureSuggestion(errorCode?: string): string {
  switch (errorCode) {
    case "sidecar_unavailable":
      return "行情服务不可用，请确认 sidecar 已启动后重试。";
    case "source_rate_limited":
      return "数据源限流，请稍后重试。";
    case "asset_not_found":
      return "数据源找不到该资产，请检查资产身份。";
    default:
      return "可尝试单资产重试；若持续失败请查看任务详情。";
  }
}

function taskStatusBadge(task: WorkerTask | null, fallback?: string) {
  const status = task?.status ?? fallback;
  switch (status) {
    case "pending":
      return <Badge variant="info">排队中</Badge>;
    case "running":
    case "pre_complete":
      return <Badge variant="info">同步中</Badge>;
    case "complete":
      return <Badge variant="positive">完成</Badge>;
    case "failed":
      return <Badge variant="danger">失败</Badge>;
    case "canceled":
      return <Badge variant="neutral">已取消</Badge>;
    default:
      return <Badge variant="neutral">—</Badge>;
  }
}

function SyncTaskRow({
  label,
  sublabel,
  task,
  existed,
  skippedReason,
  onSettled,
  onRetry,
}: {
  label: string;
  sublabel?: string;
  task?: WorkerTask | null;
  existed: boolean;
  skippedReason?: string;
  onSettled: (task: WorkerTask) => void;
  onRetry?: () => void;
}) {
  const settledTaskIdsRef = useRef(new Set<string>());
  const settle = useCallback(
    (settledTask: WorkerTask) => {
      if (settledTaskIdsRef.current.has(settledTask.id)) return;
      settledTaskIdsRef.current.add(settledTask.id);
      onSettled(settledTask);
    },
    [onSettled],
  );

  const polling = useTaskStatus(task?.id, {
    initialTask: task ?? null,
    onComplete: settle,
    onFailed: settle,
    onCanceled: settle,
  });
  const current = polling.task ?? task ?? null;
  const failed = current?.status === "failed";

  return (
    <li className="flex items-start gap-3 rounded-md border border-line bg-surface px-3 py-2 text-sm">
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium text-ink">{label}</span>
        {sublabel && (
          <span className="block text-xs text-ink-muted">{sublabel}</span>
        )}
        {existed && (
          <span className="block text-xs text-info">已有同步任务，复用中</span>
        )}
        {skippedReason && (
          <span className="block text-xs text-ink-muted">
            跳过：{skippedReason}
          </span>
        )}
        {failed && (
          <span className="mt-0.5 block text-xs text-danger">
            {current?.error_message || current?.error_code || "同步失败"}
            <span className="mt-0.5 block text-ink-muted">
              {syncFailureSuggestion(current?.error_code)}
            </span>
          </span>
        )}
        {polling.pollError && (
          <span className="block text-xs text-warning">
            状态查询失败：{polling.pollError}
          </span>
        )}
      </span>
      <span className="flex shrink-0 items-center gap-2">
        {skippedReason ? (
          <Badge variant="neutral">跳过</Badge>
        ) : (
          taskStatusBadge(current)
        )}
        {failed && onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="text-xs text-brand underline-offset-2 hover:underline"
          >
            重试
          </button>
        )}
        <TaskCancelButton
          task={current}
          shared
          className="min-h-7 px-2 py-0.5 text-xs"
          onCanceled={settle}
        />
      </span>
    </li>
  );
}

export interface DataStatusPanelProps {
  collectionId: string;
  readiness?: ResearchReadiness;
  readinessLoading: boolean;
}

export function DataStatusPanel({
  collectionId,
  readiness,
  readinessLoading,
}: DataStatusPanelProps) {
  const queryClient = useQueryClient();
  const [syncResult, setSyncResult] = useState<ResearchSyncResult | null>(null);
  const persistedSync = useQuery({
    queryKey: ["research", "sync-status", collectionId],
    queryFn: () => getCollectionSyncStatus(collectionId),
  });

  const refreshCollectionData = useCallback(() => {
    void Promise.all([
      queryClient.invalidateQueries({
        queryKey: ["research", "collection", collectionId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["research", "readiness", collectionId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["research", "optimization-readiness", collectionId],
      }),
      queryClient.invalidateQueries({
        queryKey: ["research", "sync-status", collectionId],
      }),
    ]);
  }, [collectionId, queryClient]);

  const syncMutation = useMutation({
    mutationFn: (body?: { asset_keys?: string[]; force?: boolean }) =>
      syncCollectionHistory(collectionId, body),
    onSuccess: (result, body) => {
      setSyncResult((prev) => {
        if (!body?.asset_keys || !prev) return result;
        // Single-asset retry: merge the new rows into the previous panel.
        const replaced = new Set(result.assets.map((a) => a.asset_key));
        return {
          assets: [
            ...prev.assets.filter((a) => !replaced.has(a.asset_key)),
            ...result.assets,
          ],
          fx: result.fx.length > 0 ? result.fx : prev.fx,
          blocked: [
            ...prev.blocked.filter((b) => !replaced.has(b.asset_key)),
            ...result.blocked,
          ],
        };
      });
      const active =
        result.assets.filter((a) => a.task && isTaskActive(a.task.status))
          .length +
        result.fx.filter((f) => f.task && isTaskActive(f.task.status)).length;
      if (active === 0) refreshCollectionData();
    },
  });

  const handleSettled = useCallback(
    (settledTask: WorkerTask) => {
      // Refresh each asset independently. A slow or failed sibling task must
      // not hide the history range/status already persisted by this task.
      setSyncResult((previous) => {
        if (!previous) return previous;
        const updateTask = (task?: WorkerTask | null) =>
          task?.id === settledTask.id ? settledTask : task;
        return {
          assets: previous.assets.map((row) => ({
            ...row,
            task: updateTask(row.task),
          })),
          fx: previous.fx.map((row) => ({
            ...row,
            task: updateTask(row.task),
          })),
          blocked: previous.blocked,
        };
      });
      refreshCollectionData();
    },
    [refreshCollectionData],
  );

  const rawBlocking = readiness?.blocking_reasons ?? [];
  const blocking = rawBlocking.filter(
    (issue) => issue.reason !== "weight_sum_invalid",
  );
  const warnings = readiness?.warnings ?? [];
  const visibleSyncResult = syncResult ?? persistedSync.data ?? null;
  const hasActiveSync = Boolean(
    visibleSyncResult &&
    [...visibleSyncResult.assets, ...visibleSyncResult.fx].some(
      (row) => row.task && isTaskActive(row.task.status),
    ),
  );

  const readinessBadge = useMemo(() => {
    if (readinessLoading) return <Badge variant="neutral">检查中…</Badge>;
    if (!readiness) return <Badge variant="neutral">—</Badge>;
    return blocking.length === 0 ? (
      <Badge variant="positive">数据就绪</Badge>
    ) : (
      <Badge variant="danger">{blocking.length} 项阻断</Badge>
    );
  }, [readiness, readinessLoading, blocking.length]);

  return (
    <section
      className="rounded-lg border border-line bg-surface p-4"
      data-testid="data-status"
    >
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 className="flex items-center gap-2 text-base font-semibold text-ink">
          数据状态 {readinessBadge}
        </h2>
        <div className="flex gap-2">
          <Button
            variant="secondary"
            onClick={refreshCollectionData}
            disabled={readinessLoading}
          >
            重新检查
          </Button>
          <Button
            pending={syncMutation.isPending}
            disabled={
              persistedSync.isLoading || persistedSync.isError || hasActiveSync
            }
            onClick={() => syncMutation.mutate(undefined)}
            data-testid="sync-collection"
          >
            更新组合数据
          </Button>
        </div>
      </div>

      {syncMutation.isError && (
        <p className="mb-3 text-sm text-danger" role="alert">
          创建同步任务失败：{queryErrorMessage(syncMutation.error)}
        </p>
      )}

      {persistedSync.isError && (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-sm text-danger" role="alert">
          <span>同步任务状态恢复失败，恢复前不能创建新任务。</span>
          <Button variant="ghost" onClick={() => void persistedSync.refetch()}>
            重试状态检查
          </Button>
        </div>
      )}

      {blocking.length > 0 && (
        <div
          className="mb-3 rounded-md border border-danger/25 bg-danger/5 px-3 py-2"
          role="alert"
        >
          <p className="mb-1 text-xs font-semibold text-danger">
            阻断条件（无法运行回测）
          </p>
          <ul
            className="space-y-0.5 text-xs text-ink"
            data-testid="blocking-reasons"
          >
            {blocking.map((issue, idx) => (
              <li
                key={`${issue.reason}-${issue.asset_key}-${issue.pair}-${idx}`}
              >
                {issue.asset_key && (
                  <code className="mr-1 text-ink-muted">{issue.asset_key}</code>
                )}
                {issue.pair && (
                  <code className="mr-1 text-ink-muted">{issue.pair}</code>
                )}
                {issue.message}
              </li>
            ))}
          </ul>
        </div>
      )}

      {warnings.length > 0 && (
        <div className="mb-3 rounded-md border border-warning/30 bg-warning/5 px-3 py-2">
          <p className="mb-1 text-xs font-semibold text-warning">
            警告（可运行，谨慎解读）
          </p>
          <ul className="space-y-0.5 text-xs text-ink" data-testid="warnings">
            {warnings.map((issue, idx) => (
              <li
                key={`${issue.reason}-${issue.asset_key}-${issue.pair}-${idx}`}
              >
                {issue.asset_key && (
                  <code className="mr-1 text-ink-muted">{issue.asset_key}</code>
                )}
                {issue.pair && (
                  <code className="mr-1 text-ink-muted">{issue.pair}</code>
                )}
                {issue.message}
              </li>
            ))}
          </ul>
        </div>
      )}

      {readiness && blocking.length === 0 && warnings.length === 0 && (
        <p className="mb-3 text-sm text-ink-muted">
          所有 enabled 资产历史与汇率数据均满足回测准入条件。
        </p>
      )}

      {readiness && (
        <dl className="mb-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-4">
          <div>
            <dt className="text-ink-muted">依赖资产</dt>
            <dd className="font-medium text-ink">
              {readiness.data_dependencies.asset_count}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">FX 依赖</dt>
            <dd className="font-medium text-ink">
              {readiness.data_dependencies.fx_pairs.length > 0
                ? readiness.data_dependencies.fx_pairs.join(", ")
                : "无"}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">过期资产</dt>
            <dd className="font-medium text-ink">
              {readiness.data_dependencies.stale_asset_count}
            </dd>
          </div>
          <div>
            <dt className="text-ink-muted">缺历史资产</dt>
            <dd className="font-medium text-ink">
              {readiness.data_dependencies.missing_history_count}
            </dd>
          </div>
        </dl>
      )}

      {visibleSyncResult &&
        (visibleSyncResult.assets.length > 0 ||
          visibleSyncResult.fx.length > 0 ||
          visibleSyncResult.blocked.length > 0) && (
          <div data-testid="sync-task-panel">
            <h3 className="mb-2 text-sm font-semibold text-ink">同步任务</h3>
            <ul className="space-y-1.5">
              {visibleSyncResult.assets.map((row) => (
                <SyncTaskRow
                  key={`asset-${row.asset_key}`}
                  label={row.asset_key}
                  task={row.task}
                  existed={row.status === "existed"}
                  skippedReason={
                    row.status === "skipped"
                      ? (row.reason ?? "无需同步")
                      : undefined
                  }
                  onSettled={handleSettled}
                  onRetry={() =>
                    syncMutation.mutate({
                      asset_keys: [row.asset_key],
                      force: true,
                    })
                  }
                />
              ))}
              {visibleSyncResult.fx.map((row) => (
                <SyncTaskRow
                  key={`fx-${row.pair}`}
                  label={`汇率 ${row.pair}`}
                  task={row.task}
                  existed={row.status === "existed"}
                  skippedReason={
                    row.status === "skipped" ? "无需同步" : undefined
                  }
                  onSettled={handleSettled}
                  onRetry={() => syncMutation.mutate({ force: true })}
                />
              ))}
              {visibleSyncResult.blocked.map((row) => (
                <li
                  key={`blocked-${row.asset_key}`}
                  className="flex items-start justify-between gap-3 rounded-md border border-danger/25 bg-danger/5 px-3 py-2 text-sm"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium text-ink">
                      {row.asset_key}
                    </span>
                    <span className="block text-xs text-danger">
                      {row.message}
                    </span>
                  </span>
                  <Badge variant="danger">无法同步</Badge>
                </li>
              ))}
            </ul>
          </div>
        )}
    </section>
  );
}
