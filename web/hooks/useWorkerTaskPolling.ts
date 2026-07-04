"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getTask, isTaskActive, type WorkerTask } from "@/lib/api/market-assets";

const POLL_INTERVAL_MS = 2000;

interface UseWorkerTaskPollingOptions {
  /** Seed data so UI shows status before the first poll returns. */
  initialTask?: WorkerTask | null;
  onComplete?: (task: WorkerTask) => void;
  onFailed?: (task: WorkerTask) => void;
}

interface WorkerTaskPollingState {
  task: WorkerTask | null;
  /** Last polling error; stale task data is kept so the UI never blanks out. */
  pollError: string | null;
}

/**
 * Polls GET /api/v1/tasks/{id} every 2s until the task reaches a terminal
 * status (complete/failed/canceled). Poll failures keep the previous task
 * snapshot and are surfaced via pollError.
 */
export function useWorkerTaskPolling(
  taskId: string | null | undefined,
  options?: UseWorkerTaskPollingOptions,
) {
  const [state, setState] = useState<WorkerTaskPollingState>({
    task: options?.initialTask ?? null,
    pollError: null,
  });
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const inFlightRef = useRef(false);
  const notifiedRef = useRef<string | null>(null);
  const optsRef = useRef(options);
  optsRef.current = options;

  const stop = useCallback(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
  }, []);

  const poll = useCallback(
    async (id: string) => {
      if (inFlightRef.current) return;
      inFlightRef.current = true;
      try {
        const task = await getTask(id);
        setState({ task, pollError: null });
        if (!isTaskActive(task.status)) {
          stop();
          if (notifiedRef.current !== id) {
            notifiedRef.current = id;
            if (task.status === "complete") optsRef.current?.onComplete?.(task);
            if (task.status === "failed") optsRef.current?.onFailed?.(task);
          }
        }
      } catch (err) {
        setState((prev) => ({
          ...prev,
          pollError: err instanceof Error ? err.message : "任务状态查询失败",
        }));
      } finally {
        inFlightRef.current = false;
      }
    },
    [stop],
  );

  useEffect(() => {
    stop();
    const initial = optsRef.current?.initialTask;
    setState({
      task: initial && initial.id === taskId ? initial : null,
      pollError: null,
    });
    if (!taskId) return;
    if (initial && initial.id === taskId && !isTaskActive(initial.status)) {
      return;
    }
    void poll(taskId);
    intervalRef.current = setInterval(() => void poll(taskId), POLL_INTERVAL_MS);
    return stop;
  }, [taskId, poll, stop]);

  return { task: state.task, pollError: state.pollError, isActive: isTaskActive(state.task?.status) };
}
