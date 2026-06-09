// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
}));

const updateParameters = vi.fn();

vi.mock("@/hooks/usePlanResultStale", () => ({
  usePlanResultStale: () => ({ stale: false }),
}));

const markDirty = vi.fn();
const markClean = vi.fn();

vi.mock("../layout", () => ({
  usePlanEdit: () => ({
    dirty: true,
    markDirty,
    markClean,
  }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () =>
    Promise.resolve({
      id: "plan_1",
      name: "测试计划",
      config_version: 1,
      base_currency: "CNY",
      valuation_date: "2026-06-09",
      status: "active",
      created_at: 0,
      updated_at: 0,
    }),
  getParameters: () =>
    Promise.resolve({
      parameters: {
        plan_id: "plan_1",
        current_age: 30,
        retirement_age: 55,
        end_age: 90,
        total_assets_minor: 1_000_000_00,
        annual_savings_minor: 200_000_00,
        annual_savings_growth_rate: 0,
        annual_spending_minor: 400_000_00,
        terminal_wealth_floor_minor: 0,
        inflation_mode: "fixed_real",
        fixed_inflation_rate: 0.03,
        inflation_mu: 0.03,
        inflation_phi: 0.5,
        inflation_sigma: 0.01,
        withdrawal_type: "fixed_real",
        withdrawal_rate: 0.04,
        withdrawal_floor_ratio: 0.7,
        withdrawal_ceiling_ratio: 1.3,
        withdrawal_tax_rate: 0,
        taxable_withdrawal_ratio: 0,
        rebalance_frequency: "annual",
        rebalance_threshold: 0.03,
        transaction_cost_rate: 0,
        simulation_runs: 10000,
        student_t_df: 7,
        updated_at: 0,
      },
      cash_flows: [],
    }),
  updateParameters: (...args: unknown[]) => updateParameters(...args),
}));

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: () =>
    Promise.resolve({
      holdings: [
        {
          id: "h1",
          plan_id: "plan_1",
          instrument_id: "ins_1",
          enabled: true,
          asset_class: "equity",
          region: "domestic",
          weight_within_group: 1,
          current_amount_minor: 1_000_000_00,
          simulation_snapshot_id: "snap_1",
          sort_order: 1,
        },
      ],
    }),
}));

vi.mock("@/lib/api/allocation", () => ({
  getAllocation: () =>
    Promise.resolve({
      asset_class_targets: [
        { asset_class: "equity", weight: 1 },
        { asset_class: "bond", weight: 0 },
        { asset_class: "cash", weight: 0 },
      ],
      region_targets: [
        { asset_class: "equity", region: "domestic", weight_within_class: 1 },
        { asset_class: "equity", region: "foreign", weight_within_class: 0 },
        { asset_class: "bond", region: "domestic", weight_within_class: 1 },
        { asset_class: "bond", region: "foreign", weight_within_class: 0 },
        { asset_class: "cash", region: "domestic", weight_within_class: 1 },
        { asset_class: "cash", region: "foreign", weight_within_class: 0 },
      ],
    }),
  listScenarios: () => Promise.resolve({ scenarios: [] }),
  updateAllocation: vi.fn(),
}));

import ParametersPage from "./page";

describe("ParametersPage strategy enums", () => {
  beforeEach(() => {
    updateParameters.mockReset();
    updateParameters.mockResolvedValue({});
  });

  it("sends fixed_portfolio and random_ar1 on save", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("提取与通胀");
    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "fixed_portfolio" } });
    fireEvent.change(screen.getByLabelText("通胀模式"), { target: { value: "random_ar1" } });
    expect(screen.getByLabelText("提取策略")).toHaveValue("fixed_portfolio");
    expect(screen.getByLabelText("通胀模式")).toHaveValue("random_ar1");
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updateParameters).toHaveBeenCalledTimes(1));
    const req = updateParameters.mock.calls[0]![1] as {
      parameters: { withdrawal_type: string; inflation_mode: string };
    };
    expect(req.parameters.withdrawal_type).toBe("fixed_portfolio");
    expect(req.parameters.inflation_mode).toBe("random_ar1");
  });

  it("sends max seed as string without precision loss", async () => {
    const maxSeed = "9223372036854775807";
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("模拟设置");
    const seedInput = screen.getByRole("textbox", { name: /随机种子/ });
    fireEvent.change(seedInput, { target: { value: maxSeed } });
    expect(seedInput).toHaveValue(maxSeed);
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updateParameters).toHaveBeenCalledTimes(1));
    const req = updateParameters.mock.calls[0]![1] as {
      parameters: { seed: unknown };
    };
    expect(typeof req.parameters.seed).toBe("string");
    expect(req.parameters.seed).toBe(maxSeed);
    expect(req.parameters.seed).not.toBe(Number(maxSeed));
  });
});
