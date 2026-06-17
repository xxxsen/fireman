import { describe, expect, it } from "vitest";
import { looksLikeFundCode } from "./instrument-resolve-search";

describe("looksLikeFundCode", () => {
  it("accepts fund codes with at least four characters", () => {
    expect(looksLikeFundCode("5103")).toBe(true);
    expect(looksLikeFundCode("270042")).toBe(true);
    expect(looksLikeFundCode("00700")).toBe(true);
  });

  it("rejects short or non-code queries", () => {
    expect(looksLikeFundCode("T1")).toBe(false);
    expect(looksLikeFundCode("测试")).toBe(false);
    expect(looksLikeFundCode("")).toBe(false);
  });
});
