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

vi.mock("@/hooks/useJobStatus", () => ({
  useJobStatus: () => ({ job: null, progress: 0, error: null }),
}));

vi.mock("@/components/charts/AllocationBarChart", () => ({
  AllocationBarChart: () => <div data-testid="allocation-chart" />,
}));
vi.mock("@/components/charts/RegionAllocationBarChart", () => ({
  RegionAllocationBarChart: () => <div data-testid="region-chart" />,
}));

const mockDashboard = vi.hoisted(() => ({
  data: {
    plan: { id: "plan_1", base_currency: "CNY" },
    parameters: { total_assets_minor: 450_000_00 },
    weight_checks: { passed: true, checks: [] },
    holdings_sum_minor: 500_000_00,
    holdings_gap_minor: 50_000_00,
    rebalance_summary: { actionable_count: 2 },
    allocation_bars: [
      { asset_class: "equity", target_weight: 0.6, current_weight: 0.5 },
    ],
    region_bars: [
      { region: "domestic", target_weight: 0.6, current_weight: 0.5 },
    ],
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

describe("OverviewPage §10 acceptance", () => {
  beforeEach(() => {
    mockOverviewSearchParams.set(new URLSearchParams());
  });

  it("E1: shows scale over label when holdings_gap is positive (§10.5 E1)", async () => {
    renderPage();

    expect(await screen.findByText("规模超出")).toBeInTheDocument();
    expect(screen.getByText("¥50,000.00")).toBeInTheDocument();
    const gapRow = screen.getByText("规模超出").closest("div");
    expect(gapRow?.querySelector("dd")?.className).toMatch(/amber/);
  });

  it("E3: B1-style dashboard shows zero actionable and empty structural deviations (§10.5 E3)", async () => {
    mockDashboard.data = {
      ...mockDashboard.data,
      parameters: { total_assets_minor: 450_000_00 },
      holdings_sum_minor: 400_000_00,
      holdings_gap_minor: -50_000_00,
      rebalance_summary: { actionable_count: 0 },
      top_deviations: [],
    };
    renderPage();

    expect(await screen.findByText("需调仓标的")).toBeInTheDocument();
    const actionableRow = screen.getByText("需调仓标的").closest("div");
    expect(actionableRow?.querySelector("dd")?.textContent?.trim()).toMatch(/^0/);
    expect(screen.getByText("规模缺口")).toBeInTheDocument();
    expect(screen.getByText("当前持仓与目标配置一致。")).toBeInTheDocument();
  });

  it("shows portfolio status, both allocation charts and rebalance CTA when actionable", async () => {
    mockDashboard.data = {
      ...mockDashboard.data,
      holdings_sum_minor: 490_000_00,
      holdings_gap_minor: -10_000_00,
      rebalance_summary: { actionable_count: 2 },
      top_deviations: [
        {
          instrument_name: "测试基金",
          instrument_code: "T1",
          deviation_weight: 0.1,
          deviation_amount_minor: 10_000_00,
        },
      ],
    };
    renderPage();

    expect(await screen.findByText("需调仓标的")).toBeInTheDocument();
    expect(screen.getByTestId("allocation-chart")).toBeInTheDocument();
    expect(screen.getByTestId("region-chart")).toBeInTheDocument();
    expect(screen.getByText("规模缺口")).toBeInTheDocument();
    expect(screen.getByText("¥10,000.00")).toBeInTheDocument();
    expect(
      screen.getAllByRole("link", { name: "查看调仓建议" })[0],
    ).toHaveAttribute("href", "/plans/plan_1/rebalance");
  });

  it("links asset refresh with reason=scale when scale gap is significant", async () => {
    renderPage();
    const link = await screen.findByRole("link", { name: "更新账户资产" });
    expect(link).toHaveAttribute("href", "/plans/plan_1/asset-refresh?reason=scale");
  });
});
