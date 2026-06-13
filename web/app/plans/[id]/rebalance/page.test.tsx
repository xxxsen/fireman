// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
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

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useSearchParams: () => mockSearchParams.get(),
}));

const targetLineBase = {
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

describe("RebalancePage (持仓预览)", () => {
  beforeEach(() => {
    mockSearchParams.set(new URLSearchParams());
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

  it("shows title, single primary action, and asset link", async () => {
    renderPage();

    expect(await screen.findByRole("heading", { name: "持仓预览" })).toBeInTheDocument();
    const primaryButtons = screen.getAllByTestId("asset-refresh-primary");
    expect(primaryButtons).toHaveLength(1);
    expect(primaryButtons[0]).toHaveAttribute("href", "/plans/plan_1/asset-refresh");

    const assetLink = screen.getByRole("link", { name: "测试基金" });
    expect(assetLink).toHaveAttribute("href", "/assets/instrument_1");

    expect(screen.queryByText("动作筛选")).not.toBeInTheDocument();
    expect(screen.queryByText("导出 CSV")).not.toBeInTheDocument();
    expect(screen.queryByText("按计划规模对齐（高级）")).not.toBeInTheDocument();
    expect(screen.queryByText("记录调仓后快照")).not.toBeInTheDocument();
    expect(screen.queryByText("在持仓中编辑")).not.toBeInTheDocument();
  });

  it("hides zero gap amounts on holding rows", async () => {
    getRebalance.mockResolvedValue({
      mode: "full",
      summary: {
        actionable_count: 0,
        structural_actionable_count: 0,
        configured_total_minor: 100_000_00,
        holdings_total_minor: 100_000_00,
        scale_gap_minor: 0,
        plan_scale_actionable_count: 0,
      },
      weight_checks: { passed: true, checks: [] },
      lines: [{ ...targetLineBase, structural_gap_amount_minor: 0, deviation_amount_minor: 0 }],
    });

    renderPage();
    expect(await screen.findByText("测试基金")).toBeInTheDocument();
    expect(screen.queryByText(/待投入 ¥0\.00/)).not.toBeInTheDocument();
    expect(screen.queryByText(/待减配 ¥0\.00/)).not.toBeInTheDocument();
  });

  it("shows asset refreshed banner from query param", async () => {
    mockSearchParams.set(new URLSearchParams("asset_refreshed=1"));
    renderPage();
    expect(await screen.findByText("资产变更已提交，持仓预览已更新。")).toBeInTheDocument();
  });
});
