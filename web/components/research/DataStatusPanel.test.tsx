import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchReadiness } from "@/lib/api/research";
import type { WorkerTask } from "@/lib/api/market-assets";
import { DataStatusPanel, syncFailureSuggestion } from "./DataStatusPanel";

const syncCollectionHistoryMock = vi.hoisted(() => vi.fn());
const getCollectionSyncStatusMock = vi.hoisted(() => vi.fn());
const getTaskMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  syncCollectionHistory: (...args: unknown[]) =>
    syncCollectionHistoryMock(...args),
  getCollectionSyncStatus: (...args: unknown[]) =>
    getCollectionSyncStatusMock(...args),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
}));

vi.mock("@/lib/api/simulations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/simulations")>()),
  getTask: (...args: unknown[]) => getTaskMock(...args),
  subscribeTaskEvents: () => null,
}));

function task(overrides: Partial<WorkerTask> = {}): WorkerTask {
  return {
    id: "wt_1",
    task_type: "asset_history_sync",
    status: "running",
    dedup_key: "",
    payload_json: "",
    priority: 0,
    attempts: 0,
    max_attempts: 3,
    created_at: 0,
    updated_at: 0,
    ...overrides,
  } as WorkerTask;
}

function readiness(
  overrides: Partial<ResearchReadiness> = {},
): ResearchReadiness {
  return {
    ready: true,
    weight_sum: 1,
    blocking_reasons: [],
    warnings: [],
    assets: [],
    data_dependencies: {
      asset_count: 2,
      fx_pairs: ["USDCNY"],
      stale_asset_count: 0,
      missing_history_count: 0,
    },
    ...overrides,
  };
}

function renderPanel(
  props: Partial<Parameters<typeof DataStatusPanel>[0]> = {},
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const utils = render(
    <QueryClientProvider client={client}>
      <DataStatusPanel
        collectionId="rc_1"
        readiness={readiness()}
        readinessLoading={false}
        {...props}
      />
    </QueryClientProvider>,
  );
  return { ...utils, client };
}

describe("syncFailureSuggestion", () => {
  it("maps known error codes to actionable text", () => {
    expect(syncFailureSuggestion("sidecar_unavailable")).toContain("sidecar");
    expect(syncFailureSuggestion("source_rate_limited")).toContain("限流");
    expect(syncFailureSuggestion("whatever")).toContain("重试");
  });
});

