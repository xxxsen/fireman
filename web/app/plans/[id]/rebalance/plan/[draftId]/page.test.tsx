// @vitest-environment jsdom
import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import RebalancePlanPage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1", draftId: "rbd_1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () =>
    Promise.resolve({ id: "plan_1", name: "测试", config_version: 1, base_currency: "CNY" }),
}));

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: () =>
    Promise.resolve({
      holdings: [
        {
          id: "cash1",
          plan_id: "plan_1",
          instrument_id: "system_cash_cny",
          enabled: true,
          asset_class: "cash",
          region: "domestic",
          weight_within_group: 1,
          current_amount_minor: 10_000_00,
          simulation_snapshot_id: "snap",
          sort_order: 0,
          instrument_name: "CNY 现金",
        },
      ],
    }),
}));

const { getRebalanceDraft } = vi.hoisted(() => ({
  getRebalanceDraft: vi.fn(() =>
    Promise.resolve({
      draft: {
        id: "rbd_1",
        plan_id: "plan_1",
        status: "draft",
        config_version: 1,
        baseline_holdings_total_minor: 300_000_00,
        created_at: Date.now(),
        updated_at: Date.now(),
      },
      lines: [
        {
          id: "l1",
          draft_id: "rbd_1",
          holding_id: "h1",
          instrument_id: "i1",
          instrument_name: "A",
          instrument_code: "A",
          baseline_current_minor: 150_000_00,
          planned_current_minor: 100_000_00,
          frozen_target_minor: 120_000_00,
          frozen_gap_minor: -30_000_00,
          frozen_gap_weight: -0.1,
          frozen_action: "decrease",
          frozen_suggested_trade_minor: -30_000_00,
          recommended_package_delta_minor: -300_000_00,
          last_saved_at: null,
        },
        {
          id: "l2",
          draft_id: "rbd_1",
          holding_id: "h2",
          instrument_id: "i2",
          instrument_name: "B",
          instrument_code: "B",
          baseline_current_minor: 50_000_00,
          planned_current_minor: 50_000_00,
          frozen_target_minor: 60_000_00,
          frozen_gap_minor: 10_000_00,
          frozen_gap_weight: 0.02,
          frozen_action: "hold",
          frozen_suggested_trade_minor: 0,
          recommended_package_delta_minor: 100_000_00,
        },
      ],
      events: [],
      fund_pool: { released_minor: 50_000_00, used_minor: 0, net_minor: 50_000_00 },
    }),
  ),
}));

vi.mock("@/lib/api/rebalance-drafts", () => ({
  getRebalanceDraft: (...args: unknown[]) => getRebalanceDraft(...args),
  patchRebalanceDraftLines: vi.fn(),
  undoRebalanceDraft: vi.fn(),
  commitRebalanceDraft: vi.fn(),
  cancelRebalanceDraft: vi.fn(),
}));

describe("RebalancePlanPage", () => {
  beforeEach(() => {
    getRebalanceDraft.mockClear();
  });

  it("shows reference package bar and package delta column", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <RebalancePlanPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/参考调仓方案/)).toBeInTheDocument();
    expect(screen.getByText(/A −30w/)).toBeInTheDocument();
    expect(screen.getByText(/B \+10w/)).toBeInTheDocument();
    const table = within(screen.getByTestId("rebalance-line-table"));
    expect(table.getByText("方案变动")).toBeInTheDocument();
    expect(table.getAllByText("应用推荐金额")).toHaveLength(2);
    expect(screen.queryByText("全部应用")).not.toBeInTheDocument();
    expect(screen.getByText(/行内「不动」表示未超调仓阈值/)).toBeInTheDocument();
  });

  it("preview lists cash row when sweep to cash is selected", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <RebalancePlanPage />
      </QueryClientProvider>,
    );
    await screen.findByText(/参考调仓方案/);
    fireEvent.click(screen.getByRole("button", { name: "完成并更新持仓" }));
    expect(await screen.findByRole("heading", { name: "预览最终持仓" })).toBeInTheDocument();
    const cashRow = screen.getByText("CNY 现金").closest("li");
    expect(cashRow).toHaveTextContent("¥10,000.00");
    expect(cashRow).toHaveTextContent("¥60,000.00");
  });

  it("links to holdings preview instead of legacy rebalance workspace copy", async () => {
    getRebalanceDraft.mockResolvedValueOnce({
      draft: {
        id: "rbd_1",
        plan_id: "plan_1",
        status: "committed",
        config_version: 1,
        baseline_holdings_total_minor: 300_000_00,
        created_at: Date.now(),
        updated_at: Date.now(),
      },
      lines: [],
      events: [],
      fund_pool: { released_minor: 0, used_minor: 0, net_minor: 0 },
    });

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <RebalancePlanPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText(/此调仓计划已提交/)).toBeInTheDocument();
    expect(screen.queryByText("返回调仓工作台")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "返回持仓预览" })).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance",
    );
  });
});
