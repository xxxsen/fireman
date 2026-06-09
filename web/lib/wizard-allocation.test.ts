import { describe, expect, it } from "vitest";
import { buildWizardPortfolioReview } from "./wizard-allocation";

describe("buildWizardPortfolioReview", () => {
  const postFire = [
    { asset_class: "equity", weight: 0.55 },
    { asset_class: "bond", weight: 0.35 },
    { asset_class: "cash", weight: 0.1 },
  ];

  it("flags missing bond when only equity and gap cash are configured", () => {
    const review = buildWizardPortfolioReview({
      scenarioWeights: postFire,
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
});
