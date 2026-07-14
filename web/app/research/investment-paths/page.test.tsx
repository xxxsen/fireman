import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MarketAsset, MarketAssetDetail } from "@/lib/api/market-assets";
import InvestmentPathsPage from "./page";

const getMarketAssetDetailMock = vi.hoisted(() => vi.fn());
const syncMarketAssetHistoryMock = vi.hoisted(() => vi.fn());
const listInvestmentPathRunsMock = vi.hoisted(() => vi.fn());
const investmentPathReadinessMock = vi.hoisted(() => vi.fn());
const createInvestmentPathRunMock = vi.hoisted(() => vi.fn());
const useTaskStatusMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

const catalogAsset: MarketAsset = {
  asset_key: "CN|cn_exchange_fund|sh|510300",
  market: "CN",
  instrument_type: "cn_exchange_fund",
  region_code: "sh",
  symbol: "510300",
  name: "沪深300ETF",
  exchange: "SH",
  instrument_kind: "etf",
  currency: "CNY",
  active: true,
  listing_status: "active",
  last_seen_at: 0,
  source_name: "ak.fund_etf_spot_em",
  source_as_of: "",
  refreshed_at: 0,
  created_at: 0,
  updated_at: 0,
};

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPushMock }),
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  getMarketAssetDetail: (...args: unknown[]) => getMarketAssetDetailMock(...args),
  syncMarketAssetHistory: (...args: unknown[]) => syncMarketAssetHistoryMock(...args),
}));

vi.mock("@/lib/api/investment-paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/investment-paths")>()),
  listInvestmentPathRuns: (...args: unknown[]) => listInvestmentPathRunsMock(...args),
  investmentPathReadiness: (...args: unknown[]) => investmentPathReadinessMock(...args),
  createInvestmentPathRun: (...args: unknown[]) => createInvestmentPathRunMock(...args),
}));

vi.mock("@/hooks/useTaskStatus", () => ({
  useTaskStatus: (...args: unknown[]) => useTaskStatusMock(...args),
}));

vi.mock("@/components/plans/MarketAssetPickerDialog", () => ({
  MarketAssetPickerDialog: ({
    open,
    allowCash,
    onSelect,
  }: {
    open: boolean;
    allowCash?: boolean;
    onSelect: (asset: MarketAsset) => void;
  }) =>
    open ? (
      <div data-testid="mock-investment-path-picker" data-allow-cash={String(allowCash)}>
        <button type="button" onClick={() => onSelect(catalogAsset)}>
          选择沪深300ETF
        </button>
      </div>
    ) : null,
}));

function detail(pointCount = 0, task: MarketAssetDetail["history"]["task"] = null) {
  return {
    asset: catalogAsset,
    history: {
      adjust_policy: "hfq",
      point_type: "adjusted_close",
      data_as_of: pointCount > 0 ? "2026-07-01" : "",
      point_count: pointCount,
      source_name: pointCount > 0 ? "ak.fund_etf_hist_em" : "",
      last_success_task_id: "",
      task,
      can_switch_source: false,
    },
    points: [],
    annual_returns: [],
  } satisfies MarketAssetDetail;
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <InvestmentPathsPage />
    </QueryClientProvider>,
  );
}

async function selectCatalogAsset() {
  fireEvent.click(screen.getByTestId("choose-investment-path-asset"));
  expect(screen.getByTestId("mock-investment-path-picker")).toHaveAttribute(
    "data-allow-cash",
    "false",
  );
  fireEvent.click(screen.getByRole("button", { name: "选择沪深300ETF" }));
  return screen.findByTestId("selected-investment-path-asset");
}

describe("InvestmentPathsPage asset history flow", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listInvestmentPathRunsMock.mockResolvedValue({ runs: [] });
    getMarketAssetDetailMock.mockResolvedValue(detail());
    useTaskStatusMock.mockReturnValue({ task: null, pollError: null });
  });

  it("selects an asset without history and creates the canonical history task", async () => {
    syncMarketAssetHistoryMock.mockResolvedValue({
      task: { id: "task_history", status: "pending" },
      existed: false,
    });
    renderPage();
    await selectCatalogAsset();

    expect(screen.getByText(/尚无可用本地历史/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "检查数据与计算预算" })).toBeDisabled();
    fireEvent.click(screen.getByTestId("sync-investment-path-history"));

    await waitFor(() =>
      expect(syncMarketAssetHistoryMock).toHaveBeenCalledWith({
        asset_key: catalogAsset.asset_key,
        adjust_policy: "hfq",
        point_type: "adjusted_close",
        mode: "default_refresh",
      }),
    );
    expect(screen.getByTestId("sync-investment-path-history")).toBeDisabled();
  });

  it("uses the selected history identity for readiness once data exists", async () => {
    getMarketAssetDetailMock.mockResolvedValue(detail(1200));
    investmentPathReadinessMock.mockResolvedValue({
      ready: true,
      issues: [],
      warnings: [],
      resolved: {
        source_start: "2010-01-01",
        source_end: "2026-07-01",
        primary_start: "2021-01-15",
        primary_first_execution_date: "2021-01-15",
        primary_end: "2026-01-15",
        window_starts: ["2021-01-15"],
        strategy_keys: ["income_dca", "income_cash_baseline"],
        path_day_budget: 2000,
      },
    });
    renderPage();
    await selectCatalogAsset();

    const readinessButton = screen.getByRole("button", {
      name: "检查数据与计算预算",
    });
    expect(readinessButton).toBeEnabled();
    fireEvent.click(readinessButton);
    await waitFor(() => expect(investmentPathReadinessMock).toHaveBeenCalled());
    expect(investmentPathReadinessMock.mock.calls[0]![0].asset).toEqual({
      asset_key: catalogAsset.asset_key,
      adjust_policy: "hfq",
      point_type: "adjusted_close",
    });
    expect(await screen.findByText("可以运行")).toBeInTheDocument();
  });

  it("keeps history actions locked while an existing sync task is active", async () => {
    getMarketAssetDetailMock.mockResolvedValue(
      detail(0, { id: "task_active", status: "running" } as never),
    );
    renderPage();
    await selectCatalogAsset();

    expect(screen.getByText(/历史数据拉取中/)).toBeInTheDocument();
    expect(screen.getByTestId("sync-investment-path-history")).toBeDisabled();
    expect(screen.getByRole("button", { name: "检查数据与计算预算" })).toBeDisabled();
  });

  it("shows a retryable error when history task creation fails", async () => {
    syncMarketAssetHistoryMock.mockRejectedValue(new Error("provider unavailable"));
    renderPage();
    await selectCatalogAsset();
    fireEvent.click(screen.getByTestId("sync-investment-path-history"));

    expect(await screen.findByText("历史数据拉取未完成")).toBeInTheDocument();
    expect(screen.getByText("provider unavailable")).toBeInTheDocument();
    expect(screen.getByTestId("sync-investment-path-history")).toBeEnabled();
  });
});
