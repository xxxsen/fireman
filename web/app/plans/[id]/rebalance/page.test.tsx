// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import RebalancePage from "./page";

const mockSearchParams = vi.hoisted(() => {
  let params = new URLSearchParams();
  return {
    set: (next: URLSearchParams) => {
      params = next;
    },
    get: () => params,
  };
});

const routerPush = vi.fn();

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useSearchParams: () => mockSearchParams.get(),
  useRouter: () => ({ push: routerPush }),
}));

const targetLineBase = {
  holding_id: "holding_1",
  asset_key: "CN|cn_exchange_fund|sh|510300",
  instrument_name: "测试基金",
  instrument_code: "T1",
  asset_class: "equity",
  region: "domestic",
  enabled: true,
  asset_class_weight: 1,
  region_weight: 1,
  weight_within_group: 1,
  portfolio_target_weight: 1,
  target_amount_minor: 100_000_00,
  current_amount_minor: 80_000_00,
  current_weight: 0.8,
  deviation_amount_minor: 20_000_00,
  deviation_weight: 0.2,
  structural_current_weight: 1,
  structural_gap_weight: 0.2,
  structural_gap_amount_minor: 20_000_00,
  structural_target_amount_minor: 100_000_00,
  plan_gap_weight: 0.2,
  plan_gap_amount_minor: 20_000_00,
  simulation_snapshot_id: "",
  sort_order: 0,
  action: "increase",
  suggested_trade_minor: 20_000_00,
  plan_scale_action: "increase",
  plan_scale_suggested_trade_minor: 20_000_00,
};

const getRebalance = vi.hoisted(() =>
  vi.fn(() =>
    Promise.resolve({
      mode: "full",
      summary: {
        actionable_count: 1,
        structural_actionable_count: 1,
        configured_total_minor: 100_000_00,
        holdings_total_minor: 80_000_00,
        scale_gap_minor: -20_000_00,
        plan_scale_actionable_count: 1,
      },
      weight_checks: { passed: true, checks: [] },
      lines: [targetLineBase],
    }),
  ),
);

const getActiveRebalanceExecution = vi.hoisted(() =>
  vi.fn((): Promise<unknown> => Promise.resolve(null)),
);
const createRebalanceExecution = vi.hoisted(() =>
  vi.fn(() =>
    Promise.resolve({
      execution: { id: "rbx_1" },
      lines: [],
      events: [],
      stats: { line_count: 0, done_line_count: 0, sold_total_minor: 0, bought_total_minor: 0 },
    }),
  ),
);

vi.mock("@/lib/api/holdings", () => ({
  getTargets: () =>
    Promise.resolve({
      total_assets_minor: 100_000_00,
      config_hash: "hash",
      weight_checks: { passed: true, checks: [] },
      asset_class_targets: [{ asset_class: "equity", weight: 1 }],
      region_targets: [{ asset_class: "equity", region: "domestic", weight_within_class: 1 }],
      holdings: [targetLineBase],
    }),
  getRebalance: (...args: unknown[]) => getRebalance(...args),
}));

vi.mock("@/lib/api/rebalance-executions", () => ({
  getActiveRebalanceExecution: (...args: unknown[]) => getActiveRebalanceExecution(...args),
  createRebalanceExecution: (...args: unknown[]) => createRebalanceExecution(...args),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RebalancePage />
    </QueryClientProvider>,
  );
}

describe("RebalancePage (调仓工作台)", () => {
  beforeEach(() => {
    mockSearchParams.set(new URLSearchParams());
    getActiveRebalanceExecution.mockResolvedValue(null);
    getRebalance.mockImplementation(() =>
      Promise.resolve({
        mode: "full",
        summary: {
          actionable_count: 1,
          structural_actionable_count: 1,
          configured_total_minor: 100_000_00,
          holdings_total_minor: 80_000_00,
          scale_gap_minor: -20_000_00,
          plan_scale_actionable_count: 1,
        },
        weight_checks: { passed: true, checks: [] },
        lines: [targetLineBase],
      }),
    );
  });

  it("offers exactly two workspace actions: asset refresh and rebalance execution", async () => {
    renderPage();

    expect(await screen.findByRole("heading", { name: "调仓工作台" })).toBeInTheDocument();
    expect(screen.getByTestId("asset-refresh-primary")).toHaveAttribute(
      "href",
      "/plans/plan_1/asset-refresh",
    );
    expect(screen.getByTestId("start-rebalance-execution")).toBeInTheDocument();
    // The draft (rebalance plan) entry is gone for good.
    expect(screen.queryByTestId("create-rebalance-plan")).not.toBeInTheDocument();
    expect(screen.queryByTestId("continue-rebalance-plan")).not.toBeInTheDocument();
    expect(screen.queryByText("创建调仓计划")).not.toBeInTheDocument();
  });

  it("shows error state (not open refresh) when active execution check fails", async () => {
    getActiveRebalanceExecution.mockReset();
    getActiveRebalanceExecution.mockRejectedValue(new Error("boom"));

    renderPage();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByTestId("asset-refresh-primary")).not.toBeInTheDocument();
    expect(screen.queryByTestId("start-rebalance-execution")).not.toBeInTheDocument();

    const callsBeforeRetry = getActiveRebalanceExecution.mock.calls.length;
    fireEvent.click(screen.getByTestId("error-state-retry"));
    expect(getActiveRebalanceExecution.mock.calls.length).toBeGreaterThan(callsBeforeRetry);
  });

  it("disables asset refresh and shows continue when execution is active", async () => {
    getActiveRebalanceExecution.mockResolvedValue({
      execution: { id: "rbx_active", cash_pool_minor: 50_000_00, status: "in_progress" },
      lines: [
        {
          asset_key: "CN|cn_exchange_fund|sh|510300",
          execution_status: "partial",
          remaining_delta_minor: 10_000_00,
        },
      ],
      events: [],
      stats: { line_count: 2, done_line_count: 1, sold_total_minor: 0, bought_total_minor: 0 },
    });

    renderPage();

    expect(await screen.findByTestId("execution-blocking-hint")).toBeInTheDocument();
    expect(screen.getByTestId("asset-refresh-primary-disabled")).toBeInTheDocument();
    expect(screen.getByTestId("continue-rebalance-execution")).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance/executions/rbx_active",
    );
  });

  it("opens the execution confirm dialog and navigates after creation", async () => {
    renderPage();

    fireEvent.click(await screen.findByTestId("start-rebalance-execution"));
    const confirmButton = await screen.findByRole("button", { name: "创建调仓执行" });

    fireEvent.click(confirmButton);
    expect(await screen.findByRole("heading", { name: "调仓工作台" })).toBeInTheDocument();
    expect(createRebalanceExecution).toHaveBeenCalled();
    expect(routerPush).toHaveBeenCalledWith("/plans/plan_1/rebalance/executions/rbx_1");
  });

  it("shows asset refreshed banner from query param", async () => {
    mockSearchParams.set(new URLSearchParams("asset_refreshed=1"));
    renderPage();
    expect(await screen.findByText("持仓校正已提交，调仓工作台已更新。")).toBeInTheDocument();
  });
});
