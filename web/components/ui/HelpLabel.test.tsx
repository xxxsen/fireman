import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HelpLabel } from "./HelpLabel";

describe("HelpLabel", () => {
  it("keeps the visible label and accessible topic label together", () => {
    render(<HelpLabel label="成功率" termKey="fire_success_rate" />);

    expect(screen.getByText("成功率")).toBeInTheDocument();
    const trigger = screen.getByRole("button", { name: "查看「FIRE 成功率」说明" });
    fireEvent.click(trigger);
    expect(screen.getByRole("tooltip")).toHaveTextContent("成功路径数除以");
  });
});
