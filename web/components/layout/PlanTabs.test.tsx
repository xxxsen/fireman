import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import { PlanTabs } from "./PlanTabs";

const mockPathname = vi.fn(() => "/plans/plan_1/rebalance");

vi.mock("next/navigation", () => ({
  usePathname: () => mockPathname(),
}));

describe("PlanTabs", () => {
  it("renders portfolio-first tabs without holdings", () => {
    render(<PlanTabs planId="plan_1" />);

    expect(screen.getAllByRole("link")).toHaveLength(3);
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
    expect(screen.queryByRole("link", { name: "持仓管理" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "计划设置" })).toHaveAttribute(
      "href",
      "/plans/plan_1/settings",
    );
  });

  it("highlights 调仓工作台 on the asset-refresh sub flow", () => {
    mockPathname.mockReturnValue("/plans/plan_1/asset-refresh");
    render(<PlanTabs planId="plan_1" />);

    expect(screen.getByRole("link", { name: "调仓工作台" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });
});
