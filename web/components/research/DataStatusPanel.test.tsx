import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchReadiness } from "@/lib/api/research";
import type { WorkerTask } from "@/lib/api/market-assets";
import { DataStatusPanel, syncFailureSuggestion } from "./DataStatusPanel";

const syncCollectionHistoryMock = vi.hoisted(() => vi.fn());
const getTaskMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  syncCollectionHistory: (...args: unknown[]) => syncCollectionHistoryMock(...args),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  getTask: (...args: unknown[]) => getTaskMock(...args),
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

function readiness(overrides: Partial<ResearchReadiness> = {}): ResearchReadiness {
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

function renderPanel(props: Partial<Parameters<typeof DataStatusPanel>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onReadinessRefresh = vi.fn();
  const utils = render(
    <QueryClientProvider client={client}>
      <DataStatusPanel
        collectionId="rc_1"
        readiness={readiness()}
        readinessLoading={false}
        onReadinessRefresh={onReadinessRefresh}
        {...props}
      />
    </QueryClientProvider>,
  );
  return { ...utils, onReadinessRefresh };
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
    getTaskMock.mockResolvedValue(task({ status: "complete" }));
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
    expect(screen.getByTestId("blocking-reasons")).toHaveTextContent("缺少历史数据");
    expect(screen.getByTestId("warnings")).toHaveTextContent("历史长度不足 3 年");
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
    expect(screen.getByTestId("blocking-reasons")).toHaveTextContent("缺少历史数据");
    expect(screen.getByTestId("blocking-reasons")).not.toHaveTextContent("权重合计不是 100%");
  });

  it("shows ready when only the weight-sum block exists", () => {
    renderPanel({
      readiness: readiness({
        ready: false,
        weight_sum: 0.5,
        blocking_reasons: [{ reason: "weight_sum_invalid", message: "权重合计不是 100%" }],
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
        { asset_key: "SYS|cash||CNY", status: "skipped", reason: "现金资产无需历史" },
      ],
      fx: [{ pair: "USDCNY", status: "created", task: task({ id: "wt_fx" }) }],
      blocked: [{ asset_key: "CN|c", code: "asset_inactive", message: "资产已停用" }],
    });
    renderPanel();
    fireEvent.click(screen.getByTestId("sync-collection"));
    expect(await screen.findByTestId("sync-task-panel")).toBeInTheDocument();
    expect(syncCollectionHistoryMock).toHaveBeenCalledWith("rc_1", undefined);
    expect(screen.getByText("已有同步任务，复用中")).toBeInTheDocument();
    expect(screen.getByText("跳过：现金资产无需历史")).toBeInTheDocument();
    expect(screen.getByText("汇率 USDCNY")).toBeInTheDocument();
    expect(screen.getByText("资产已停用")).toBeInTheDocument();
    expect(screen.getByText("无法同步")).toBeInTheDocument();
  });

  it("refreshes readiness after all active tasks settle", async () => {
    syncCollectionHistoryMock.mockResolvedValue({
      assets: [{ asset_key: "CN|a", status: "created", task: task({ id: "wt_a" }) }],
      fx: [],
      blocked: [],
    });
    getTaskMock.mockResolvedValue(task({ id: "wt_a", status: "complete" }));
    const { onReadinessRefresh } = renderPanel();
    fireEvent.click(screen.getByTestId("sync-collection"));
    await screen.findByTestId("sync-task-panel");
    await waitFor(() => expect(onReadinessRefresh).toHaveBeenCalled());
    expect(await screen.findByText("完成")).toBeInTheDocument();
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
    fireEvent.click(screen.getByTestId("sync-collection"));
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
