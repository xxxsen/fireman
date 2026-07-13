import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuickFireResult } from "@/lib/api/quick-fire";
import { ApiError } from "@/lib/api/client";
import { QUICK_FIRE_DRAFT_KEY, QUICK_FIRE_TRANSFER_KEY } from "@/lib/quick-fire-draft";

const { push, calculateQuickFire, echartsRender } = vi.hoisted(() => ({
  push: vi.fn(),
  calculateQuickFire: vi.fn(),
  echartsRender: vi.fn(),
}));

vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));
vi.mock("@/lib/api/quick-fire", () => ({ calculateQuickFire }));
vi.mock("echarts-for-react", () => ({
  default: ({ option }: { option: unknown }) => {
    echartsRender();
    return <div data-testid="quick-fire-chart" data-option={JSON.stringify(option)} />;
  },
}));

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

  afterEach(() => {
    vi.restoreAllMocks();
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
    expect(screen.getByTestId("quick-fire-results")).toHaveAttribute("aria-hidden", "true");
    expect(screen.getByText(/请先修正输入参数/)).toBeInTheDocument();
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

  it("does not recalculate or rerender results for an unchanged money focus and blur", async () => {
    const storageSet = vi.spyOn(Storage.prototype, "setItem");
    render(<QuickFirePage />);
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toBeInTheDocument());
    expect(calculateQuickFire).toHaveBeenCalledTimes(1);
    expect(echartsRender).toHaveBeenCalledTimes(1);
    const draftWrites = () => storageSet.mock.calls.filter(([key]) => key === QUICK_FIRE_DRAFT_KEY).length;
    const writesBeforeFocus = draftWrites();

    const money = screen.getByRole("textbox", { name: "当前可投资资产" });
    fireEvent.focus(money);
    fireEvent.blur(money);
    await new Promise((resolve) => window.setTimeout(resolve, 350));

    expect(calculateQuickFire).toHaveBeenCalledTimes(1);
    expect(echartsRender).toHaveBeenCalledTimes(1);
    expect(draftWrites()).toBe(writesBeforeFocus);
  });

  it("accepts 2.29 percent inflation in natural typing order", async () => {
    render(<QuickFirePage />);
    await waitFor(() => expect(calculateQuickFire).toHaveBeenCalledTimes(1));
    const inflation = screen.getByRole("textbox", { name: "固定通胀率" });
    fireEvent.focus(inflation);
    for (const draft of ["2", "2.", "2.2", "2.29"]) {
      fireEvent.change(inflation, { target: { value: draft } });
      expect(inflation).toHaveValue(draft);
    }

    await waitFor(() => expect(calculateQuickFire).toHaveBeenCalledTimes(2));
    expect(calculateQuickFire.mock.calls[1]![0]).toEqual(expect.objectContaining({ inflation_rate: 0.0229 }));
  });

  it("keeps the result and chart mounted while a changed input is recalculated", async () => {
    let resolveNext!: (value: QuickFireResult) => void;
    render(<QuickFirePage />);
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toBeInTheDocument());
    const chart = screen.getByTestId("quick-fire-chart");
    calculateQuickFire.mockImplementationOnce(() => new Promise((resolve) => { resolveNext = resolve; }));

    const spending = screen.getByRole("textbox", { name: "退休首年支出（当前购买力）" });
    fireEvent.focus(spending);
    fireEvent.change(spending, { target: { value: "130000" } });
    expect(screen.getByTestId("quick-fire-outcome")).toBeVisible();
    expect(screen.getByTestId("quick-fire-chart")).toBe(chart);
    expect(echartsRender).toHaveBeenCalledTimes(1);

    await waitFor(() => expect(calculateQuickFire).toHaveBeenCalledTimes(2));
    expect(screen.getByText(/正在计算，暂显示上次结果/)).toBeInTheDocument();
    resolveNext({
      ...result,
      terminal_wealth_minor: 80_0000_00,
      years: result.years.map((year) => ({ ...year, end_wealth_minor: year.end_wealth_minor + 1 })),
    });
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toHaveTextContent("800,000"));
    expect(screen.getByTestId("quick-fire-chart")).toBe(chart);
    expect(echartsRender).toHaveBeenCalledTimes(2);
  });

  it("ignores a superseded response that resolves after the latest request", async () => {
    let resolveOld!: (value: QuickFireResult) => void;
    let resolveLatest!: (value: QuickFireResult) => void;
    render(<QuickFirePage />);
    await waitFor(() => expect(calculateQuickFire).toHaveBeenCalledTimes(1));
    calculateQuickFire
      .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveLatest = resolve; }));

    const spending = screen.getByRole("textbox", { name: "退休首年支出（当前购买力）" });
    fireEvent.focus(spending);
    fireEvent.change(spending, { target: { value: "130000" } });
    await waitFor(() => expect(calculateQuickFire).toHaveBeenCalledTimes(2));
    fireEvent.change(spending, { target: { value: "140000" } });
    await waitFor(() => expect(calculateQuickFire).toHaveBeenCalledTimes(3));

    resolveLatest({ ...result, terminal_wealth_minor: 70_0000_00 });
    await waitFor(() => expect(screen.getByTestId("quick-fire-outcome")).toHaveTextContent("700,000"));
    resolveOld({ ...result, terminal_wealth_minor: 90_0000_00 });
    await new Promise((resolve) => window.setTimeout(resolve, 0));
    expect(screen.getByTestId("quick-fire-outcome")).toHaveTextContent("700,000");
  });
});
