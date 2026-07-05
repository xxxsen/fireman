import { describe, expect, it } from "vitest";
import {
  addInstrumentToGroup,
  buildRegionTargetsPayload,
  buildWizardPortfolioReview,
  complementRegionWeight,
  computeExpectedAmountMinor,
  dedupeWizardSelectionsByAssetKey,
  defaultWizardRegionTargets,
  getWizardAllocationGroups,
  isWizardRegionEnabled,
  pruneSelectedByRegionTargets,
  redistributeGroupWeights,
  updateInstrumentWeightInGroup,
} from "./wizard-allocation";
import type { WizardAsset, WizardHoldingSelection } from "./wizard-allocation";

const holding = (
  id: string,
  asset_class: string,
  region: string,
): WizardHoldingSelection => ({
  inst: { id, code: id, name: id, asset_class, region } as never,
  weight: 1,
  amount: 0,
});

describe("complementRegionWeight", () => {
  it("returns complement clamped to [0, 1]", () => {
    expect(complementRegionWeight(0.7)).toBeCloseTo(0.3);
    expect(complementRegionWeight(0.3)).toBeCloseTo(0.7);
  });
});

describe("redistributeGroupWeights", () => {
  const inst = (id: string) => ({ id, code: id, name: id } as never);

  it("assigns 100% to a single auto item", () => {
    const items: WizardHoldingSelection[] = [
      { inst: inst("A"), weight: 0, amount: 0, weightManual: false },
    ];
    expect(redistributeGroupWeights(items)[0]?.weight).toBe(1);
  });

  it("splits equally among four auto items", () => {
    const items: WizardHoldingSelection[] = ["A", "B", "C", "D"].map((id) => ({
      inst: inst(id),
      weight: 0,
      amount: 0,
      weightManual: false,
    }));
    const weights = redistributeGroupWeights(items).map((s) => s.weight);
    expect(weights).toEqual([0.25, 0.25, 0.25, 0.25]);
  });

  it("reserves manual weight and splits remainder", () => {
    let items: WizardHoldingSelection[] = ["A", "B", "C", "D"].map((id) => ({
      inst: inst(id),
      weight: 0.25,
      amount: 0,
      weightManual: false,
    }));
    items = updateInstrumentWeightInGroup(items, "D", 0.4);
    const byId = Object.fromEntries(items.map((s) => [s.inst.id, s.weight]));
    expect(byId.D).toBeCloseTo(0.4);
    expect(byId.A).toBeCloseTo(0.2);
    expect(byId.B).toBeCloseTo(0.2);
    expect(byId.C).toBeCloseTo(0.2);

    items = updateInstrumentWeightInGroup(items, "C", 0.4);
    const byId2 = Object.fromEntries(items.map((s) => [s.inst.id, s.weight]));
    expect(byId2.D).toBeCloseTo(0.4);
    expect(byId2.C).toBeCloseTo(0.4);
    expect(byId2.A).toBeCloseTo(0.1);
    expect(byId2.B).toBeCloseTo(0.1);
  });
});

