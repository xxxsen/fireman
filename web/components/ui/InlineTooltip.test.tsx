import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { InlineTooltip } from "./InlineTooltip";

describe("InlineTooltip", () => {
  it("shows tooltip on hover", () => {
    render(
      <InlineTooltip content="说明文字">
        <span>指标</span>
      </InlineTooltip>,
    );

    expect(screen.queryByTestId("inline-tooltip-content")).not.toBeInTheDocument();
    fireEvent.mouseEnter(screen.getByTestId("inline-tooltip-trigger"));
    expect(screen.getByTestId("inline-tooltip-content")).toHaveTextContent("说明文字");
  });
});
