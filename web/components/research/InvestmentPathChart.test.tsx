import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { InvestmentPathPoint } from "@/lib/api/investment-paths";
import { InvestmentPathChart } from "./InvestmentPathChart";

const points: InvestmentPathPoint[] = [
  {
    strategy_key: "income_dca",
    valuation_date: "2024-01-01",
    account_value_minor: 1_000_000,
    asset_value_minor: 999_000,
    cash_value_minor: 1_000,
    cumulative_external_contribution_minor: 1_000_000,
    unit_nav: 1,
    drawdown: 0,
  },
  {
    strategy_key: "income_dca",
    valuation_date: "2024-01-02",
    account_value_minor: 1_120_000,
    asset_value_minor: 1_120_000,
    cash_value_minor: 0,
    cumulative_external_contribution_minor: 1_000_000,
    unit_nav: 1.12,
    drawdown: 0,
  },
  {
    strategy_key: "income_dca",
    valuation_date: "2024-01-03",
    account_value_minor: 2_080_000,
    asset_value_minor: 2_080_000,
    cash_value_minor: 0,
    cumulative_external_contribution_minor: 2_000_000,
    unit_nav: 1.04,
    drawdown: -0.0714,
  },
];

describe("InvestmentPathChart", () => {
  it("renders labeled date and money axes", () => {
    render(<InvestmentPathChart points={points} currency="CNY" />);

    expect(screen.getByRole("img", { name: "账户价值与累计投入的历史路径折线图" })).toBeVisible();
    expect(screen.getByText("日期")).toBeVisible();
    expect(screen.getByText("账户金额（CNY）")).toBeVisible();
    expect(screen.getByText("2024-01-01")).toBeVisible();
    expect(screen.getByText("2024-01-03")).toBeVisible();
  });

  it("shows the nearest date point details on pointer movement", () => {
    render(<InvestmentPathChart points={points} currency="CNY" />);
    const hitArea = screen.getByTestId("investment-path-chart-hit-area");
    Object.defineProperty(hitArea, "getBoundingClientRect", {
      value: () => ({ left: 0, top: 0, width: 736, height: 244, right: 736, bottom: 244, x: 0, y: 0, toJSON: () => ({}) }),
    });

    fireEvent.pointerMove(hitArea, { clientX: 736, clientY: 100 });

    const tooltip = screen.getByTestId("investment-path-chart-tooltip");
    expect(tooltip).toHaveTextContent("2024-01-03");
    expect(tooltip).toHaveTextContent("账户价值");
    expect(tooltip).toHaveTextContent("¥20,800.00");
    expect(tooltip).toHaveTextContent("累计投入");
  });

  it("supports keyboard selection at both path boundaries", () => {
    render(<InvestmentPathChart points={points} currency="CNY" />);
    const hitArea = screen.getByRole("slider", { name: "查看投入路径日期点详情" });

    fireEvent.focus(hitArea);
    expect(screen.getByTestId("investment-path-chart-tooltip")).toHaveTextContent("2024-01-01");
    fireEvent.keyDown(hitArea, { key: "End" });
    expect(screen.getByTestId("investment-path-chart-tooltip")).toHaveTextContent("2024-01-03");
    fireEvent.keyDown(hitArea, { key: "ArrowRight" });
    expect(screen.getByTestId("investment-path-chart-tooltip")).toHaveTextContent("2024-01-03");
  });

  it("explains when the path has fewer than two points", () => {
    render(<InvestmentPathChart points={points.slice(0, 1)} currency="CNY" />);
    expect(screen.getByText("没有足够路径点。")).toBeVisible();
  });
});
