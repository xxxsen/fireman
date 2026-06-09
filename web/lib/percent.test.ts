import { describe, expect, it } from "vitest";
import {
  decimalToPercentString,
  formatPercent,
  percentToDecimal,
  validatePercentSum,
} from "./percent";

describe("percent conversion", () => {
  it("converts user input 3 to API 0.03", () => {
    expect(percentToDecimal("3")).toBe(0.03);
    expect(percentToDecimal("3%")).toBe(0.03);
  });

  it("formats decimal to percent string", () => {
    expect(decimalToPercentString(0.03)).toBe("3");
    expect(formatPercent(0.03)).toBe("3%");
  });

  it("validates weight sum with tolerance", () => {
    const ok = validatePercentSum([
      { label: "a", value: 0.7 },
      { label: "b", value: 0.3 },
    ]);
    expect(ok.passed).toBe(true);

    const bad = validatePercentSum([
      { label: "a", value: 0.95 },
      { label: "b", value: 0 },
    ]);
    expect(bad.passed).toBe(false);
    expect(bad.message).toContain("还差");
  });
});
