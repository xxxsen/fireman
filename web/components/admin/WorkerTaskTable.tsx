"use client";

import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { Tooltip } from "@/components/ui/Tooltip";
import type { AdminWorkerTaskItem } from "@/lib/api/admin";
import { workerTaskTypeLabel } from "@/lib/api/admin";
import { formatDurationMs, formatRelativeTime, middleTruncate } from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";

export interface WorkerTaskTableRowsProps {
  items: AdminWorkerTaskItem[];
  onSelect: (taskId: string) => void;
}

/**
 * Body rows of the worker task listing. Rows are keyboard reachable: Tab
 * focuses a row, Enter/Space opens its detail drawer.
 */
export function WorkerTaskTableRows({ items, onSelect }: WorkerTaskTableRowsProps) {
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
          <td className="whitespace-nowrap px-3 py-2 text-ink">{workerTaskTypeLabel(task.type)}</td>
          <td className="max-w-72 px-3 py-2">
            <Tooltip
              content={<span className="break-all font-mono text-xs">{task.dedupe_key || task.id}</span>}
              className="max-w-full"
            >
              <span className="block truncate font-mono text-xs text-ink-muted">
                {middleTruncate(task.dedupe_key || task.id, 44)}
              </span>
            </Tooltip>
          </td>
          <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink-muted">
            {formatDurationMs(task.duration_ms)}
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
            <span title={formatDateTimeFromMs(task.created_at)}>
              {formatRelativeTime(task.created_at)}
            </span>
          </td>
        </tr>
      ))}
    </>
  );
}

export const WORKER_TASK_TABLE_HEADERS = ["状态", "类型", "dedupe_key / id", "耗时", "创建时间"];
