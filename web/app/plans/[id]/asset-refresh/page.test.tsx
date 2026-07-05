// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import AssetRefreshPage from "./page";

const push = vi.fn();
const replace = vi.fn();

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ push, replace }),
  useSearchParams: () => new URLSearchParams(),
}));

const submitAssetRefresh = vi.fn();

vi.mock("@/lib/api/plans", () => ({
  getPlan: vi.fn(() =>
    Promise.resolve({
      id: "plan_1",
      name: "测试计划",
      config_version: 1,
      base_currency: "CNY",
      valuation_date: "2026-06-09",
    }),
  ),
  getParameters: () =>
    Promise.resolve({
      parameters: { selected_scenario_id: "scn_1" },
    }),
}));

vi.mock("@/lib/api/allocation", () => ({
  listScenarios: () =>
    Promise.resolve({
      scenarios: [
        {
          id: "scn_1",
          name: "均衡",
          weights: [{ asset_class: "equity", weight: 1 }],
          region_targets: [
            { asset_class: "equity", region: "domestic", weight_within_class: 0.7 },
            { asset_class: "equity", region: "foreign", weight_within_class: 0.3 },
          ],
          description: "",
          is_builtin: false,
          plan_count: 1,
          created_at: 0,
          updated_at: 0,
        },
        {
          id: "scn_2",
          name: "已 FIRE",
          weights: [{ asset_class: "bond", weight: 1 }],
          region_targets: [
            { asset_class: "bond", region: "domestic", weight_within_class: 1 },
          ],
          description: "",
          is_builtin: false,
          plan_count: 0,
          created_at: 0,
          updated_at: 0,
        },
      ],
    }),
}));

const defaultHoldingsResp = {
  holdings: [
    {
      id: "h1",
      plan_id: "plan_1",
      asset_key: "CN|cn_exchange_fund|sh|FA",
      instrument_name: "基金A",
      instrument_code: "FA",
      asset_class: "equity",
      region: "domestic",
      enabled: true,
      weight_within_group: 0.6,
      current_amount_minor: 30_000_00,
      simulation_snapshot_id: "",
      sort_order: 0,
    },
    {
      id: "h2",
      plan_id: "plan_1",
      asset_key: "CN|cn_exchange_fund|sh|FC",
      instrument_name: "基金C",
      instrument_code: "FC",
      asset_class: "equity",
      region: "domestic",
      enabled: true,
      weight_within_group: 0.4,
      current_amount_minor: 20_000_00,
      simulation_snapshot_id: "",
      sort_order: 1,
    },
  ],
};
const defaultTargets = {
  asset_class_targets: [{ asset_class: "equity", weight: 1 }],
  region_targets: [
    { asset_class: "equity", region: "domestic", weight_within_class: 0.7 },
    { asset_class: "equity", region: "foreign", weight_within_class: 0.3 },
  ],
  holdings: [],
};
const defaultAssetsResp = {
  assets: [
    {
      asset_key: "CN|cn_exchange_fund|sh|FB",
      market: "CN",
      instrument_type: "cn_exchange_fund",
      region_code: "sh",
      symbol: "FB",
      name: "基金B",
      exchange: "SH",
      instrument_kind: "etf",
      currency: "CNY",
      active: true,
      listing_status: "active",
      last_seen_at: 0,
      source_name: "ak.fund_etf_spot_em",
      source_as_of: "",
      refreshed_at: 0,
      created_at: 0,
      updated_at: 0,
      has_history: true,
      history_data_as_of: "2026-07-01",
    },
  ],
  syncs: [],
  total: 1,
};
const getHoldingsMock = vi.hoisted(() => vi.fn());
const getTargetsMock = vi.hoisted(() => vi.fn());
const listMarketAssetsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: (...args: unknown[]) => getHoldingsMock(...args),
  getTargets: (...args: unknown[]) => getTargetsMock(...args),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssetsMock(...args),
}));

vi.mock("@/lib/api/asset-refresh", () => ({
  submitAssetRefresh: (...args: unknown[]) => submitAssetRefresh(...args),
}));

const getActiveRebalanceExecutionMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/rebalance-executions", () => ({
  getActiveRebalanceExecution: (...args: unknown[]) =>
    getActiveRebalanceExecutionMock(...args),
}));

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AssetRefreshPage />
    </QueryClientProvider>,
  );
}

async function goToEntryStep() {
  renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "下一步" }));
  await screen.findByText("录入当前资产");
}

