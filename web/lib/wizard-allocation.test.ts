import { describe, expect, it } from "vitest";
import {
  buildRegionTargetsPayload,
  buildWizardPortfolioReview,
  complementRegionWeight,
  computeExpectedAmountMinor,
  defaultWizardRegionTargets,
  redistributeGroupWeights,
  updateInstrumentWeightInGroup,
} from "./wizard-allocation";
import type { WizardHoldingSelection } from "./wizard-allocation";

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

describe("buildRegionTargetsPayload", () => {
  it("emits six region_targets entries", () => {
    const payload = buildRegionTargetsPayload(defaultWizardRegionTargets());
    expect(payload).toHaveLength(6);
    expect(payload.filter((t) => t.asset_class === "equity")).toHaveLength(2);
    expect(payload.find((t) => t.asset_class === "cash" && t.region === "domestic")?.weight_within_class).toBe(1);
  });
});
