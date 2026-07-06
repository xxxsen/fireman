import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchAssetView } from "@/lib/api/research";
import ResearchScreenerPage from "./page";

const listResearchAssetsMock = vi.hoisted(() => vi.fn());
const listSavedFiltersMock = vi.hoisted(() => vi.fn());
const createSavedFilterMock = vi.hoisted(() => vi.fn());
const syncHistoryMock = vi.hoisted(() => vi.fn());
const createCollectionMock = vi.hoisted(() => vi.fn());
const getCollectionMock = vi.hoisted(() => vi.fn());
const addCollectionItemMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());
const searchParamsGetMock = vi.hoisted(() => vi.fn<(key: string) => string | null>(() => null));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPushMock }),
  useSearchParams: () => ({ get: searchParamsGetMock }),
}));

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  listResearchAssets: (...args: unknown[]) => listResearchAssetsMock(...args),
  listSavedFilters: (...args: unknown[]) => listSavedFiltersMock(...args),
  createSavedFilter: (...args: unknown[]) => createSavedFilterMock(...args),
  createCollection: (...args: unknown[]) => createCollectionMock(...args),
  getCollection: (...args: unknown[]) => getCollectionMock(...args),
  addCollectionItem: (...args: unknown[]) => addCollectionItemMock(...args),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  syncMarketAssetHistory: (...args: unknown[]) => syncHistoryMock(...args),
  getMarketAssetDetail: vi.fn(() => Promise.resolve({ points: [] })),
}));

vi.mock("echarts-for-react", () => ({
  default: () => <div data-testid="echarts" />,
}));

function asset(overrides: Partial<ResearchAssetView> = {}): ResearchAssetView {
  return {
    asset_key: "CN|cn_exchange_fund|sh|510300",
    market: "cn",
    instrument_type: "cn_exchange_fund",
    instrument_type_label: "场内 ETF / LOF",
    region_code: "sh",
    symbol: "510300",
    name: "沪深300ETF",
    exchange: "SSE",
    instrument_kind: "index_etf",
    currency: "CNY",
    active: true,
    listing_status: "active",
    is_cash: false,
    has_history: true,
    adjust_policy: "qfq",
    point_type: "adjusted_close",
    data_as_of: "2026-07-03",
    point_count: 2400,
    stale: false,
    fx_available: true,
    backtest_ready: true,
    quality_badges: ["normal"],
    metrics: {
      asset_key: "CN|cn_exchange_fund|sh|510300",
      adjust_policy: "qfq",
      point_type: "adjusted_close",
      start_date: "2016-07-01",
      end_date: "2026-07-03",
      point_count: 2400,
      history_years: 10,
      cagr: 0.07,
      annual_volatility: 0.2,
      max_drawdown: -0.35,
      sharpe: 0.4,
      calmar: 0.2,
      return_1y: 0.1,
      return_3y: 0.2,
      return_5y: 0.4,
      computed_at: 1750000000000,
    },
    ...overrides,
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ResearchScreenerPage />
    </QueryClientProvider>,
  );
}

