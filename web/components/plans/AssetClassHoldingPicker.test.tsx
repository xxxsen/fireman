// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import { AssetClassHoldingPicker } from "./AssetClassHoldingPicker";

const resolveImport = vi.fn();
const importAsync = vi.fn();
const getFetchStatus = vi.fn();
const getInstrument = vi.fn();
const searchInstruments = vi.fn();

vi.mock("@/lib/api/instruments", () => ({
  resolveImport: (...args: unknown[]) => resolveImport(...args),
  importAsync: (...args: unknown[]) => importAsync(...args),
  getFetchStatus: (...args: unknown[]) => getFetchStatus(...args),
  getInstrument: (...args: unknown[]) => getInstrument(...args),
  searchInstruments: (...args: unknown[]) => searchInstruments(...args),
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
    resolveImport.mockReset();
    importAsync.mockReset();
    getFetchStatus.mockReset();
    getInstrument.mockReset();
    searchInstruments.mockReset();
    pool = [makeInstrument(1)];
    searchInstruments.mockImplementation((params: SearchParams) => filterPool(params));
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

  it("queries AKShare when library has no exact code match", async () => {
    pool = [];
    resolveImport.mockResolvedValueOnce({
      ambiguous: false,
      resolved: {
        code: "270042",
        provider_symbol: "270042",
        name: "广发纳指100ETF联接（QDII）人民币A",
        exchange: "",
        instrument_kind: "mutual_fund",
        ticket_id: "ticket_1",
        is_importable: true,
      },
    });
    renderPicker();

    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });

    await waitFor(() => {
      expect(resolveImport).toHaveBeenCalledWith({
        market: "CN",
        instrument_type: "cn_exchange_fund",
        code: "270042",
      });
    });

    expect(await screen.findByTestId("wizard-external-results")).toBeInTheDocument();
    expect(screen.getByText(/资料库未收录/)).toBeInTheDocument();
  });

  it("never queries AKShare when the library search (still pending) ends up holding the code", async () => {
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
    // While the local paginated search is pending, AKShare must not be queried.
    await new Promise((r) => setTimeout(r, 50));
    expect(resolveImport).not.toHaveBeenCalled();

    // Settle the local search with an exact library hit.
    settleCodeSearch();
    expect(await screen.findByRole("button", { name: /资料库已收录基金/ })).toBeInTheDocument();
    // The library holds the code → external resolution must never run.
    expect(resolveImport).not.toHaveBeenCalled();
  });

  it("imports external candidate and adds instrument to selection", async () => {
    pool = [];
    resolveImport.mockResolvedValueOnce({
      ambiguous: false,
      resolved: {
        code: "270042",
        provider_symbol: "270042",
        name: "广发纳指100ETF联接（QDII）人民币A",
        exchange: "",
        instrument_kind: "mutual_fund",
        ticket_id: "ticket_1",
        is_importable: true,
      },
    });
    importAsync.mockResolvedValueOnce({ instrument_id: "ins_new", job_id: "job_1", status: "queued" });
    getFetchStatus.mockResolvedValueOnce({ instrument_status: "active", progress_current: 1, progress_total: 1 });
    getInstrument.mockResolvedValueOnce({
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
      expect(importAsync).toHaveBeenCalledWith({
        ticket_id: "ticket_1",
        asset_class: "equity",
        region: "domestic",
      });
    });
    await waitFor(() => {
      expect(onSelectedChange).toHaveBeenCalled();
    });
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
});
