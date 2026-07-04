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

  it("places a followCursor tooltip near the cursor and tracks movement", () => {
    render(
      <Tooltip content="提示" followCursor contentTestId="follow-content" triggerTestId="follow-trigger">
        <button type="button">触发</button>
      </Tooltip>,
    );

    const trigger = screen.getByTestId("follow-trigger");
    fireEvent.mouseEnter(trigger, { clientX: 100, clientY: 200 });
    const tooltip = screen.getByTestId("follow-content");
    // cursor + TOOLTIP_CURSOR_OFFSET (12); tooltip has zero size under jsdom.
    expect(tooltip.style.left).toBe("112px");
    expect(tooltip.style.top).toBe("212px");

    fireEvent.mouseMove(trigger, { clientX: 300, clientY: 250 });
    expect(tooltip.style.left).toBe("312px");
    expect(tooltip.style.top).toBe("262px");
  });
});