describe("AssetRefreshPage", () => {
  beforeEach(() => {
    push.mockClear();
    replace.mockClear();
    submitAssetRefresh.mockReset();
    submitAssetRefresh.mockResolvedValue({
      holdings: [],
      before_total_minor: 50_000_00,
      after_total_minor: 50_000_00,
    });
    getHoldingsMock.mockReset();
    getHoldingsMock.mockResolvedValue(defaultHoldingsResp);
    getTargetsMock.mockReset();
    getTargetsMock.mockResolvedValue(defaultTargets);
    listMarketAssetsMock.mockReset();
    listMarketAssetsMock.mockResolvedValue(defaultAssetsResp);
    getActiveRebalanceExecutionMock.mockReset();
    getActiveRebalanceExecutionMock.mockResolvedValue(null);
  });

  it("shows error state when active execution check fails (does not open refresh)", async () => {
    getActiveRebalanceExecutionMock.mockReset();
    getActiveRebalanceExecutionMock.mockRejectedValue(new Error("boom"));
    renderPage();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByText("1. 确认范围")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "提交持仓校正" })).not.toBeInTheDocument();
  });

  it("blocks asset refresh when an active execution is in progress", async () => {
    getActiveRebalanceExecutionMock.mockReset();
    getActiveRebalanceExecutionMock.mockResolvedValue({
      execution: { id: "rbx_9", status: "in_progress" },
    });
    renderPage();

    expect(await screen.findByTestId("asset-refresh-blocked")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "继续调仓执行" })).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance/executions/rbx_9",
    );
  });

  it("shows error state when targets load fails", async () => {
    getTargetsMock.mockReset();
    getTargetsMock.mockRejectedValue(new Error("boom"));
    renderPage();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByText("1. 确认范围")).not.toBeInTheDocument();
  });

  it("shows the 3-step wizard with scope confirmation copy", async () => {
    renderPage();
    expect(await screen.findByRole("heading", { name: "持仓校正" })).toBeInTheDocument();
    expect(screen.getByText("1. 确认范围")).toBeInTheDocument();
    expect(screen.getByText("2. 录入当前资产")).toBeInTheDocument();
    expect(screen.getByText("3. 确认提交")).toBeInTheDocument();
    expect(screen.getByText(/维护当前计划下的真实持仓/)).toBeInTheDocument();
    expect(screen.getByText("测试计划")).toBeInTheDocument();
  });

  it("shows the config template read-only with a settings link instead of a scenario select", async () => {
    renderPage();
    expect(await screen.findByTestId("asset-refresh-scenario-name")).toHaveTextContent("均衡");
    expect(screen.queryByTestId("asset-refresh-scenario-select")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "前往计划设置修改" })).toHaveAttribute(
      "href",
      "/plans/plan_1/settings?section=plan-targets",
    );
    expect(replace).not.toHaveBeenCalled();
  });

  it("navigates from scope confirmation to entry and back", async () => {
    await goToEntryStep();
    expect(screen.getAllByText("权益").length).toBeGreaterThan(0);
    expect(screen.getAllByText("国内").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "上一步" }));
    expect(await screen.findByText("确认范围")).toBeInTheDocument();
  });

  it("shows inline money unit on entry step", async () => {
    await goToEntryStep();
    const units = screen.getAllByTestId("money-inline-unit");
    expect(units.some((unit) => unit.textContent === "CNY(万)")).toBe(true);
  });

  it("opens centered dialog to add instrument", async () => {
    await goToEntryStep();
    fireEvent.click(screen.getByTestId("asset-refresh-add-instrument"));
    expect(screen.getByTestId("dialog")).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-modal", "true");
    fireEvent.change(screen.getByTestId("asset-refresh-instrument-filter"), {
      target: { value: "FB" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /基金B/ }));
    expect(screen.getByText("FB")).toBeInTheDocument();
    expect(screen.queryByTestId("dialog")).not.toBeInTheDocument();
  });

  it("submits structure changes in a single asset refresh request", async () => {
    await goToEntryStep();
    fireEvent.click(screen.getByTestId("asset-refresh-add-instrument"));
    fireEvent.change(screen.getByTestId("asset-refresh-instrument-filter"), {
      target: { value: "FB" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /基金B/ }));

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "提交持仓校正" }));

    await waitFor(() => expect(submitAssetRefresh).toHaveBeenCalledTimes(1));

    expect(submitAssetRefresh.mock.calls[0]?.[1]).toMatchObject({
      config_version: 1,
      config_changed: true,
      sync_total_assets_minor: true,
      holdings: expect.arrayContaining([
        expect.objectContaining({ asset_key: "CN|cn_exchange_fund|sh|FA" }),
        expect.objectContaining({
          asset_key: "CN|cn_exchange_fund|sh|FB",
          asset_class: "equity",
          region: "domestic",
        }),
      ]),
    });
    // The wizard no longer switches config templates, so no scenario_id is sent.
    expect(submitAssetRefresh.mock.calls[0]?.[1]).not.toHaveProperty("scenario_id");
    expect(push).toHaveBeenCalledWith("/plans/plan_1/rebalance?asset_refreshed=1");
  });

  it("submits weight-only changes in a single asset refresh request", async () => {
    await goToEntryStep();
    const weightInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(weightInputs[0]!, { target: { value: "70" } });
    fireEvent.change(weightInputs[1]!, { target: { value: "30" } });

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByTestId("asset-refresh-change-count")).toHaveTextContent("2 项");
    fireEvent.click(await screen.findByRole("button", { name: "提交持仓校正" }));

    await waitFor(() => expect(submitAssetRefresh).toHaveBeenCalledTimes(1));
    expect(submitAssetRefresh.mock.calls[0]?.[1]).toMatchObject({
      config_changed: true,
    });
  });

  it("shows read-only plan targets on the scope confirmation step", async () => {
    renderPage();

    expect(await screen.findByText("权益 · 地区组内目标（只读）")).toBeInTheDocument();
    expect(screen.getByText(/国内\s*70%/)).toBeInTheDocument();
    expect(screen.getByText(/国外\s*30%/)).toBeInTheDocument();
  });

  it("shows structure-only message when totals match on confirm step", async () => {
    await goToEntryStep();
    const weightInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(weightInputs[0]!, { target: { value: "70" } });
    fireEvent.change(weightInputs[1]!, { target: { value: "30" } });

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/仅更新了持仓结构或资产分配/)).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: /同步计划基准规模/ })).not.toBeInTheDocument();
  });

  it("disables submit and shows zero-change hint when nothing changed", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));

    expect(await screen.findByTestId("asset-refresh-change-count")).toHaveTextContent("0");
    expect(screen.getByTestId("asset-refresh-no-changes")).toHaveTextContent("本次未修改任何资产");
    expect(screen.queryByText(/仅更新了持仓结构或资产分配/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交持仓校正" })).toBeDisabled();
  });
});
