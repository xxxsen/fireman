// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import RebalancePage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

const { targetLineBase, mockState, syncPlanTotalAssets } = vi.hoisted(() => {
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
    structural_gap_weight: 0,
    structural_gap_amount_minor: 0,
    structural_target_amount_minor: 80_000_00,
    plan_gap_weight: 0.2,
    plan_gap_amount_minor: 20_000_00,
    simulation_snapshot_id: "",
    sort_order: 0,
  };
  const mockState = {
    rebalanceSummary: {
      actionable_count: 0,
      structural_actionable_count: 0,
      configured_total_minor: 100_000_00,
      holdings_total_minor: 80_000_00,
      scale_gap_minor: -20_000_00,
      plan_scale_actionable_count: 1,
    },
    rebalanceLines: [
      {
        ...targetLineBase,
        action: "hold",
        suggested_trade_minor: 0,
        plan_scale_action: "increase",
        plan_scale_suggested_trade_minor: 20_000_00,
      },
    ],
    targetsHoldings: [targetLineBase],
    assetClassTargets: [{ asset_class: "equity", weight: 1 }],
    regionTargets: [
      { asset_class: "equity", region: "domestic", weight_within_class: 0.6 },
      { asset_class: "equity", region: "foreign", weight_within_class: 0.4 },
    ],
  };
  return { targetLineBase, mockState, syncPlanTotalAssets: vi.fn() };
});

vi.mock("@/lib/api/holdings", () => ({
  getTargets: () =>
    Promise.resolve({
      total_assets_minor: 100_000_00,
      config_hash: "hash",
      weight_checks: { passed: true, checks: [] },
      asset_class_targets: mockState.assetClassTargets,
      region_targets: mockState.regionTargets,
      holdings: mockState.targetsHoldings,
    }),
  getRebalance: () =>
    Promise.resolve({
      mode: "full",
      summary: mockState.rebalanceSummary,
      weight_checks: { passed: true, checks: [] },
      lines: mockState.rebalanceLines,
    }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () => Promise.resolve({ id: "plan_1", config_version: 1 }),
  getParameters: () =>
    Promise.resolve({
      parameters: { total_assets_minor: 100_000_00, rebalance_threshold: 0.03 },
      cash_flows: [],
    }),
  createPortfolioSnapshot: vi.fn(),
  syncPlanTotalAssets: (...args: unknown[]) => syncPlanTotalAssets(...args),
}));

vi.mock("@/lib/api/rebalance-drafts", () => ({
  getActiveRebalanceDraft: () => Promise.resolve(null),
  createRebalanceDraft: vi.fn(),
}));

function renderPage() {
  sessionStorage.clear();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RebalancePage />
    </QueryClientProvider>,
  );
}

function setB1Scenario() {
  mockState.rebalanceSummary = {
    actionable_count: 0,
    structural_actionable_count: 0,
    configured_total_minor: 450_000_00,
    holdings_total_minor: 400_000_00,
    scale_gap_minor: -50_000_00,
    plan_scale_actionable_count: 1,
  };
  mockState.rebalanceLines = [
    {
      ...targetLineBase,
      instrument_name: "长城短债",
      instrument_code: "CC",
      target_amount_minor: 450_000_00,
      current_amount_minor: 400_000_00,
      structural_target_amount_minor: 400_000_00,
      plan_gap_amount_minor: 50_000_00,
      action: "hold",
      suggested_trade_minor: 0,
      plan_scale_action: "increase",
      plan_scale_suggested_trade_minor: 50_000_00,
    },
  ];
  mockState.targetsHoldings = [
    {
      ...targetLineBase,
      instrument_name: "长城短债",
      instrument_code: "CC",
      target_amount_minor: 450_000_00,
      current_amount_minor: 400_000_00,
      structural_target_amount_minor: 400_000_00,
      plan_gap_amount_minor: 50_000_00,
    },
  ];
  mockState.assetClassTargets = [{ asset_class: "bond", weight: 1 }];
  mockState.regionTargets = [
    { asset_class: "bond", region: "domestic", weight_within_class: 1 },
    { asset_class: "bond", region: "foreign", weight_within_class: 0 },
  ];
}

