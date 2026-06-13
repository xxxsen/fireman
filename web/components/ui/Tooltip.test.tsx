import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Tooltip } from "./Tooltip";

describe("Tooltip", () => {
  it("positions content with viewport-aware fixed coordinates", () => {
    render(
      <Tooltip
        content="提示内容"
        align="center"
        contentTestId="tooltip-content"
        triggerTestId="tooltip-trigger"
      >
        <button type="button">触发</button>
      </Tooltip>,
    );

    fireEvent.mouseEnter(screen.getByTestId("tooltip-trigger"));
    const tooltip = screen.getByTestId("tooltip-content");

    expect(tooltip).toHaveStyle({ position: "fixed" });
    expect(tooltip.style.left).not.toBe("");
    expect(tooltip.style.top).not.toBe("");
  });
});
