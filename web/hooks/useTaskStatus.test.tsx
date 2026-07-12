import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Task } from "@/types/api";
import { useTaskStatus } from "./useTaskStatus";

const getTaskMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/simulations", () => ({
  getTask: (...args: unknown[]) => getTaskMock(...args),
  subscribeTaskEvents: () => null,
}));

function task(status: Task["status"]): Task {
  return {
    id: `task_${status}`,
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
  };
}

describe("useTaskStatus", () => {
  beforeEach(() => {
    getTaskMock.mockReset();
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
    getTaskMock.mockResolvedValue(current);
    const onComplete = vi.fn();
    const onFailed = vi.fn();
    const onCanceled = vi.fn();

    const { result, unmount } = renderHook(() =>
      useTaskStatus(current.id, { onComplete, onFailed, onCanceled }),
    );

    await waitFor(() => expect(result.current.task?.status).toBe(status));
    expect(result.current.progress).toBe(progress);
    expect(result.current.isActive).toBe(active);
    expect(result.current.pollError).toBeNull();
    expect(result.current.error).toBe(status === "failed" ? "计算失败" : null);
    expect(onComplete).toHaveBeenCalledTimes(status === "complete" ? 1 : 0);
    expect(onFailed).toHaveBeenCalledTimes(status === "failed" ? 1 : 0);
    expect(onCanceled).toHaveBeenCalledTimes(status === "canceled" ? 1 : 0);
    if (status === "failed") expect(onFailed).toHaveBeenCalledWith(current);
    unmount();
  });
});
