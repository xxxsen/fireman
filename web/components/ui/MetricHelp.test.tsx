import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MetricHelp } from "./MetricHelp";

describe("MetricHelp", () => {
  it("shows tooltip on click for touch users", () => {
    render(<MetricHelp termKey="fire_success_rate" />);
    expect(screen.queryByTestId("metric-help-tooltip")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("metric-help-trigger"));
    expect(screen.getByTestId("metric-help-tooltip")).toHaveTextContent("模拟路径");
    expect(screen.getByTestId("metric-help-tooltip")).toHaveTextContent("计算：");
    expect(screen.getByTestId("metric-help-tooltip")).toHaveTextContent("数据：");
    expect(screen.getByTestId("metric-help-tooltip")).toHaveTextContent("解读：");
  });

  it("fails loudly for an unknown topic outside production", () => {
    expect(() => render(<MetricHelp termKey="unknown_key" />)).toThrow(
      "Unknown help topic: unknown_key",
    );
  });

  it("supports an explicit one-off explanation without a topic key", () => {
    render(<MetricHelp text="临时说明" />);
    fireEvent.click(screen.getByTestId("metric-help-trigger"));
    expect(screen.getByTestId("metric-help-tooltip")).toHaveTextContent("临时说明");
  });
});
