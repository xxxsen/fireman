// @vitest-environment jsdom
import { fireEvent, render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1", run_id: "run_1", path_no: "0" }),
}));

const monthly = Array.from({ length: 240 }, (_, i) => ({
  month_offset: i,
  total_wealth_minor: 1_000_000_00 - i * 1000_00,
  income_minor: 10_000_00,
  spending_minor: 20_000_00,
  tax_minor: 0,
  transaction_cost: 100_00,
  drawdown: 0.01 * (i % 10),
  rebalanced: i % 12 === 0,
  cum_inflation: 1 + i * 0.001,
  real_total_wealth_minor: Math.round((1_000_000_00 - i * 1000_00) * 0.5),
}));

const yearly = Array.from({ length: 20 }, (_, i) => ({
  year: 2026 + i,
  start_wealth_minor: 1_000_000_00,
  income_minor: 120_000_00,
  spending_minor: 240_000_00,
  tax_minor: 0,
  transaction_cost: 1_200_00,
  investment_gain_loss: 50_000_00,
  end_wealth_minor: 900_000_00,
  year_end_drawdown: 0.05,
  max_intra_year_dd: 0.08,
  annual_return: 0.0512,
  rebalanced: true,
  asset_weights: { equity: 0.7, bond: 0.3 },
  cum_inflation: 1.2,
  real_start_wealth_minor: 800_000_00,
  real_end_wealth_minor: 450_000_00,
}));

const mockGetPathDetail = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/simulations", () => ({
  getPathDetail: mockGetPathDetail,
}));

import PathDetailPage from "./page";

