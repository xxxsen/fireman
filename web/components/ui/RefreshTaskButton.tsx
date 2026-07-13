"use client";

import { useState } from "react";
import { ApiError } from "@/lib/api/client";
import type { TaskCreateResult, WorkerTask } from "@/lib/api/market-assets";
import { isTaskActive } from "@/lib/api/tasks";
import { getTask } from "@/lib/api/simulations";
import { Button, type ButtonVariant } from "./Button";

export interface RefreshTaskButtonProps {
  /** Creates (or dedupes to) a worker task on the backend. */
  createTask: () => Promise<TaskCreateResult>;
  /** Called with the created or existing task; parent starts polling from here. */
  onTask: (task: WorkerTask, existed: boolean) => void;
  onError?: (message: string) => void;
  /** Currently tracked task; button disables while it is active. */
  activeTask?: WorkerTask | null;
  disabled?: boolean;
  variant?: ButtonVariant;
  className?: string;
  children: React.ReactNode;
  "data-testid"?: string;
}

/**
 * Task-creating button: disables while a task is active or the create request
 * is in flight, and treats backend "existing active task" responses as a task
 * to poll instead of an error.
 */
export function RefreshTaskButton({
  createTask,
  onTask,
  onError,
  activeTask,
  disabled,
  variant = "primary",
  className,
  children,
  "data-testid": testId,
}: RefreshTaskButtonProps) {
  const [creating, setCreating] = useState(false);
  const taskActive = isTaskActive(activeTask?.status);

  const handleClick = async () => {
    if (creating || taskActive) return;
    setCreating(true);
    try {
      const result = await createTask();
      onTask(result.task, result.existed);
    } catch (err) {
      if (err instanceof ApiError && err.code === "task_already_active") {
        const taskId = err.details?.task_id;
        if (typeof taskId === "string" && taskId) {
          try {
            onTask(await getTask(taskId), true);
            return;
          } catch {
            // Fall through to the original conflict message if recovery fails.
          }
        }
      }
      const message =
        err instanceof ApiError ? err.message : err instanceof Error ? err.message : "创建任务失败";
      onError?.(message);
    } finally {
      setCreating(false);
    }
  };

  return (
    <Button
      variant={variant}
      className={className}
      data-testid={testId}
      disabled={disabled || taskActive}
      pending={creating || taskActive}
      onClick={() => void handleClick()}
    >
      {children}
    </Button>
  );
}
