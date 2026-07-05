"use client";

import { Badge } from "@/components/ui/Badge";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import type { AdminSyncHealth } from "@/lib/api/admin";
import { directoryScopeLabel } from "@/lib/api/admin";
import type { WorkerTaskStatus } from "@/lib/api/market-assets";
import { formatRelativeTime } from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";

/**
 * Sync health block shared by the overview and data-versions pages:
 * directory scopes, FX pairs and the history-dimension summary.
 */
export function SyncHealthPanel({ health }: { health: AdminSyncHealth }) {
  return (
    <div
      className="rounded-lg border border-line bg-surface p-4"
      data-testid="sync-health-panel"
    >
      <h2 className="text-sm font-medium text-ink">同步健康</h2>
      <ul className="mt-3 space-y-2 text-sm">
        {health.directory_scopes.map((scope) => (
          <li
            key={scope.scope}
            className="flex flex-wrap items-center gap-2"
            data-testid="sync-health-scope"
          >
            <span className="text-ink">{directoryScopeLabel(scope.scope)}</span>
            <span className="font-mono text-xs text-ink-muted">{scope.scope}</span>
            <span className="ml-auto flex items-center gap-2">
              {scope.active_task_status && (
                <TaskStatusBadge status={scope.active_task_status as WorkerTaskStatus} />
              )}
              {scope.stale && <Badge variant="warning">超 7 天未成功</Badge>}
              <span
                className="text-xs text-ink-muted"
                title={scope.last_success_at ? formatDateTimeFromMs(scope.last_success_at) : undefined}
              >
                {scope.last_success_at ? formatRelativeTime(scope.last_success_at) : "从未成功"}
              </span>
            </span>
          </li>
        ))}
        {health.fx_pairs.map((pair) => (
          <li
            key={pair.pair}
            className="flex items-center gap-2"
            data-testid="sync-health-fx"
          >
            <span className="text-ink">汇率</span>
            <span className="font-mono text-xs text-ink-muted">{pair.pair}</span>
            <span
              className="ml-auto text-xs text-ink-muted"
              title={pair.last_success_at ? formatDateTimeFromMs(pair.last_success_at) : undefined}
            >
              {pair.last_success_at ? formatRelativeTime(pair.last_success_at) : "从未成功"}
            </span>
          </li>
        ))}
        {health.directory_scopes.length === 0 && health.fx_pairs.length === 0 && (
          <li className="text-xs text-ink-muted">尚无同步记录。</li>
        )}
      </ul>
      <p className="mt-3 border-t border-line pt-3 text-xs text-ink-muted" data-testid="history-dimensions">
        历史维度 {health.history_dimensions.total} 个
        {health.history_dimensions.stale_over_7d > 0 && (
          <span className="text-warning"> · {health.history_dimensions.stale_over_7d} 个超 7 天未更新</span>
        )}
        {health.history_dimensions.never_synced > 0 && (
          <span className="text-warning"> · {health.history_dimensions.never_synced} 个从未同步</span>
        )}
      </p>
    </div>
  );
}
