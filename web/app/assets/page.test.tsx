import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MarketAsset, MarketAssetSyncView } from "@/lib/api/market-assets";
import { formatDateTimeFromMs } from "@/lib/format";
import MarketAssetsPage from "./page";

const listMarketAssetsMock = vi.hoisted(() => vi.fn());
const syncMarketAssetsMock = vi.hoisted(() => vi.fn());
const syncFXRatesMock = vi.hoisted(() => vi.fn());
const useWorkerTaskPollingMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssetsMock(...args),
  syncMarketAssets: (...args: unknown[]) => syncMarketAssetsMock(...args),
  syncFXRates: (...args: unknown[]) => syncFXRatesMock(...args),
}));

vi.mock("@/hooks/useWorkerTaskPolling", () => ({
  useWorkerTaskPolling: (...args: unknown[]) => useWorkerTaskPollingMock(...args),
}));

function makeAsset(overrides: Partial<MarketAsset> = {}): MarketAsset {
  return {
    asset_key: "cn:cn_exchange_fund:sh:510300",
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
    last_seen_at: 1751000000000,
    source_name: "ak.fund_etf_spot_em",
    source_as_of: "2026-07-01",
    refreshed_at: 1751000000000,
    created_at: 0,
    updated_at: 0,
    ...overrides,
  };
}

function makeSyncs(overrides: Partial<MarketAssetSyncView>[] = []): MarketAssetSyncView[] {
  const base: MarketAssetSyncView[] = [
    { scope: "cn_all", last_success_at: Date.now() - 60_000, last_success_task_id: "wt_cn" },
    { scope: "hk_all", last_success_at: null, last_success_task_id: "" },
    { scope: "us_all", last_success_at: null, last_success_task_id: "" },
  ];
  overrides.forEach((patch, i) => Object.assign(base[i], patch));
  return base;
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MarketAssetsPage />
    </QueryClientProvider>,
  );
}

