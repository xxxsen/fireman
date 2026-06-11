import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import HoldingsPage from "./page";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useSearchParams: () => new URLSearchParams(),
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
      holdings: [
        {
          holding_id: "holding_1",
          enabled: true,
          asset_class: "equity",
          region: "domestic",
          portfolio_target_weight: 1,
          target_amount_minor: 100_000_00,
        },
      ],
    }),
  updateHoldings: vi.fn(),
  syncHoldingSnapshot: vi.fn(),
}));

describe("HoldingsPage", () => {
  it("groups holdings and shows target and gap amounts", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <HoldingsPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("权益", { selector: "h2" })).toBeInTheDocument();
    expect(screen.getByText("国内", { selector: "h3" })).toBeInTheDocument();
    expect(screen.getByText("全组合目标 100%")).toBeInTheDocument();
    expect(screen.getByText("¥100,000.00")).toBeInTheDocument();
    expect(screen.getByText("¥20,000.00")).toBeInTheDocument();
  });
});
