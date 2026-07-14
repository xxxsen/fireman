import { expect, test } from "@playwright/test";

const LONG_ASSET_NAME =
  "博时标普石油天然气勘探及生产精选行业指数发起(QDII)A美元现汇";

const marketAsset = {
  asset_key: "CN|cn_mutual_fund||018852",
  market: "CN",
  instrument_type: "cn_mutual_fund",
  instrument_type_label: "公募基金",
  instrument_type_priority: 0,
  region_code: "",
  symbol: "018852",
  name: LONG_ASSET_NAME,
  exchange: "",
  instrument_kind: "QDII",
  currency: "USD",
  active: true,
  listing_status: "active",
  last_seen_at: 0,
  source_name: "ak.fund_name_em",
  source_as_of: "2026-07-15",
  refreshed_at: 0,
  created_at: 0,
  updated_at: 0,
  has_history: false,
};

function envelope(data: unknown) {
  return { code: "ok", message: "", data, request_id: "e2e-request" };
}

test("wide asset picker displays a long asset name without truncation", async ({
  page,
}, testInfo) => {
  await page.route("**/api/v1/research/investment-path-runs?*", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ runs: [] })) }),
  );
  await page.route("**/api/v1/market-assets?*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({ assets: [marketAsset], syncs: [], total: 1 })),
    }),
  );

  await page.goto("/research/investment-paths");
  await page.getByTestId("choose-investment-path-asset").click();

  const dialog = page.getByTestId("dialog");
  const assetName = dialog.getByText(LONG_ASSET_NAME, { exact: true });
  await expect(dialog).toBeVisible();
  await expect(assetName).toBeVisible();

  const dialogBox = await dialog.boundingBox();
  expect(dialogBox?.width).toBeGreaterThanOrEqual(1000);

  const nameWidth = await assetName.evaluate((element) => ({
    clientWidth: element.clientWidth,
    scrollWidth: element.scrollWidth,
  }));
  expect(nameWidth.scrollWidth).toBeLessThanOrEqual(nameWidth.clientWidth);

  await page.screenshot({
    path: testInfo.outputPath("asset-picker-wide.png"),
    fullPage: true,
  });
});
