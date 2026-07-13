"use client";

import { useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  isTaskActive,
  isTaskTerminal,
  taskQueryKey,
} from "@/lib/api/tasks";
import { getTask, subscribeTaskEvents } from "@/lib/api/simulations";
import type { Task } from "@/types/api";
import { ApiError } from "@/lib/api/client";

interface UseTaskStatusOptions {
  initialTask?: Task | null;
  onComplete?: (task: Task) => void;
  onFailed?: (task: Task) => void;
  onCanceled?: (task: Task) => void;
}

function taskProgress(task: Task | null | undefined): number {
  if (!task) return 0;
  if (task.status === "complete") return 1;
  if (task.progress_total <= 0) return 0;
  return Math.max(
    0,
    Math.min(1, task.progress_current / task.progress_total),
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "状态更新失败";
}

export function useTaskStatus(
  taskId: string | null | undefined,
  options?: UseTaskStatusOptions,
) {
  const queryClient = useQueryClient();
  const optsRef = useRef(options);
  const handledTerminalRef = useRef("");
  useEffect(() => {
    optsRef.current = options;
  }, [options]);

  const initialTask = options?.initialTask;
  const matchingInitial = initialTask?.id === taskId ? initialTask : undefined;
  const query = useQuery({
    queryKey: taskQueryKey(taskId),
    queryFn: () => getTask(taskId!),
    enabled: Boolean(taskId),
    initialData: matchingInitial,
    // A terminal task cannot change again. Active initial data still performs
    // an immediate authoritative GET on mount.
    staleTime: isTaskTerminal(matchingInitial?.status) ? Infinity : 0,
    retry: false,
    refetchOnWindowFocus: "always",
    refetchOnReconnect: "always",
    refetchIntervalInBackground: false,
    refetchInterval: (current) => {
      if (!taskId) return false;
      if (current.state.status === "error") return 5_000;
      return isTaskActive(current.state.data?.status) ? 2_000 : false;
    },
  });

  const status = query.data?.status;
  const terminal = isTaskTerminal(status);
  useEffect(() => {
    if (!taskId || terminal) return;
    const es = subscribeTaskEvents(taskId, {
      onEvent: () => {
        void queryClient.invalidateQueries({ queryKey: taskQueryKey(taskId) });
      },
      onError: () => {
        // HTTP polling remains active and is the authoritative recovery path.
      },
    });
    return () => es?.close();
  }, [queryClient, taskId, terminal]);

  useEffect(() => {
    if (!(query.error instanceof ApiError) || query.error.code !== "task_not_found") {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: ["active-task-restore"] });
  }, [query.error, queryClient]);

  useEffect(() => {
    if (!taskId || !query.data || !isTaskTerminal(query.data.status)) return;
    const terminalKey = `${taskId}:${query.data.status}`;
    if (handledTerminalRef.current === terminalKey) return;
    handledTerminalRef.current = terminalKey;
    if (query.data.status === "complete") optsRef.current?.onComplete?.(query.data);
    if (query.data.status === "failed") optsRef.current?.onFailed?.(query.data);
    if (query.data.status === "canceled") optsRef.current?.onCanceled?.(query.data);
  }, [query.data, taskId]);

  useEffect(() => {
    if (!taskId || !handledTerminalRef.current.startsWith(`${taskId}:`)) {
      handledTerminalRef.current = "";
    }
  }, [taskId]);

  const task = query.data ?? matchingInitial ?? null;
  const notFound =
    query.error instanceof ApiError && query.error.code === "task_not_found";
  return {
    task,
    progress: taskProgress(task),
    error:
      task?.status === "failed" ? (task.error_message ?? "任务失败") : null,
    pollError: query.error ? errorMessage(query.error) : null,
    pollFailureCount: query.failureCount,
    notFound,
    loading: Boolean(taskId) && query.isPending,
    isActive: isTaskActive(task?.status),
    refetch: query.refetch,
  };
}
