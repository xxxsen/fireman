import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
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

  it("preserves decimal editing states and emits the final semantic value", () => {
    const onChange = vi.fn();
    const { rerender } = render(<PercentInput value={0.02} onChange={onChange} />);
    const input = screen.getByTestId("percent-input");
    fireEvent.focus(input);

    for (const draft of ["2", "2.", "2.2", "2.29"]) {
      fireEvent.change(input, { target: { value: draft } });
      expect(input).toHaveValue(draft);
    }
    expect(onChange).toHaveBeenLastCalledWith(0.0229);
    rerender(<PercentInput value={0.0229} onChange={onChange} />);
    expect(input).toHaveValue("2.29");
    fireEvent.blur(input);
    expect(input).toHaveValue("2.29");
    expect(onChange.mock.calls.filter(([value]) => value === 0.0229)).toHaveLength(1);
  });

  it.each([".", "-", "-."])("keeps incomplete draft %s and restores the value on blur", (draft) => {
    const onChange = vi.fn();
    render(<PercentInput value={0.03} onChange={onChange} />);
    const input = screen.getByTestId("percent-input");
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: draft } });
    expect(input).toHaveValue(draft);
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.blur(input);
    expect(input).toHaveValue("3");
    expect(onChange).not.toHaveBeenCalled();
  });

  it("does not emit for unchanged focus and blur", () => {
    const onChange = vi.fn();
    render(<PercentInput value={0.03} onChange={onChange} />);
    const input = screen.getByTestId("percent-input");
    fireEvent.focus(input);
    fireEvent.blur(input);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("supports negative and leading-decimal input", () => {
    function Harness() {
      const [value, setValue] = useState(0);
      return <PercentInput value={value} onChange={setValue} />;
    }
    render(<Harness />);
    const input = screen.getByTestId("percent-input");
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: ".5" } });
    expect(input).toHaveValue(".5");
    fireEvent.change(input, { target: { value: "-0.5" } });
    expect(input).toHaveValue("-0.5");
    fireEvent.blur(input);
    expect(input).toHaveValue("-0.5");
  });

  it("reflects external value changes while not editing", () => {
    const onChange = vi.fn();
    const { rerender } = render(<PercentInput value={0.03} onChange={onChange} />);
    rerender(<PercentInput value={0.0229} onChange={onChange} />);
    expect(screen.getByTestId("percent-input")).toHaveValue("2.29");
  });
});
