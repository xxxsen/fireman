import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import type { WorkerTask } from "@/lib/api/market-assets";
import { RefreshTaskButton } from "./RefreshTaskButton";

const getTaskMock = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/simulations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/simulations")>()),
  getTask: (...args: unknown[]) => getTaskMock(...args),
}));

function task(status: WorkerTask["status"]): WorkerTask {
  return {
    id: "task_existing",
    worker_type: "sidecar_worker",
    type: "asset_history_sync",
    status,
    scope_type: "market_asset",
    scope_id: "asset_1",
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

describe("RefreshTaskButton", () => {
  beforeEach(() => getTaskMock.mockReset());

  it("attaches to the existing task returned by a stable-key conflict", async () => {
    const existing = task("running");
    const createTask = vi.fn().mockRejectedValue(
      new ApiError("task_already_active", "已有任务", {
        task_id: existing.id,
      }),
    );
    getTaskMock.mockResolvedValue(existing);
    const onTask = vi.fn();
    const onError = vi.fn();
    render(
      <RefreshTaskButton
        createTask={createTask}
        onTask={onTask}
        onError={onError}
      >
        刷新
      </RefreshTaskButton>,
    );

    fireEvent.click(screen.getByRole("button", { name: "刷新" }));
    await waitFor(() => expect(onTask).toHaveBeenCalledWith(existing, true));
    expect(getTaskMock).toHaveBeenCalledWith(existing.id);
    expect(onError).not.toHaveBeenCalled();
  });

  it("stays disabled through pre_complete", () => {
    render(
      <RefreshTaskButton
        createTask={vi.fn()}
        onTask={vi.fn()}
        activeTask={task("pre_complete")}
      >
        刷新
      </RefreshTaskButton>,
    );
    expect(screen.getByRole("button", { name: "处理中…" })).toBeDisabled();
  });
});
