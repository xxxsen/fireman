import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { PlanTabs } from "./PlanTabs";

vi.mock("next/navigation", () => ({
  usePathname: () => "/plans/plan_1/rebalance",
}));

describe("PlanTabs", () => {
  it("renders the four portfolio-first plan tabs", () => {
    render(<PlanTabs planId="plan_1" />);

    expect(screen.getAllByRole("link")).toHaveLength(4);
    expect(screen.getByRole("link", { name: "组合总览" })).toHaveAttribute(
      "href",
      "/plans/plan_1/overview",
    );
    expect(screen.getByRole("link", { name: "调仓工作台" })).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance",
    );
    expect(screen.getByRole("link", { name: "调仓工作台" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "持仓管理" })).toHaveAttribute(
      "href",
      "/plans/plan_1/holdings",
    );
    expect(screen.getByRole("link", { name: "计划设置" })).toHaveAttribute(
      "href",
      "/plans/plan_1/settings",
    );
  });
});
