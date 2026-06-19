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
      cash_flows: [],
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
      instrument_id: "i1",
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
      instrument_id: "i3",
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
const defaultInstruments = {
  instruments: [
    {
      id: "i2",
      code: "FB",
      name: "基金B",
      asset_class: "equity",
      region: "domestic",
      status: "active",
      quality_status: "available",
      simulation_eligible: true,
      is_system: false,
      market: "CN",
      instrument_type: "fund",
    },
  ],
};
const getHoldingsMock = vi.hoisted(() => vi.fn());
const getTargetsMock = vi.hoisted(() => vi.fn());
const listInstrumentsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: (...args: unknown[]) => getHoldingsMock(...args),
  getTargets: (...args: unknown[]) => getTargetsMock(...args),
}));

vi.mock("@/lib/api/instruments", () => ({
  listInstruments: (...args: unknown[]) => listInstrumentsMock(...args),
}));

vi.mock("@/lib/api/asset-refresh", () => ({
  submitAssetRefresh: (...args: unknown[]) => submitAssetRefresh(...args),
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
    listInstrumentsMock.mockReset();
    listInstrumentsMock.mockResolvedValue(defaultInstruments);
  });

  it("shows error state when targets load fails", async () => {
    getTargetsMock.mockReset();
    getTargetsMock.mockRejectedValue(new Error("boom"));
    renderPage();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByText("1. 说明")).not.toBeInTheDocument();
  });

  it("shows error state when instruments load fails", async () => {
    listInstrumentsMock.mockReset();
    listInstrumentsMock.mockRejectedValue(new Error("boom"));
    renderPage();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });

  it("shows wizard steps and intro copy", async () => {
    renderPage();
    expect(await screen.findByText("资产变更")).toBeInTheDocument();
    expect(screen.getByText("1. 说明")).toBeInTheDocument();
    expect(screen.getByText(/维护当前计划下的真实持仓结构/)).toBeInTheDocument();
  });

  it("supports step navigation and FIRE scenario switching without changing plan route", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));

    expect(await screen.findByText("配置确认")).toBeInTheDocument();
    expect(screen.getByText("当前计划：")).toBeInTheDocument();
    expect(screen.getByText("测试计划")).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("asset-refresh-scenario-select"), {
      target: { value: "scn_2" },
    });
    expect(replace).not.toHaveBeenCalled();
    expect(await screen.findByText(/债券\s*100%/)).toBeInTheDocument();
    expect(await screen.findByText(/债券 · 地区组内目标/)).toBeInTheDocument();
    expect(await screen.findByText(/国内\s*100%/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("录入当前资产")).toBeInTheDocument();
    expect(screen.getAllByText("权益").length).toBeGreaterThan(0);
    expect(screen.getAllByText("国内").length).toBeGreaterThan(0);
  });

  it("navigates back from entry step to config confirmation", async () => {
    await goToEntryStep();
    fireEvent.click(screen.getByRole("button", { name: "上一步" }));
    expect(await screen.findByText("配置确认")).toBeInTheDocument();
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
    fireEvent.click(await screen.findByRole("button", { name: "提交资产变更" }));

    await waitFor(() => expect(submitAssetRefresh).toHaveBeenCalledTimes(1));

    expect(submitAssetRefresh.mock.calls[0]?.[1]).toMatchObject({
      config_version: 1,
      config_changed: true,
      sync_total_assets_minor: true,
      holdings: expect.arrayContaining([
        expect.objectContaining({ instrument_id: "i1" }),
        expect.objectContaining({ instrument_id: "i2" }),
      ]),
    });
    expect(push).toHaveBeenCalledWith("/plans/plan_1/rebalance?asset_refreshed=1");
  });

  it("submits weight-only changes in a single asset refresh request", async () => {
    await goToEntryStep();
    const weightInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(weightInputs[0]!, { target: { value: "70" } });
    fireEvent.change(weightInputs[1]!, { target: { value: "30" } });

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByTestId("asset-refresh-change-count")).toHaveTextContent("2 项");
    fireEvent.click(await screen.findByRole("button", { name: "提交资产变更" }));

    await waitFor(() => expect(submitAssetRefresh).toHaveBeenCalledTimes(1));
    expect(submitAssetRefresh.mock.calls[0]?.[1]).toMatchObject({
      config_changed: true,
    });
  });

  it("shows region group targets from selected scenario on config step", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));

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
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));

    expect(await screen.findByTestId("asset-refresh-change-count")).toHaveTextContent("0");
    expect(screen.getByTestId("asset-refresh-no-changes")).toHaveTextContent("本次未修改任何资产");
    expect(screen.queryByText(/仅更新了持仓结构或资产分配/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交资产变更" })).toBeDisabled();
  });

  it("includes scenario_id in single asset refresh request when scenario changed", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));
    fireEvent.change(await screen.findByTestId("asset-refresh-scenario-select"), {
      target: { value: "scn_2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "提交资产变更" }));

    await waitFor(() => expect(submitAssetRefresh).toHaveBeenCalledTimes(1));
    expect(submitAssetRefresh.mock.calls[0]?.[1]).toMatchObject({
      scenario_id: "scn_2",
      config_changed: true,
    });
  });
});
