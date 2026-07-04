// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import type { MarketAsset } from "@/lib/api/market-assets";
import { AssetClassHoldingPicker } from "./AssetClassHoldingPicker";

const searchInstruments = vi.fn();
const getInstrument = vi.fn();
const listMarketAssets = vi.fn();
const importFromMarketAsset = vi.fn();

vi.mock("@/lib/api/instruments", () => ({
  searchInstruments: (...args: unknown[]) => searchInstruments(...args),
  getInstrument: (...args: unknown[]) => getInstrument(...args),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssets(...args),
  importFromMarketAsset: (...args: unknown[]) => importFromMarketAsset(...args),
}));

interface SearchParams {
  q?: string;
  assetClass?: string;
  region?: string;
  excludeIds?: string[];
  cursor?: number;
  limit?: number;
}

function makeInstrument(i: number) {
  return {
    id: `ins_${i}`,
    code: `EQ${i}`,
    name: `资料库基金${i}`,
    market: "CN",
    instrument_type: "cn_mutual_fund",
    asset_class: "equity",
    region: "domestic",
    currency: "CNY",
    provider: "akshare",
    is_system: false,
    status: "active",
    simulation_eligible: true,
    expense_ratio_status: "unknown",
    fee_treatment: "net",
    data_stale: false,
    created_at: 0,
    updated_at: 0,
  };
}

function makeMarketAsset(overrides: Partial<MarketAsset> = {}): MarketAsset {
  return {
    asset_key: "cn:cn_mutual_fund:270042",
    market: "CN",
    instrument_type: "cn_mutual_fund",
    region_code: "",
    symbol: "270042",
    name: "广发纳指100ETF联接（QDII）人民币A",
    exchange: "",
    instrument_kind: "指数型-海外股票",
    currency: "CNY",
    active: true,
    listing_status: "active",
    last_seen_at: 0,
    source_name: "ak.fund_name_em",
    source_as_of: "",
    refreshed_at: 0,
    created_at: 0,
    updated_at: 0,
    ...overrides,
  };
}

let pool = [makeInstrument(1)];

function filterPool(params: SearchParams) {
  const q = (params.q ?? "").toLowerCase();
  const exclude = new Set(params.excludeIds ?? []);
  const items = pool.filter(
    (i) =>
      !exclude.has(i.id) &&
      (!q || i.code.toLowerCase().includes(q) || i.name.toLowerCase().includes(q)),
  );
  const cursor = params.cursor ?? 0;
  const limit = params.limit ?? 10;
  const page = items.slice(cursor, cursor + limit);
  const next = cursor + page.length < items.length ? cursor + page.length : null;
  return Promise.resolve({ instruments: page, next_cursor: next, total: items.length });
}

function renderPicker(selected: unknown[] = []) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onSelectedChange = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <AssetClassHoldingPicker
        assetClass="equity"
        classWeight={1}
        regionWeight={1}
        region="domestic"
        totalAssetsMinor={1_000_000}
        selected={selected as never}
        onSelectedChange={onSelectedChange}
      />
    </QueryClientProvider>,
  );
  return { onSelectedChange, client };
}