describe("PathDetailPage", () => {
  it("shows all monthly rows and yearly design fields", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "9223372036854775807",
      succeeded: true,
      monthly,
      yearly,
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText(/月度 \(240\)/)).toBeInTheDocument();
    expect(screen.getByText(/共 240 行/)).toBeInTheDocument();
    expect(screen.getByText("9223372036854775807")).toBeInTheDocument();
    expect(screen.getByText("交易成本")).toBeInTheDocument();
    expect(screen.getByText("调仓")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /年度 \(20\)/ }));
    expect(screen.getByText(/共 20 行/)).toBeInTheDocument();
    expect(screen.getByText("投资损益")).toBeInTheDocument();
    expect(screen.getByText("年内最大回撤")).toBeInTheDocument();
    // 年末收益率 added next to the retained 年末回撤 column.
    expect(screen.getByRole("columnheader", { name: "年末收益率" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /年末回撤/ })).toBeInTheDocument();
    expect(screen.getAllByText("5.12%").length).toBeGreaterThan(0);
    // Weights collapsed under 年末配置 / per-row 查看 controls.
    expect(screen.getByRole("columnheader", { name: "年末配置" })).toBeInTheDocument();
    expect(screen.queryByText("年末权重")).toBeNull();
    expect(screen.getAllByRole("button", { name: /查看 \d+ 年末资产配置/ })).toHaveLength(20);
  });

  it("toggles wealth columns between nominal and real purchasing power", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: true,
      monthly,
      yearly,
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );

    await screen.findByText(/月度 \(240\)/);
    // Default nominal: terminal card shows last nominal wealth ¥76.10w.
    expect(screen.getByRole("columnheader", { name: "资产（名义金额）" })).toBeInTheDocument();
    expect(screen.getAllByText("¥76.10w").length).toBeGreaterThanOrEqual(1);

    fireEvent.click(screen.getByRole("button", { name: "起点购买力" }));
    // Real: last real wealth = round(76,100,000 * 0.5) = 38,050,000 -> ¥38.05w.
    expect(screen.getByRole("columnheader", { name: "资产（起点购买力）" })).toBeInTheDocument();
    expect(screen.getAllByText("¥38.05w").length).toBeGreaterThanOrEqual(1);

    fireEvent.click(screen.getByRole("button", { name: /年度 \(20\)/ }));
    expect(screen.getByRole("columnheader", { name: "年初资产（起点购买力）" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "期末资产（起点购买力）" })).toBeInTheDocument();
    // Real year-end wealth 450,000,00 -> ¥45.00w (distinct from nominal ¥90.00w).
    expect(screen.getAllByText("¥45.00w").length).toBeGreaterThan(0);
  });

  it("renders first/last monthly rows and all money in ¥xx.xxw format", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: true,
      monthly,
      yearly,
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );

    // Terminal wealth (top) uses the last monthly row: 76,100,000 minor -> ¥76.10w.
    // Appears at the top summary and in the last monthly row.
    expect((await screen.findAllByText("¥76.10w")).length).toBeGreaterThanOrEqual(1);
    // First monthly row asset: 100,000,000 minor -> ¥100.00w (no thousands separators).
    expect(screen.getAllByText("¥100.00w").length).toBeGreaterThan(0);
    // No legacy comma-grouped currency anywhere.
    expect(screen.queryByText(/¥[\d,]{4,}\.\d{2}(?!w)/)).toBeNull();
  });

  it("opens year-end weights via 查看 tooltip with graceful labels, never holding IDs", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: true,
      monthly,
      yearly: [
        {
          ...yearly[0],
          asset_weights: { hold_eq: 0.7, hold_cash: 0.2, hold_legacy: 0.1 },
        },
      ],
      asset_labels: {
        hold_eq: {
          instrument_name: "沪深300ETF",
          instrument_code: "510300",
          asset_class: "equity",
          is_cash: false,
        },
        hold_cash: {
          instrument_name: "现金",
          instrument_code: "CASH",
          asset_class: "cash",
          is_cash: true,
        },
      },
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByText(/月度 \(240\)/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /年度 \(1\)/ }));

    // Weights are not inline; they sit behind a labeled 查看 control.
    expect(screen.queryByText(/沪深300ETF（510300）: 70\.00%/)).toBeNull();
    const viewBtn = screen.getByRole("button", { name: "查看 2026 年末资产配置" });

    // Hover opens the tooltip.
    fireEvent.mouseEnter(viewBtn);
    expect(screen.getByText(/沪深300ETF（510300）: 70\.00%/)).toBeInTheDocument();
    expect(screen.getByText(/现金\/其他: 20\.00%/)).toBeInTheDocument();
    // Missing label degrades to 未知资产, never a holding UUID.
    expect(screen.getByText(/未知资产: 10\.00%/)).toBeInTheDocument();
    expect(screen.queryByText(/hold_/)).toBeNull();
  });

  it("year-end config control opens on keyboard focus and click", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: true,
      monthly,
      yearly: [{ ...yearly[0], asset_weights: { hold_eq: 1 } }],
      asset_labels: {
        hold_eq: {
          instrument_name: "沪深300ETF",
          instrument_code: "510300",
          asset_class: "equity",
          is_cash: false,
        },
      },
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/月度 \(240\)/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /年度 \(1\)/ }));

    const viewBtn = screen.getByRole("button", { name: "查看 2026 年末资产配置" });
    // Keyboard focus opens it.
    fireEvent.focus(viewBtn);
    expect(screen.getByText(/沪深300ETF（510300）: 100\.00%/)).toBeInTheDocument();
    // A click actually toggles it closed, then open again.
    fireEvent.click(viewBtn);
    expect(screen.queryByText(/沪深300ETF（510300）: 100\.00%/)).toBeNull();
    fireEvent.click(viewBtn);
    expect(screen.getByText(/沪深300ETF（510300）: 100\.00%/)).toBeInTheDocument();
  });

  it("renders — for a null annual_return", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: true,
      monthly,
      yearly: [{ ...yearly[0], annual_return: null }],
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/月度 \(240\)/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /年度 \(1\)/ }));
    // No 5.12% return is shown; the cell falls back to —.
    expect(screen.queryByText("5.12%")).toBeNull();
    const rows = screen.getAllByRole("row");
    const dataRow = rows[rows.length - 1]!;
    expect(within(dataRow).getAllByText("—").length).toBeGreaterThan(0);
  });

  it("renders — for years without weights and creates no 查看 control", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: true,
      monthly,
      yearly: [{ ...yearly[0], asset_weights: {} }],
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/月度 \(240\)/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /年度 \(1\)/ }));

    expect(screen.queryByRole("button", { name: /年末资产配置/ })).toBeNull();
    const rows = screen.getAllByRole("row");
    const dataRow = rows[rows.length - 1]!;
    expect(within(dataRow).getByText("—")).toBeInTheDocument();
  });

  it("shows empty state when monthly/yearly are empty", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: false,
      failure_month: 12,
      failure_reason: "insufficient_funds",
      monthly: [],
      yearly: [],
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/当月资金不足/)).toBeInTheDocument();
		expect(screen.getByText("失败状态")).toBeInTheDocument();
    expect(screen.getByText("暂无月度路径数据")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /年度 \(0\)/ }));
    expect(screen.getByText("暂无年度路径数据")).toBeInTheDocument();
  });
});
