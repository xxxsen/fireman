"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { isTaskActive } from "@/lib/api/market-assets";
import { getTask, subscribeTaskEvents } from "@/lib/api/simulations";
import type { Task } from "@/types/api";

interface TaskStatusState {
  task: Task | null;
  progress: number;
  error: string | null;
  pollError: string | null;
  loading: boolean;
}

interface UseTaskStatusOptions {
  initialTask?: Task | null;
  onComplete?: (task: Task) => void;
  onFailed?: (task: Task) => void;
  onCanceled?: (task: Task) => void;
}

export function useTaskStatus(
  taskId: string | null | undefined,
  options?: UseTaskStatusOptions,
) {
  const [state, setState] = useState<TaskStatusState>({
    task: options?.initialTask ?? null,
    progress: 0,
    error: null,
    pollError: null,
    loading: false,
  });
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const esRef = useRef<EventSource | null>(null);
  const pollingRef = useRef(false);
  const optsRef = useRef(options);
  optsRef.current = options;

  const applyTask = useCallback((task: Task) => {
    let progress = 0;
    if (task.status === "complete") progress = 1;
    else if (task.progress_total > 0)
      progress = task.progress_current / task.progress_total;
    else if (task.status === "running") progress = 0.1;
    const error =
      task.status === "failed" ? (task.error_message ?? "任务失败") : null;
    setState({ task, progress, error, pollError: null, loading: false });

    if (task.status === "complete") optsRef.current?.onComplete?.(task);
    if (task.status === "failed") optsRef.current?.onFailed?.(task);
    if (task.status === "canceled") optsRef.current?.onCanceled?.(task);
    return task.status;
  }, []);

  const poll = useCallback(async () => {
    if (!taskId || pollingRef.current) return;
    pollingRef.current = true;
    try {
      const task = await getTask(taskId);
      const status = applyTask(task);
      if (
        status === "complete" ||
        status === "failed" ||
        status === "canceled"
      ) {
        if (intervalRef.current) {
          clearInterval(intervalRef.current);
          intervalRef.current = null;
        }
      }
    } catch (err) {
      setState((prev) => ({
        ...prev,
        error: err instanceof Error ? err.message : "轮询失败",
        pollError: err instanceof Error ? err.message : "轮询失败",
        loading: false,
      }));
    } finally {
      pollingRef.current = false;
    }
  }, [taskId, applyTask]);

  const startPolling = useCallback(() => {
    if (intervalRef.current) return;
    void poll();
    intervalRef.current = setInterval(() => void poll(), 2000);
  }, [poll]);

  useEffect(() => {
    if (!taskId) return;
    const initial = optsRef.current?.initialTask;
    setState({
      task: initial && initial.id === taskId ? initial : null,
      progress: 0,
      error: null,
      pollError: null,
      loading: true,
    });
    if (initial && initial.id === taskId && !isTaskActive(initial.status)) {
      applyTask(initial);
      return;
    }

    const es = subscribeTaskEvents(taskId, {
      onEvent: (ev) => {
        const progress =
          ev.progress_total > 0
            ? ev.progress_current / ev.progress_total
            : 0.05;
        setState((prev) => ({
          ...prev,
          task: prev.task
            ? {
                ...prev.task,
                status: ev.status as Task["status"],
                progress_current: ev.progress_current,
                progress_total: ev.progress_total,
                phase: ev.phase ?? prev.task.phase,
              }
            : null,
          progress,
          pollError: null,
          loading: false,
        }));
      },
      onTerminal: () => {
        void getTask(taskId).then(applyTask).catch(startPolling);
      },
      onError: startPolling,
    });

    if (es) {
      esRef.current = es;
    } else {
      startPolling();
    }

    return () => {
      esRef.current?.close();
      esRef.current = null;
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [taskId, applyTask, startPolling]);

  return { ...state, isActive: isTaskActive(state.task?.status) };
}
