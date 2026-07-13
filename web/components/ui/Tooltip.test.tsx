import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

  it("opens on touch-style activation and closes when tapping outside", () => {
    render(
      <div>
        <Tooltip content="提示" clickToggle contentTestId="touch-content">
          <button type="button">帮助</button>
        </Tooltip>
        <button type="button">外部</button>
      </div>,
    );

    const trigger = screen.getByRole("button", { name: "帮助" });
    fireEvent.pointerDown(trigger);
    fireEvent.focus(trigger);
    fireEvent.click(trigger);
    expect(screen.getByTestId("touch-content")).toBeInTheDocument();

    fireEvent.pointerDown(screen.getByRole("button", { name: "外部" }));
    expect(screen.queryByTestId("touch-content")).not.toBeInTheDocument();
  });

  it("closes with Escape and restores focus to the trigger", async () => {
    render(
      <Tooltip content="提示" clickToggle contentTestId="escape-content">
        <button type="button">帮助</button>
      </Tooltip>,
    );

    const trigger = screen.getByRole("button", { name: "帮助" });
    fireEvent.focus(trigger);
    expect(screen.getByTestId("escape-content")).toBeInTheDocument();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(screen.queryByTestId("escape-content")).not.toBeInTheDocument();
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("toggles from the trigger and exposes expanded state", () => {
    render(
      <Tooltip content="提示" clickToggle contentTestId="toggle-content">
        <button type="button">帮助</button>
      </Tooltip>,
    );

    const trigger = screen.getByRole("button", { name: "帮助" });
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute("aria-expanded", "false");
  });
});
