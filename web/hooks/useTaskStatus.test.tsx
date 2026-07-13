import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Task } from "@/types/api";
import { useTaskStatus } from "./useTaskStatus";

const api = vi.hoisted(() => ({
  getTask: vi.fn(),
  subscribe: vi.fn(),
}));

vi.mock("@/lib/api/simulations", () => ({
  getTask: (...args: unknown[]) => api.getTask(...args),
  subscribeTaskEvents: (...args: unknown[]) => api.subscribe(...args),
}));

function task(status: Task["status"], id = "task_1"): Task {
  return {
    id,
    worker_type: "go_worker",
    type: "simulation",
    status,
    scope_type: "plan",
    scope_id: "plan_1",
    progress_current: status === "complete" ? 10 : 4,
    progress_total: 10,
    phase: status === "pre_complete" ? "finalizing" : "simulating",
    cancel_requested: false,
    attempt_count: 1,
    max_attempts: 2,
    error_code: status === "failed" ? "worker_internal_error" : "",
    error_message: status === "failed" ? "计算失败" : "",
    created_at: 1,
    updated_at: 2,
  };
}

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  return function TestQueryProvider({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

describe("useTaskStatus", () => {
  beforeEach(() => {
    api.getTask.mockReset();
    api.subscribe.mockReset();
    api.subscribe.mockReturnValue(null);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it.each([
    ["pending", 0.4, true],
    ["running", 0.4, true],
    ["pre_complete", 0.4, true],
    ["complete", 1, false],
    ["failed", 0.4, false],
    ["canceled", 0.4, false],
  ] as const)("maps %s consistently", async (status, progress, active) => {
    const current = task(status);
    api.getTask.mockResolvedValue(current);
    const onComplete = vi.fn();
    const onFailed = vi.fn();
    const onCanceled = vi.fn();

    const { result } = renderHook(
      () => useTaskStatus(current.id, { onComplete, onFailed, onCanceled }),
      { wrapper: wrapper() },
    );

    await waitFor(() => expect(result.current.task?.status).toBe(status));
    expect(result.current.progress).toBe(progress);
    expect(result.current.isActive).toBe(active);
    expect(result.current.pollError).toBeNull();
    expect(result.current.error).toBe(status === "failed" ? "计算失败" : null);
    expect(onComplete).toHaveBeenCalledTimes(status === "complete" ? 1 : 0);
    expect(onFailed).toHaveBeenCalledTimes(status === "failed" ? 1 : 0);
    expect(onCanceled).toHaveBeenCalledTimes(status === "canceled" ? 1 : 0);
  });

  it("keeps polling while SSE is connected but silent", async () => {
    vi.useFakeTimers();
    const close = vi.fn();
    api.subscribe.mockReturnValue({ close });
    api.getTask
      .mockResolvedValueOnce(task("pending"))
      .mockResolvedValueOnce(task("running"))
      .mockResolvedValueOnce(task("complete"));
    const onComplete = vi.fn();

    const { result, unmount } = renderHook(
      () => useTaskStatus("task_1", { onComplete }),
      { wrapper: wrapper() },
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(result.current.task?.status).toBe("pending");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(result.current.task?.status).toBe("running");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(result.current.task?.status).toBe("complete");
    expect(onComplete).toHaveBeenCalledTimes(1);
    unmount();
    expect(close).toHaveBeenCalled();
  });

  it("retains the last task and retries after a transient polling error", async () => {
    vi.useFakeTimers();
    api.getTask
      .mockResolvedValueOnce(task("running"))
      .mockRejectedValueOnce(new Error("temporary outage"))
      .mockResolvedValueOnce(task("complete"));

    const { result } = renderHook(() => useTaskStatus("task_1"), {
      wrapper: wrapper(),
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(result.current.task?.status).toBe("running");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(result.current.task?.status).toBe("running");
    expect(result.current.pollError).toBe("temporary outage");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(result.current.task?.status).toBe("complete");
    expect(result.current.pollError).toBeNull();
  });

  it("handles a terminal state exactly once when SSE and polling race", async () => {
    let onEvent: (() => void) | undefined;
    api.subscribe.mockImplementation(
      (_taskId: string, handlers: { onEvent?: () => void }) => {
        onEvent = handlers.onEvent;
        return { close: vi.fn() };
      },
    );
    api.getTask.mockResolvedValue(task("complete"));
    const onComplete = vi.fn();
    const { result } = renderHook(
      () => useTaskStatus("task_1", { onComplete }),
      { wrapper: wrapper() },
    );

    await waitFor(() => expect(result.current.task?.status).toBe("complete"));
    await act(async () => {
      onEvent?.();
      onEvent?.();
      await Promise.resolve();
    });
    await waitFor(() => expect(api.getTask).toHaveBeenCalled());
    expect(onComplete).toHaveBeenCalledTimes(1);
  });

  it("uses a matching terminal initial task without opening SSE", async () => {
    const initial = task("complete");
    const onComplete = vi.fn();
    const { result } = renderHook(
      () => useTaskStatus(initial.id, { initialTask: initial, onComplete }),
      { wrapper: wrapper() },
    );

    await waitFor(() => expect(result.current.task?.status).toBe("complete"));
    expect(api.subscribe).not.toHaveBeenCalled();
    expect(onComplete).toHaveBeenCalledTimes(1);
  });
});
