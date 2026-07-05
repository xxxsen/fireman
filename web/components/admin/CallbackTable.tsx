"use client";

import Link from "next/link";
import { Tooltip } from "@/components/ui/Tooltip";
import type { AdminPostProcessRecord } from "@/lib/api/admin";
import { workerTaskTypeLabel } from "@/lib/api/admin";
import { formatDurationMs, middleTruncate } from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";
import { CallbackResultBadge } from "./CallbackResultBadge";

export const CALLBACK_TABLE_HEADERS = [
  "结果",
  "task id",
  "任务类型",
  "尝试",
  "错误码",
  "耗时",
  "时间",
];

/**
 * Body rows of the global callback listing. The task id links back to the
 * worker-tasks board with the detail drawer opened via ?task_id=.
 */
export function CallbackTableRows({ items }: { items: AdminPostProcessRecord[] }) {
  return (
    <>
      {items.map((rec) => (
        <tr
          key={rec.id}
          data-testid="callback-row"
          className="border-b border-line/60 last:border-b-0"
        >
          <td className="px-3 py-2">
            <CallbackResultBadge result={rec.result} />
          </td>
          <td className="max-w-60 px-3 py-2">
            <Link
              href={`/admin/worker-tasks?task_id=${encodeURIComponent(rec.task_id)}`}
              className="block truncate font-mono text-xs text-brand hover:underline"
              title={rec.task_id}
            >
              {middleTruncate(rec.task_id, 32)}
            </Link>
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
            {rec.task_type ? workerTaskTypeLabel(rec.task_type) : "—"}
          </td>
          <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink-muted">
            {rec.attempt_no}
          </td>
          <td className="max-w-48 px-3 py-2">
            {rec.error_code ? (
              <Tooltip
                content={
                  <span className="break-all text-xs">
                    <span className="font-mono">{rec.error_code}</span>
                    {rec.error_message && `：${rec.error_message}`}
                  </span>
                }
              >
                <span className="block truncate font-mono text-xs text-ink-muted">
                  {rec.error_code}
                </span>
              </Tooltip>
            ) : (
              <span className="text-ink-muted">—</span>
            )}
          </td>
          <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink-muted">
            {formatDurationMs(rec.duration_ms)}
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
            {formatDateTimeFromMs(rec.created_at)}
          </td>
        </tr>
      ))}
    </>
  );
}
