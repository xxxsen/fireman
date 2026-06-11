import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import RebalancePage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
}));

const targetLine = {
  holding_id: "holding_1",
  instrument_id: "instrument_1",
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
  simulation_snapshot_id: "",
  sort_order: 0,
};

vi.mock("@/lib/api/holdings", () => ({
  getTargets: () =>
    Promise.resolve({
      total_assets_minor: 100_000_00,
      config_hash: "hash",
      weight_checks: { passed: true, checks: [] },
      asset_class_targets: [{ asset_class: "equity", weight: 1 }],
      region_targets: [
        { asset_class: "equity", region: "domestic", weight_within_class: 0.6 },
        { asset_class: "equity", region: "foreign", weight_within_class: 0.4 },
      ],
      holdings: [targetLine],
    }),
  getRebalance: () =>
    Promise.resolve({
      mode: "full",
      summary: { actionable_count: 1 },
      weight_checks: { passed: true, checks: [] },
      lines: [{ ...targetLine, action: "increase", suggested_trade_minor: 20_000_00 }],
    }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () => Promise.resolve({ id: "plan_1", config_version: 1 }),
  createPortfolioSnapshot: vi.fn(),
}));

describe("RebalancePage", () => {
  it("renders unified gap summary with amounts and suggestions", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <RebalancePage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("配置缺口汇总")).toBeInTheDocument();
    expect(screen.getByText("目标金额")).toBeInTheDocument();
    expect(screen.getByText("当前金额")).toBeInTheDocument();
    expect(screen.getByText("建议")).toBeInTheDocument();
    expect(screen.getByText("测试基金")).toBeInTheDocument();
    expect(screen.getAllByText("¥100,000.00")).toHaveLength(1);
    expect(screen.getAllByText("¥80,000.00")).toHaveLength(1);
    expect(screen.getAllByText("增配").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/待投入 ¥20,000\.00/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByRole("link", { name: "更新持仓" })).toHaveAttribute(
      "href",
      "/plans/plan_1/holdings?highlight=holding_1",
    );
    expect(screen.queryByText("仅用新增资金")).not.toBeInTheDocument();
    expect(screen.queryByText("完整调仓")).not.toBeInTheDocument();
    const domesticRow = screen.getByText("国内").closest("tr");
    expect(domesticRow).not.toBeNull();
    const [domesticTargetWeight, domesticCurrentWeight] = within(
      domesticRow as HTMLElement,
    ).getAllByTestId("inline-tooltip-trigger");
    expect(domesticTargetWeight).toHaveTextContent("100%");
    expect(domesticTargetWeight).toHaveTextContent("(60%)");
    expect(domesticCurrentWeight).toHaveTextContent("80%");
    expect(domesticCurrentWeight).toHaveTextContent("(100%)");
    fireEvent.mouseEnter(domesticTargetWeight);
    expect(await screen.findByTestId("inline-tooltip-content")).toHaveTextContent(
      /占全组合的目标占比/,
    );
    expect(screen.getByTestId("inline-tooltip-content")).toHaveTextContent(
      /占同一大类内的目标配比/,
    );
    fireEvent.mouseLeave(domesticTargetWeight);
    fireEvent.mouseEnter(domesticCurrentWeight);
    expect(await screen.findByTestId("inline-tooltip-content")).toHaveTextContent(
      /占全组合的当前占比/,
    );
    expect(screen.getByTestId("inline-tooltip-content")).toHaveTextContent(
      /占同一大类内的当前配比/,
    );
  });
});
