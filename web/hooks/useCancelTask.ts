"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { cancelAdminWorkerTask } from "@/lib/api/admin";
import { ApiError } from "@/lib/api/client";
import { cancelTask, getTask } from "@/lib/api/simulations";
import { taskQueryKey } from "@/lib/api/tasks";
import type { Task } from "@/types/api";

export interface UseCancelTaskOptions {
  admin?: boolean;
  onCanceled?: (task: Task) => void | Promise<void>;
}

export function useCancelTask(options: UseCancelTaskOptions = {}) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (taskId: string) => {
      try {
        return await (options.admin
          ? cancelAdminWorkerTask(taskId)
          : cancelTask(taskId));
      } catch (error) {
        if (error instanceof ApiError && error.code === "task_already_terminal") {
          return getTask(taskId);
        }
        throw error;
      }
    },
    onSuccess: async (task) => {
      queryClient.setQueryData(taskQueryKey(task.id), task);
      await queryClient.invalidateQueries({ queryKey: ["active-task-restore"] });
      await options.onCanceled?.(task);
    },
  });
}