describe("dedupeWizardSelectionsByAssetKey", () => {
  it("keeps the first selection when the same asset_key appears under two asset classes", () => {
    const first = holding("CN|cn_exchange_fund|sz|159007", "equity", "domestic");
    const second = holding("CN|cn_exchange_fund|sz|159007", "bond", "domestic");
    const out = dedupeWizardSelectionsByAssetKey([first, second]);
    expect(out).toEqual([first]);
    expect(out[0]?.inst.asset_class).toBe("equity");
    const keys = out.map((s) => s.inst.id);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("keeps the first selection when the same asset_key appears under two regions", () => {
    const first = holding("CN|cn_exchange_fund|sz|159007", "equity", "domestic");
    const second = holding("CN|cn_exchange_fund|sz|159007", "equity", "foreign");
    const out = dedupeWizardSelectionsByAssetKey([first, second]);
    expect(out).toEqual([first]);
    expect(out[0]?.inst.region).toBe("domestic");
  });

  it("keeps distinct asset_keys untouched in order", () => {
    const a = holding("A", "equity", "domestic");
    const b = holding("B", "bond", "domestic");
    expect(dedupeWizardSelectionsByAssetKey([a, b])).toEqual([a, b]);
  });
});

describe("addInstrumentToGroup", () => {
  const asset = (id: string): WizardAsset => ({
    id,
    code: id,
    name: id,
    asset_class: "equity",
    region: "domestic",
    has_history: true,
  });

  it("adds a new asset and redistributes auto weights", () => {
    const out = addInstrumentToGroup(
      [{ inst: asset("A"), weight: 1, amount: 0, weightManual: false }],
      asset("B"),
    );
    expect(out.map((s) => s.inst.id)).toEqual(["A", "B"]);
    expect(out.map((s) => s.weight)).toEqual([0.5, 0.5]);
  });

  it("is a no-op for an asset_key already in the group: no growth, no reweighting", () => {
    const items: WizardHoldingSelection[] = [
      { inst: asset("A"), weight: 0.7, amount: 0, weightManual: true },
      { inst: asset("B"), weight: 0.3, amount: 0, weightManual: false },
    ];
    const out = addInstrumentToGroup(items, asset("A"));
    expect(out).toBe(items);
    expect(out).toHaveLength(2);
    expect(out.map((s) => s.weight)).toEqual([0.7, 0.3]);
  });
});

describe("computeExpectedAmountMinor", () => {
  it("computes three-factor nested allocation amount", () => {
    const totalMinor = 400_000_000_00;
    expect(computeExpectedAmountMinor(totalMinor, 0.45, 0.3, 1.0)).toBe(54_000_000_00);
  });

  it("computes two-factor when region weight is 1", () => {
    const totalMinor = 400_000_000_00;
    expect(computeExpectedAmountMinor(totalMinor, 0.2, 1, 0.5)).toBe(40_000_000_00);
  });
});

describe("buildWizardPortfolioReview", () => {
  const postFire = [
    { asset_class: "equity", weight: 0.55 },
    { asset_class: "bond", weight: 0.35 },
    { asset_class: "cash", weight: 0.1 },
  ];

  const regionTargets = defaultWizardRegionTargets();

  it("satisfies cash class implicitly without cash instruments", () => {
    const review = buildWizardPortfolioReview({
      scenarioWeights: postFire,
      regionTargets,
      selectedInstruments: [
        {
          inst: {
            id: "ins_1",
            code: "510300",
            name: "沪深300ETF",
            asset_class: "equity",
            region: "domestic",
          } as never,
          weight: 1,
          amount: 550_000_00,
        },
        {
          inst: {
            id: "ins_2",
            code: "B1",
            name: "短债",
            asset_class: "bond",
            region: "domestic",
          } as never,
          weight: 1,
          amount: 350_000_00,
        },
      ],
      totalAssetsMinor: 1_000_000_00,
      gapToCash: true,
      assetGapMinor: 100_000_00,
      implicitCash: true,
    });

    expect(review.passed).toBe(true);
    expect(review.rows.some((r) => r.isVirtualCash)).toBe(true);
  });

  it("flags missing bond when only equity and gap cash are configured", () => {
    const review = buildWizardPortfolioReview({
      scenarioWeights: postFire,
      regionTargets,
      selectedInstruments: [
        {
          inst: {
            id: "ins_1",
            code: "510300",
            name: "沪深300ETF",
            asset_class: "equity",
            region: "domestic",
          } as never,
          weight: 1,
          amount: 650_000_00,
        },
      ],
      totalAssetsMinor: 1_000_000_00,
      gapToCash: true,
      assetGapMinor: 100_000_00,
    });

    expect(review.passed).toBe(false);
    expect(review.missingClasses).toHaveLength(1);
    expect(review.missingClasses[0]?.assetClass).toBe("bond");
    expect(review.message).toContain("还缺少");
    expect(review.message).toContain("债券");
    // Batch D 术语统一：提示应引导用户调整「配置模板」而非旧称「场景配置」。
    expect(review.message).toContain("调整配置模板");
    expect(review.message).not.toContain("场景");
    expect(review.rows).toHaveLength(2);
  });

  it("applies region_targets to portfolio target weight", () => {
    const customTargets = {
      equity: { domestic: 0.7, foreign: 0.3 },
      bond: { domestic: 1, foreign: 0 },
    };
    const review = buildWizardPortfolioReview({
      scenarioWeights: [
        { asset_class: "equity", weight: 0.45 },
        { asset_class: "bond", weight: 0.45 },
        { asset_class: "cash", weight: 0.1 },
      ],
      regionTargets: customTargets,
      selectedInstruments: [
        {
          inst: {
            id: "ins_foreign",
            code: "006075",
            name: "标普500联接",
            asset_class: "equity",
            region: "foreign",
          } as never,
          weight: 1,
          amount: 0,
        },
        {
          inst: {
            id: "ins_domestic",
            code: "510300",
            name: "沪深300ETF",
            asset_class: "equity",
            region: "domestic",
          } as never,
          weight: 1,
          amount: 0,
        },
        {
          inst: {
            id: "ins_bond",
            code: "B1",
            name: "短债",
            asset_class: "bond",
            region: "domestic",
          } as never,
          weight: 1,
          amount: 0,
        },
        {
          inst: {
            id: "ins_cash",
            code: "CASH1",
            name: "货币基金",
            asset_class: "cash",
            region: "domestic",
          } as never,
          weight: 1,
          amount: 0,
        },
      ],
      totalAssetsMinor: 400_000_000_00,
      gapToCash: false,
      assetGapMinor: 0,
    });

    const foreignRow = review.rows.find((r) => r.instrumentCode === "006075");
    expect(foreignRow?.portfolioTargetWeight).toBeCloseTo(0.135, 4);
    expect(foreignRow?.targetAmountMinor).toBe(54_000_000_00);
    expect(review.passed).toBe(true);
  });
});

describe("isWizardRegionEnabled", () => {
  const targets = { equity: { domestic: 0, foreign: 1 }, bond: { domestic: 1, foreign: 0 } };
  it("requires both class weight and region target above eps", () => {
    expect(isWizardRegionEnabled(0.6, targets, "equity", "domestic")).toBe(false);
    expect(isWizardRegionEnabled(0.6, targets, "equity", "foreign")).toBe(true);
    expect(isWizardRegionEnabled(0.4, targets, "bond", "domestic")).toBe(true);
    expect(isWizardRegionEnabled(0.4, targets, "bond", "foreign")).toBe(false);
    expect(isWizardRegionEnabled(0, targets, "equity", "foreign")).toBe(false);
  });
  it("treats cash as domestic-only", () => {
    expect(isWizardRegionEnabled(0.1, targets, "cash", "domestic")).toBe(true);
    expect(isWizardRegionEnabled(0.1, targets, "cash", "foreign")).toBe(false);
  });
});

describe("pruneSelectedByRegionTargets", () => {
  it("drops domestic holdings when domestic target is 0%", () => {
    const targets = { equity: { domestic: 0, foreign: 1 }, bond: { domestic: 1, foreign: 0 } };
    const { selected, removed } = pruneSelectedByRegionTargets(
      [holding("dom", "equity", "domestic"), holding("for", "equity", "foreign")],
      targets,
    );
    expect(selected.map((s) => s.inst.id)).toEqual(["for"]);
    expect(removed.map((s) => s.inst.id)).toEqual(["dom"]);
  });
  it("drops foreign holdings when foreign target is 0% (mirror)", () => {
    const targets = { equity: { domestic: 1, foreign: 0 }, bond: { domestic: 1, foreign: 0 } };
    const { selected, removed } = pruneSelectedByRegionTargets(
      [holding("dom", "equity", "domestic"), holding("for", "equity", "foreign")],
      targets,
    );
    expect(selected.map((s) => s.inst.id)).toEqual(["dom"]);
    expect(removed.map((s) => s.inst.id)).toEqual(["for"]);
  });
  it("keeps both directions when 70/30", () => {
    const targets = { equity: { domestic: 0.7, foreign: 0.3 }, bond: { domestic: 1, foreign: 0 } };
    const { selected, removed } = pruneSelectedByRegionTargets(
      [holding("dom", "equity", "domestic"), holding("for", "equity", "foreign")],
      targets,
    );
    expect(selected).toHaveLength(2);
    expect(removed).toHaveLength(0);
  });
});

describe("getWizardAllocationGroups", () => {
  const weights = [
    { asset_class: "equity", weight: 0.6 },
    { asset_class: "bond", weight: 0.3 },
    { asset_class: "cash", weight: 0.1 },
  ];
  it("creates only enabled-region groups (domestic 0 / foreign 100)", () => {
    const groups = getWizardAllocationGroups(weights, {
      equity: { domestic: 0, foreign: 1 },
      bond: { domestic: 1, foreign: 0 },
    });
    const keys = groups.map((g) => g.key);
    expect(keys).toContain("equity-foreign");
    expect(keys).not.toContain("equity-domestic");
    expect(keys).toContain("bond-domestic");
    expect(keys).not.toContain("bond-foreign");
    expect(keys).toContain("cash");
  });
  it("creates both groups when split 70/30", () => {
    const groups = getWizardAllocationGroups(weights, {
      equity: { domestic: 0.7, foreign: 0.3 },
      bond: { domestic: 1, foreign: 0 },
    });
    const keys = groups.map((g) => g.key);
    expect(keys).toContain("equity-domestic");
    expect(keys).toContain("equity-foreign");
  });
  it("creates no group for a zero-weight asset class", () => {
    const groups = getWizardAllocationGroups(
      [
        { asset_class: "equity", weight: 0 },
        { asset_class: "bond", weight: 0.9 },
        { asset_class: "cash", weight: 0.1 },
      ],
      defaultWizardRegionTargets(),
    );
    expect(groups.some((g) => g.assetClass === "equity")).toBe(false);
  });
});

describe("buildRegionTargetsPayload", () => {
  it("emits six region_targets entries", () => {
    const payload = buildRegionTargetsPayload(defaultWizardRegionTargets());
    expect(payload).toHaveLength(6);
    expect(payload.filter((t) => t.asset_class === "equity")).toHaveLength(2);
    expect(payload.find((t) => t.asset_class === "cash" && t.region === "domestic")?.weight_within_class).toBe(1);
  });
});
