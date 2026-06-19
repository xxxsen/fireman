// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import RebalanceExecutionWorkspacePage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1", executionId: "rbx_1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () =>
    Promise.resolve({
      id: "plan_1",
      name: "测试计划",
      config_version: 1,
    }),
}));

const getRebalanceExecutionMock = vi.hoisted(() => vi.fn());

const successDetail = {
  execution: {
    id: "rbx_1",
    status: "in_progress",
    created_at: Date.now(),
    cash_pool_minor: 100_000_00,
  },
  lines: [
    {
      id: "line_1",
      instrument_id: "ins_1",
      instrument_name: "测试基金",
      action_direction: "decrease",
      target_delta_minor: -100_000_00,
      executed_delta_minor: 0,
      remaining_delta_minor: -100_000_00,
      execution_status: "not_started",
    },
    {
      id: "line_2",
      instrument_id: "ins_2",
      instrument_name: "债券基金",
      action_direction: "increase",
      target_delta_minor: 50_000_00,
      executed_delta_minor: 0,
      remaining_delta_minor: 50_000_00,
      execution_status: "not_started",
    },
  ],
  events: [],
  stats: {
    line_count: 2,
    done_line_count: 0,
    sold_total_minor: 0,
    bought_total_minor: 0,
  },
};

vi.mock("@/lib/api/rebalance-executions", () => ({
  getRebalanceExecution: (...args: unknown[]) => getRebalanceExecutionMock(...args),
  sellRebalanceExecution: vi.fn(),
  buyRebalanceExecution: vi.fn(),
  skipRebalanceExecutionLine: vi.fn(),
  noteRebalanceExecution: vi.fn(),
  completeRebalanceExecution: vi.fn(),
  cancelRebalanceExecution: vi.fn(),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RebalanceExecutionWorkspacePage />
    </QueryClientProvider>,
  );
}

describe("RebalanceExecutionWorkspacePage", () => {
  beforeEach(() => {
    getRebalanceExecutionMock.mockReset();
    getRebalanceExecutionMock.mockResolvedValue(successDetail);
  });

  it("shows error state with retry when detail load fails", async () => {
    getRebalanceExecutionMock.mockRejectedValueOnce(new Error("boom"));
    renderPage();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByText("加载调仓执行工作区…")).not.toBeInTheDocument();
    expect(screen.getByTestId("error-state-retry")).toBeInTheDocument();
    expect(screen.getByTestId("error-state-back")).toBeInTheDocument();
  });

  it("shows cash pool and complete action", async () => {
    renderPage();

    expect(await screen.findByTestId("cash-pool-balance")).toHaveTextContent("¥100,000.00");
    expect(screen.getByTestId("complete-execution")).toBeInTheDocument();
    expect(screen.getByTestId("cancel-execution")).toBeInTheDocument();
  });

  it("shows quick trade actions and skip buttons", async () => {
    renderPage();

    expect(await screen.findByTestId("quick-sell")).toBeInTheDocument();
    expect(screen.getByTestId("quick-buy")).toBeInTheDocument();
    expect(screen.getByTestId("skip-line-line_1")).toBeInTheDocument();
    expect(screen.getByTestId("skip-line-line_2")).toBeInTheDocument();
  });
});
