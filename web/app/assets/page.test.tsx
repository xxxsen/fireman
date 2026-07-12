import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  DirectoryScopeSyncView,
  DirectorySyncUnitView,
  MarketAsset,
} from "@/lib/api/market-assets";
import { formatDateTimeFromMs } from "@/lib/format";
import MarketAssetsPage from "./page";

const listMarketAssetsMock = vi.hoisted(() => vi.fn());
const syncMarketAssetsMock = vi.hoisted(() => vi.fn());
const syncFXRatesMock = vi.hoisted(() => vi.fn());
const useTaskStatusMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssetsMock(...args),
  syncMarketAssets: (...args: unknown[]) => syncMarketAssetsMock(...args),
  syncFXRates: (...args: unknown[]) => syncFXRatesMock(...args),
}));

vi.mock("@/hooks/useTaskStatus", () => ({
  useTaskStatus: (...args: unknown[]) => useTaskStatusMock(...args),
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

const SCOPE_UNITS: Record<string, [string, string][]> = {
  cn_all: [
    ["cn_exchange_stock", "A 股股票"],
    ["cn_exchange_fund", "场内基金（ETF/LOF）"],
    ["cn_mutual_fund", "场外基金"],
  ],
  hk_all: [
    ["hk_stock", "港股股票"],
    ["hk_etf", "港股 ETF"],
  ],
  us_all: [
    ["us_stock", "美股股票"],
    ["us_etf", "美股 ETF"],
  ],
};

const SCOPE_TITLES: Record<string, string> = {
  cn_all: "中国市场目录",
  hk_all: "港股市场目录",
  us_all: "美股市场目录",
};

function makeScope(
  scope: string,
  overrides: Partial<DirectoryScopeSyncView> = {},
  unitOverrides: Record<string, Partial<DirectorySyncUnitView>> = {},
): DirectoryScopeSyncView {
  const units: DirectorySyncUnitView[] = SCOPE_UNITS[scope].map(
    ([syncKey, label]) => ({
      sync_key: syncKey,
      label,
      last_success_at: null,
      last_success_task_id: "",
      ...unitOverrides[syncKey],
    }),
  );
  return {
    scope,
    label: SCOPE_TITLES[scope],
    status: "never",
    last_success_at: null,
    units,
    ...overrides,
  };
}

/** cn_all fully synced a minute ago; hk_all/us_all never synced. */
function makeSyncs(): DirectoryScopeSyncView[] {
  const successAt = Date.now() - 60_000;
  return [
    makeScope(
      "cn_all",
      { status: "complete", last_success_at: successAt },
      {
        cn_exchange_stock: {
          last_success_at: successAt,
          last_success_task_id: "wt_cn",
        },
        cn_exchange_fund: {
          last_success_at: successAt,
          last_success_task_id: "wt_cn",
        },
        cn_mutual_fund: {
          last_success_at: successAt,
          last_success_task_id: "wt_cn",
        },
      },
    ),
    makeScope("hk_all"),
    makeScope("us_all"),
  ];
}

/** Every scope complete. */
function makeCompleteSyncs(): DirectoryScopeSyncView[] {
  const successAt = Date.now() - 60_000;
  return ["cn_all", "hk_all", "us_all"].map((scope) =>
    makeScope(
      scope,
      { status: "complete", last_success_at: successAt },
      Object.fromEntries(
        SCOPE_UNITS[scope].map(([syncKey]) => [
          syncKey,
          { last_success_at: successAt, last_success_task_id: `wt_${syncKey}` },
        ]),
      ),
    ),
  );
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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
    useTaskStatusMock.mockReset();
    useTaskStatusMock.mockReturnValue({
      task: null,
      pollError: null,
      isActive: false,
    });
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeSyncs(),
      fx_sync: {
        scope: "fx_rates",
        last_success_at: null,
        last_success_task_id: "",
      },
      total: 1,
    });
  });

  it("renders the sync panel with all three scopes and the asset table", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(
      screen.getByRole("heading", { name: "资产目录" }),
    ).toBeInTheDocument();

    const panel = screen.getByTestId("directory-sync-panel");
    expect(within(panel).getByText("中国市场目录")).toBeInTheDocument();
    expect(within(panel).getByText("港股市场目录")).toBeInTheDocument();
    expect(within(panel).getByText("美股市场目录")).toBeInTheDocument();
    // Unit rows render under each scope.
    expect(
      within(panel).getByTestId("directory-sync-unit-cn_exchange_stock"),
    ).toBeInTheDocument();
    expect(
      within(panel).getByTestId("directory-sync-unit-cn_mutual_fund"),
    ).toBeInTheDocument();
    expect(
      within(panel).getByTestId("directory-sync-unit-hk_etf"),
    ).toBeInTheDocument();

    const row = screen.getByTestId("market-asset-row");
    expect(within(row).getByRole("link", { name: "510300" })).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent("cn:cn_exchange_fund:sh:510300")}`,
    );
    // The user asset library was removed: no import entry anywhere.
    expect(
      within(row).queryByRole("link", { name: "录入" }),
    ).not.toBeInTheDocument();
  });

  it("has no import or library entry points", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(screen.queryByTestId("my-library-link")).not.toBeInTheDocument();
    expect(screen.queryByTestId("page-header-primary")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "录入资产" }),
    ).not.toBeInTheDocument();
  });

  it("collapses the sync panel by default when every scope is complete", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: makeCompleteSyncs(),
      fx_sync: {
        scope: "fx_rates",
        last_success_at: null,
        last_success_task_id: "",
      },
      total: 1,
    });
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(
      screen.queryByTestId("directory-sync-cn_all"),
    ).not.toBeInTheDocument();
    expect(screen.getByTestId("directory-sync-summary")).toHaveTextContent(
      "目录已同步",
    );

    // Manual expand is respected.
    fireEvent.click(screen.getByTestId("directory-sync-toggle"));
    expect(screen.getByTestId("directory-sync-cn_all")).toBeInTheDocument();
  });

  it("expands the sync panel by default when a scope is not complete", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(screen.getByTestId("directory-sync-hk_all")).toBeInTheDocument();
  });

  it("expands the sync panel when a scope is partial even with a success record", async () => {
    const successAt = Date.now() - 60_000;
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: [
        makeScope(
          "cn_all",
          { status: "partial" },
          { cn_exchange_stock: { last_success_at: successAt } },
        ),
        ...makeCompleteSyncs().slice(1),
      ],
      fx_sync: {
        scope: "fx_rates",
        last_success_at: null,
        last_success_task_id: "",
      },
      total: 1,
    });
    renderPage();
    await screen.findByTestId("market-assets-table");
    const row = screen.getByTestId("directory-sync-cn_all");
    expect(within(row).getByTestId("scope-status-cn_all")).toHaveAttribute(
      "data-status",
      "partial",
    );
    // Aggregate success time absent: the row explains partial sync.
    expect(
      within(row).getByText("部分未同步", { selector: ".font-mono-numeric" }),
    ).toBeInTheDocument();
  });

  it("shows the never-synced empty state before the first directory sync", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [],
      syncs: [makeScope("cn_all"), makeScope("hk_all"), makeScope("us_all")],
      total: 0,
    });
    renderPage();
    expect(await screen.findByText("当前没有资产基础信息")).toBeInTheDocument();
  });

  it("searches by symbol and name with dedicated params after the debounce", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.change(screen.getByTestId("market-assets-symbol-search"), {
      target: { value: "510300" },
    });
    await waitFor(
      () =>
        expect(listMarketAssetsMock).toHaveBeenCalledWith(
          expect.objectContaining({ symbolQ: "510300", offset: 0 }),
        ),
      { timeout: 2000 },
    );

    fireEvent.change(screen.getByTestId("market-assets-name-search"), {
      target: { value: "沪深300" },
    });
    await waitFor(
      () =>
        expect(listMarketAssetsMock).toHaveBeenCalledWith(
          expect.objectContaining({ nameQ: "沪深300", offset: 0 }),
        ),
      { timeout: 2000 },
    );
  });

  it("filters by instrument type immediately", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.change(screen.getByTestId("market-assets-type-filter"), {
      target: { value: "hk_etf" },
    });
    await waitFor(() =>
      expect(listMarketAssetsMock).toHaveBeenCalledWith(
        expect.objectContaining({ instrumentTypes: ["hk_etf"], offset: 0 }),
      ),
    );
  });

  it("shows the range, total and page jump in the pagination bar", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: Array.from({ length: 50 }, (_, i) =>
        makeAsset({
          asset_key: `cn:cn_exchange_fund:sh:a${i}`,
          symbol: `a${i}`,
        }),
      ),
      syncs: makeSyncs(),
      fx_sync: {
        scope: "fx_rates",
        last_success_at: null,
        last_success_task_id: "",
      },
      total: 120,
    });
    renderPage();
    await screen.findByTestId("market-assets-table");
    expect(screen.getByTestId("market-assets-range")).toHaveTextContent(
      "当前第 1-50 条，共 120 条 · 第 1 / 3 页",
    );

    fireEvent.change(screen.getByTestId("market-assets-page-input"), {
      target: { value: "3" },
    });
    fireEvent.click(screen.getByTestId("market-assets-page-jump"));
    await waitFor(() =>
      expect(listMarketAssetsMock).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 100 }),
      ),
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
    const staleAt = Date.now() - 8 * 24 * 60 * 60 * 1000;
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: [
        makeScope(
          "cn_all",
          { status: "complete", last_success_at: staleAt },
          {
            cn_exchange_stock: { last_success_at: staleAt },
            cn_exchange_fund: { last_success_at: staleAt },
            cn_mutual_fund: { last_success_at: staleAt },
          },
        ),
        makeScope("hk_all"),
        makeScope("us_all"),
      ],
      total: 1,
    });
    renderPage();
    const banner = await screen.findByTestId("directory-stale-banner");
    expect(banner).toHaveTextContent("中国市场目录");
    expect(banner).toHaveTextContent("超过 7 天未同步");
  });

  it("creates unit tasks for the whole scope from the split button main action", async () => {
    syncMarketAssetsMock.mockResolvedValue({
      scope: "hk_all",
      tasks: [
        {
          sync_key: "hk_stock",
          scope: "hk_all",
          task: {
            id: "wt_hk1",
            type: "asset_directory_sync",
            status: "pending",
            created_at: 0,
          },
          existed: false,
        },
        {
          sync_key: "hk_etf",
          scope: "hk_all",
          task: {
            id: "wt_hk2",
            type: "asset_directory_sync",
            status: "pending",
            created_at: 0,
          },
          existed: false,
        },
      ],
    });
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.click(screen.getByTestId("sync-button-hk_all-main"));
    await waitFor(() =>
      expect(syncMarketAssetsMock).toHaveBeenCalledWith({ scope: "hk_all" }),
    );
    // Scope aggregation comes from the backend: the list query is refetched.
    await waitFor(() =>
      expect(listMarketAssetsMock.mock.calls.length).toBeGreaterThan(1),
    );
  });

  it("creates a single unit task from the split button dropdown", async () => {
    syncMarketAssetsMock.mockResolvedValue({
      scope: "hk_all",
      tasks: [
        {
          sync_key: "hk_etf",
          scope: "hk_all",
          task: {
            id: "wt_hk2",
            type: "asset_directory_sync",
            status: "pending",
            created_at: 0,
          },
          existed: false,
        },
      ],
    });
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.click(screen.getByTestId("sync-button-hk_all-toggle"));
    fireEvent.click(screen.getByTestId("sync-button-hk_all-item-hk_etf"));
    await waitFor(() =>
      expect(syncMarketAssetsMock).toHaveBeenCalledWith({ sync_key: "hk_etf" }),
    );
    // Menu closes after selecting.
    expect(
      screen.queryByTestId("sync-button-hk_all-menu"),
    ).not.toBeInTheDocument();
  });

  it("disables only the running unit in the dropdown and keeps the main button clickable", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: [
        makeScope(
          "cn_all",
          { status: "running" },
          {
            cn_exchange_stock: {
              task: {
                id: "wt_running",
                type: "asset_directory_sync",
                status: "running",
                created_at: 0,
              },
            },
          },
        ),
        makeScope("hk_all"),
        makeScope("us_all"),
      ],
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("directory-sync-cn_all");
    expect(within(row).getByTestId("scope-status-cn_all")).toHaveAttribute(
      "data-status",
      "running",
    );
    // Backend dedupe makes "sync all" safe while one unit is running.
    expect(within(row).getByTestId("sync-button-cn_all-main")).toBeEnabled();

    fireEvent.click(within(row).getByTestId("sync-button-cn_all-toggle"));
    expect(
      within(row).getByTestId("sync-button-cn_all-item-cn_exchange_stock"),
    ).toBeDisabled();
    expect(
      within(row).getByTestId("sync-button-cn_all-item-cn_mutual_fund"),
    ).toBeEnabled();

    const unitRow = within(row).getByTestId(
      "directory-sync-unit-cn_exchange_stock",
    );
    expect(within(unitRow).getByTestId("task-status-badge")).toHaveAttribute(
      "data-status",
      "running",
    );
    expect(within(unitRow).getByText("同步进行中…")).toBeInTheDocument();
  });

  it("surfaces the error code when a unit's latest sync task failed", async () => {
    listMarketAssetsMock.mockResolvedValue({
      assets: [makeAsset()],
      syncs: [
        makeScope(
          "cn_all",
          { status: "failed" },
          {
            cn_mutual_fund: {
              task: {
                id: "wt_failed",
                type: "asset_directory_sync",
                status: "failed",
                error_code: "directory_data_incomplete",
                error_message: "category CN/cn_mutual_fund returned no assets",
                created_at: 0,
              },
            },
          },
        ),
        makeScope("hk_all"),
        makeScope("us_all"),
      ],
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("directory-sync-cn_all");
    expect(within(row).getByTestId("scope-status-cn_all")).toHaveAttribute(
      "data-status",
      "failed",
    );
    const unitRow = within(row).getByTestId(
      "directory-sync-unit-cn_mutual_fund",
    );
    expect(within(unitRow).getByTestId("task-status-badge")).toHaveAttribute(
      "data-status",
      "failed",
    );
    expect(within(unitRow).getByTestId("task-error-inline")).toHaveTextContent(
      "category CN/cn_mutual_fund returned no assets",
    );
  });

  it("keeps every sync row visible while the asset list is filtered by market", async () => {
    renderPage();
    await screen.findByTestId("market-assets-table");

    fireEvent.change(screen.getByTestId("market-assets-market-filter"), {
      target: { value: "CN" },
    });
    await waitFor(() =>
      expect(listMarketAssetsMock).toHaveBeenCalledWith(
        expect.objectContaining({ market: "CN" }),
      ),
    );

    const panel = screen.getByTestId("directory-sync-panel");
    expect(
      within(panel).getByTestId("directory-sync-cn_all"),
    ).toBeInTheDocument();
    expect(
      within(panel).getByTestId("directory-sync-hk_all"),
    ).toBeInTheDocument();
    expect(
      within(panel).getByTestId("directory-sync-us_all"),
    ).toBeInTheDocument();
    expect(within(panel).getByTestId("fx-sync-row")).toBeInTheDocument();
  });

  it("displays HK/US ETF directory entries with type labels", async () => {
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
    expect(
      within(hkRow).queryByRole("link", { name: "录入" }),
    ).not.toBeInTheDocument();

    const usRow = rows[1];
    expect(within(usRow).getByText("美国 ETF")).toBeInTheDocument();
    expect(
      within(usRow).queryByRole("link", { name: "录入" }),
    ).not.toBeInTheDocument();
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
      task: {
        id: "wt_fx",
        type: "fx_rate_sync",
        status: "pending",
        created_at: 0,
      },
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
      expect(useTaskStatusMock).toHaveBeenLastCalledWith(
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
    expect(
      within(row).getByText(formatDateTimeFromMs(lastSuccessAt)),
    ).toBeInTheDocument();
    expect(within(row).getByTestId("task-status-badge")).toBeInTheDocument();
  });

  it("surfaces polling errors without hiding the unit row", async () => {
    useTaskStatusMock.mockReturnValue({
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
      syncs: [
        makeScope(
          "cn_all",
          { status: "running" },
          {
            cn_exchange_stock: {
              task: {
                id: "wt_running",
                type: "asset_directory_sync",
                status: "running",
                created_at: 0,
              },
            },
          },
        ),
        makeScope("hk_all"),
        makeScope("us_all"),
      ],
      total: 1,
    });
    renderPage();
    const row = await screen.findByTestId("directory-sync-cn_all");
    const unitRow = within(row).getByTestId(
      "directory-sync-unit-cn_exchange_stock",
    );
    expect(within(unitRow).getByText(/任务状态查询失败/)).toBeInTheDocument();
    expect(
      within(unitRow).getByTestId("task-status-badge"),
    ).toBeInTheDocument();
  });
});
