import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { PercentInput } from "./PercentInput";

describe("PercentInput", () => {
  it("displays percent and emits decimal on change", () => {
    const onChange = vi.fn();
    render(<PercentInput value={0.03} onChange={onChange} />);
    const input = screen.getByTestId("percent-input");
    expect(input).toHaveValue("3");
    fireEvent.change(input, { target: { value: "5" } });
    expect(onChange).toHaveBeenCalledWith(0.05);
  });
});
