// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import type {
  SimulationReadiness,
  SyncMissingHistoryResult,
} from "@/lib/api/simulations";
import {
  buildSyncResultMessage,
  readinessPollInterval,
  SimulationReadinessPanel,
} from "./SimulationReadinessPanel";

const getSimulationReadiness = vi.fn();
const syncMissingAssetHistory = vi.fn();

vi.mock("@/lib/api/simulations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/simulations")>()),
  getSimulationReadiness: (...args: unknown[]) => getSimulationReadiness(...args),
  syncMissingAssetHistory: (...args: unknown[]) => syncMissingAssetHistory(...args),
}));

function makeReadiness(overrides: Partial<SimulationReadiness> = {}): SimulationReadiness {
  return {
    ready: false,
    blocking_assets: [
      {
        holding_id: "hold_1",
        asset_key: "CN|cn_exchange_fund|sz|150015",
        symbol: "150015",
        name: "银河银富货币B",
        reason: "history_missing",
      },
    ],
    active_tasks: [],
    ...overrides,
  };
}

function emptySyncResult(
  overrides: Partial<SyncMissingHistoryResult> = {},
): SyncMissingHistoryResult {
  return { created: [], existing: [], ready: [], blocked: [], ...overrides };
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <SimulationReadinessPanel planId="plan_1" />
    </QueryClientProvider>,
  );
  return client;
}

describe("SimulationReadinessPanel", () => {
  beforeEach(() => {
    getSimulationReadiness.mockReset();
    syncMissingAssetHistory.mockReset();
  });

  it("shows the generic blocked title instead of the old missing-history one", async () => {
    getSimulationReadiness.mockResolvedValue(makeReadiness());
    renderPanel();
    expect(await screen.findByText("以下持仓暂时无法创建模拟：")).toBeInTheDocument();
    expect(screen.queryByText(/还没有可用的历史数据/)).not.toBeInTheDocument();
  });

  it("labels history_missing without implying a broken sync", async () => {
    getSimulationReadiness.mockResolvedValue(makeReadiness());
    renderPanel();
    expect(await screen.findByText("未同步历史数据")).toBeInTheDocument();
  });

  it("labels asset_identity_conflict as a wrong identity, not missing history", async () => {
    getSimulationReadiness.mockResolvedValue(
      makeReadiness({
        blocking_assets: [
          {
            holding_id: "hold_1",
            asset_key: "CN|cn_exchange_fund|sz|150015",
            symbol: "150015",
            name: "银河银富货币B",
            reason: "asset_identity_conflict",
            message: "该代码存在多个资产身份，请切换为「公募基金」身份",
            candidate_asset_keys: ["CN|cn_mutual_fund||150015"],
          },
        ],
      }),
    );
    renderPanel();
    expect(
      await screen.findByText("资产身份可能选错，当前历史不可用于模拟"),
    ).toBeInTheDocument();
    expect(screen.queryByText("未同步历史数据")).not.toBeInTheDocument();
    expect(
      screen.getByText("该代码存在多个资产身份，请切换为「公募基金」身份"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("readiness-go-asset-refresh")).toHaveAttribute(
      "href",
      "/plans/plan_1/asset-refresh",
    );
  });

  it("labels provider_data_anomaly and links to the asset detail page", async () => {
    getSimulationReadiness.mockResolvedValue(
      makeReadiness({
        blocking_assets: [
          {
            holding_id: "hold_1",
            asset_key: "CN|cn_exchange_fund|sz|150015",
            symbol: "150015",
            name: "银河银富货币B",
            reason: "provider_data_anomaly",
          },
        ],
      }),
    );
    renderPanel();
    expect(
      await screen.findByText("历史已同步，但数据质量异常，暂不可模拟"),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看资产详情" })).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent("CN|cn_exchange_fund|sz|150015")}`,
    );
  });

  it("labels simulation_insufficient_history as synced but not simulatable", async () => {
    getSimulationReadiness.mockResolvedValue(
      makeReadiness({
        blocking_assets: [
          {
            holding_id: "hold_1",
            asset_key: "CN|cn_exchange_fund|sh|512999",
            symbol: "512999",
            name: "短历史ETF",
            reason: "simulation_insufficient_history",
          },
        ],
      }),
    );
    renderPanel();
    expect(
      await screen.findByText("历史已同步，但完整年度不足，暂不可模拟"),
    ).toBeInTheDocument();
  });

  it("shows the blocked outcome after a sync that creates nothing", async () => {
    getSimulationReadiness.mockResolvedValue(
      makeReadiness({
        blocking_assets: [
          {
            holding_id: "hold_1",
            asset_key: "CN|cn_exchange_fund|sz|150015",
            symbol: "150015",
            name: "银河银富货币B",
            reason: "asset_identity_conflict",
          },
        ],
      }),
    );
    syncMissingAssetHistory.mockResolvedValue(
      emptySyncResult({
        blocked: [
          {
            asset_key: "CN|cn_exchange_fund|sz|150015",
            reason: "asset_identity_conflict",
            message: "请切换资产身份",
          },
        ],
      }),
    );
    renderPanel();
    fireEvent.click(await screen.findByTestId("sync-missing-history-button"));
    await waitFor(() =>
      expect(screen.getByTestId("readiness-sync-message")).toHaveTextContent(
        "没有可创建的同步任务；部分资产历史已同步但不可用于模拟，请按提示处理。",
      ),
    );
    expect(screen.queryByText(/正在重新检查/)).not.toBeInTheDocument();
    expect(screen.queryByText(/同步任务进行中/)).not.toBeInTheDocument();
  });

  it("reports created and blocked together without pretending everything syncs", async () => {
    const res = emptySyncResult({
      created: [{ asset_key: "CN|cn_exchange_fund|sh|510300" }],
      blocked: [
        { asset_key: "CN|cn_exchange_fund|sz|150015", reason: "provider_data_anomaly" },
      ],
    });
    expect(buildSyncResultMessage(res)).toBe(
      "已创建 1 个同步任务，1 个资产历史已同步但不可用于模拟，请按提示处理",
    );
  });
});

describe("readinessPollInterval", () => {
  const base = makeReadiness();

  it("polls only while history sync tasks are active", () => {
    expect(
      readinessPollInterval({
        ...base,
        active_tasks: [{ id: "task_1", status: "running" } as never],
      }),
    ).toBeGreaterThan(0);
  });

  it("does not poll for terminal blocked reasons without active tasks", () => {
    expect(
      readinessPollInterval(
        makeReadiness({
          blocking_assets: [
            {
              holding_id: "hold_1",
              asset_key: "CN|cn_exchange_fund|sz|150015",
              symbol: "150015",
              name: "银河银富货币B",
              reason: "asset_identity_conflict",
            },
          ],
        }),
      ),
    ).toBe(false);
  });

  it("does not poll when ready or when data is absent", () => {
    expect(readinessPollInterval({ ...base, ready: true, blocking_assets: [] })).toBe(false);
    expect(readinessPollInterval(undefined)).toBe(false);
  });
});
