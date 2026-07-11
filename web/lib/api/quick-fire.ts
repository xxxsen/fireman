import { apiPost, type ApiRequestOptions } from "./client";

export type QuickFireOutcomeStatus =
  | "sustainable"
  | "insufficient_funds"
  | "wealth_depleted"
  | "terminal_floor_not_met";

export interface QuickFireInput {
  base_currency: "CNY";
  current_age: number;
  planned_fire_age: number;
  end_age: number;
  current_assets_minor: number;
  annual_savings_minor: number;
  annual_savings_growth_rate: number;
  annual_spending_minor: number;
  annual_retirement_income_minor: number;
  annual_retirement_income_growth_rate: number;
  annual_return_rate: number;
  inflation_rate: number;
  terminal_wealth_floor_minor: number;
}

export interface QuickFireYear {
  age: number;
  months_in_period: number;
  phase: "accumulation" | "retirement";
  start_wealth_minor: number;
  income_minor: number;
  spending_minor: number;
  investment_gain_minor: number;
  end_wealth_minor: number;
  real_end_wealth_minor: number;
  required_wealth_minor: number;
}

export interface QuickFireResult {
  engine_version: "quick_fire_v1";
  base_currency: "CNY";
  outcome_status: QuickFireOutcomeStatus;
  sustainable_through_end_age: boolean;
  projected_assets_at_fire_minor: number;
  required_assets_at_fire_minor: number;
  fire_funding_gap_minor: number;
  support_months_after_fire: number;
  depletion_month_offset?: number | null;
  depletion_age_years?: number | null;
  depletion_age_months?: number | null;
  unfunded_spending_minor: number;
  terminal_wealth_minor: number;
  terminal_wealth_floor_minor: number;
  real_terminal_wealth_minor: number;
  real_annual_return_rate: number;
  earliest_fire_month_offset?: number | null;
  earliest_fire_age_years?: number | null;
  earliest_fire_age_months?: number | null;
  years: QuickFireYear[];
}

export function calculateQuickFire(
  input: QuickFireInput,
  options?: Pick<ApiRequestOptions, "signal">,
): Promise<QuickFireResult> {
  return apiPost<QuickFireResult>("/api/v1/fire/quick-calculations", input, undefined, options);
}