describe("MarketAssetsPage", () => {
  beforeEach(() => {
    listMarketAssetsMock.mockReset();
    syncMarketAssetsMock.mockReset();
    syncFXRatesMock.mockReset();
    useWorkerTaskPollingMock.mockReset();
    useWorkerTaskPollingMock.mockReturnValue({ task: null, pollError: null, isActive: false });
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs(),
      fx_sync: { scope: "fx_rates", last_success_at: null, last_success_task_id: "" },
      total: 1,
    });
  });

  it("renders the sync panel with all three scopes and the asset table", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(screen.getByRole("heading", { name: "资产目录" })).toBeInTheDocument();

    const panel = screen.getByTestId("directory-sync-panel");
    expect(within(panel).getByText("A 股 / 场内基金")).toBeInTheDocument();
    expect(within(panel).getByText("港股 / 港股 ETF")).toBeInTheDocument();
    expect(within(panel).getByText("美股 / 美股 ETF")).toBeInTheDocument();

    const row = screen.getByTestId("market-asset-row");
    expect(within(row).getByRole("link", { name: "510300" })).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent("cn:cn_exchange_fund:sh:510300")}`,
    );
    expect(within(row).getByRole("link", { name: "录入" })).toHaveAttribute(
      "href",
      `/assets/import?asset_key=${encodeURIComponent("cn:cn_exchange_fund:sh:510300")}`,
    );
  });

  it("links to the user library and import flow from the header", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(screen.getByTestId("my-library-link")).toHaveAttribute("href", "/assets/library");
    expect(screen.getByTestId("page-header-primary")).toHaveAttribute("href", "/assets/import");
  });

  it("shows the never-synced empty state before the first directory sync", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [],
      syncs: makeSyncs([{ last_success_at: null, last_success_task_id: "" }]),
      total: 0,
    });
    renderPage();
    expect(await screen.findByText("当前没有资产基础信息")).toBeInTheDocument();
  });

  it("searches the local directory after the debounce", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.change(screen.getByTestId("market-assets-search"), {
      target: { value: "沪深300" },
    });
    await waitFor(
      () =>
        expect(listMarketAssetsMock).toHaveBeenCalledWith(
          expect.objectContaining({ q: "沪深300", offset: 0 }),
        ),
      { timeout: 2000 },
    );
  });

  it("filters by market immediately", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.change(screen.getByTestId("market-assets-market-filter"), {
      target: { value: "HK" },
    });
    await waitFor(() =>
      expect(listMarketAssetsMock).toHaveBeenCalledWith(
        expect.objectContaining({ market: "HK" }),
      ),
    );
  });

  it("shows a stale banner when a scope has not synced for over 7 days", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs([
        { last_success_at: Date.now() - 8 * 24 * 60 * 60 * 1000, last_success_task_id: "wt_cn" },
      ]),
      total: 1,
    });
    renderPage();
    const banner = await screen.findByTestId("directory-stale-banner");
    expect(banner).toHaveTextContent("A 股 / 场内基金");
    expect(banner).toHaveTextContent("超过 7 天未同步");
  });

  it("creates a directory sync task from the scope row", async () => {
    syncMarketAssetsMock.mockResolvedValue({
      task: { id: "wt_new", type: "asset_directory_sync", status: "pending", created_at: 0 },
      existed: false,
    });
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.click(screen.getByTestId("sync-button-hk_all"));
    await waitFor(() =>
      expect(syncMarketAssetsMock).toHaveBeenCalledWith({ scope: "hk_all" }),
    );
  });

  it("disables the sync button and shows progress while a task is active", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs([
        {
          task: {
            id: "wt_running",
            type: "asset_directory_sync",
            status: "running",
            created_at: 0,
          },
        },
      ]),
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("directory-sync-cn_all");
    expect(within(row).getByTestId("sync-button-cn_all")).toBeDisabled();
    expect(within(row).getByText("同步进行中…")).toBeInTheDocument();
    expect(within(row).getByTestId("task-status-badge")).toHaveAttribute(
      "data-status",
      "running",
    );
  });

  it("surfaces the error code when the latest sync task failed", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs([
        {
          task: {
            id: "wt_failed",
            type: "asset_directory_sync",
            status: "failed",
            error_code: "directory_data_incomplete",
            error_message: "category CN/cn_mutual_fund returned no assets",
            created_at: 0,
          },
        },
      ]),
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("directory-sync-cn_all");
    expect(within(row).getByTestId("task-status-badge")).toHaveAttribute(
      "data-status",
      "failed",
    );
    expect(within(row).getByTestId("task-error-inline")).toHaveTextContent(
      "category CN/cn_mutual_fund returned no assets",
    );
  });

  it("displays HK/US ETF directory entries with type labels and import links", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [
        makeAsset({
          asset_key: "hk:hk_etf:hk:02800",
          market: "HK",
          instrument_type: "hk_etf",
          region_code: "hk",
          symbol: "02800",
          name: "盈富基金",
          exchange: "HK",
          instrument_kind: "etf",
          currency: "HKD",
          source_name: "em.hk_fund_list",
        }),
        makeAsset({
          asset_key: "us:us_etf:us:SPY",
          market: "US",
          instrument_type: "us_etf",
          region_code: "us",
          symbol: "SPY",
          name: "标普500ETF-SPDR",
          exchange: "US",
          instrument_kind: "etf",
          currency: "USD",
          source_name: "em.us_etf_list",
        }),
      ],
      syncs: makeSyncs(),
      total: 2,
    });
    renderPage();
    const rows = await screen.findAllByTestId("market-asset-row");
    expect(rows).toHaveLength(2);

    const hkRow = rows[0];
    expect(within(hkRow).getByText("香港 ETF")).toBeInTheDocument();
    expect(within(hkRow).getByRole("link", { name: "02800" })).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent("hk:hk_etf:hk:02800")}`,
    );
    expect(within(hkRow).getByRole("link", { name: "录入" })).toHaveAttribute(
      "href",
      `/assets/import?asset_key=${encodeURIComponent("hk:hk_etf:hk:02800")}`,
    );

    const usRow = rows[1];
    expect(within(usRow).getByText("美国 ETF")).toBeInTheDocument();
    expect(within(usRow).getByRole("link", { name: "录入" })).toHaveAttribute(
      "href",
      `/assets/import?asset_key=${encodeURIComponent("us:us_etf:us:SPY")}`,
    );
  });

  it("marks inactive assets as delisted in the table", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset({ active: false })],
      syncs: makeSyncs(),
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("market-asset-row");
    expect(within(row).getByText("已退市/未在目录")).toBeInTheDocument();
  });

  it("passes include_inactive when the delisted filter is checked", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.click(screen.getByTestId("market-assets-include-inactive"));
    await waitFor(() =>
      expect(listMarketAssetsMock).toHaveBeenCalledWith(
        expect.objectContaining({ includeInactive: true, offset: 0 }),
      ),
    );
  });

  it("creates an fx_rate_sync task from the FX row", async () => {
    syncFXRatesMock.mockResolvedValue({
      task: { id: "wt_fx", type: "fx_rate_sync", status: "pending", created_at: 0 },
      existed: false,
    });
    renderPage();
    await screen.findByTestId("market-assets-table");

    const row = screen.getByTestId("fx-sync-row");
    expect(within(row).getByText("汇率（USD/HKD）")).toBeInTheDocument();
    fireEvent.click(within(row).getByTestId("fx-sync-button"));
    await waitFor(() => expect(syncFXRatesMock).toHaveBeenCalled());
    // The created task id is handed to the polling hook.
    await waitFor(() =>
      expect(useWorkerTaskPollingMock).toHaveBeenLastCalledWith(
        "wt_fx",
        expect.objectContaining({ onComplete: expect.any(Function) }),
      ),
    );
  });

  it("hydrates the FX row last success time from the API after page reload", async () => {
    const lastSuccessAt = 1751300000000;
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs(),
      fx_sync: {
        scope: "fx_rates",
        last_success_at: lastSuccessAt,
        last_success_task_id: "wt_fx_done",
        task: {
          id: "wt_fx_done",
          type: "fx_rate_sync",
          status: "complete",
          created_at: lastSuccessAt - 1000,
          finished_at: lastSuccessAt,
        },
      },
      total: 1,
    });

    renderPage();
    const row = await screen.findByTestId("fx-sync-row");
    expect(within(row).getByText(formatDateTimeFromMs(lastSuccessAt))).toBeInTheDocument();
    expect(within(row).getByTestId("task-status-badge")).toBeInTheDocument();
  });

  it("surfaces polling errors without hiding the sync row", async () => {
    useWorkerTaskPollingMock.mockReturnValue({
      task: {
        id: "wt_running",
        type: "asset_directory_sync",
        status: "running",
        created_at: 0,
      },
      pollError: "network down",
      isActive: true,
    });
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs([
        {
          task: {
            id: "wt_running",
            type: "asset_directory_sync",
            status: "running",
            created_at: 0,
          },
        },
      ]),
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("directory-sync-cn_all");
    expect(within(row).getByText(/任务状态查询失败/)).toBeInTheDocument();
    expect(within(row).getByTestId("task-status-badge")).toBeInTheDocument();
  });
});