function setA2Scenario() {
  const equityLine = {
    holding_id: "holding_eq",
    instrument_id: "instrument_eq",
    instrument_name: "权益基金",
    instrument_code: "EQ",
    asset_class: "equity",
    region: "domestic",
    enabled: true,
    asset_class_weight: 0.6,
    region_weight: 1,
    weight_within_group: 1,
    portfolio_target_weight: 0.6,
    target_amount_minor: 300_000_00,
    current_amount_minor: 350_000_00,
    current_weight: 0.7,
    deviation_amount_minor: -50_000_00,
    deviation_weight: -0.1,
    structural_current_weight: 0.7,
    structural_gap_weight: -0.1,
    structural_gap_amount_minor: -50_000_00,
    structural_target_amount_minor: 300_000_00,
    plan_gap_weight: -0.1,
    plan_gap_amount_minor: -50_000_00,
    simulation_snapshot_id: "",
    sort_order: 0,
    action: "decrease",
    suggested_trade_minor: -50_000_00,
    plan_scale_action: "decrease",
    plan_scale_suggested_trade_minor: -50_000_00,
  };
  const bondLine = {
    ...equityLine,
    holding_id: "holding_bd",
    instrument_id: "instrument_bd",
    instrument_name: "债券基金",
    instrument_code: "BD",
    asset_class: "bond",
    asset_class_weight: 0.4,
    portfolio_target_weight: 0.4,
    target_amount_minor: 200_000_00,
    current_amount_minor: 150_000_00,
    current_weight: 0.3,
    deviation_amount_minor: 50_000_00,
    deviation_weight: 0.1,
    structural_current_weight: 0.3,
    structural_gap_weight: 0.1,
    structural_gap_amount_minor: 50_000_00,
    structural_target_amount_minor: 200_000_00,
    plan_gap_weight: 0.1,
    plan_gap_amount_minor: 50_000_00,
    sort_order: 1,
    action: "increase",
    suggested_trade_minor: 50_000_00,
    plan_scale_action: "increase",
    plan_scale_suggested_trade_minor: 50_000_00,
  };
  mockState.rebalanceSummary = {
    actionable_count: 2,
    structural_actionable_count: 2,
    configured_total_minor: 450_000_00,
    holdings_total_minor: 500_000_00,
    scale_gap_minor: 50_000_00,
    plan_scale_actionable_count: 2,
  };
  mockState.rebalanceLines = [equityLine, bondLine];
  mockState.targetsHoldings = [equityLine, bondLine];
  mockState.assetClassTargets = [
    { asset_class: "equity", weight: 0.6 },
    { asset_class: "bond", weight: 0.4 },
  ];
  mockState.regionTargets = [
    { asset_class: "equity", region: "domestic", weight_within_class: 1 },
    { asset_class: "bond", region: "domestic", weight_within_class: 1 },
  ];
}

function setB3Scenario() {
  const equityLine = {
    ...targetLineBase,
    holding_id: "holding_eq",
    instrument_name: "权益基金",
    instrument_code: "EQ",
    asset_class: "equity",
    asset_class_weight: 0.6,
    portfolio_target_weight: 0.6,
    target_amount_minor: 270_000_00,
    current_amount_minor: 240_000_00,
    structural_current_weight: 0.6,
    structural_gap_weight: 0,
    structural_gap_amount_minor: 0,
    structural_target_amount_minor: 240_000_00,
    plan_gap_amount_minor: 30_000_00,
    action: "hold",
    suggested_trade_minor: 0,
    plan_scale_action: "increase",
    plan_scale_suggested_trade_minor: 30_000_00,
  };
  const bondLine = {
    ...equityLine,
    holding_id: "holding_bd",
    instrument_name: "债券基金",
    instrument_code: "BD",
    asset_class: "bond",
    asset_class_weight: 0.4,
    portfolio_target_weight: 0.4,
    target_amount_minor: 180_000_00,
    current_amount_minor: 160_000_00,
    structural_current_weight: 0.4,
    structural_gap_weight: 0,
    structural_gap_amount_minor: 0,
    structural_target_amount_minor: 160_000_00,
    plan_gap_amount_minor: 20_000_00,
    sort_order: 1,
    plan_scale_suggested_trade_minor: 20_000_00,
  };
  mockState.rebalanceSummary = {
    actionable_count: 0,
    structural_actionable_count: 0,
    configured_total_minor: 450_000_00,
    holdings_total_minor: 400_000_00,
    scale_gap_minor: -50_000_00,
    plan_scale_actionable_count: 2,
  };
  mockState.rebalanceLines = [equityLine, bondLine];
  mockState.targetsHoldings = [equityLine, bondLine];
  mockState.assetClassTargets = [
    { asset_class: "equity", weight: 0.6 },
    { asset_class: "bond", weight: 0.4 },
  ];
  mockState.regionTargets = [
    { asset_class: "equity", region: "domestic", weight_within_class: 1 },
    { asset_class: "bond", region: "domestic", weight_within_class: 1 },
  ];
}

