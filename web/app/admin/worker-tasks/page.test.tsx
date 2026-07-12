import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AdminPage,
  AdminWorkerTaskDetail,
  AdminWorkerTaskItem,
} from "@/lib/api/admin";
import AdminWorkerTasksPage from "./page";

const replaceMock = vi.hoisted(() => vi.fn());
const searchParamsMock = vi.hoisted(() => ({ value: new URLSearchParams() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
  usePathname: () => "/admin/worker-tasks",
  useSearchParams: () => searchParamsMock.value,
}));

const listMock = vi.hoisted(() => vi.fn());
const detailMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  listAdminWorkerTasks: (...args: unknown[]) => listMock(...args),
  getAdminWorkerTask: (...args: unknown[]) => detailMock(...args),
}));

function makeTask(
  overrides: Partial<AdminWorkerTaskItem> = {},
): AdminWorkerTaskItem {
  return {
    id: "wt_1",
    worker_type: "sidecar_worker",
    type: "asset_history_sync",
    status: "failed",
    scope_type: "asset",
    scope_id: "CN|cn_exchange_fund|sh|510300",
    dedupe_key: "asset_history|SH|cn_exchange_fund|sh|510300|none|close",
    attempt_count: 2,
    max_attempts: 3,
    progress_current: 10,
    progress_total: 10,
    phase: "",
    error_code: "source_unavailable",
    error_message: "tickflow down",
    finalize_attempts: 2,
    created_at: Date.now() - 180_000,
    started_at: Date.now() - 170_000,
    finished_at: Date.now() - 151_000,
    duration_ms: 19_000,
    ...overrides,
  };
}

function makePage(
  items: AdminWorkerTaskItem[],
  total = items.length,
): AdminPage<AdminWorkerTaskItem> {
  return { items, total, limit: 20, offset: 0 };
}

function makeDetail(): AdminWorkerTaskDetail {
  const t0 = Date.now() - 180_000;
  return {
    task: {
      id: "wt_1",
      version_no: 3,
      worker_type: "sidecar_worker",
      type: "asset_history_sync",
      status: "failed",
      scope_type: "market_asset",
      scope_id: "CN|cn_exchange_fund|sh|510300",
      dedupe_key: "asset_history|SH|cn_exchange_fund|sh|510300|none|close",
      payload_json: '{"scope":"cn_all"}',
      result_key: "",
      result_meta_json: "",
      attempt_count: 2,
      max_attempts: 3,
      heartbeat_at: null,
      error_code: "source_unavailable",
      error_message: "tickflow down",
      finalize_attempts: 2,
      next_finalize_at: null,
      created_at: t0,
      started_at: t0 + 1000,
      pre_completed_at: t0 + 15_000,
      finished_at: t0 + 20_000,
    },
    timeline: [
      { phase: "created", at: t0 },
      { phase: "started", at: t0 + 1000 },
      { phase: "pre_complete", at: t0 + 15_000 },
      { phase: "finished", at: t0 + 20_000, status: "failed" },
    ],
    heartbeat: null,
    attempts: [
      {
        task_id: "wt_1",
        attempt_no: 1,
        worker_type: "sidecar_worker",
        worker_id: "sidecar-test",
        claimed_at: t0 + 1000,
        last_heartbeat_at: t0 + 10_000,
        released_at: t0 + 20_000,
        outcome: "failed",
        error_code: "source_unavailable",
        error_message: "tickflow down",
      },
    ],
    finalize_records: [
      {
        id: 12,
        task_id: "wt_1",
        task_type: "asset_history_sync",
        attempt_no: 1,
        result: "retryable_error",
        error_code: "resource_not_found",
        error_message: "gone",
        duration_ms: 45,
        created_at: t0 + 16_000,
      },
    ],
  };
}

