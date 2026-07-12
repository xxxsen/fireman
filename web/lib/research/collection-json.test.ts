import { describe, expect, it } from "vitest";
import type { ResearchCollectionDetail } from "@/lib/api/research";
import { collectionToJSON, parseCollectionJSON } from "./collection-json";

function detail(): ResearchCollectionDetail {
  return {
    id: "rc_1",
    name: "中美宽基",
    description: "测试",
    base_currency: "CNY",
    initial_amount_minor: 100000000,
    rebalance_policy: "quarterly",
    rebalance_threshold: 0,
    start_policy: "common_intersection",
    window_start: "",
    window_end: "",
    benchmark_asset_key: "CN|cn_exchange_fund|sh|510300",
    risk_free_rate: 0.02,
    transaction_cost_rate: 0,
    status: "active",
    created_at: 1,
    updated_at: 2,
    tags: ["宽基"],
    items: [
      {
        id: "ri_1",
        collection_id: "rc_1",
        asset_key: "CN|cn_exchange_fund|sh|510300",
        enabled: true,
        weight: 0.6,
        weight_locked: true,
        adjust_policy: "hfq",
        point_type: "adjusted_close",
        asset_class: "equity",
        region: "cn",
        note: "核心",
        sort_order: 0,
        created_at: 1,
        updated_at: 2,
        name: "沪深300ETF",
        symbol: "510300",
        market: "cn",
        instrument_type: "cn_exchange_fund",
        instrument_type_label: "场内 ETF / LOF",
        currency: "CNY",
        listing_status: "active",
        is_cash: false,
      },
    ],
  };
}

describe("collection JSON round trip", () => {
  it("exports and re-imports all parameters and items", () => {
    const doc = collectionToJSON(detail());
    expect(doc.format).toBe("fireman.research_collection");
    const parsed = parseCollectionJSON(JSON.stringify(doc));
    expect(parsed.name).toBe("中美宽基");
    expect(parsed.base_currency).toBe("CNY");
    expect(parsed.rebalance_policy).toBe("quarterly");
    expect(parsed.risk_free_rate).toBe(0.02);
    expect(parsed.benchmark_asset_key).toBe("CN|cn_exchange_fund|sh|510300");
    expect(parsed.tags).toEqual(["宽基"]);
    expect(parsed.items).toHaveLength(1);
    expect(parsed.items![0]).toMatchObject({
      asset_key: "CN|cn_exchange_fund|sh|510300",
      weight: 0.6,
      weight_locked: true,
      adjust_policy: "hfq",
      asset_class: "equity",
      region: "cn",
      note: "核心",
    });
  });

  it("accepts a bare collection object without the wrapper", () => {
    const parsed = parseCollectionJSON(
      JSON.stringify({ name: "裸对象", items: [{ asset_key: "CN|a", weight: 1 }] }),
    );
    expect(parsed.name).toBe("裸对象");
    expect(parsed.items).toHaveLength(1);
  });

  it("rejects invalid JSON, missing name, bad enum and bad items", () => {
    expect(() => parseCollectionJSON("{oops")).toThrow("有效的 JSON");
    expect(() => parseCollectionJSON(JSON.stringify({ items: [] }))).toThrow("name");
    expect(() =>
      parseCollectionJSON(JSON.stringify({ name: "x", rebalance_policy: "hourly" })),
    ).toThrow("再平衡");
    expect(() =>
      parseCollectionJSON(JSON.stringify({ name: "x", items: [{ weight: 1 }] })),
    ).toThrow("asset_key");
  });
});
