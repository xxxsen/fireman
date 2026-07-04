// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MarketAssetDetail } from "@/lib/api/market-assets";

const getMarketAssetDetailMock = vi.hoisted(() => vi.fn());
const syncMarketAssetHistoryMock = vi.hoisted(() => vi.fn());
const useWorkerTaskPollingMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useParams: () => ({ assetKey: encodeURIComponent("cn:cn_exchange_fund:sh:510300") }),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  getMarketAssetDetail: (...args: unknown[]) => getMarketAssetDetailMock(...args),
  syncMarketAssetHistory: (...args: unknown[]) => syncMarketAssetHistoryMock(...args),
}));

vi.mock("@/hooks/useWorkerTaskPolling", () => ({
  useWorkerTaskPolling: (...args: unknown[]) => useWorkerTaskPollingMock(...args),
}));

vi.mock("@/components/charts/ReturnSeriesChart", () => ({
  ReturnSeriesChart: () => <div data-testid="return-series-chart" />,
}));

import MarketAssetDetailPage from "./page";

const ASSET_KEY = "cn:cn_exchange_fund:sh:510300";

function makeDetail(overrides: Partial<MarketAssetDetail> = {}): MarketAssetDetail {
  return {
    asset: {
      asset_key: ASSET_KEY,
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
    },
    history: {
      adjust_policy: "none",
      point_type: "adjusted_close",
      source_name: "ak.fund_etf_hist_em",
      data_as_of: "2026-07-01",
      point_count: 3,
      last_success_at: 1751300000000,
      last_success_task_id: "wt_prev",
      can_switch_source: false,
      task: null,
    },
    points: [
      { date: "2026-06-27", value: 4.0 },
      { date: "2026-06-30", value: 4.1 },
      { date: "2026-07-01", value: 4.2 },
    ],
    annual_returns: [
      {
        year: 2025,
        annual_return: 0.08,
        is_partial: false,
        start_date: "2025-01-02",
        end_date: "2025-12-31",
        start_value: 3.8,
        end_value: 4.1,
        observations: 242,
      },
    ],
    ...overrides,
  };
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MarketAssetDetailPage />
    </QueryClientProvider>,
  );
}

describe("MarketAssetDetailPage", () => {
  beforeEach(() => {
    getMarketAssetDetailMock.mockReset();
    syncMarketAssetHistoryMock.mockReset();
    useWorkerTaskPollingMock.mockReset();
    useWorkerTaskPollingMock.mockReturnValue({ task: null, pollError: null, isActive: false });
    getMarketAssetDetailMock.mockResolvedValue(makeDetail());
  });

  it("renders asset info, history state and the chart", async () => {
    renderPage();
    expect(await screen.findByRole("heading", { name: "沪深300ETF" })).toBeInTheDocument();
    expect(getMarketAssetDetailMock).toHaveBeenCalledWith(ASSET_KEY);

    const panel = screen.getByTestId("history-state-panel");
    expect(within(panel).getByTestId("history-data-as-of")).toHaveTextContent("2026-07-01");
    expect(within(panel).getByTestId("history-point-count")).toHaveTextContent("3");
    expect(within(panel).getByText("无进行中任务")).toBeInTheDocument();

    expect(screen.getByTestId("return-series-chart")).toBeInTheDocument();
    expect(screen.getByTestId("annual-returns-table")).toBeInTheDocument();
    expect(screen.getByTestId("import-from-detail")).toHaveAttribute(
      "href",
      `/assets/import?asset_key=${encodeURIComponent(ASSET_KEY)}`,
    );
  });

  it("shows the no-history empty state before the first sync", async () => {
    getMarketAssetDetailMock.mockResolvedValue(
      makeDetail({
        history: {
          ...makeDetail().history,
          point_count: 0,
          data_as_of: "",
          source_name: "",
          last_success_at: null,
        },
        points: [],
        annual_returns: [],
      }),
    );
    renderPage();
    expect(await screen.findByText("尚未同步历史数据")).toBeInTheDocument();
    expect(screen.queryByTestId("market-asset-chart")).not.toBeInTheDocument();
  });

  it("creates a default_refresh history task from the refresh button", async () => {
    syncMarketAssetHistoryMock.mockResolvedValue({
      task: { id: "wt_h1", type: "asset_history_sync", status: "pending", created_at: 0 },
      existed: false,
    });
    renderPage();
    await screen.findByTestId("history-state-panel");

    fireEvent.click(screen.getByTestId("refresh-history-button"));
    await waitFor(() =>
      expect(syncMarketAssetHistoryMock).toHaveBeenCalledWith({
        asset_key: ASSET_KEY,
        adjust_policy: "none",
        point_type: "adjusted_close",
        mode: "default_refresh",
      }),
    );
  });

  it("only offers switch_source_full when the escape hatch is unlocked", async () => {
    renderPage();
    await screen.findByTestId("history-state-panel");
    expect(screen.queryByTestId("switch-source-button")).not.toBeInTheDocument();
  });

  it("offers the switch-source button after a source_unavailable failure", async () => {
    getMarketAssetDetailMock.mockResolvedValue(
      makeDetail({
        history: {
          ...makeDetail().history,
          can_switch_source: true,
          task: {
            id: "wt_failed",
            type: "asset_history_sync",
            status: "failed",
            error_code: "source_unavailable",
            error_message: "pinned source rejected the symbol",
            created_at: 0,
          },
        },
      }),
    );
    syncMarketAssetHistoryMock.mockResolvedValue({
      task: { id: "wt_switch", type: "asset_history_sync", status: "pending", created_at: 0 },
      existed: false,
    });
    renderPage();

    const switchButton = await screen.findByTestId("switch-source-button");
    expect(screen.getAllByTestId("task-error-inline")[0]).toHaveTextContent(
      "pinned source rejected the symbol",
    );

    fireEvent.click(switchButton);
    await waitFor(() =>
      expect(syncMarketAssetHistoryMock).toHaveBeenCalledWith(
        expect.objectContaining({ mode: "switch_source_full" }),
      ),
    );
  });

  it("disables refresh and shows progress while a task is active", async () => {
    getMarketAssetDetailMock.mockResolvedValue(
      makeDetail({
        history: {
          ...makeDetail().history,
          task: {
            id: "wt_running",
            type: "asset_history_sync",
            status: "running",
            created_at: 0,
          },
        },
      }),
    );
    renderPage();
    await screen.findByTestId("history-state-panel");

    expect(screen.getByTestId("refresh-history-button")).toBeDisabled();
    expect(screen.getByText("历史数据同步中…")).toBeInTheDocument();
    // The active server task is handed to the polling hook.
    expect(useWorkerTaskPollingMock).toHaveBeenLastCalledWith(
      "wt_running",
      expect.objectContaining({ onComplete: expect.any(Function) }),
    );
  });

  it("shows the error state with retry when the detail query fails", async () => {
    getMarketAssetDetailMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.getByTestId("error-state-back")).toHaveAttribute("href", "/assets");
  });
});