function setD1ScaleOverScenario() {
  mockState.rebalanceSummary = {
    actionable_count: 0,
    structural_actionable_count: 0,
    configured_total_minor: 450_000_00,
    holdings_total_minor: 500_000_00,
    scale_gap_minor: 50_000_00,
    plan_scale_actionable_count: 1,
  };
  mockState.rebalanceLines = [
    {
      ...targetLineBase,
      target_amount_minor: 450_000_00,
      current_amount_minor: 500_000_00,
      structural_target_amount_minor: 500_000_00,
      plan_gap_amount_minor: -50_000_00,
      action: "hold",
      suggested_trade_minor: 0,
      plan_scale_action: "decrease",
      plan_scale_suggested_trade_minor: -50_000_00,
    },
  ];
  mockState.targetsHoldings = [mockState.rebalanceLines[0]!];
}

describe("RebalancePage §10 acceptance", () => {
  beforeEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    syncPlanTotalAssets.mockReset();
    syncPlanTotalAssets.mockResolvedValue({});
    setB1Scenario();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("B1: structural hold, scale gap bar, and plan_scale coexist in collapsed area (§10.6)", async () => {
    renderPage();

    expect(await screen.findByText("结构偏差汇总")).toBeInTheDocument();
    expect(screen.getByText(/规模缺口/)).toBeInTheDocument();
    expect(screen.getAllByText("不动").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("结构无调整建议；请处理规模偏差。")).toBeInTheDocument();

    fireEvent.click(screen.getByText("按计划规模对齐（高级）"));
    const advancedTable = screen.getByText("计划规模建议").closest("table");
    expect(advancedTable).not.toBeNull();
    expect(within(advancedTable as HTMLElement).getByText("增配")).toBeInTheDocument();
    expect(screen.getByText(/日常调仓请以上方结构偏差为准/)).toBeInTheDocument();
  });

  it("A2: structural rotation with scale over (§10.1 A2)", async () => {
    setA2Scenario();
    renderPage();

    expect(await screen.findByText(/规模超出/)).toBeInTheDocument();
    expect(screen.getAllByText("减配").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText("增配").length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText("结构无调整建议；请处理规模偏差。")).not.toBeInTheDocument();
  });

  it("B3: proportional shrink structural hold with scale gap hint (§10.2 B3)", async () => {
    setB3Scenario();
    renderPage();

    expect(await screen.findByText(/规模缺口/)).toBeInTheDocument();
    expect(screen.getAllByText("不动").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("结构无调整建议；请处理规模偏差。")).toBeInTheDocument();
    const mainSection = screen.getByText("结构偏差汇总").closest("section");
    expect(mainSection).not.toBeNull();
    expect(within(mainSection as HTMLElement).queryByText("增配")).not.toBeInTheDocument();
    expect(within(mainSection as HTMLElement).queryByText("减配")).not.toBeInTheDocument();
  });

  it("D1: sync success message auto-dismisses after 3 seconds (§10.4 D1)", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    setD1ScaleOverScenario();
    vi.spyOn(window, "confirm").mockReturnValue(true);
    renderPage();

    expect(await screen.findByText(/规模超出/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "同步计划基准至持仓合计" }));

    await waitFor(() => expect(syncPlanTotalAssets).toHaveBeenCalledTimes(1));
    expect(await screen.findByRole("status")).toHaveTextContent(
      "计划基准规模已同步至持仓合计",
    );

    await act(async () => {
      vi.advanceTimersByTime(3000);
    });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("D1: sync scale over shows success feedback (§10.4 D1)", async () => {
    setD1ScaleOverScenario();
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    renderPage();

    expect(await screen.findByText(/规模超出/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "同步计划基准至持仓合计" }));

    await waitFor(() => expect(syncPlanTotalAssets).toHaveBeenCalledTimes(1));
    expect(confirmSpy).toHaveBeenCalledWith(
      expect.stringMatching(/450,000\.00.*500,000\.00/s),
    );
    expect(await screen.findByText("计划基准规模已同步至持仓合计")).toBeInTheDocument();
  });

  it("D3: dismiss scale bar for current session (§10.4 D3)", async () => {
    renderPage();

    expect(await screen.findByText(/规模缺口/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "暂不处理" }));
    expect(screen.queryByText(/规模缺口/)).not.toBeInTheDocument();
    expect(sessionStorage.getItem("fireman_scale_gap_dismissed:plan_1")).toBe("1");
  });

  it("does not show error when sync confirm is cancelled", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false);
    renderPage();

    expect(await screen.findByText(/规模缺口/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "同步计划基准至持仓合计" }));

    await waitFor(() => expect(syncPlanTotalAssets).not.toHaveBeenCalled());
    expect(screen.queryByText("已取消")).not.toBeInTheDocument();
    expect(screen.queryByText("同步失败")).not.toBeInTheDocument();
  });
});
