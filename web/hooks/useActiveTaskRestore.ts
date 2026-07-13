"use client";

import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@/lib/api/client";
import { getTask } from "@/lib/api/simulations";
import {
  isTaskActive,
  listTasks,
  type WorkerType,
} from "@/lib/api/tasks";
import type { Task } from "@/types/api";

interface ActiveTaskRestoreOptions {
  workerType: WorkerType;
  taskType: string;
  scopeType: string;
  scopeId: string | null | undefined;
  preferredTaskId?: string | null;
  businessTaskId?: string | null;
  enabled?: boolean;
}

async function getCandidate(taskId: string | null | undefined): Promise<Task | null> {
  if (!taskId) return null;
  try {
    const task = await getTask(taskId);
    return isTaskActive(task.status) ? task : null;
  } catch (error) {
    if (error instanceof ApiError && error.code === "task_not_found") return null;
    throw error;
  }
}

export function useActiveTaskRestore(options: ActiveTaskRestoreOptions) {
  const enabled = (options.enabled ?? true) && Boolean(options.scopeId);
  const query = useQuery({
    queryKey: [
      "active-task-restore",
      options.workerType,
      options.taskType,
      options.scopeType,
      options.scopeId,
      options.businessTaskId ?? "",
      options.preferredTaskId ?? "",
    ],
    enabled,
    staleTime: 0,
    retry: 1,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
    queryFn: async () => {
      const seen = new Set<string>();
      for (const taskId of [options.businessTaskId, options.preferredTaskId]) {
        if (!taskId || seen.has(taskId)) continue;
        seen.add(taskId);
        const task = await getCandidate(taskId);
        if (task) return task;
      }
      const page = await listTasks({
        worker_type: options.workerType,
        type: options.taskType,
        status: "active",
        scope_type: options.scopeType,
        scope_id: options.scopeId!,
        limit: 20,
      });
      return page.items[0] ?? null;
    },
  });

  return {
    task: query.data ?? null,
    taskId: query.data?.id ?? null,
    restoring: enabled && query.isPending,
    restoreError: query.error,
    retryRestore: query.refetch,
  };
}
