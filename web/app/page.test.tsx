import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import HomePage from "./page";

const { mockPlans } = vi.hoisted(() => ({
  mockPlans: [
    {
      id: "plan_1",
      name: "测试计划",
      base_currency: "CNY",
      valuation_date: "2026-01-01",
      status: "active",
      config_version: 1,
      config_hash: "",
      created_at: 0,
      updated_at: 0,
      rebalance_actionable_count: 2,
      holdings_gap_minor: 500_000,
    },
  ],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: mockPlans,
    isLoading: false,
    error: null,
  }),
}));

describe("HomePage", () => {
  it("renders plan cards instead of auto-redirecting", () => {
    render(<HomePage />);
    expect(screen.getByRole("heading", { name: /我的 FIRE 计划/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /测试计划/ })).toHaveAttribute(
      "href",
      "/plans/plan_1/overview",
    );
    expect(screen.getByText("2 个标的")).toBeInTheDocument();
    expect(screen.getByText("规模超出")).toBeInTheDocument();
    expect(screen.getByText("¥5,000.00")).toBeInTheDocument();
    expect(screen.getByText("查看详情 →")).toBeInTheDocument();
  });

  it("hides scale gap on plan card when within tolerance", () => {
    mockPlans[0]!.holdings_gap_minor = 50;
    render(<HomePage />);
    expect(screen.queryByText("规模超出")).not.toBeInTheDocument();
    expect(screen.queryByText("规模缺口")).not.toBeInTheDocument();
    mockPlans[0]!.holdings_gap_minor = 500_000;
  });
});
