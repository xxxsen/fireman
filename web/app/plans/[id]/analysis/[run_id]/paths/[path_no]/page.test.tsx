// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
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
  rebalanced: true,
  asset_weights: { equity: 0.7, bond: 0.3 },
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
    expect(screen.getByText("年末权重")).toBeInTheDocument();
  });

  it("renders first/last monthly rows and all money in ¥xx.xxw format (td/056 §2.2/§3.1)", async () => {
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

  it("renders yearly weights as 名称（代码） / 现金 / 未知资产, never holding IDs (td/056 §3.2)", async () => {
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

    expect(screen.getByText(/沪深300ETF（510300）: 70\.00%/)).toBeInTheDocument();
    expect(screen.getByText(/现金\/其他: 20\.00%/)).toBeInTheDocument();
    // Missing label degrades to 未知资产, never a holding UUID.
    expect(screen.getByText(/未知资产: 10\.00%/)).toBeInTheDocument();
    expect(screen.queryByText(/hold_/)).toBeNull();
  });

  it("shows empty state when monthly/yearly are empty (td/056 §3.1)", async () => {
    mockGetPathDetail.mockResolvedValue({
      path_no: 0,
      path_seed: "1",
      succeeded: false,
      failure_month: 12,
      failure_reason: "early_sequence_risk",
      monthly: [],
      yearly: [],
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <PathDetailPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/早期序列风险/)).toBeInTheDocument();
    expect(screen.getByText("暂无月度路径数据")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /年度 \(0\)/ }));
    expect(screen.getByText("暂无年度路径数据")).toBeInTheDocument();
  });
});