describe("DataStatusPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getCollectionSyncStatusMock.mockResolvedValue({
      assets: [],
      fx: [],
      blocked: [],
    });
    getTaskMock.mockResolvedValue(task({ status: "complete" }));
  });

  it("restores active sync tasks after remount and blocks duplicate submission", async () => {
    getCollectionSyncStatusMock.mockResolvedValue({
      assets: [
        {
          asset_key: "CN|a",
          status: "existed",
          task: task({ id: "wt_restored", status: "running" }),
        },
      ],
      fx: [],
      blocked: [],
    });
    getTaskMock.mockResolvedValue(
      task({ id: "wt_restored", status: "running" }),
    );

    renderPanel();

    expect(await screen.findByText("已有同步任务，复用中")).toBeInTheDocument();
    expect(screen.getByTestId("sync-collection")).toBeDisabled();
    expect(syncCollectionHistoryMock).not.toHaveBeenCalled();
  });

  it("keeps sync disabled when persisted task recovery fails", async () => {
    getCollectionSyncStatusMock.mockRejectedValue(new Error("network down"));
    renderPanel();
    expect(
      await screen.findByText("同步任务状态恢复失败，恢复前不能创建新任务。"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("sync-collection")).toBeDisabled();
    fireEvent.click(screen.getByTestId("sync-collection"));
    expect(syncCollectionHistoryMock).not.toHaveBeenCalled();
  });

  it("shows ready badge and dependency facts", () => {
    renderPanel();
    expect(screen.getByText("数据就绪")).toBeInTheDocument();
    expect(screen.getByText("USDCNY")).toBeInTheDocument();
  });

  it("lists blocking reasons and warnings", () => {
    renderPanel({
      readiness: readiness({
        ready: false,
        blocking_reasons: [
          {
            asset_key: "CN|x",
            reason: "history_missing",
            message: "缺少历史数据",
          },
        ],
        warnings: [{ reason: "history_short", message: "历史长度不足 3 年" }],
      }),
    });
    expect(screen.getByText("1 项阻断")).toBeInTheDocument();
    expect(screen.getByTestId("blocking-reasons")).toHaveTextContent(
      "缺少历史数据",
    );
    expect(screen.getByTestId("warnings")).toHaveTextContent(
      "历史长度不足 3 年",
    );
  });

  it("hides weight-sum blocking from the data status panel", () => {
    renderPanel({
      readiness: readiness({
        ready: false,
        weight_sum: 0.8,
        blocking_reasons: [
          { reason: "weight_sum_invalid", message: "权重合计不是 100%" },
          { reason: "history_missing", message: "缺少历史数据" },
        ],
      }),
    });
    expect(screen.getByText("1 项阻断")).toBeInTheDocument();
    expect(screen.getByTestId("blocking-reasons")).toHaveTextContent(
      "缺少历史数据",
    );
    expect(screen.getByTestId("blocking-reasons")).not.toHaveTextContent(
      "权重合计不是 100%",
    );
  });

  it("shows ready when only the weight-sum block exists", () => {
    renderPanel({
      readiness: readiness({
        ready: false,
        weight_sum: 0.5,
        blocking_reasons: [
          { reason: "weight_sum_invalid", message: "权重合计不是 100%" },
        ],
      }),
    });
    expect(screen.getByText("数据就绪")).toBeInTheDocument();
    expect(screen.queryByTestId("blocking-reasons")).not.toBeInTheDocument();
  });

  it("creates sync tasks and renders per-asset rows with existed reuse", async () => {
    syncCollectionHistoryMock.mockResolvedValue({
      assets: [
        { asset_key: "CN|a", status: "created", task: task({ id: "wt_a" }) },
        { asset_key: "CN|b", status: "existed", task: task({ id: "wt_b" }) },
        {
          asset_key: "SYS|cash||CNY",
          status: "skipped",
          reason: "现金资产无需历史",
        },
      ],
      fx: [{ pair: "USDCNY", status: "created", task: task({ id: "wt_fx" }) }],
      blocked: [
        { asset_key: "CN|c", code: "asset_inactive", message: "资产已停用" },
      ],
    });
    renderPanel();
    const syncButton = screen.getByTestId("sync-collection");
    await waitFor(() => expect(syncButton).toBeEnabled());
    fireEvent.click(syncButton);
    expect(await screen.findByTestId("sync-task-panel")).toBeInTheDocument();
    expect(syncCollectionHistoryMock).toHaveBeenCalledWith("rc_1", undefined);
    expect(screen.getByText("已有同步任务，复用中")).toBeInTheDocument();
    expect(screen.getByText("跳过：现金资产无需历史")).toBeInTheDocument();
    expect(screen.getByText("汇率 USDCNY")).toBeInTheDocument();
    expect(screen.getByText("资产已停用")).toBeInTheDocument();
    expect(screen.getByText("无法同步")).toBeInTheDocument();
  });

  it("refreshes collection data when an individual task settles", async () => {
    syncCollectionHistoryMock.mockResolvedValue({
      assets: [
        { asset_key: "CN|a", status: "created", task: task({ id: "wt_a" }) },
      ],
      fx: [],
      blocked: [],
    });
    getTaskMock.mockResolvedValue(task({ id: "wt_a", status: "complete" }));
    const { client } = renderPanel();
    const invalidateSpy = vi.spyOn(client, "invalidateQueries");
    const syncButton = screen.getByTestId("sync-collection");
    await waitFor(() => expect(syncButton).toBeEnabled());
    fireEvent.click(syncButton);
    await screen.findByTestId("sync-task-panel");
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["research", "readiness", "rc_1"],
      });
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["research", "optimization-readiness", "rc_1"],
      });
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["research", "collection", "rc_1"],
      });
    });
    expect(await screen.findByText("完成")).toBeInTheDocument();
  });

  it("refreshes a successful asset while a sibling task is still running", async () => {
    syncCollectionHistoryMock.mockResolvedValue({
      assets: [
        { asset_key: "CN|a", status: "created", task: task({ id: "wt_a" }) },
        { asset_key: "CN|b", status: "created", task: task({ id: "wt_b" }) },
      ],
      fx: [],
      blocked: [],
    });
    getTaskMock.mockImplementation((id: string) =>
      Promise.resolve(
        task({ id, status: id === "wt_a" ? "complete" : "running" }),
      ),
    );
    const { client } = renderPanel();
    const invalidateSpy = vi.spyOn(client, "invalidateQueries");

    const syncButton = screen.getByTestId("sync-collection");
    await waitFor(() => expect(syncButton).toBeEnabled());
    fireEvent.click(syncButton);
    await screen.findByTestId("sync-task-panel");

    await waitFor(() => {
      expect(screen.getByText("完成")).toBeInTheDocument();
      expect(screen.getByText("同步中")).toBeInTheDocument();
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: ["research", "collection", "rc_1"],
      });
    });
  });

  it("shows failure suggestion and retries a single asset", async () => {
    const failedTask = task({
      id: "wt_a",
      status: "failed",
      error_code: "sidecar_unavailable",
      error_message: "sidecar unreachable",
    });
    syncCollectionHistoryMock.mockResolvedValue({
      assets: [{ asset_key: "CN|a", status: "created", task: failedTask }],
      fx: [],
      blocked: [],
    });
    getTaskMock.mockResolvedValue(failedTask);
    renderPanel();
    const syncButton = screen.getByTestId("sync-collection");
    await waitFor(() => expect(syncButton).toBeEnabled());
    fireEvent.click(syncButton);
    await screen.findByTestId("sync-task-panel");
    expect(await screen.findByText("sidecar unreachable")).toBeInTheDocument();
    expect(screen.getByText(/sidecar 已启动后重试/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() =>
      expect(syncCollectionHistoryMock).toHaveBeenLastCalledWith("rc_1", {
        asset_keys: ["CN|a"],
        force: true,
      }),
    );
  });
});
