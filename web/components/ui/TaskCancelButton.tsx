"use client";

import { useState } from "react";
import { useCancelTask } from "@/hooks/useCancelTask";
import { isTaskActive } from "@/lib/api/tasks";
import { queryErrorMessage } from "@/lib/query-error";
import type { Task } from "@/types/api";
import { Button } from "./Button";
import { ConfirmDialog } from "./ConfirmDialog";

type CancelableTask = Pick<Task, "id" | "status"> &
  Partial<
    Pick<
      Task,
      | "type"
      | "scope_type"
      | "scope_id"
      | "progress_current"
      | "progress_total"
    >
  >;

function cancellationDescription(task: CancelableTask, shared: boolean): string {
  let description = "任务正在执行。取消后将停止计算且不保存结果，不能恢复。";
  if (task.status === "pending") {
    description = "任务尚未开始。取消后将不会执行，且不能恢复。";
  } else if (task.status === "pre_complete") {
    description = "结果正在保存。取消成功后本次结果不会写入业务数据，不能恢复。";
  }
  if (shared) {
    description += " 该同步任务可能同时被其他页面使用，所有等待本次同步的页面都会停止等待。";
  }
  return description;
}

export interface TaskCancelButtonProps {
  task: CancelableTask | null | undefined;
  admin?: boolean;
  shared?: boolean;
  label?: string;
  className?: string;
  onCanceled?: (task: Task) => void | Promise<void>;
}

export function TaskCancelButton({
  task,
  admin = false,
  shared = false,
  label = "取消任务",
  className,
  onCanceled,
}: TaskCancelButtonProps) {
  const [openTaskId, setOpenTaskId] = useState<string | null>(null);
  const [terminalNoticeTaskId, setTerminalNoticeTaskId] = useState<string | null>(null);
  const mutation = useCancelTask({ admin, onCanceled });

  if (!task || !isTaskActive(task.status)) return null;

  if (terminalNoticeTaskId === task.id) {
    return <span className={className} role="status">任务已结束，无法取消</span>;
  }

  return (
    <>
      <Button
        variant="danger"
        className={className}
        disabled={mutation.isPending}
        onClick={() => {
          mutation.reset();
          setTerminalNoticeTaskId(null);
          setOpenTaskId(task.id);
        }}
      >
        {mutation.isPending ? "取消中…" : label}
      </Button>
      <ConfirmDialog
        open={openTaskId === task.id}
        title="取消任务"
        description={
          <div className="space-y-3">
            <p>{cancellationDescription(task, shared)}</p>
            {admin && (
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs text-ink-muted">
                <dt>任务</dt>
                <dd className="break-all font-mono text-ink">{task.id}</dd>
                {task.type && <><dt>类型</dt><dd className="break-all text-ink">{task.type}</dd></>}
                {(task.scope_type || task.scope_id) && (
                  <><dt>范围</dt><dd className="break-all text-ink">{[task.scope_type, task.scope_id].filter(Boolean).join(" / ")}</dd></>
                )}
                {task.progress_total !== undefined && task.progress_total > 0 && (
                  <><dt>进度</dt><dd className="text-ink">{task.progress_current ?? 0} / {task.progress_total}</dd></>
                )}
              </dl>
            )}
          </div>
        }
        confirmLabel="确认取消"
        variant="danger"
        pending={mutation.isPending}
        error={mutation.isError ? queryErrorMessage(mutation.error) : null}
        onConfirm={() =>
          mutation.mutate(task.id, {
            onSuccess: (resolved) => {
              setOpenTaskId(null);
              if (resolved.status !== "canceled") {
                setTerminalNoticeTaskId(task.id);
              }
            },
          })
        }
        onClose={() => {
          mutation.reset();
          setOpenTaskId(null);
        }}
      />
    </>
  );
}
