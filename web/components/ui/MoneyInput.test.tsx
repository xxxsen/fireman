// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { MoneyInput } from "./MoneyInput";

function Harness({
  initialMinor,
  plain = false,
  currency,
}: {
  initialMinor: number;
  plain?: boolean;
  currency?: string;
}) {
  const [minor, setMinor] = useState(initialMinor);
  return (
    <MoneyInput
      label="金额"
      valueMinor={minor}
      onChange={setMinor}
      plain={plain}
      currency={currency}
    />
  );
}

describe("MoneyInput", () => {
  it("renders CNY unit as a 元 suffix and associates the label", () => {
    render(<Harness initialMinor={4_000_000_00} plain />);
    expect(screen.getByTestId("money-inline-unit")).toHaveTextContent("元");
    expect(screen.getByLabelText(/金额/)).toHaveValue("4000000");
  });

  it("keeps non-CNY currency codes as the suffix", () => {
    render(<Harness initialMinor={100_00} currency="USD" />);
    expect(screen.getByTestId("money-inline-unit")).toHaveTextContent("USD");
  });

  it("shows a magnitude hint for plain amounts and updates while typing", () => {
    render(<Harness initialMinor={4_000_000_00} plain />);
    expect(screen.getByTestId("money-unit-hint")).toHaveTextContent("约 400.00 万");
    fireEvent.change(screen.getByTestId("money-input"), { target: { value: "12000" } });
    expect(screen.getByTestId("money-unit-hint")).toHaveTextContent("约 1.20 万");
  });

  it("hides the hint for zero plain values", () => {
    render(<Harness initialMinor={0} plain />);
    expect(screen.queryByTestId("money-unit-hint")).not.toBeInTheDocument();
  });

  it("hides the hint in formatted (non-plain) mode", () => {
    render(<Harness initialMinor={4_000_000_00} />);
    expect(screen.queryByTestId("money-unit-hint")).not.toBeInTheDocument();
    expect(screen.getByTestId("money-input")).toHaveValue("4,000,000.00");
  });

  it("does not emit for unchanged focus and blur", () => {
    const onChange = vi.fn();
    render(<MoneyInput valueMinor={123_45} onChange={onChange} />);
    const input = screen.getByTestId("money-input");
    fireEvent.focus(input);
    fireEvent.blur(input);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("emits a changed amount once and does not duplicate it on blur", () => {
    const onChange = vi.fn();
    const { rerender } = render(<MoneyInput valueMinor={100_00} onChange={onChange} />);
    const input = screen.getByTestId("money-input");
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: "123.45" } });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(123_45);
    rerender(<MoneyInput valueMinor={123_45} onChange={onChange} />);
    fireEvent.blur(input);
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("does not re-emit zero when an already empty amount blurs", () => {
    const onChange = vi.fn();
    render(<MoneyInput valueMinor={0} onChange={onChange} />);
    const input = screen.getByTestId("money-input");
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("restores an incomplete amount without emitting", () => {
    const onChange = vi.fn();
    render(<MoneyInput valueMinor={123_45} onChange={onChange} />);
    const input = screen.getByTestId("money-input");
    fireEvent.focus(input);
    fireEvent.change(input, { target: { value: "." } });
    expect(input).toHaveValue(".");
    fireEvent.blur(input);
    expect(input).toHaveValue("123.45");
    expect(onChange).not.toHaveBeenCalled();
  });
});
