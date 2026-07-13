import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import OverviewPage from "./page";

const mockOverviewSearchParams = vi.hoisted(() => {
  let params = new URLSearchParams();
  return {
    set: (next: URLSearchParams) => {
      params = next;
    },
    get: () => params,
  };
});

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useSearchParams: () => mockOverviewSearchParams.get(),
}));

const mockJobState = vi.hoisted(() => ({
  value: {
    task: null as { status: string } | null,
    progress: 0,
    error: null as string | null,
  },
}));

vi.mock("@/hooks/useTaskStatus", () => ({
  useTaskStatus: () => mockJobState.value,
}));
vi.mock("@/hooks/useActiveTaskRestore", () => ({
  useActiveTaskRestore: () => ({
    task: null,
    taskId: null,
    restoring: false,
    restoreError: null,
    retryRestore: vi.fn(),
  }),
}));

vi.mock("@/components/charts/AllocationBarChart", () => ({
  AllocationBarChart: () => <div data-testid="allocation-chart" />,
}));
vi.mock("@/components/charts/RegionAllocationBarChart", () => ({
  RegionAllocationBarChart: () => <div data-testid="region-chart" />,
}));
vi.mock("@/components/charts/AssetClassRegionGroups", () => ({
  AssetClassRegionGroups: () => <div data-testid="asset-class-region-groups" />,
}));

const mockDashboard = vi.hoisted(() => ({
  data: {
    plan: { id: "plan_1", base_currency: "CNY" },
    parameters: { total_assets_minor: 500_000_00 },
    weight_checks: { passed: true, checks: [] },
    holdings_sum_minor: 400_000_00,
    invested_minor: 320_000_00,
    invested_ratio: 0.64,
    holdings_gap_minor: 50_000_00,
    rebalance_summary: { actionable_count: 2 },
    allocation_bars: [
      { asset_class: "equity", target_weight: 0.6, current_weight: 0.5 },
    ],
    region_bars: [
      { region: "domestic", target_weight: 0.6, current_weight: 0.5 },
    ],
    asset_class_region_groups: [],
    top_deviations: [
      {
        instrument_name: "测试基金",
        instrument_code: "T1",
        deviation_weight: 0.1,
        deviation_amount_minor: 10_000_00,
      },
    ],
    data_warnings: [],
    latest_simulation: null,
  },
}));

vi.mock("@/lib/api/dashboard", () => ({
  getDashboard: () => Promise.resolve(mockDashboard.data),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <OverviewPage />
    </QueryClientProvider>,
  );
}

describe("OverviewPage", () => {
  beforeEach(() => {
    mockOverviewSearchParams.set(new URLSearchParams());
    mockJobState.value = { task: null, progress: 0, error: null };
    mockDashboard.data = {
      ...mockDashboard.data,
      active_rebalance_execution: undefined,
    };
  });

  it("shows invested metrics instead of scale gap", async () => {
    renderPage();

    expect(await screen.findByText("已投资金")).toBeInTheDocument();
    expect(screen.getByText("已投资金占比")).toBeInTheDocument();
    expect(screen.getByText("64%")).toBeInTheDocument();
    expect(screen.queryByText("规模一致")).not.toBeInTheDocument();
    expect(screen.queryByText("规模超出")).not.toBeInTheDocument();
  });

  it("links actionable count to rebalance preview when no active execution", async () => {
    renderPage();

    const link = await screen.findByTestId("actionable-rebalance-link");
    expect(link).toHaveAttribute("href", "/plans/plan_1/rebalance");
  });

  it("links deviations to active execution workspace when in progress", async () => {
    mockDashboard.data = {
      ...mockDashboard.data,
      active_rebalance_execution: {
        id: "rbx_1",
        status: "in_progress",
        cash_pool_minor: 0,
        done_line_count: 1,
        line_count: 3,
      },
    };
    renderPage();

    const links = await screen.findAllByTestId("deviation-amount-link");
    expect(links[0]).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance/executions/rbx_1",
    );
  });

  it("does not show bottom action button row", async () => {
    renderPage();
    await screen.findByText("已投资金");
    expect(
      screen.queryByRole("link", { name: "刷新" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "持仓预览" }),
    ).not.toBeInTheDocument();
  });

  it("shows the plan improver for a current completed simulation", async () => {
    mockDashboard.data = {
      ...mockDashboard.data,
      latest_simulation: {
        id: "sim_1",
        task_status: "complete",
        result_stale: false,
        summary_json: {
          success_probability: 0.72,
          success_wilson_low: 0.69,
          success_wilson_high: 0.75,
        },
      },
    };
    renderPage();

    expect(await screen.findByText("FIRE 状态")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "改善计划" })).toHaveAttribute(
      "href",
      "/plans/plan_1/improvement?simulation_run_id=sim_1",
    );
    expect(screen.getByRole("link", { name: "达标前沿" })).toHaveAttribute(
      "href",
      "/plans/plan_1/frontier?simulation_run_id=sim_1",
    );
    expect(screen.getByText(/95% 区间 69% - 75%/)).toBeInTheDocument();
  });

  it("shows a running banner while the tracked simulation job is in progress", async () => {
    mockOverviewSearchParams.set(new URLSearchParams("task_id=job_1"));
    mockJobState.value = {
      task: { status: "running" },
      progress: 0.4,
      error: null,
    };
    renderPage();

    expect(
      await screen.findByText(/FIRE 模拟正在后台运行：40%/),
    ).toBeInTheDocument();
  });

  it("shows a failure banner with the error when the tracked simulation job failed", async () => {
    mockOverviewSearchParams.set(new URLSearchParams("task_id=job_1"));
    mockJobState.value = {
      task: { status: "failed" },
      progress: 0,
      error: "引擎崩溃",
    };
    renderPage();

    expect(
      await screen.findByText(/FIRE 模拟运行失败：引擎崩溃/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "前往计划设置重试" }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/正在后台运行/)).not.toBeInTheDocument();
  });

  it("stays silent when the tracked simulation job was canceled", async () => {
    mockOverviewSearchParams.set(new URLSearchParams("task_id=job_1"));
    mockJobState.value = {
      task: { status: "canceled" },
      progress: 0,
      error: null,
    };
    renderPage();

    await screen.findByText("已投资金");
    expect(screen.queryByText(/正在后台运行/)).not.toBeInTheDocument();
    expect(screen.queryByText(/FIRE 模拟运行失败/)).not.toBeInTheDocument();
  });
});
