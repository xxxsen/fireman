import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { QuickFireResult } from "@/lib/api/quick-fire";
import { ApiError } from "@/lib/api/client";
import { QUICK_FIRE_TRANSFER_KEY } from "@/lib/quick-fire-draft";

const { push, calculateQuickFire } = vi.hoisted(() => ({ push: vi.fn(), calculateQuickFire: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));
vi.mock("@/lib/api/quick-fire", () => ({ calculateQuickFire }));
vi.mock("echarts-for-react", () => ({ default: ({ option }: { option: unknown }) => <div data-testid="quick-fire-chart" data-option={JSON.stringify(option)} /> }));

import QuickFirePage from "./page";

const result: QuickFireResult = {
  engine_version: "quick_fire_v1",
  base_currency: "CNY",
  outcome_status: "sustainable",
  sustainable_through_end_age: true,
  projected_assets_at_fire_minor: 300_0000_00,
  required_assets_at_fire_minor: 200_0000_00,
  fire_funding_gap_minor: 100_0000_00,
  support_months_after_fire: 540,
  terminal_wealth_minor: 100_0000_00,
  terminal_wealth_floor_minor: 0,
  real_terminal_wealth_minor: 50_0000_00,
  real_annual_return_rate: 0.02,
  years: [{ age: 35, months_in_period: 12, phase: "accumulation", start_wealth_minor: 1, income_minor: 2, spending_minor: 0, investment_gain_minor: 3, end_wealth_minor: 6, real_end_wealth_minor: 5, required_wealth_minor: 4 }],
};

describe("QuickFirePage", () => {
  beforeEach(() => {
	vi.useRealTimers();
    vi.clearAllMocks();
    window.localStorage.clear();
    window.sessionStorage.clear();
    window.history.replaceState({}, "", "/quick-fire");
    calculateQuickFire.mockResolvedValue(result);
  });

  it("calculates deterministically and transfers only documented inputs", async () => {
    render(<QuickFirePage />);
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toHaveTextContent("计划可行"));
    expect(screen.getByText(/固定收益率下的确定性估算/)).toBeInTheDocument();
    expect(screen.getByTestId("quick-fire-chart")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "创建完整计划" }));
    expect(push).toHaveBeenCalledWith("/plans/new?source=quick-fire");
    const raw = window.sessionStorage.getItem(QUICK_FIRE_TRANSFER_KEY);
    expect(raw).not.toBeNull();
    expect(raw).not.toContain("annual_return_rate");
    expect(raw).toContain("annual_retirement_income_minor");
  });

  it("hides stale results when input becomes invalid", async () => {
    render(<QuickFirePage />);
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toBeInTheDocument());
    const age = screen.getByLabelText(/当前年龄/);
    fireEvent.change(age, { target: { value: "17" } });
    expect(screen.queryByTestId("quick-fire-outcome")).not.toBeInTheDocument();
    expect(screen.getByText("请先修正输入参数。")).toBeInTheDocument();
  });

  it("shows a retryable error instead of retaining an old conclusion", async () => {
    calculateQuickFire.mockRejectedValueOnce(new ApiError("network_error", "网络不可用"));
    render(<QuickFirePage />);
    expect(await screen.findByText("网络不可用")).toBeInTheDocument();
    expect(screen.queryByTestId("quick-fire-outcome")).not.toBeInTheDocument();

    calculateQuickFire.mockResolvedValueOnce(result);
    fireEvent.click(screen.getByRole("button", { name: /重试/ }));
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toBeInTheDocument());
    expect(calculateQuickFire).toHaveBeenCalledTimes(2);
  });
});
