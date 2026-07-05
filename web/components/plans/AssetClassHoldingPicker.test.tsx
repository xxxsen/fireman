// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import type { MarketAsset } from "@/lib/api/market-assets";
import { AssetClassHoldingPicker, marketAssetToWizardAsset } from "./AssetClassHoldingPicker";

const listMarketAssets = vi.fn();

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssets(...args),
}));

interface ListParams {
  symbolQ?: string;
  nameQ?: string;
  market?: string;
  instrumentTypes?: string[];
  limit?: number;
  offset?: number;
}

function makeMarketAsset(i: number, overrides: Partial<MarketAsset> = {}): MarketAsset {
  return {
    asset_key: `CN|cn_exchange_fund|sh|51030${i}`,
    market: "CN",
    instrument_type: "cn_exchange_fund",
    region_code: "sh",
    symbol: `51030${i}`,
    name: `目录基金${i}`,
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
    has_history: true,
    history_data_as_of: "2026-07-01",
    history_source_name: "ak.fund_etf_hist_em",
    ...overrides,
  };
}

let pool: MarketAsset[] = [makeMarketAsset(1)];

function filterPool(params: ListParams) {
  const symbolQ = (params.symbolQ ?? "").toLowerCase();
  const nameQ = (params.nameQ ?? "").toLowerCase();
  const items = pool.filter(
    (a) =>
      (!symbolQ || a.symbol.toLowerCase().includes(symbolQ)) &&
      (!nameQ || a.name.toLowerCase().includes(nameQ)),
  );
  const offset = params.offset ?? 0;
  const limit = params.limit ?? 10;
  const page = items.slice(offset, offset + limit);
  return Promise.resolve({ assets: page, syncs: [], total: items.length });
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
    listMarketAssets.mockReset();
    pool = [makeMarketAsset(1)];
    listMarketAssets.mockImplementation((params: ListParams) => filterPool(params));
  });

  it("loads the first page of directory assets on focus without typing", async () => {
    pool = Array.from({ length: 5 }, (_, i) => makeMarketAsset(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    expect(await screen.findByRole("button", { name: /目录基金1/ })).toBeInTheDocument();
    await waitFor(() =>
      expect(listMarketAssets).toHaveBeenCalledWith(
        expect.objectContaining({ offset: 0, limit: 10 }),
      ),
    );
  });

  it("appends the next page when the sentinel scrolls into view", async () => {
    pool = Array.from({ length: 12 }, (_, i) => makeMarketAsset(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    // Page 2 contains the 11th and 12th assets.
    expect(await screen.findByRole("button", { name: /目录基金11/ })).toBeInTheDocument();
    await waitFor(() =>
      expect(listMarketAssets).toHaveBeenCalledWith(expect.objectContaining({ offset: 10 })),
    );
  });

  it("hides already-selected assets from the candidate list", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeMarketAsset(i + 1));
    renderPicker([
      {
        inst: marketAssetToWizardAsset(makeMarketAsset(1), "equity", "domestic"),
        weight: 1,
        amount: 0,
      },
    ]);
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    expect(await screen.findByRole("button", { name: /目录基金2/ })).toBeInTheDocument();
    const list = screen.getByTestId("wizard-library-results");
    expect(list).not.toHaveTextContent("目录基金1");
  });

  it("searches by symbol_q for code-like queries after the debounce", async () => {
    renderPicker();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "510301" },
    });
    await waitFor(() =>
      expect(listMarketAssets).toHaveBeenCalledWith(
        expect.objectContaining({ symbolQ: "510301" }),
      ),
    );
  });

  it("searches by name_q for non-code queries after the debounce", async () => {
    renderPicker();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "目录基金" },
    });
    await waitFor(() =>
      expect(listMarketAssets).toHaveBeenCalledWith(
        expect.objectContaining({ nameQ: "目录基金" }),
      ),
    );
  });

  it("passes market and instrument type filters to the directory query", async () => {
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    fireEvent.change(screen.getByTestId("wizard-picker-market-filter"), {
      target: { value: "HK" },
    });
    fireEvent.change(screen.getByTestId("wizard-picker-type-filter"), {
      target: { value: "hk_etf" },
    });
    await waitFor(() =>
      expect(listMarketAssets).toHaveBeenCalledWith(
        expect.objectContaining({ market: "HK", instrumentTypes: ["hk_etf"] }),
      ),
    );
  });

  it("adds a selected asset with the group's classification", async () => {
    const { onSelectedChange } = renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    fireEvent.click(await screen.findByRole("button", { name: /目录基金1/ }));

    await waitFor(() => expect(onSelectedChange).toHaveBeenCalled());
    const next = onSelectedChange.mock.calls[0][0];
    expect(next).toHaveLength(1);
    expect(next[0].inst).toMatchObject({
      id: "CN|cn_exchange_fund|sh|510301",
      code: "510301",
      asset_class: "equity",
      region: "domestic",
      has_history: true,
    });
  });

  it("allows selecting an asset without history and shows the sync hint", async () => {
    pool = [
      makeMarketAsset(1, {
        has_history: false,
        history_data_as_of: undefined,
        history_source_name: undefined,
      }),
    ];
    const { onSelectedChange } = renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    const option = await screen.findByRole("button", { name: /目录基金1/ });
    expect(option).toHaveTextContent("未同步历史，模拟前需要同步");

    fireEvent.click(option);
    await waitFor(() => expect(onSelectedChange).toHaveBeenCalled());
    expect(onSelectedChange.mock.calls[0][0][0].inst.has_history).toBe(false);
  });

  it("shows history status for synced candidates", async () => {
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    const option = await screen.findByRole("button", { name: /目录基金1/ });
    expect(option).toHaveTextContent("数据截至 2026-07-01");
  });

  it("shows the syncing state while a history sync task is active", async () => {
    pool = [
      makeMarketAsset(1, {
        has_history: false,
        history_data_as_of: undefined,
        history_source_name: undefined,
        history_sync_status: "running",
      }),
    ];
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    const option = await screen.findByRole("button", { name: /目录基金1/ });
    expect(option).toHaveTextContent("历史同步中…");
  });

  it("shows the failure summary when the history sync task failed", async () => {
    pool = [
      makeMarketAsset(1, {
        has_history: false,
        history_data_as_of: undefined,
        history_source_name: undefined,
        history_sync_status: "failed",
        history_sync_error: "上游超时",
      }),
    ];
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    const option = await screen.findByRole("button", { name: /目录基金1/ });
    expect(option).toHaveTextContent("历史同步失败：上游超时，可在详情页重新同步");
  });

  it("links each candidate to the asset detail page", async () => {
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    await screen.findByRole("button", { name: /目录基金1/ });
    expect(screen.getByRole("link", { name: "详情" })).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent("CN|cn_exchange_fund|sh|510301")}`,
    );
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
    pool = Array.from({ length: 3 }, (_, i) => makeMarketAsset(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    expect(await screen.findByTestId("wizard-library-results")).toBeInTheDocument();

    fireEvent.pointerDown(document.body);
    await waitFor(() =>
      expect(screen.queryByTestId("wizard-library-results")).not.toBeInTheDocument(),
    );
  });

  it("closes the candidate dropdown on Escape", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeMarketAsset(i + 1));
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
    renderPicker([
      {
        inst: marketAssetToWizardAsset(makeMarketAsset(1), "equity", "domestic"),
        weight: 1,
        amount: 0,
      },
    ]);
    const selectedRows = screen.getByTestId("wizard-selected-rows");
    const search = screen.getByTestId("wizard-holding-search");
    expect(
      selectedRows.compareDocumentPosition(search) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("renders the dropdown at a fixed 10-row height with single-line rows", async () => {
    pool = Array.from({ length: 3 }, (_, i) => makeMarketAsset(i + 1));
    renderPicker();
    fireEvent.focus(screen.getByTestId("wizard-holding-search"));
    const option = await screen.findByRole("button", { name: /目录基金1/ });

    const list = screen.getByTestId("wizard-library-results");
    expect(list).toHaveClass("h-[30rem]");

    expect(option).toHaveClass("h-full");
    expect(option.parentElement).toHaveClass("h-12");
    expect(option.parentElement).toHaveClass("whitespace-nowrap");
    expect(screen.getByText("目录基金1")).toHaveClass("truncate");
  });
});
