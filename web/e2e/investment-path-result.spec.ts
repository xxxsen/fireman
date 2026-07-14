import { expect, test } from "@playwright/test";

function envelope(data: unknown) {
  return { code: "ok", message: "", data, request_id: "e2e-investment-path" };
}

const primary = {
  strategy_key: "income_dca",
  window_start: "2021-01-15",
  window_end: "2022-01-14",
  total_contribution_minor: 12_000_000,
  terminal_value_minor: 13_200_000,
  profit_minor: 1_200_000,
  xirr: 0.201,
  twr_total: 0.11,
  twr_annualized: 0.11,
  max_drawdown: -0.12,
  max_drawdown_start: "2021-03-01",
  max_drawdown_end: "2021-04-01",
  longest_underwater_days: 45,
  max_principal_deficit_minor: 300_000,
  max_principal_deficit_ratio: 0.1,
  longest_below_principal_days: 30,
  first_recovery_above_principal_date: "2021-02-01",
  average_cash_weight: 0,
  total_transaction_cost_minor: 12_000,
  trade_count: 12,
  turnover: 1,
};

const points = [
  { strategy_key: "income_dca", valuation_date: "2021-01-15", account_value_minor: 1_000_000, asset_value_minor: 999_000, cash_value_minor: 1_000, cumulative_external_contribution_minor: 1_000_000, unit_nav: 0.999, drawdown: -0.001 },
  { strategy_key: "income_dca", valuation_date: "2021-07-15", account_value_minor: 6_500_000, asset_value_minor: 6_500_000, cash_value_minor: 0, cumulative_external_contribution_minor: 6_000_000, unit_nav: 1.08, drawdown: 0 },
  { strategy_key: "income_dca", valuation_date: "2022-01-14", account_value_minor: 13_200_000, asset_value_minor: 13_200_000, cash_value_minor: 0, cumulative_external_contribution_minor: 12_000_000, unit_nav: 1.11, drawdown: 0 },
];

test("investment path result explains metrics and exposes chart coordinates", async ({ page }, testInfo) => {
  await page.route("**/api/v1/research/investment-path-runs/ipr_chart**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith("/points")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ points })) });
      return;
    }
    if (url.pathname.endsWith("/trades")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ trades: [] })) });
      return;
    }
    if (url.pathname.endsWith("/windows")) {
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(envelope({ windows: [primary] })) });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(envelope({
        id: "ipr_chart",
        task_id: "task_chart",
        asset_key: "CN|cn_mutual_fund||270042",
        mode: "income_dca",
        input_hash: "input",
        source_hash: "source",
        input_snapshot_json: "{}",
        engine_version: "single_asset_investment_path_v2",
        base_currency: "CNY",
        evaluation_start: "2021-01-01",
        evaluation_end: "2022-01-31",
        primary_start: "2021-01-15",
        primary_end: "2022-01-14",
        horizon_months: 12,
        created_at: 0,
        completed_at: 1,
        task: { id: "task_chart", status: "complete", progress_current: 25, progress_total: 25, phase: "" },
        strategies: ["income_dca"],
        summary: {
          primary: [primary],
          aggregates: [{
            strategy_key: "income_dca",
            window_count: 1,
            terminal_value_minor: { p10: 13_200_000, p50: 13_200_000, p90: 13_200_000 },
            xirr: { p10: 0.201, p50: 0.201, p90: 0.201 },
            xirr_count: 1,
            max_drawdown: { p10: -0.12, p50: -0.12, p90: -0.12 },
            best_start: "2021-01-15",
            worst_start: "2021-01-15",
          }],
        },
        data_quality: { source_start: "2021-01-15", source_end: "2022-01-14" },
      })),
    });
  });

  await page.goto("/research/investment-paths/runs/ipr_chart");

  await expect(page.getByText("横轴：估值日期 · 纵轴：账户金额（CNY）")).toBeVisible();
  await expect(page.getByText("日期", { exact: true })).toBeVisible();
  await expect(page.getByText("账户金额（CNY）", { exact: true })).toBeVisible();

  const hitArea = page.getByTestId("investment-path-chart-hit-area");
  const box = await hitArea.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.move(box!.x + box!.width - 2, box!.y + box!.height / 2);
  const pointTooltip = page.getByTestId("investment-path-chart-tooltip");
  await expect(pointTooltip).toContainText("2022-01-14");
  await expect(pointTooltip).toContainText("账户价值");
  await expect(pointTooltip).toContainText("¥132,000.00");
  await expect(pointTooltip).toContainText("累计投入");

  await page.getByRole("button", { name: "查看「投入路径 XIRR」说明" }).first().click();
  await expect(page.getByRole("tooltip")).toContainText("使净现值为零");

  await page.screenshot({ path: testInfo.outputPath("investment-path-result-chart.png"), fullPage: true });
});
