// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
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
});
