import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import HomePage from "./page";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: [
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
      },
    ],
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
      "/plans/plan_1/dashboard",
    );
    expect(screen.getByText("查看详情 →")).toBeInTheDocument();
  });
});
