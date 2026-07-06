import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchAssetView } from "@/lib/api/research";
import { CandidateCompareDialog } from "./CandidateCompareDialog";

const getMarketAssetDetailMock = vi.hoisted(() => vi.fn());
const downloadCsvMock = vi.hoisted(() => vi.fn());

vi.mock("echarts-for-react", () => ({
  default: () => <div data-testid="echarts" />,
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  getMarketAssetDetail: (...args: unknown[]) => getMarketAssetDetailMock(...args),
}));

vi.mock("@/lib/csv", () => ({
  downloadCsv: (...args: unknown[]) => downloadCsvMock(...args),
}));

function candidate(key: string, cagr: number): ResearchAssetView {
  return {
    asset_key: key,
    market: "cn",
    instrument_type: "cn_exchange_fund",
    instrument_type_label: "场内 ETF / LOF",
    region_code: "sh",
    symbol: key,
    name: `资产${key}`,
    exchange: "SSE",
    instrument_kind: "index_etf",
    currency: "CNY",
    active: true,
    listing_status: "active",
    is_cash: false,
    has_history: true,
    adjust_policy: "qfq",
    point_type: "adjusted_close",
    point_count: 300,
    stale: false,
    fx_available: true,
    backtest_ready: true,
    quality_badges: ["normal"],
    metrics: {
      asset_key: key,
      adjust_policy: "qfq",
      point_type: "adjusted_close",
      start_date: "2020-01-01",
      end_date: "2024-01-01",
      point_count: 300,
      history_years: 4,
      cagr,
      annual_volatility: 0.15,
      downside_volatility: 0.1,
      max_drawdown: -0.2,
      sharpe: 0.5,
      calmar: 0.4,
      return_1y: 0.05,
      return_3y: 0.2,
      return_5y: null,
      computed_at: 0,
    },
  };
}

function renderDialog(onAddSelected?: (assets: ResearchAssetView[]) => void) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  getMarketAssetDetailMock.mockResolvedValue({
    points: [
      { trade_date: "2023-01-01", value: 1 },
      { trade_date: "2023-06-01", value: 1.1 },
    ],
  });
  return render(
    <QueryClientProvider client={client}>
      <CandidateCompareDialog
        open
        onClose={() => {}}
        candidates={[candidate("a", 0.1), candidate("b", 0.3)]}
        onRemove={() => {}}
        onAddSelected={onAddSelected}
      />
    </QueryClientProvider>,
  );
}

describe("CandidateCompareDialog", () => {
  beforeEach(() => vi.clearAllMocks());

  it("sorts the metrics table by the chosen metric", async () => {
    renderDialog();
    const table = within(await screen.findByTestId("compare-metrics-table"));
    // Default sort is CAGR descending: b (30%) before a (10%).
    let rows = table.getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("资产b");
    fireEvent.click(table.getByRole("button", { name: /CAGR/ }));
    rows = table.getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("资产a");
  });

  it("exports the comparison as CSV", async () => {
    renderDialog();
    await screen.findByTestId("compare-metrics-table");
    fireEvent.click(screen.getByTestId("compare-export-csv"));
    expect(downloadCsvMock).toHaveBeenCalledWith(
      "research-candidates.csv",
      expect.arrayContaining(["资产", "CAGR"]),
      expect.any(Array),
    );
  });

  it("adds the checked assets through onAddSelected", async () => {
    const onAddSelected = vi.fn();
    renderDialog(onAddSelected);
    await screen.findByTestId("compare-metrics-table");
    const addButton = screen.getByTestId("compare-add-selected");
    expect(addButton).toBeDisabled();
    fireEvent.click(screen.getByTestId("compare-select-a"));
    fireEvent.click(addButton);
    expect(onAddSelected).toHaveBeenCalledTimes(1);
    expect(onAddSelected.mock.calls[0]![0].map((c: ResearchAssetView) => c.asset_key)).toEqual([
      "a",
    ]);

    fireEvent.click(screen.getByTestId("compare-select-all"));
    fireEvent.click(addButton);
    expect(onAddSelected.mock.calls[1]![0]).toHaveLength(2);
  });

  it("hides the multi-select controls without onAddSelected", async () => {
    renderDialog();
    await screen.findByTestId("compare-metrics-table");
    expect(screen.queryByTestId("compare-add-selected")).not.toBeInTheDocument();
    expect(screen.queryByTestId("compare-select-all")).not.toBeInTheDocument();
  });
});
