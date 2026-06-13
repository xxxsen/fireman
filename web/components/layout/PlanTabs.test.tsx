import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { PlanTabs } from "./PlanTabs";

vi.mock("next/navigation", () => ({
  usePathname: () => "/plans/plan_1/rebalance",
}));

describe("PlanTabs", () => {
  it("renders portfolio-first tabs without holdings", () => {
    render(<PlanTabs planId="plan_1" />);

    expect(screen.getAllByRole("link")).toHaveLength(3);
    expect(screen.getByRole("link", { name: "组合总览" })).toHaveAttribute(
      "href",
      "/plans/plan_1/overview",
    );
    expect(screen.getByRole("link", { name: "持仓预览" })).toHaveAttribute(
      "href",
      "/plans/plan_1/rebalance",
    );
    expect(screen.getByRole("link", { name: "持仓预览" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.queryByRole("link", { name: "持仓管理" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "计划设置" })).toHaveAttribute(
      "href",
      "/plans/plan_1/settings",
    );
  });
});
