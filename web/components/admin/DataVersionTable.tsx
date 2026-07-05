"use client";

import Link from "next/link";
import type { AdminDataVersion } from "@/lib/api/admin";
import { formatRelativeTime, middleTruncate } from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";

export const DATA_VERSION_TABLE_HEADERS = ["版本键", "version_no", "来源任务", "更新时间"];

/**
 * Body rows of the market_data_versions listing. The source task links back
 * to the worker-tasks board with the detail drawer opened.
 */
export function DataVersionTableRows({ items }: { items: AdminDataVersion[] }) {
  return (
    <>
      {items.map((v) => (
        <tr
          key={v.version_key}
          data-testid="data-version-row"
          className="border-b border-line/60 last:border-b-0"
        >
          <td className="max-w-80 px-3 py-2">
            <span className="block truncate font-mono text-xs text-ink" title={v.version_key}>
              {v.version_key}
            </span>
          </td>
          <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink">{v.version_no}</td>
          <td className="max-w-60 px-3 py-2">
            {v.task_id ? (
              <Link
                href={`/admin/worker-tasks?task_id=${encodeURIComponent(v.task_id)}`}
                className="block truncate font-mono text-xs text-brand hover:underline"
                title={v.task_id}
              >
                {middleTruncate(v.task_id, 32)}
              </Link>
            ) : (
              <span className="text-ink-muted">—</span>
            )}
          </td>
          <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
            <span title={formatDateTimeFromMs(v.updated_at)}>
              {formatRelativeTime(v.updated_at)}
            </span>
          </td>
        </tr>
      ))}
    </>
  );
}
