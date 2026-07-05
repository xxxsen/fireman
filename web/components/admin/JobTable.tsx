"use client";

import { Fragment, useState } from "react";
import Link from "next/link";
import type { AdminJobItem } from "@/lib/api/admin";
import { jobTypeLabel } from "@/lib/api/admin";
import { formatDurationMs, formatRelativeTime, middleTruncate } from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";
import { JobStatusBadge } from "./JobStatusBadge";

export const JOB_TABLE_HEADERS = ["状态", "类型", "计划", "进度", "耗时", "创建时间"];

const COLUMN_COUNT = JOB_TABLE_HEADERS.length;

export interface JobTableRowsProps {
  items: AdminJobItem[];
  /**
   * Phase-2 mount point for per-row actions (e.g. cancel); when provided each
   * row gains a trailing action cell. Render-only today.
   */
  actions?: (job: AdminJobItem) => React.ReactNode;
}

/**
 * Body rows of the simulation job listing. Failed rows can be expanded
 * inline (click / Enter) to reveal error_code + error_message — jobs carry no
 * payload or timeline, so one expansion level replaces a drawer.
 */
export function JobTableRows({ items, actions }: JobTableRowsProps) {
  const [expandedId, setExpandedId] = useState<string | null>(null);

  return (
    <>
      {items.map((job) => {
        const expandable = job.status === "failed" && Boolean(job.error_code || job.error_message);
        const expanded = expandedId === job.id;
        return (
          <Fragment key={job.id}>
            <tr
              data-testid="job-row"
              data-job-id={job.id}
              tabIndex={expandable ? 0 : undefined}
              onClick={expandable ? () => setExpandedId(expanded ? null : job.id) : undefined}
              onKeyDown={
                expandable
                  ? (e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        setExpandedId(expanded ? null : job.id);
                      }
                    }
                  : undefined
              }
              className={
                expandable
                  ? "cursor-pointer border-b border-line/60 transition-colors last:border-b-0 hover:bg-surface-muted focus-visible:bg-surface-muted focus-visible:outline-none"
                  : "border-b border-line/60 last:border-b-0"
              }
            >
              <td className="px-3 py-2">
                <span className="flex items-center gap-1.5">
                  <JobStatusBadge status={job.status} />
                  {expandable && (
                    <span aria-hidden="true" className="text-xs text-ink-muted">
                      {expanded ? "▾" : "▸"}
                    </span>
                  )}
                </span>
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-ink">{jobTypeLabel(job.type)}</td>
              <td className="max-w-56 px-3 py-2">
                {job.plan_id ? (
                  <Link
                    href={`/plans/${job.plan_id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="block truncate text-brand hover:underline"
                    title={job.plan_name || job.plan_id}
                  >
                    {job.plan_name || middleTruncate(job.plan_id, 20)}
                  </Link>
                ) : (
                  <span className="text-ink-muted">系统作业</span>
                )}
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
                {job.status === "running" ? (
                  <div className="min-w-32" data-testid="job-progress">
                    <p className="text-xs tabular-nums">
                      {job.phase && <span className="mr-1 font-mono">{job.phase}</span>}
                      {job.progress_total > 0 && `${job.progress_current} / ${job.progress_total}`}
                    </p>
                    {job.progress_total > 0 && (
                      <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-surface-muted">
                        <div
                          className="h-full rounded-full bg-brand transition-[width]"
                          style={{
                            width: `${Math.min(100, (job.progress_current / job.progress_total) * 100)}%`,
                          }}
                        />
                      </div>
                    )}
                  </div>
                ) : (
                  "—"
                )}
              </td>
              <td className="whitespace-nowrap px-3 py-2 tabular-nums text-ink-muted">
                {formatDurationMs(job.duration_ms)}
              </td>
              <td className="whitespace-nowrap px-3 py-2 text-ink-muted">
                <span title={formatDateTimeFromMs(job.created_at)}>
                  {formatRelativeTime(job.created_at)}
                </span>
              </td>
              {actions && (
                <td
                  className="whitespace-nowrap px-3 py-2"
                  data-testid="job-row-actions"
                  onClick={(e) => e.stopPropagation()}
                >
                  {actions(job)}
                </td>
              )}
            </tr>
            {expanded && (
              <tr data-testid="job-error-row" className="border-b border-line/60 last:border-b-0">
                <td colSpan={COLUMN_COUNT + (actions ? 1 : 0)} className="bg-danger/5 px-4 py-2.5">
                  <p className="text-xs text-danger">
                    {job.error_code && <span className="mr-2 font-mono">{job.error_code}</span>}
                    <span className="break-all">{job.error_message || "无更多错误信息"}</span>
                  </p>
                </td>
              </tr>
            )}
          </Fragment>
        );
      })}
    </>
  );
}
