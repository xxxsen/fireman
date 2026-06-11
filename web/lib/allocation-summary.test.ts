import { describe, expect, it } from "vitest";
import {
  buildAllocationSummary,
  buildRebalanceWorkspaceRows,
} from "./allocation-summary";
import type { RebalanceLine, TargetView } from "@/types/api";

const targets: TargetView = {
  total_assets_minor: 100_000,
  config_hash: "hash",
  weight_checks: { passed: true, checks: [] },
  asset_class_targets: [
    { asset_class: "equity", weight: 0.6 },
    { asset_class: "bond", weight: 0.4 },
    { asset_class: "cash", weight: 0 },
  ],
  region_targets: [
    { asset_class: "equity", region: "domestic", weight_within_class: 0.5 },
    { asset_class: "equity", region: "foreign", weight_within_class: 0.5 },
    { asset_class: "bond", region: "domestic", weight_within_class: 1 },
    { asset_class: "bond", region: "foreign", weight_within_class: 0 },
  ],
  holdings: [
    {
      holding_id: "h1",
      instrument_id: "i1",
      asset_class: "equity",
      region: "domestic",
      enabled: true,
      asset_class_weight: 0.6,
      region_weight: 0.5,
      weight_within_group: 1,
      portfolio_target_weight: 0.3,
      target_amount_minor: 30_000,
      current_amount_minor: 20_000,
      current_weight: 0.2,
      deviation_amount_minor: 10_000,
      deviation_weight: 0.1,
      structural_current_weight: 1 / 3,
      structural_gap_weight: 0.1,
      structural_gap_amount_minor: 6_000,
      structural_target_amount_minor: 36_000,
      plan_gap_weight: 0.1,
      plan_gap_amount_minor: 10_000,
      simulation_snapshot_id: "",
      sort_order: 0,
    },
    {
      holding_id: "h2",
      instrument_id: "i2",
      asset_class: "equity",
      region: "foreign",
      enabled: true,
      asset_class_weight: 0.6,
      region_weight: 0.5,
      weight_within_group: 1,
      portfolio_target_weight: 0.3,
      target_amount_minor: 30_000,
      current_amount_minor: 40_000,
      current_weight: 0.4,
      deviation_amount_minor: -10_000,
      deviation_weight: -0.1,
      structural_current_weight: 2 / 3,
      structural_gap_weight: -0.1,
      structural_gap_amount_minor: -6_000,
      structural_target_amount_minor: 36_000,
      plan_gap_weight: -0.1,
      plan_gap_amount_minor: -10_000,
      simulation_snapshot_id: "",
      sort_order: 10,
    },
  ],
};

describe("buildAllocationSummary", () => {
  it("builds asset class and active region rows with structural fields", () => {
    const rows = buildAllocationSummary(targets);
    expect(rows.map((row) => row.key)).toEqual([
      "equity",
      "equity:domestic",
      "equity:foreign",
      "bond",
      "bond:domestic",
    ]);
    expect(rows[0].target_weight).toBeCloseTo(0.6);
    // Structural current weight sums enabled holdings (all equity here → 100%).
    expect(rows[0].current_weight).toBeCloseTo(1);
    expect(rows[0].gap_amount_minor).toBe(0);
    expect(rows[1].gap_amount_minor).toBe(6_000);
    expect(rows[1].gap_weight).toBeCloseTo(0.1);
    expect(rows[1].target_weight_within_parent).toBeCloseTo(0.5);
    expect(rows[2].target_weight_within_parent).toBeCloseTo(0.5);
    expect(rows[1].current_weight_within_parent).toBeCloseTo(1 / 3);
    expect(rows[2].current_weight_within_parent).toBeCloseTo(2 / 3);
  });

  // C1-style: proportional holdings → zero structural gap at summary level.
  it("shows zero structural gap when holdings match target weights (C1)", () => {
    const aligned: TargetView = {
      ...targets,
      total_assets_minor: 450_000_00,
      holdings: [
        {
          ...targets.holdings[0],
          portfolio_target_weight: 0.6,
          current_amount_minor: 270_000_00,
          structural_current_weight: 0.6,
          structural_gap_weight: 0,
          structural_gap_amount_minor: 0,
          structural_target_amount_minor: 270_000_00,
        },
        {
          ...targets.holdings[1],
          asset_class: "bond",
          region: "domestic",
          portfolio_target_weight: 0.4,
          current_amount_minor: 180_000_00,
          structural_current_weight: 0.4,
          structural_gap_weight: 0,
          structural_gap_amount_minor: 0,
          structural_target_amount_minor: 180_000_00,
        },
      ],
    };
    const rows = buildAllocationSummary(aligned);
    const equity = rows.find((row) => row.key === "equity");
    expect(equity?.gap_amount_minor).toBe(0);
    expect(equity?.gap_weight).toBeCloseTo(0);
  });
});

const rebalanceLines: RebalanceLine[] = targets.holdings.map((holding) => ({
  ...holding,
  action: holding.structural_gap_amount_minor >= 0 ? "increase" : "decrease",
  suggested_trade_minor: Math.abs(holding.structural_gap_amount_minor),
  plan_scale_action: holding.plan_gap_amount_minor >= 0 ? "increase" : "decrease",
  plan_scale_suggested_trade_minor: Math.abs(holding.plan_gap_amount_minor),
}));

describe("buildRebalanceWorkspaceRows", () => {
  it("nests holdings under region summary rows", () => {
    const rows = buildRebalanceWorkspaceRows(targets, rebalanceLines);
    expect(rows.map((row) => row.key)).toEqual([
      "equity",
      "equity:domestic",
      "h1",
      "equity:foreign",
      "h2",
      "bond",
      "bond:domestic",
    ]);
    expect(rows.find((row) => row.key === "h1")?.action).toBe("increase");
    expect(rows.find((row) => row.key === "equity")?.action).toBeUndefined();
  });

  it("filters holdings by structural action", () => {
    const rows = buildRebalanceWorkspaceRows(targets, rebalanceLines, "decrease");
    expect(rows.map((row) => row.key)).toEqual([
      "equity",
      "equity:domestic",
      "equity:foreign",
      "h2",
      "bond",
      "bond:domestic",
    ]);
  });
});
