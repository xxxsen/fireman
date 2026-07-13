import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import type { Task } from "@/types/api";
import { useActiveTaskRestore } from "./useActiveTaskRestore";

const api = vi.hoisted(() => ({ getTask: vi.fn(), listTasks: vi.fn() }));

vi.mock("@/lib/api/simulations", () => ({
  getTask: (...args: unknown[]) => api.getTask(...args),
}));
vi.mock("@/lib/api/tasks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/tasks")>();
  return { ...actual, listTasks: (...args: unknown[]) => api.listTasks(...args) };
});

function task(status: Task["status"], id: string): Task {
  return {
    id,
    worker_type: "go_worker",
    type: "simulation",
    status,
    scope_type: "plan",
    scope_id: "plan_1",
    progress_current: 0,
    progress_total: 1,
    phase: "",
    cancel_requested: false,
    attempt_count: 0,
    max_attempts: 2,
    created_at: 1,
    updated_at: 1,
  };
}

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function TestQueryProvider({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  };
}

const base = {
  workerType: "go_worker" as const,
  taskType: "simulation",
  scopeType: "plan",
  scopeId: "plan_1",
};

describe("useActiveTaskRestore", () => {
  beforeEach(() => {
    api.getTask.mockReset();
    api.listTasks.mockReset();
  });

  it("prefers an active business task without listing the scope", async () => {
    api.getTask.mockResolvedValue(task("running", "business"));
    const { result } = renderHook(
      () => useActiveTaskRestore({ ...base, businessTaskId: "business" }),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.taskId).toBe("business"));
    expect(api.listTasks).not.toHaveBeenCalled();
  });

  it("falls back from missing candidates to the latest scoped active task", async () => {
    api.getTask.mockRejectedValue(new ApiError("task_not_found", "missing"));
    api.listTasks.mockResolvedValue({
      items: [task("pre_complete", "scoped")],
      total: 1,
    });
    const { result } = renderHook(
      () =>
        useActiveTaskRestore({
          ...base,
          businessTaskId: "missing",
          preferredTaskId: "also_missing",
        }),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.taskId).toBe("scoped"));
    expect(api.listTasks).toHaveBeenCalledWith(
      expect.objectContaining({ status: "active", scope_id: "plan_1" }),
    );
  });

  it("keeps restoration failed instead of treating a network error as no task", async () => {
    api.getTask.mockRejectedValue(new ApiError("network_error", "offline"));
    const { result } = renderHook(
      () => useActiveTaskRestore({ ...base, businessTaskId: "business" }),
      { wrapper: wrapper() },
    );
    await waitFor(() => expect(result.current.restoreError).toBeTruthy(), {
      timeout: 2_500,
    });
    expect(result.current.taskId).toBeNull();
    expect(api.listTasks).not.toHaveBeenCalled();
  });
});
