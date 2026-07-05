import { describe, expect, it } from "vitest";
import {
  applyRecommendedOneLine,
  buildReferencePackageItems,
  computeFundPool,
  countStagedChanges,
  findCashSweepHolding,
  formatPackageDeltaLabel,
  hasReferencePackage,
  isFundPoolBalanced,
  recommendedPlannedMinor,
} from "./rebalance-plan";

describe("computeFundPool", () => {
  it("computes released and used capital", () => {
    const pool = computeFundPool([
      { baseline_current_minor: 120_000_00, planned_current_minor: 100_000_00 },
      { baseline_current_minor: 90_000_00, planned_current_minor: 110_000_00 },
    ]);
    expect(pool.releasedMinor).toBe(20_000_00);
    expect(pool.usedMinor).toBe(20_000_00);
    expect(pool.netMinor).toBe(0);
    expect(isFundPoolBalanced(pool.netMinor)).toBe(true);
  });

  it("detects imbalanced pool", () => {
    const pool = computeFundPool([
      { baseline_current_minor: 120_000_00, planned_current_minor: 100_000_00 },
      { baseline_current_minor: 90_000_00, planned_current_minor: 90_000_00 },
    ]);
    expect(pool.netMinor).toBe(20_000_00);
    expect(isFundPoolBalanced(pool.netMinor)).toBe(false);
  });
});

describe("countStagedChanges", () => {
  it("counts lines with planned delta", () => {
    expect(
      countStagedChanges([
        { baseline_current_minor: 100, planned_current_minor: 100 },
        { baseline_current_minor: 200, planned_current_minor: 150 },
      ]),
    ).toBe(1);
  });
});

describe("reference package helpers", () => {
  const lines = [
    {
      instrument_name: "A",
      instrument_code: "A",
      recommended_package_delta_minor: -300_000_00,
    },
    {
      instrument_name: "B",
      instrument_code: "B",
      recommended_package_delta_minor: 100_000_00,
    },
    {
      instrument_name: "C",
      instrument_code: "C",
      recommended_package_delta_minor: 0,
    },
  ];

  it("formats package delta labels", () => {
    expect(formatPackageDeltaLabel(-300_000_00)).toBe("−30w");
    expect(formatPackageDeltaLabel(100_000_00)).toBe("+10w");
    expect(formatPackageDeltaLabel(0)).toBe("—");
  });

  it("builds reference package summary", () => {
    expect(buildReferencePackageItems(lines)).toEqual(["A −30w", "B +10w"]);
    expect(hasReferencePackage(lines)).toBe(true);
  });

  it("computes recommended planned amount", () => {
    expect(recommendedPlannedMinor(1_500_000_00, -300_000_00)).toBe(1_200_000_00);
  });

  it("applyRecommendedOneLine returns single-line patch", () => {
    expect(
      applyRecommendedOneLine({
        id: "l1",
        baseline_current_minor: 1_500_000_00,
        recommended_package_delta_minor: -300_000_00,
      }),
    ).toEqual({ line_id: "l1", planned_current_minor: 1_200_000_00 });
  });
});

describe("findCashSweepHolding", () => {
  it("prefers system cash", () => {
    const got = findCashSweepHolding([
      {
        id: "h1",
        asset_key: "other_cash",
        enabled: true,
        asset_class: "cash",
        sort_order: 0,
        current_amount_minor: 100,
      },
      {
        id: "h2",
        asset_key: "SYS|cash||CNY",
        enabled: true,
        asset_class: "cash",
        sort_order: 10,
        current_amount_minor: 200,
      },
    ]);
    expect(got?.holding_id).toBe("h2");
  });

  it("falls back to lowest sort_order cash", () => {
    const got = findCashSweepHolding([
      {
        id: "h1",
        asset_key: "c1",
        enabled: true,
        asset_class: "cash",
        sort_order: 5,
        current_amount_minor: 100,
      },
      {
        id: "h2",
        asset_key: "c2",
        enabled: true,
        asset_class: "cash",
        sort_order: 1,
        current_amount_minor: 200,
      },
    ]);
    expect(got?.holding_id).toBe("h2");
  });
});