describe("AssetClassHoldingPicker", () => {
  beforeEach(() => {
    searchInstruments.mockReset();
    getInstrument.mockReset();
    listMarketAssets.mockReset();
    importFromMarketAsset.mockReset();
    pool = [makeInstrument(1)];
    searchInstruments.mockImplementation((params: SearchParams) => filterPool(params));
    listMarketAssets.mockResolvedValue({ assets: [], syncs: [], total: 0 });
  });

  it("loads the first page of recent instruments on focus without typing", async () => {
    pool = Array.from({ length: 5 }, (_, i) => makeInstrument(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    expect(await screen.findByRole("button", { name: /资料库基金1/ })).toBeInTheDocument();
    await waitFor(() =>
      expect(searchInstruments).toHaveBeenCalledWith(
        expect.objectContaining({ assetClass: "equity", region: "domestic", cursor: 0 }),
      ),
    );
  });

  it("appends the next page when the sentinel scrolls into view", async () => {
    pool = Array.from({ length: 12 }, (_, i) => makeInstrument(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    // Page 2 contains the 11th and 12th instruments.
    expect(await screen.findByRole("button", { name: /资料库基金11/ })).toBeInTheDocument();
    await waitFor(() =>
      expect(searchInstruments).toHaveBeenCalledWith(expect.objectContaining({ cursor: 10 })),
    );
  });

  it("excludes already-selected instruments from the query", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeInstrument(i + 1));
    renderPicker([{ inst: makeInstrument(1), weight: 1, amount: 0 }]);
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    await waitFor(() =>
      expect(searchInstruments).toHaveBeenCalledWith(
        expect.objectContaining({ excludeIds: ["ins_1"] }),
      ),
    );
  });

  it("searches the local market asset directory when the library has no exact code match", async () => {
    pool = [];
    listMarketAssets.mockResolvedValue({
      assets: [makeMarketAsset()],
      syncs: [],
      total: 1,
    });
    renderPicker();

    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });

    await waitFor(() =>
      expect(listMarketAssets).toHaveBeenCalledWith(
        expect.objectContaining({ q: "270042" }),
      ),
    );
    expect(await screen.findByTestId("wizard-external-results")).toBeInTheDocument();
    expect(screen.getByText(/资产库未收录/)).toBeInTheDocument();
  });

  it("never searches the directory when the library search (still pending) ends up holding the code", async () => {
    const libInst = {
      ...makeInstrument(1),
      id: "ins_270042",
      code: "270042",
      name: "资料库已收录基金",
    };
    let settleCodeSearch: () => void = () => {};
    searchInstruments.mockImplementation((params: SearchParams) => {
      if (params.q === "270042") {
        return new Promise((res) => {
          settleCodeSearch = () =>
            res({ instruments: [libInst], next_cursor: null, total: 1 });
        });
      }
      return Promise.resolve({ instruments: [], next_cursor: null, total: 0 });
    });

    renderPicker();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });

    // Wait until the local code search is actually in flight.
    await waitFor(() =>
      expect(searchInstruments).toHaveBeenCalledWith(expect.objectContaining({ q: "270042" })),
    );
    // While the local paginated search is pending, the directory must not be queried.
    await new Promise((r) => setTimeout(r, 50));
    expect(listMarketAssets).not.toHaveBeenCalled();

    // Settle the local search with an exact library hit.
    settleCodeSearch();
    expect(await screen.findByRole("button", { name: /资料库已收录基金/ })).toBeInTheDocument();
    // The library holds the code → directory search must never run.
    expect(listMarketAssets).not.toHaveBeenCalled();
  });

  it("imports a directory candidate and adds the instrument to the selection", async () => {
    pool = [];
    listMarketAssets.mockResolvedValue({
      assets: [makeMarketAsset()],
      syncs: [],
      total: 1,
    });
    importFromMarketAsset.mockResolvedValue({
      ...makeInstrument(99),
      id: "ins_new",
      code: "270042",
      name: "广发纳指100ETF联接（QDII）人民币A",
    });

    const { onSelectedChange } = renderPicker();

    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /点击录入并添加/ }));

    await waitFor(() => {
      expect(importFromMarketAsset).toHaveBeenCalledWith({
        asset_key: "cn:cn_mutual_fund:270042",
        asset_class: "equity",
        region: "domestic",
      });
    });
    await waitFor(() => {
      expect(onSelectedChange).toHaveBeenCalled();
    });
  });

  it("shows the history-empty guidance when the candidate has no synced history", async () => {
    pool = [];
    listMarketAssets.mockResolvedValue({
      assets: [makeMarketAsset()],
      syncs: [],
      total: 1,
    });
    const { ApiError } = await import("@/lib/api/client");
    importFromMarketAsset.mockRejectedValue(
      new ApiError("market_asset_history_empty", "history empty"),
    );

    const { onSelectedChange } = renderPicker();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /点击录入并添加/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent("该资产还没有本地历史数据");
    expect(onSelectedChange).not.toHaveBeenCalled();
  });

  it("filters out inactive directory entries from external candidates", async () => {
    pool = [];
    listMarketAssets.mockResolvedValue({
      assets: [makeMarketAsset({ active: false })],
      syncs: [],
      total: 1,
    });
    renderPicker();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });

    expect(
      await screen.findByText("未在本地资产目录中找到可录入的标的"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("wizard-external-results")).not.toBeInTheDocument();
  });

  it("exposes combobox semantics on the search input", async () => {
    renderPicker();
    const search = screen.getByTestId("wizard-holding-search");
    expect(search).toHaveAttribute("role", "combobox");
    expect(search).toHaveAttribute("aria-expanded", "false");
    fireEvent.focus(search);
    await waitFor(() => expect(search).toHaveAttribute("aria-expanded", "true"));
  });

  it("closes the candidate dropdown when clicking outside", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeInstrument(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    expect(await screen.findByTestId("wizard-library-results")).toBeInTheDocument();

    fireEvent.pointerDown(document.body);
    await waitFor(() =>
      expect(screen.queryByTestId("wizard-library-results")).not.toBeInTheDocument(),
    );
  });

  it("closes the candidate dropdown on Escape", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeInstrument(i + 1));
    renderPicker();
    const search = screen.getByTestId("wizard-holding-search");
    fireEvent.focus(search);
    expect(await screen.findByTestId("wizard-library-results")).toBeInTheDocument();

    fireEvent.keyDown(search, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByTestId("wizard-library-results")).not.toBeInTheDocument(),
    );
  });

  it("renders selected holdings above the search input", () => {
    renderPicker([{ inst: makeInstrument(1), weight: 1, amount: 0 }]);
    const selectedRows = screen.getByTestId("wizard-selected-rows");
    const search = screen.getByTestId("wizard-holding-search");
    expect(
      selectedRows.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("closes external directory candidates on outside click", async () => {
    pool = [];
    listMarketAssets.mockResolvedValue({
      assets: [makeMarketAsset()],
      syncs: [],
      total: 1,
    });
    renderPicker();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), { target: { value: "270042" } });
    expect(await screen.findByTestId("wizard-external-results")).toBeInTheDocument();

    fireEvent.pointerDown(document.body);
    await waitFor(() =>
      expect(screen.queryByTestId("wizard-external-results")).not.toBeInTheDocument(),
    );
  });

  it("closes external directory candidates on Escape", async () => {
    pool = [];
    listMarketAssets.mockResolvedValue({
      assets: [makeMarketAsset()],
      syncs: [],
      total: 1,
    });
    renderPicker();
    const search = screen.getByTestId("wizard-holding-search");
    fireEvent.change(search, { target: { value: "270042" } });
    expect(await screen.findByTestId("wizard-external-results")).toBeInTheDocument();

    fireEvent.keyDown(search, { key: "Escape" });
    await waitFor(() =>
      expect(screen.queryByTestId("wizard-external-results")).not.toBeInTheDocument(),
    );
  });

  it("renders the local dropdown at a fixed 10-row height with single-line rows", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeInstrument(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    const option = await screen.findByRole("button", { name: /资料库基金1/ });

    const list = screen.getByTestId("wizard-library-results");
    expect(list).toHaveClass("h-[30rem]");
    expect(list).not.toHaveClass("max-h-80");

    expect(option).toHaveClass("h-12");
    expect(option).toHaveClass("whitespace-nowrap");
    expect(screen.getByText("资料库基金1")).toHaveClass("truncate");
  });
});
