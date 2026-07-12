import { describe, expect, it } from "vitest";
import { researchItemInputFromAsset, type ResearchAssetView } from "./research";

describe("researchItemInputFromAsset", () => {
  it("does not copy a legacy screener history dimension into add-item requests", () => {
    const asset = {
      asset_key: "CN|cn_exchange_fund|sz|159612",
      adjust_policy: "none",
      point_type: "adjusted_close",
    } as ResearchAssetView;

    expect(researchItemInputFromAsset(asset)).toEqual({
      asset_key: "CN|cn_exchange_fund|sz|159612",
      weight: 0,
      enabled: true,
    });
  });
});
