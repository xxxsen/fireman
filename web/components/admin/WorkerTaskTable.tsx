"use client";

import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { Tooltip } from "@/components/ui/Tooltip";
import type { AdminWorkerTaskItem } from "@/lib/api/admin";
import { workerTaskTypeLabel } from "@/lib/api/admin";
import { isTaskActive } from "@/lib/api/tasks";
import {
  formatDurationMs,
  formatRelativeTime,
  middleTruncate,
} from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";

export interface WorkerTaskTableRowsProps {
  items: AdminWorkerTaskItem[];
  onSelect: (taskId: string) => void;
  onCanceled?: () => void;
}

/**
 * Body rows of the worker task listing. Rows are keyboard reachable: Tab
 * focuses a row, Enter/Space opens its detail drawer.
 */
export function WorkerTaskTableRows({
  items,
  onSelect,
  onCanceled,
}: WorkerTaskTableRowsProps) {
  return (
    <>
      {items.map((task) => (
        <tr
          key={task.id}
          tabIndex={0}
          data-testid="worker-task-row"
          data-task-id={task.id}
          onClick={() => onSelect(task.id)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onSelect(task.id);
            }
          }}
          className="cursor-pointer border-b border-line/60 transition-colors last:border-b-0 hover:bg-surface-muted focus-visible:bg-surface-muted focus-visible:outline-none"
        >
          <td className="px-3 py-2">
            <TaskStatusBadge status={task.status} />
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-ink">
            {workerTaskTypeLabel(task.type)}
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-xs text-ink-muted">
            {task.worker_type === "go_worker" ? "Go" : "Sidecar"}
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-xs text-ink-muted">
            {task.scope_type && task.scope_id
              ? `${task.scope_type} / ${middleTruncate(task.scope_id, 18)}`
              : "—"}
          </td>
          <td className="max-w-72 px-3 py-2">
            <Tooltip
              content={
                <span className="break-all font-mono text-xs">
                  {task.dedupe_key || task.id}
                </span>
              }
              className="max-w-full"
            >
              <span className="block truncate font-mono text-xs text-ink-muted">
                {middleTruncate(task.dedupe_key || task.id, 44)}
              </span>
            </Tooltip>
          </td>
          <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink-muted">
            {task.attempt_count}/{task.max_attempts}
          </td>
          <td className="max-w-56 px-3 py-2 text-xs text-ink-muted">
            <span className="block truncate text-ink">{task.phase || "—"}</span>
            <span className="block truncate font-mono">
              {task.progress_total > 0
                ? `${task.progress_current}/${task.progress_total}`
                : "无进度总量"}
              {task.claimed_by
                ? ` · ${middleTruncate(task.claimed_by, 20)}`
                : ""}
            </span>
            {task.heartbeat_at && (
              <span
                className="block"
                title={
                  task.lease_expires_at
                    ? `lease ${formatDateTimeFromMs(task.lease_expires_at)}`
                    : undefined
                }
              >
                心跳 {formatRelativeTime(task.heartbeat_at)}
              </span>
            )}
          </td>
          <td className="max-w-48 px-3 py-2 text-xs">
            {task.error_code || task.error_message ? (
              <Tooltip
                content={
                  <span className="break-all">
                    {task.error_message || task.error_code}
                  </span>
                }
              >
                <span className="block truncate text-danger">
                  {task.error_code || task.error_message}
                </span>
              </Tooltip>
            ) : (
              <span className="text-ink-muted">—</span>
            )}
          </td>
          <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink-muted">
            {formatDurationMs(task.duration_ms)}
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
            <span title={formatDateTimeFromMs(task.created_at)}>
              {formatRelativeTime(task.created_at)}
            </span>
          </td>
          <td
            className="whitespace-nowrap px-3 py-2"
            onClick={(event) => event.stopPropagation()}
            onKeyDown={(event) => event.stopPropagation()}
          >
            <TaskCancelButton
              task={task}
              admin
              className="min-h-8 px-2 py-1 text-xs"
              onCanceled={onCanceled}
            />
            {!isTaskActive(task.status) && <span className="text-ink-muted">—</span>}
          </td>
        </tr>
      ))}
    </>
  );
}

export const WORKER_TASK_TABLE_HEADERS = [
  "状态",
  "类型",
  "Worker",
  "范围",
  "dedupe_key / id",
  "Attempt",
  "执行",
  "错误",
  "耗时",
  "创建时间",
  "操作",
];
