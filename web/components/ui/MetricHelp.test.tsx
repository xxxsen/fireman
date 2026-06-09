import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MetricHelp } from "./MetricHelp";

describe("MetricHelp", () => {
  it("shows tooltip on click for touch users", () => {
    render(<MetricHelp termKey="fire_success_rate" />);
    expect(screen.queryByTestId("metric-help-tooltip")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("metric-help-trigger"));
    expect(screen.getByTestId("metric-help-tooltip")).toHaveTextContent("模拟路径");
  });

  it("hides when no term", () => {
    const { container } = render(<MetricHelp termKey="unknown_key" />);
    expect(container).toBeEmptyDOMElement();
  });
});
