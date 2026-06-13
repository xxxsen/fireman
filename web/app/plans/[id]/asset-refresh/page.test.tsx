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

const updateHoldings = vi.fn();
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
  listPlans: () =>
    Promise.resolve([
      { id: "plan_1", name: "测试计划" },
      { id: "plan_2", name: "计划 B" },
    ]),
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
          weights: [],
          description: "",
          is_builtin: false,
          plan_count: 1,
          created_at: 0,
          updated_at: 0,
        },
      ],
    }),
}));

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: () =>
    Promise.resolve({
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
          weight_within_group: 1,
          current_amount_minor: 50_000_00,
          simulation_snapshot_id: "",
          sort_order: 0,
        },
      ],
    }),
  getTargets: () =>
    Promise.resolve({
      asset_class_targets: [{ asset_class: "equity", weight: 1 }],
      holdings: [],
    }),
  updateHoldings: (...args: unknown[]) => updateHoldings(...args),
}));

vi.mock("@/lib/api/instruments", () => ({
  listInstruments: () =>
    Promise.resolve({
      instruments: [
        {
          id: "i2",
          code: "FB",
          name: "基金B",
          asset_class: "equity",
          region: "domestic",
          status: "active",
          quality_status: "available",
          is_system: false,
          market: "CN",
          instrument_type: "fund",
        },
      ],
    }),
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
    updateHoldings.mockReset();
    submitAssetRefresh.mockReset();
    updateHoldings.mockResolvedValue({ holdings: [] });
    submitAssetRefresh.mockResolvedValue({
      holdings: [],
      before_total_minor: 50_000_00,
      after_total_minor: 50_000_00,
    });
  });

  it("shows wizard steps and intro copy", async () => {
    renderPage();
    expect(await screen.findByText("资产变更")).toBeInTheDocument();
    expect(screen.getByText("1. 说明")).toBeInTheDocument();
    expect(screen.getByText(/维护当前计划下的真实持仓结构/)).toBeInTheDocument();
  });

  it("supports step navigation and plan switching", async () => {
    renderPage();
    expect(await screen.findByText("下一步")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));

    expect(await screen.findByText("配置确认")).toBeInTheDocument();
    fireEvent.change(screen.getByTestId("asset-refresh-plan-select"), {
      target: { value: "plan_2" },
    });
    expect(replace).toHaveBeenCalledWith("/plans/plan_2/asset-refresh");

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

  it("shows money unit hint on entry step", async () => {
    await goToEntryStep();
    const hints = screen.getAllByTestId("money-unit-hint");
    expect(hints.some((hint) => hint.textContent?.includes("约 5.00 万"))).toBe(true);
  });

  it("can add instrument from drawer", async () => {
    await goToEntryStep();
    fireEvent.click(screen.getByTestId("asset-refresh-add-instrument"));
    fireEvent.change(screen.getByTestId("asset-refresh-instrument-filter"), {
      target: { value: "FB" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /基金B/ }));
    expect(screen.getByText("FB")).toBeInTheDocument();
  });

  it("can toggle instrument enabled state", async () => {
    await goToEntryStep();
    const toggle = screen.getByRole("checkbox", { name: "基金A 启用" });
    fireEvent.click(toggle);
    expect(toggle).not.toBeChecked();
  });

  it("submits structure changes via updateHoldings and config_changed", async () => {
    const { getPlan } = await import("@/lib/api/plans");
    vi.mocked(getPlan)
      .mockResolvedValueOnce({
        id: "plan_1",
        name: "测试计划",
        config_version: 1,
        base_currency: "CNY",
        valuation_date: "2026-06-09",
        status: "active",
        created_at: 0,
        updated_at: 0,
      })
      .mockResolvedValueOnce({
        id: "plan_1",
        name: "测试计划",
        config_version: 2,
        base_currency: "CNY",
        valuation_date: "2026-06-09",
        status: "active",
        created_at: 0,
        updated_at: 0,
      });

    await goToEntryStep();
    fireEvent.click(screen.getByTestId("asset-refresh-add-instrument"));
    fireEvent.change(screen.getByTestId("asset-refresh-instrument-filter"), {
      target: { value: "FB" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /基金B/ }));

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "提交资产变更" }));

    await waitFor(() => expect(updateHoldings).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(submitAssetRefresh).toHaveBeenCalledTimes(1));

    expect(updateHoldings.mock.calls[0]?.[1]).toMatchObject({
      config_version: 1,
      holdings: expect.arrayContaining([
        expect.objectContaining({ instrument_id: "i1" }),
        expect.objectContaining({ instrument_id: "i2", enabled: true }),
      ]),
    });
    expect(submitAssetRefresh.mock.calls[0]?.[1]).toMatchObject({
      config_version: 2,
      config_changed: true,
      sync_total_assets_minor: true,
    });
    expect(push).toHaveBeenCalledWith("/plans/plan_1/rebalance?asset_refreshed=1");
  });

  it("shows structure-only message when totals match on confirm step", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "下一步" }));

    expect(await screen.findByText(/仅更新了持仓结构或资产分配/)).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: /同步计划基准规模/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "提交资产变更" })).toBeInTheDocument();
  });
});
