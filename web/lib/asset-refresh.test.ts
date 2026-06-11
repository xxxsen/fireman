import { describe, expect, it } from "vitest";
import {
  buildAssetRefreshBody,
  validateAssetRefreshTotal,
} from "./asset-refresh";

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
