import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import HoldingsPage from "./page";

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

vi.mock("@/hooks/usePlanEdit", () => ({
  usePlanEdit: () => ({
    dirty: false,
    markDirty: vi.fn(),
    markClean: vi.fn(),
  }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () =>
    Promise.resolve({
      id: "plan_1",
      config_version: 1,
      valuation_date: "2026-06-11",
      base_currency: "CNY",
    }),
}));

vi.mock("@/lib/api/instruments", () => ({
  listInstruments: () =>
    Promise.resolve({
      instruments: [
        {
          id: "instrument_1",
          code: "T1",
          name: "测试基金",
          status: "active",
          quality_status: "available",
          asset_class: "equity",
          region: "domestic",
          is_system: false,
        },
      ],
    }),
}));

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: () =>
    Promise.resolve({
      holdings: [
        {
          id: "holding_1",
          plan_id: "plan_1",
          instrument_id: "instrument_1",
          instrument_name: "测试基金",
          instrument_code: "T1",
          enabled: true,
          asset_class: "equity",
          region: "domestic",
          weight_within_group: 1,
          current_amount_minor: 80_000_00,
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
  updateHoldings: vi.fn(),
  syncHoldingSnapshot: vi.fn(),
}));

describe("HoldingsPage", () => {
  beforeEach(() => {
    mockSearchParams.set(new URLSearchParams());
  });

  it("shows summary header without target/gap columns", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <HoldingsPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("更新账户资产")).toBeInTheDocument();
    expect(screen.getByText("查看调仓工作台 →")).toBeInTheDocument();
    expect(screen.queryByText("结构还差")).not.toBeInTheDocument();
    expect(screen.queryByText("目标金额")).not.toBeInTheDocument();
  });

  it("shows current amount and group weight as read-only with asset refresh links", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <HoldingsPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("¥80,000.00")).toBeInTheDocument();
    expect(screen.getByText("100%")).toBeInTheDocument();
    expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
    expect(screen.getAllByText("在更新账户资产中调整").length).toBeGreaterThan(0);
  });

  it("shows asset refreshed banner when query param set", async () => {
    mockSearchParams.set(new URLSearchParams("asset_refreshed=1"));
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <HoldingsPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("账户资产已更新。")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "查看调仓工作台" })).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance",
    );
  });
});
