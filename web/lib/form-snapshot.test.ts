import { describe, expect, it } from "vitest";
import {
  buildParametersFormSnapshot,
  isParametersFormDirty,
} from "./form-snapshot";
import type { PlanCashFlow, PlanParameters } from "@/types/api";

const baseParams: PlanParameters = {
  plan_id: "plan_1",
  current_age: 30,
  retirement_age: 55,
  end_age: 90,
  total_assets_minor: 1_000_000_00,
  annual_savings_minor: 100_000_00,
  annual_savings_growth_rate: 0.03,
  annual_spending_minor: 400_000_00,
  terminal_wealth_floor_minor: 0,
  selected_scenario_id: "scn_1",
  inflation_mode: "fixed_real",
  fixed_inflation_rate: 0.02,
  inflation_mu: 0.02,
  inflation_phi: 0.5,
  inflation_sigma: 0.01,
  withdrawal_type: "fixed_real",
  withdrawal_rate: 0.04,
  withdrawal_floor_ratio: 0.8,
  withdrawal_ceiling_ratio: 1.2,
  withdrawal_tax_rate: 0,
  taxable_withdrawal_ratio: 1,
  rebalance_frequency: "annual",
  rebalance_threshold: 0.03,
  transaction_cost_rate: 0.001,
  simulation_runs: 5000,
  student_t_df: 10,
  seed: null,
  updated_at: 1,
};

const baseFlows: PlanCashFlow[] = [];

describe("form-snapshot", () => {
  it("detects dirty only when values actually change", () => {
    const initial = buildParametersFormSnapshot("计划 A", baseParams, baseFlows, "");
    expect(isParametersFormDirty(initial, initial)).toBe(false);

    const changedName = buildParametersFormSnapshot("计划 B", baseParams, baseFlows, "");
    expect(isParametersFormDirty(initial, changedName)).toBe(true);

    const reverted = buildParametersFormSnapshot("计划 A", baseParams, baseFlows, "");
    expect(isParametersFormDirty(initial, reverted)).toBe(false);
  });
});
