"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert } from "@/components/ui/Alert";
import { Drawer } from "@/components/ui/Drawer";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { TaskStatusBadge } from "@/components/ui/TaskStatusBadge";
import { getAdminWorkerTask, workerTaskTypeLabel } from "@/lib/api/admin";
import { isTaskActive } from "@/lib/api/market-assets";
import { formatDurationMs } from "@/lib/admin-format";
import { formatDateTimeFromMs } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import { CallbackResultBadge } from "./CallbackResultBadge";
import { JsonViewer } from "./JsonViewer";
import { TaskTimeline } from "./TaskTimeline";

const ACTIVE_POLL_MS = 2000;

export interface WorkerTaskDetailDrawerProps {
  taskId: string | null;
  onClose: () => void;
  /** Phase-2 mount point for row actions (retry etc.); render-only today. */
  actions?: React.ReactNode;
}

/**
 * Right-side task detail drawer: full row, execution timeline, failure info,
 * callback records and raw payload/result envelopes. While the task is
 * active the detail re-polls every 2s until it reaches a terminal state.
 */
export function WorkerTaskDetailDrawer({ taskId, onClose, actions }: WorkerTaskDetailDrawerProps) {
  const [copied, setCopied] = useState(false);
  const detail = useQuery({
    queryKey: ["admin", "worker-task", taskId],
    queryFn: () => getAdminWorkerTask(taskId!),
    enabled: taskId !== null,
    refetchInterval: (query) =>
      isTaskActive(query.state.data?.task.status) ? ACTIVE_POLL_MS : false,
  });

  const task = detail.data?.task;

  const copyId = async () => {
    if (!task) return;
    try {
      await navigator.clipboard.writeText(task.id);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard unavailable; ignore.
    }
  };

  return (
    <Drawer open={taskId !== null} onClose={onClose} title="任务详情" className="max-w-xl">
      {detail.isLoading && <LoadingState label="加载任务详情…" />}
      {detail.isError && (
        <ErrorState
          message={queryErrorMessage(detail.error, "任务详情加载失败")}
          onRetry={() => void detail.refetch()}
        />
      )}
      {task && (
        <div className="space-y-5" data-testid="worker-task-detail">
          <div className="flex flex-wrap items-center gap-2">
            <TaskStatusBadge status={task.status} />
            <span className="text-sm text-ink">{workerTaskTypeLabel(task.type)}</span>
            <span className="ml-auto flex min-w-0 items-center gap-1">
              <span className="truncate font-mono text-xs text-ink-muted">{task.id}</span>
              <button
                type="button"
                onClick={copyId}
                data-testid="task-id-copy"
                className="shrink-0 rounded px-1.5 py-0.5 text-[10px] text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink"
              >
                {copied ? "已复制" : "复制"}
              </button>
            </span>
          </div>

          {task.dedupe_key && (
            <p className="break-all font-mono text-xs text-ink-muted">{task.dedupe_key}</p>
          )}

          <section>
            <h3 className="mb-2 text-sm font-medium text-ink">执行时间线</h3>
            <TaskTimeline
              timeline={detail.data!.timeline}
              heartbeat={detail.data!.heartbeat}
              running={task.status === "running"}
            />
          </section>

          {(task.error_code || task.error_message) && (
            <Alert variant="danger" title={task.error_code || "任务失败"}>
              <span className="break-all">{task.error_message || "无更多错误信息"}</span>
            </Alert>
          )}

          <section>
            <h3 className="mb-2 text-sm font-medium text-ink">回调记录</h3>
            {detail.data!.post_process_records.length === 0 ? (
              <p className="text-xs text-ink-muted">尚未收到 post-process 回调。</p>
            ) : (
              <div className="overflow-x-auto rounded-md border border-line">
                <table className="w-full text-xs" data-testid="task-callback-table">
                  <thead>
                    <tr className="border-b border-line text-left text-ink-muted">
                      <th className="px-2 py-1.5 font-medium">#</th>
                      <th className="px-2 py-1.5 font-medium">结果</th>
                      <th className="px-2 py-1.5 font-medium">错误码</th>
                      <th className="px-2 py-1.5 font-medium">耗时</th>
                      <th className="px-2 py-1.5 font-medium">时间</th>
                    </tr>
                  </thead>
                  <tbody>
                    {detail.data!.post_process_records.map((rec) => (
                      <tr key={rec.id} className="border-b border-line/60 last:border-b-0">
                        <td className="px-2 py-1.5 tabular-nums text-ink-muted">{rec.attempt_no}</td>
                        <td className="px-2 py-1.5">
                          <CallbackResultBadge result={rec.result} />
                        </td>
                        <td className="px-2 py-1.5 font-mono text-ink-muted">
                          {rec.error_code || "—"}
                        </td>
                        <td className="px-2 py-1.5 tabular-nums text-ink-muted">
                          {formatDurationMs(rec.duration_ms)}
                        </td>
                        <td className="whitespace-nowrap px-2 py-1.5 text-ink-muted">
                          {formatDateTimeFromMs(rec.created_at)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-medium text-ink">payload / 结果</h3>
            <JsonViewer label="payload_json" raw={task.payload_json} />
            <JsonViewer label="result_data（资源 envelope）" raw={task.result_data} />
          </section>

          {actions && <div data-testid="task-detail-actions">{actions}</div>}
        </div>
      )}
    </Drawer>
  );
}
