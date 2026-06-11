import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import OverviewPage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useSearchParams: () => new URLSearchParams(),
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
vi.mock("@/lib/api/dashboard", () => ({
  getDashboard: () =>
    Promise.resolve({
      plan: { id: "plan_1", base_currency: "CNY" },
      parameters: { total_assets_minor: 100_000_00 },
      weight_checks: { passed: true, checks: [] },
      holdings_sum_minor: 90_000_00,
      holdings_gap_minor: 10_000_00,
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
    }),
}));

describe("OverviewPage", () => {
  it("shows portfolio status, both allocation charts and rebalance CTA", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <OverviewPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("需调仓标的")).toBeInTheDocument();
    expect(screen.getByTestId("allocation-chart")).toBeInTheDocument();
    expect(screen.getByTestId("region-chart")).toBeInTheDocument();
    expect(screen.getByText("还差 ¥10,000.00")).toBeInTheDocument();
    expect(
      screen.getAllByRole("link", { name: "查看调仓建议" })[0],
    ).toHaveAttribute("href", "/plans/plan_1/rebalance");
    expect(screen.queryByText("FIRE 模拟（可选）")).not.toBeInTheDocument();
  });
});