function makePendingDetail(): AdminWorkerTaskDetail {
  const detail = makeDetail();
  detail.task.status = "pending";
  detail.task.result_key = undefined;
  detail.task.started_at = null;
  detail.task.pre_completed_at = null;
  detail.task.finished_at = null;
  detail.timeline = detail.timeline.slice(0, 1);
  detail.attempts = [];
  detail.finalize_records = [];
  return detail;
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminWorkerTasksPage />
    </QueryClientProvider>,
  );
}

describe("AdminWorkerTasksPage", () => {
  beforeEach(() => {
    replaceMock.mockReset();
    listMock.mockReset();
    detailMock.mockReset();
    searchParamsMock.value = new URLSearchParams();
    listMock.mockResolvedValue(makePage([makeTask()], 57));
  });

  it("renders task rows with status badge, type label and pagination", async () => {
    renderPage();
    const row = await screen.findByTestId("worker-task-row");
    expect(within(row).getByTestId("task-status-badge")).toHaveAttribute(
      "data-status",
      "failed",
    );
    expect(within(row).getByText("历史同步")).toBeInTheDocument();
    expect(within(row).getByText("19s")).toBeInTheDocument();

    const pagination = screen.getByTestId("admin-pagination");
    expect(pagination).toHaveTextContent("共 57 条");
    expect(pagination).toHaveTextContent("第 1 / 3 页");
  });

  it("navigates to the next page via the offset query param", async () => {
    renderPage();
    await screen.findByTestId("worker-task-row");
    fireEvent.click(screen.getByTestId("admin-page-next"));
    expect(replaceMock).toHaveBeenCalledWith("/admin/worker-tasks?offset=20", {
      scroll: false,
    });
  });

  it("writes filter changes into the URL and resets the offset", async () => {
    searchParamsMock.value = new URLSearchParams("offset=20");
    renderPage();
    await screen.findByTestId("worker-task-row");

    fireEvent.change(screen.getByTestId("admin-filter-status"), {
      target: { value: "failed" },
    });
    expect(replaceMock).toHaveBeenCalledWith(
      "/admin/worker-tasks?status=failed",
      {
        scroll: false,
      },
    );
  });

  it("debounces the search input before writing q into the URL", async () => {
    renderPage();
    await screen.findByTestId("worker-task-row");

    fireEvent.change(screen.getByTestId("admin-filter-search"), {
      target: { value: "510300" },
    });
    expect(replaceMock).not.toHaveBeenCalled();
    await waitFor(
      () =>
        expect(replaceMock).toHaveBeenCalledWith(
          "/admin/worker-tasks?q=510300",
          {
            scroll: false,
          },
        ),
      { timeout: 2000 },
    );
  });

  it("passes URL filters to the API call", async () => {
    searchParamsMock.value = new URLSearchParams(
      "type=fx_rate_sync&status=active&scope_type=system&scope_id=fx&offset=40",
    );
    renderPage();
    await waitFor(() =>
      expect(listMock).toHaveBeenCalledWith(
        expect.objectContaining({
          type: "fx_rate_sync",
          status: "active",
          scopeType: "system",
          scopeId: "fx",
          limit: 20,
          offset: 40,
        }),
      ),
    );
  });

  it("resets all filters at once", async () => {
    searchParamsMock.value = new URLSearchParams(
      "type=fx_rate_sync&q=abc&offset=20",
    );
    renderPage();
    await screen.findByTestId("worker-task-row");
    fireEvent.click(screen.getByTestId("admin-filter-reset"));
    expect(replaceMock).toHaveBeenCalledWith("/admin/worker-tasks", {
      scroll: false,
    });
  });

  it("opens the detail drawer via row click by writing task_id into the URL", async () => {
    renderPage();
    const row = await screen.findByTestId("worker-task-row");
    fireEvent.click(row);
    expect(replaceMock).toHaveBeenCalledWith(
      "/admin/worker-tasks?task_id=wt_1",
      {
        scroll: false,
      },
    );
  });

  it("renders the drawer with timeline, finalization records and payload viewer", async () => {
    searchParamsMock.value = new URLSearchParams("task_id=wt_1");
    detailMock.mockResolvedValue(makeDetail());
    renderPage();

    const detail = await screen.findByTestId("worker-task-detail");
    expect(detailMock).toHaveBeenCalledWith("wt_1");

    const timeline = within(detail).getByTestId("task-timeline");
    expect(within(timeline).getByText("任务创建")).toBeInTheDocument();
    expect(within(timeline).getByText("开始执行")).toBeInTheDocument();
    expect(within(timeline).getByText("结果上传")).toBeInTheDocument();
    expect(within(timeline).getByText("执行结束")).toBeInTheDocument();
    expect(within(timeline).getByText("同步失败")).toBeInTheDocument();

    const finalizationTable = within(detail).getByTestId("task-finalize-table");
    expect(
      within(finalizationTable).getByTestId("finalize-result-badge"),
    ).toHaveAttribute("data-result", "retryable_error");
    expect(
      within(finalizationTable).getByText("resource_not_found"),
    ).toBeInTheDocument();

    expect(within(detail).getByText("payload_json")).toBeInTheDocument();
    // Empty result metadata renders the empty marker instead of a viewer.
    expect(within(detail).getByTestId("json-viewer-empty")).toBeInTheDocument();
  });

  it("shows the heartbeat node for a running task", async () => {
    searchParamsMock.value = new URLSearchParams("task_id=wt_run");
    const detail = makeDetail();
    detail.task.id = "wt_run";
    detail.task.status = "running";
    detail.task.finished_at = null;
    detail.timeline = detail.timeline.slice(0, 2);
    detail.heartbeat = { at: Date.now() - 5000, stale: false };
    detailMock.mockResolvedValue(detail);
    renderPage();

    const node = await screen.findByTestId("task-timeline-heartbeat");
    expect(node).toHaveTextContent("心跳正常");
  });

  it("renders a pending task whose result key has not been produced", async () => {
    searchParamsMock.value = new URLSearchParams("task_id=wt_pending");
    const detail = makePendingDetail();
    detail.task.id = "wt_pending";
    detailMock.mockResolvedValue(detail);
    renderPage();

    const drawer = await screen.findByTestId("worker-task-detail");
    expect(
      within(drawer).getByText("任务尚未被 worker 领取。"),
    ).toBeInTheDocument();
    expect(
      within(drawer).queryByText("查看关联业务结果"),
    ).not.toBeInTheDocument();
  });

  it("marks a stale heartbeat as waiting for reclaim", async () => {
    searchParamsMock.value = new URLSearchParams("task_id=wt_stale");
    const detail = makeDetail();
    detail.task.id = "wt_stale";
    detail.task.status = "running";
    detail.timeline = detail.timeline.slice(0, 2);
    detail.heartbeat = { at: Date.now() - 120_000, stale: true };
    detailMock.mockResolvedValue(detail);
    renderPage();

    const node = await screen.findByTestId("task-timeline-heartbeat");
    expect(node).toHaveTextContent("心跳滞留，等待回收");
  });

  it("closes the drawer by clearing task_id from the URL", async () => {
    searchParamsMock.value = new URLSearchParams("task_id=wt_1");
    detailMock.mockResolvedValue(makeDetail());
    renderPage();
    await screen.findByTestId("worker-task-detail");

    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(replaceMock).toHaveBeenCalledWith("/admin/worker-tasks", {
      scroll: false,
    });
  });

  it("shows the empty state when no task matches", async () => {
    listMock.mockResolvedValue(makePage([]));
    renderPage();
    expect(await screen.findByText("没有匹配的任务")).toBeInTheDocument();
  });

  it("shows the error state with retry when the list request fails", async () => {
    listMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.getByTestId("error-state-retry")).toBeInTheDocument();
  });
});
