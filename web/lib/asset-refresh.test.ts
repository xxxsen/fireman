import { describe, expect, it } from "vitest";
import {
  buildAssetRefreshBody,
  countAssetRefreshChanges,
  defaultWeightWithinGroup,
  hasAssetRefreshDraftChanges,
  hasAssetRefreshStructureChange,
  validateAssetRefreshGroupWeights,
  validateAssetRefreshTotal,
} from "./asset-refresh";
import type { PlanHolding } from "@/types/api";

describe("validateAssetRefreshTotal", () => {
  it("passes when sum matches total within tolerance", () => {
    const rows = [
      { asset_key: "a", current_amount_minor: 50_000_00 },
      { asset_key: "b", current_amount_minor: 50_000_00 },
    ];
    expect(validateAssetRefreshTotal(rows, 100_000_00).ok).toBe(true);
  });

  it("blocks when gap exceeds 1 yuan", () => {
    const rows = [{ asset_key: "a", current_amount_minor: 50_000_00 }];
    const result = validateAssetRefreshTotal(rows, 100_000_00);
    expect(result.ok).toBe(false);
    expect(result.message).toContain("分项合计");
  });
});

describe("buildAssetRefreshBody", () => {
  it("includes sync flag and optional scenario", () => {
    const body = buildAssetRefreshBody(
      2,
      [
        {
          id: "h1",
          asset_key: "a",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 1,
          sort_order: 0,
          is_system: false,
        },
      ],
      100,
      true,
      true,
      "scn_2",
    );
    expect(body.config_version).toBe(2);
    expect(body.scenario_id).toBe("scn_2");
    expect(body.sync_total_assets_minor).toBe(true);
    expect(body.config_changed).toBe(true);
    expect(body.holdings[0]).toMatchObject({
      asset_key: "a",
      weight_within_group: 1,
    });
    expect(body.holdings[0]).not.toHaveProperty("enabled");
  });
});

describe("hasAssetRefreshStructureChange", () => {
  const base: PlanHolding[] = [
    {
      id: "h1",
      plan_id: "plan_1",
      asset_key: "i1",
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
          asset_key: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 1,
          sort_order: 0,
          is_system: false,
        },
        {
          id: "h2",
          asset_key: "i2",
          label: "B",
          code: "B",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 0,
          weight_within_group: 1,
          sort_order: 10,
          is_system: false,
        },
      ]),
    ).toBe(true);
  });

  it("detects weight_within_group changes", () => {
    expect(
      hasAssetRefreshStructureChange(base, [
        {
          id: "h1",
          asset_key: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 0.6,
          sort_order: 0,
          is_system: false,
        },
      ]),
    ).toBe(true);
  });
});

describe("countAssetRefreshChanges", () => {
  const base: PlanHolding[] = [
    {
      id: "h1",
      plan_id: "plan_1",
      asset_key: "i1",
      enabled: true,
      asset_class: "equity",
      region: "domestic",
      weight_within_group: 0.6,
      current_amount_minor: 100,
      simulation_snapshot_id: "",
      sort_order: 0,
    },
    {
      id: "h2",
      plan_id: "plan_1",
      asset_key: "i2",
      enabled: true,
      asset_class: "equity",
      region: "domestic",
      weight_within_group: 0.4,
      current_amount_minor: 100,
      simulation_snapshot_id: "",
      sort_order: 1,
    },
  ];

  it("returns zero when nothing changed", () => {
    expect(
      countAssetRefreshChanges(
        base,
        base.map((holding) => ({
          id: holding.id,
          asset_key: holding.asset_key,
          label: "X",
          code: "X",
          asset_class: holding.asset_class,
          region: holding.region,
          current_amount_minor: holding.current_amount_minor,
          weight_within_group: holding.weight_within_group,
          sort_order: holding.sort_order,
          is_system: false,
        })),
      ),
    ).toBe(0);
  });

  it("counts weight-only changes per instrument", () => {
    expect(
      countAssetRefreshChanges(base, [
        {
          id: "h1",
          asset_key: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 0.7,
          sort_order: 0,
          is_system: false,
        },
        {
          id: "h2",
          asset_key: "i2",
          label: "B",
          code: "B",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 0.3,
          sort_order: 1,
          is_system: false,
        },
      ]),
    ).toBe(2);
  });

  it("counts amount-only changes per instrument", () => {
    expect(
      countAssetRefreshChanges(base, [
        {
          id: "h1",
          asset_key: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 200,
          weight_within_group: 0.6,
          sort_order: 0,
          is_system: false,
        },
        {
          id: "h2",
          asset_key: "i2",
          label: "B",
          code: "B",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 0.4,
          sort_order: 1,
          is_system: false,
        },
      ]),
    ).toBe(1);
  });

  it("counts removed instruments", () => {
    expect(
      countAssetRefreshChanges(base, [
        {
          id: "h1",
          asset_key: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 0.6,
          sort_order: 0,
          is_system: false,
        },
      ]),
    ).toBe(1);
  });
});

describe("hasAssetRefreshDraftChanges", () => {
  const base: PlanHolding[] = [
    {
      id: "h1",
      plan_id: "plan_1",
      asset_key: "i1",
      enabled: true,
      asset_class: "equity",
      region: "domestic",
      weight_within_group: 1,
      current_amount_minor: 100_00,
      simulation_snapshot_id: "",
      sort_order: 0,
    },
  ];

  it("detects total asset changes", () => {
    expect(
      hasAssetRefreshDraftChanges(
        base,
        base.map((holding) => ({
          id: holding.id,
          asset_key: holding.asset_key,
          label: "A",
          code: "A",
          asset_class: holding.asset_class,
          region: holding.region,
          current_amount_minor: holding.current_amount_minor,
          weight_within_group: holding.weight_within_group,
          sort_order: holding.sort_order,
          is_system: false,
        })),
        200_00,
      ),
    ).toBe(true);
  });
});

describe("validateAssetRefreshGroupWeights", () => {
  it("passes when each group sums to 100%", () => {
    const result = validateAssetRefreshGroupWeights([
      {
        id: "h1",
        asset_key: "i1",
        label: "A",
        code: "A",
        asset_class: "equity",
        region: "domestic",
        current_amount_minor: 100,
        weight_within_group: 0.6,
        sort_order: 0,
        is_system: false,
      },
      {
        id: "h2",
        asset_key: "i2",
        label: "B",
        code: "B",
        asset_class: "equity",
        region: "domestic",
        current_amount_minor: 100,
        weight_within_group: 0.4,
        sort_order: 1,
        is_system: false,
      },
    ]);
    expect(result.ok).toBe(true);
  });

  it("blocks when group weights do not sum to 100%", () => {
    const result = validateAssetRefreshGroupWeights([
      {
        id: "h1",
        asset_key: "i1",
        label: "A",
        code: "A",
        asset_class: "equity",
        region: "domestic",
        current_amount_minor: 100,
        weight_within_group: 0.5,
        sort_order: 0,
        is_system: false,
      },
    ]);
    expect(result.ok).toBe(false);
    expect(result.message).toContain("组内配比");
  });
});

describe("defaultWeightWithinGroup", () => {
  it("assigns remaining weight to new holding", () => {
    const weight = defaultWeightWithinGroup(
      [
        {
          id: "h1",
          asset_key: "i1",
          label: "A",
          code: "A",
          asset_class: "equity",
          region: "domestic",
          current_amount_minor: 100,
          weight_within_group: 0.7,
          sort_order: 0,
          is_system: false,
        },
      ],
      "equity",
      "domestic",
    );
    expect(weight).toBeCloseTo(0.3);
  });
});
