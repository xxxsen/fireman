// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import type { Task } from "@/types/api";
import { TaskCancelButton } from "./TaskCancelButton";

const api = vi.hoisted(() => ({
  cancel: vi.fn(),
  cancelAdmin: vi.fn(),
  getTask: vi.fn(),
}));

vi.mock("@/lib/api/simulations", () => ({
  cancelTask: (...args: unknown[]) => api.cancel(...args),
  getTask: (...args: unknown[]) => api.getTask(...args),
}));
vi.mock("@/lib/api/admin", () => ({
  cancelAdminWorkerTask: (...args: unknown[]) => api.cancelAdmin(...args),
}));

function task(status: Task["status"]): Task {
  return {
    id: "task_cancel", worker_type: "go_worker", type: "simulation", status,
    scope_type: "plan", scope_id: "plan_1", progress_current: 1, progress_total: 10,
    phase: "running", cancel_requested: false, attempt_count: 1, max_attempts: 3,
    created_at: 1, updated_at: 1,
  };
}

function renderButton(value: Task, props: { admin?: boolean; shared?: boolean; onCanceled?: (task: Task) => void } = {}) {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <TaskCancelButton task={value} {...props} />
    </QueryClientProvider>,
  );
}

describe("TaskCancelButton", () => {
  beforeEach(() => {
    api.cancel.mockReset();
    api.cancelAdmin.mockReset();
    api.getTask.mockReset();
  });

  it.each([
    ["pending", "任务尚未开始"],
    ["running", "任务正在执行"],
    ["pre_complete", "结果正在保存"],
  ] as const)("shows the %s cancellation contract", (status, description) => {
    renderButton(task(status));
    fireEvent.click(screen.getByRole("button", { name: "取消任务" }));
    expect(screen.getByRole("dialog")).toHaveTextContent(description);
    expect(screen.getByRole("dialog")).toHaveTextContent("不能恢复");
  });

  it("does not render for terminal tasks", () => {
    renderButton(task("canceled"));
    expect(screen.queryByRole("button", { name: "取消任务" })).not.toBeInTheDocument();
  });

  it("confirms through the public API and publishes the terminal task", async () => {
    const canceled = { ...task("canceled"), cancel_requested: true };
    const onCanceled = vi.fn();
    api.cancel.mockResolvedValue(canceled);
    renderButton(task("running"), { onCanceled });

    fireEvent.click(screen.getByRole("button", { name: "取消任务" }));
    fireEvent.click(screen.getByRole("button", { name: "确认取消" }));

    await waitFor(() => expect(api.cancel).toHaveBeenCalledWith("task_cancel"));
    await waitFor(() => expect(onCanceled).toHaveBeenCalledWith(canceled));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("uses the admin API and retains a failed dialog for retry", async () => {
    api.cancelAdmin.mockRejectedValue(new Error("取消失败"));
    renderButton(task("running"), { admin: true, shared: true });

    fireEvent.click(screen.getByRole("button", { name: "取消任务" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("其他页面");
    expect(screen.getByRole("dialog")).toHaveTextContent("task_cancel");
    expect(screen.getByRole("dialog")).toHaveTextContent("simulation");
    fireEvent.click(screen.getByRole("button", { name: "确认取消" }));

    await waitFor(() => expect(api.cancelAdmin).toHaveBeenCalledWith("task_cancel"));
    expect(await screen.findByRole("alert")).toHaveTextContent("取消失败");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("reconciles a terminal race instead of leaving a stale cancel button", async () => {
    api.cancel.mockRejectedValue(
      new ApiError("task_already_terminal", "task already finished", { status: "complete" }, 409),
    );
    api.getTask.mockResolvedValue(task("complete"));
    renderButton(task("running"));

    fireEvent.click(screen.getByRole("button", { name: "取消任务" }));
    fireEvent.click(screen.getByRole("button", { name: "确认取消" }));

    expect(await screen.findByRole("status")).toHaveTextContent("任务已结束，无法取消");
    expect(api.getTask).toHaveBeenCalledWith("task_cancel");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
