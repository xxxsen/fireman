"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getJob, subscribeJobEvents } from "@/lib/api/simulations";
import type { Job } from "@/types/api";

interface JobStatusState {
  job: Job | null;
  progress: number;
  error: string | null;
  loading: boolean;
}

interface UseJobStatusOptions {
  onComplete?: () => void;
  onFailed?: (message: string) => void;
  onCanceled?: () => void;
}

export function useJobStatus(jobId: string | null, options?: UseJobStatusOptions) {
  const [state, setState] = useState<JobStatusState>({
    job: null,
    progress: 0,
    error: null,
    loading: false,
  });
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const esRef = useRef<EventSource | null>(null);
  const pollingRef = useRef(false);
  const optsRef = useRef(options);
  optsRef.current = options;

  const applyJob = useCallback((job: Job) => {
    let progress = 0;
    if (job.status === "succeeded") progress = 1;
    else if (job.progress_total > 0) progress = job.progress_current / job.progress_total;
    else if (job.status === "running") progress = 0.1;
    const error =
      job.status === "failed" ? job.error_message ?? "任务失败" : null;
    setState({ job, progress, error, loading: false });

    if (job.status === "succeeded") optsRef.current?.onComplete?.();
    if (job.status === "failed") optsRef.current?.onFailed?.(error ?? "任务失败");
    if (job.status === "canceled") optsRef.current?.onCanceled?.();
    return job.status;
  }, []);

  const poll = useCallback(async () => {
    if (!jobId || pollingRef.current) return;
    pollingRef.current = true;
    try {
      const job = await getJob(jobId);
      const status = applyJob(job);
      if (status === "succeeded" || status === "failed" || status === "canceled") {
        if (intervalRef.current) {
          clearInterval(intervalRef.current);
          intervalRef.current = null;
        }
      }
    } catch (err) {
      setState((prev) => ({
        ...prev,
        error: err instanceof Error ? err.message : "轮询失败",
        loading: false,
      }));
    } finally {
      pollingRef.current = false;
    }
  }, [jobId, applyJob]);

  const startPolling = useCallback(() => {
    if (intervalRef.current) return;
    void poll();
    intervalRef.current = setInterval(() => void poll(), 2000);
  }, [poll]);

  useEffect(() => {
    if (!jobId) return;
    setState({ job: null, progress: 0, error: null, loading: true });

    const es = subscribeJobEvents(jobId, {
      onEvent: (ev) => {
        const progress =
          ev.progress_total > 0 ? ev.progress_current / ev.progress_total : 0.05;
        setState((prev) => ({
          ...prev,
          job: prev.job
            ? {
                ...prev.job,
                status: ev.status as Job["status"],
                progress_current: ev.progress_current,
                progress_total: ev.progress_total,
                phase: ev.phase ?? prev.job.phase,
              }
            : null,
          progress,
          loading: false,
        }));
      },
      onTerminal: (_ev) => {
        void getJob(jobId).then(applyJob).catch(startPolling);
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
  }, [jobId, applyJob, startPolling]);

  return state;
}