describe("ResearchScreenerPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsGetMock.mockReturnValue(null);
    listResearchAssetsMock.mockResolvedValue({ assets: [asset()], total: 1 });
    listSavedFiltersMock.mockResolvedValue({ filters: [] });
  });

  it("renders the asset table with metric columns and quality badges", async () => {
    renderPage();
    expect(await screen.findByTestId("screener-table")).toBeInTheDocument();
    expect(screen.getByText("沪深300ETF")).toBeInTheDocument();
    expect(screen.getByText("7%")).toBeInTheDocument();
    expect(screen.getByText("-35%")).toBeInTheDocument();
    expect(screen.getByText("正常")).toBeInTheDocument();
    expect(screen.getByTestId("screener-total")).toHaveTextContent("共 1 项");
  });

  it("passes filter conditions to the API", async () => {
    renderPage();
    await screen.findByTestId("screener-table");
    fireEvent.click(screen.getByTestId("filter-backtest-ready"));
    await waitFor(() => {
      const lastCall = listResearchAssetsMock.mock.calls.at(-1)![0];
      expect(lastCall.backtestReady).toBe(true);
    });
    fireEvent.change(screen.getByTestId("screener-search"), {
      target: { value: "510300" },
    });
    await waitFor(() => {
      const lastCall = listResearchAssetsMock.mock.calls.at(-1)![0];
      expect(lastCall.q).toBe("510300");
    });
  });

  it("sorts by column and flips direction on repeat clicks", async () => {
    renderPage();
    await screen.findByTestId("screener-table");
    fireEvent.click(screen.getByTestId("sort-cagr"));
    await waitFor(() => {
      const lastCall = listResearchAssetsMock.mock.calls.at(-1)![0];
      expect(lastCall.sortBy).toBe("cagr");
      expect(lastCall.sortDesc).toBe(true);
    });
    fireEvent.click(screen.getByTestId("sort-cagr"));
    await waitFor(() => {
      const lastCall = listResearchAssetsMock.mock.calls.at(-1)![0];
      expect(lastCall.sortDesc).toBe(false);
    });
  });

  it("paginates with offset", async () => {
    listResearchAssetsMock.mockResolvedValue({ assets: [asset()], total: 120 });
    renderPage();
    await screen.findByTestId("screener-table");
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    await waitFor(() => {
      const lastCall = listResearchAssetsMock.mock.calls.at(-1)![0];
      expect(lastCall.offset).toBe(50);
    });
  });

  it("adds and removes candidates from the pool", async () => {
    renderPage();
    await screen.findByTestId("screener-table");
    expect(screen.getByTestId("candidate-pool-empty")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("add-candidate-CN|cn_exchange_fund|sh|510300"));
    expect(screen.getByTestId("candidate-pool")).toBeInTheDocument();
    expect(screen.getByTestId("common-window-estimate")).toHaveTextContent(
      "2016-07-01 ~ 2026-07-03",
    );

    fireEvent.click(screen.getByRole("button", { name: "移除 沪深300ETF" }));
    expect(screen.getByTestId("candidate-pool-empty")).toBeInTheDocument();
  });

  it("creates a collection from the candidate pool with equal weights", async () => {
    createCollectionMock.mockResolvedValue({ id: "rc_9" });
    renderPage();
    await screen.findByTestId("screener-table");
    fireEvent.click(screen.getByTestId("add-candidate-CN|cn_exchange_fund|sh|510300"));
    fireEvent.click(screen.getByTestId("create-collection-from-pool"));
    await waitFor(() => expect(createCollectionMock).toHaveBeenCalled());
    const body = createCollectionMock.mock.calls[0]![0];
    expect(body.items).toHaveLength(1);
    expect(body.items[0].weight).toBe(1);
    await waitFor(() =>
      expect(routerPushMock).toHaveBeenCalledWith("/research/collections/rc_9"),
    );
  });

  it("saves the current filter conditions", async () => {
    createSavedFilterMock.mockResolvedValue({ id: "sf_1" });
    renderPage();
    await screen.findByTestId("screener-table");
    fireEvent.change(screen.getByTestId("save-filter-name"), {
      target: { value: "美元资产候选" },
    });
    fireEvent.click(screen.getByTestId("save-filter-btn"));
    await waitFor(() => expect(createSavedFilterMock).toHaveBeenCalled());
    expect(createSavedFilterMock.mock.calls[0]![0].name).toBe("美元资产候选");
  });

  it("applies a saved filter when clicked", async () => {
    listSavedFiltersMock.mockResolvedValue({
      filters: [
        {
          id: "sf_1",
          name: "低回撤",
          filters_json: JSON.stringify({ market: "us", backtestReady: true }),
          sort_order: 0,
          created_at: 0,
          updated_at: 0,
        },
      ],
    });
    renderPage();
    await screen.findByTestId("screener-table");
    fireEvent.click(await screen.findByTestId("saved-filter-sf_1"));
    await waitFor(() => {
      const lastCall = listResearchAssetsMock.mock.calls.at(-1)![0];
      expect(lastCall.market).toBe("us");
      expect(lastCall.backtestReady).toBe(true);
    });
  });

  it("switches mobile tabs between filters, results and candidates", async () => {
    renderPage();
    await screen.findByTestId("screener-table");
    const filtersTab = screen.getByTestId("mobile-tab-filters");
    const resultsTab = screen.getByTestId("mobile-tab-results");
    const candidatesTab = screen.getByTestId("mobile-tab-candidates");
    expect(resultsTab).toHaveAttribute("aria-selected", "true");

    fireEvent.click(filtersTab);
    expect(filtersTab).toHaveAttribute("aria-selected", "true");
    expect(resultsTab).toHaveAttribute("aria-selected", "false");

    fireEvent.click(candidatesTab);
    expect(candidatesTab).toHaveAttribute("aria-selected", "true");
  });

  it("toggles optional columns through the column config", async () => {
    renderPage();
    const table = within(await screen.findByTestId("screener-table"));
    expect(table.queryByText("近 5 年")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("column-config-toggle"));
    fireEvent.click(screen.getByTestId("column-toggle-return_5y"));
    fireEvent.click(screen.getByTestId("column-toggle-return_drawdown"));
    expect(table.getByText("近 5 年")).toBeInTheDocument();
    expect(table.getByText("收益回撤比")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("column-toggle-return_5y"));
    expect(table.queryByText("近 5 年")).not.toBeInTheDocument();
  });

  it("adds an asset to the target collection when ?collection= is present", async () => {
    searchParamsGetMock.mockImplementation((key) => (key === "collection" ? "rc_7" : null));
    getCollectionMock.mockResolvedValue({
      id: "rc_7",
      name: "目标集合",
      items: [],
      tags: [],
    });
    addCollectionItemMock.mockResolvedValue({
      id: "rc_7",
      name: "目标集合",
      items: [{ asset_key: "CN|cn_exchange_fund|sh|510300" }],
      tags: [],
    });
    renderPage();
    await screen.findByTestId("screener-table");
    expect(await screen.findByTestId("target-collection-banner")).toHaveTextContent("目标集合");

    fireEvent.click(
      screen.getByTestId("add-to-collection-CN|cn_exchange_fund|sh|510300"),
    );
    await waitFor(() => expect(addCollectionItemMock).toHaveBeenCalled());
    expect(addCollectionItemMock.mock.calls[0]![0]).toBe("rc_7");
    expect(addCollectionItemMock.mock.calls[0]![1]).toMatchObject({
      asset_key: "CN|cn_exchange_fund|sh|510300",
      weight: 0,
    });
    expect(await screen.findByText(/已把「沪深300ETF」加入集合/)).toBeInTheDocument();
    expect(screen.getByText("已在集合")).toBeInTheDocument();
  });

  it("has a CSV export entry and creates history sync tasks", async () => {
    syncHistoryMock.mockResolvedValue({ existed: false, task: { id: "wt_1" } });
    renderPage();
    await screen.findByTestId("screener-table");
    expect(screen.getByTestId("screener-export-csv")).toBeEnabled();

    fireEvent.click(screen.getByTestId("refresh-CN|cn_exchange_fund|sh|510300"));
    await waitFor(() => expect(syncHistoryMock).toHaveBeenCalled());
    expect(syncHistoryMock.mock.calls[0]![0]).toMatchObject({
      asset_key: "CN|cn_exchange_fund|sh|510300",
      mode: "default_refresh",
    });
    expect(await screen.findByText(/已为「沪深300ETF」创建历史同步任务/)).toBeInTheDocument();
  });
});
