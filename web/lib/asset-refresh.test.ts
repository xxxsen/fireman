import { describe, expect, it } from "vitest";
import {
  buildAssetRefreshBody,
  hasAssetRefreshStructureChange,
  validateAssetRefreshTotal,
} from "./asset-refresh";
import type { PlanHolding } from "@/types/api";

describe("validateAssetRefreshTotal", () => {
  it("passes when sum matches total within tolerance", () => {
    const rows = [
      { instrument_id: "a", current_amount_minor: 50_000_00 },
      { instrument_id: "b", current_amount_minor: 50_000_00 },
    ];
    expect(validateAssetRefreshTotal(rows, 100_000_00).ok).toBe(true);
  });

  it("blocks when gap exceeds 1 yuan", () => {
    const rows = [{ instrument_id: "a", current_amount_minor: 50_000_00 }];
    const result = validateAssetRefreshTotal(rows, 100_000_00);
    expect(result.ok).toBe(false);
    expect(result.message).toContain("分项合计");
  });
});

describe("buildAssetRefreshBody", () => {
  it("includes sync flag", () => {
    const body = buildAssetRefreshBody(
      2,
      [{ instrument_id: "a", current_amount_minor: 100 }],
      100,
      true,
      true,
    );
    expect(body.config_version).toBe(2);
    expect(body.sync_total_assets_minor).toBe(true);
    expect(body.config_changed).toBe(true);
  });
});

describe("hasAssetRefreshStructureChange", () => {
  const base: PlanHolding[] = [
    {
      id: "h1",
      plan_id: "plan_1",
      instrument_id: "i1",
      enabled: true,
      asset_class: "equity",
      region: "domestic",
      weight_within_group: 1,
      current_amount_minor: 100,
      simulation_snapshot_id: "",
      sort_order: 0,
    },
  ];

  it("detects added instruments", () => {
    expect(
      hasAssetRefreshStructureChange(base, [
        {
          id: "h1",
          instrument_id: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          enabled: true,
          current_amount_minor: 100,
          weight_within_group: 1,
          sort_order: 0,
          is_system: false,
        },
        {
          id: "h2",
          instrument_id: "i2",
          label: "B",
          code: "B",
          asset_class: "equity",
          region: "domestic",
          enabled: true,
          current_amount_minor: 0,
          weight_within_group: 1,
          sort_order: 10,
          is_system: false,
        },
      ]),
    ).toBe(true);
  });

  it("detects enabled flag changes", () => {
    expect(
      hasAssetRefreshStructureChange(base, [
        {
          id: "h1",
          instrument_id: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          enabled: false,
          current_amount_minor: 100,
          weight_within_group: 1,
          sort_order: 0,
          is_system: false,
        },
      ]),
    ).toBe(true);
  });
});
