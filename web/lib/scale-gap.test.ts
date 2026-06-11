import { describe, expect, it } from "vitest";
import { isSignificantScaleGap, SCALE_GAP_TOLERANCE_MINOR } from "./scale-gap";

describe("scale-gap", () => {
  it("treats gaps within 1 CNY as insignificant", () => {
    expect(SCALE_GAP_TOLERANCE_MINOR).toBe(100);
    expect(isSignificantScaleGap(0)).toBe(false);
    expect(isSignificantScaleGap(50)).toBe(false);
    expect(isSignificantScaleGap(-100)).toBe(false);
    expect(isSignificantScaleGap(101)).toBe(true);
    expect(isSignificantScaleGap(-101)).toBe(true);
  });
});
