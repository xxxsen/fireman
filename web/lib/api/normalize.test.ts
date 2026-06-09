import { describe, expect, it } from "vitest";
import { normalizeApiArrays } from "./normalize";

describe("normalizeApiArrays", () => {
  it("converts known null array fields to empty arrays", () => {
    const input = {
      historical_snapshots: null,
      referencing_plans: null,
      simulation_window: {
        excluded_years: null,
        selected_years: null,
      },
    };
    const out = normalizeApiArrays(input);
    expect(out.historical_snapshots).toEqual([]);
    expect(out.referencing_plans).toEqual([]);
    expect(out.simulation_window.excluded_years).toEqual([]);
    expect(out.simulation_window.selected_years).toEqual([]);
  });

  it("leaves unrelated null values unchanged", () => {
    const input = { latest_simulation: null };
    expect(normalizeApiArrays(input)).toEqual(input);
  });
});
