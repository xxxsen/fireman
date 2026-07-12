"use client";

import { useQuery } from "@tanstack/react-query";
import {
  ADMIN_OVERVIEW_POLL_MS,
  ADMIN_OVERVIEW_QUERY_KEY,
} from "@/components/admin/AdminNav";
import { StatCard } from "@/components/admin/StatCard";
import { SyncHealthPanel } from "@/components/admin/SyncHealthPanel";
import { ErrorState } from "@/components/ui/ErrorState";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { getAdminOverview } from "@/lib/api/admin";
import { formatBytes } from "@/lib/admin-format";
import { queryErrorMessage } from "@/lib/query-error";

export default function AdminOverviewPage() {
  const overview = useQuery({
    queryKey: ADMIN_OVERVIEW_QUERY_KEY,
    queryFn: getAdminOverview,
    refetchInterval: ADMIN_OVERVIEW_POLL_MS,
  });

  if (overview.isLoading) {
    return <PageSkeleton label="加载概览…" />;
  }
  if (overview.isError || !overview.data) {
    return (
      <ErrorState
        message={queryErrorMessage(overview.error, "概览加载失败")}
        onRetry={() => void overview.refetch()}
      />
    );
  }

  const {
    worker_tasks: tasks,
    finalizations,
    sync_health: health,
    storage,
  } = overview.data;

  const activeBreakdown = Object.entries(tasks.by_status)
    .filter(([, count]) => count > 0)
    .map(([status, count]) => `${status} ${count}`)
    .join(" · ");

  return (
    <div className="space-y-6" data-testid="admin-overview">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <StatCard
          label="活跃任务"
          value={tasks.active}
          hint={activeBreakdown || "无进行中的任务"}
          tone={tasks.stale_running > 0 ? "warning" : "normal"}
          href="/admin/worker-tasks?status=active"
        />
        <StatCard
          label="24h 任务失败"
          value={tasks.failed_last_24h}
          hint={
            tasks.stale_running > 0
              ? `另有 ${tasks.stale_running} 个任务心跳滞留`
              : `24h 完成 ${tasks.completed_last_24h}`
          }
          tone={
            tasks.failed_last_24h > 0
              ? "danger"
              : tasks.stale_running > 0
                ? "warning"
                : "normal"
          }
          href="/admin/worker-tasks?status=failed"
        />
        <StatCard
          label="24h 终结"
          value={finalizations.total_last_24h}
          hint={
            finalizations.failed_last_24h > 0
              ? `${finalizations.failed_last_24h} 次失败`
              : "全部成功"
          }
          tone={finalizations.failed_last_24h > 0 ? "danger" : "normal"}
          href="/admin/finalizations"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[3fr_2fr]">
        <SyncHealthPanel health={health} />
        <div
          className="rounded-lg border border-line bg-surface p-4"
          data-testid="storage-panel"
        >
          <h2 className="text-sm font-medium text-ink">存储</h2>
          <dl className="mt-3 space-y-2 text-sm">
            <div className="flex items-center justify-between">
              <dt className="text-ink-muted">主库</dt>
              <dd className="tabular-nums text-ink">
                {formatBytes(storage.main_db_bytes)}
              </dd>
            </div>
            <div className="flex items-center justify-between">
              <dt className="text-ink-muted">
                资源库（{storage.resource_count} 条）
              </dt>
              <dd className="tabular-nums text-ink">
                {formatBytes(storage.resource_db_bytes)}
              </dd>
            </div>
          </dl>
          <p className="mt-3 border-t border-line pt-3 text-xs text-ink-muted">
            资源库存放 sidecar 上传的任务结果（7 天 TTL），主库为业务数据。
          </p>
        </div>
      </div>
    </div>
  );
}
