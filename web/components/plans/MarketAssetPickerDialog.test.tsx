// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import type { MarketAsset } from "@/lib/api/market-assets";
import { MarketAssetPickerDialog } from "./MarketAssetPickerDialog";

const listMarketAssets = vi.fn();

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssets(...args),
}));

// Mirrors the backend's instrument_type_priority so mocked list responses
// carry the same ordering facts as GET /market-assets.
const BACKEND_TYPE_PRIORITY: Record<string, number> = {
  cn_mutual_fund: 0,
  cn_exchange_fund: 1,
  cn_exchange_stock: 2,
};

function makeMarketAsset(i: number, overrides: Partial<MarketAsset> = {}): MarketAsset {
  const base: MarketAsset = {
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
  base.instrument_type_priority ??=
    BACKEND_TYPE_PRIORITY[base.instrument_type] ?? 3;
  return base;
}

let pool: MarketAsset[] = [];

function renderDialog(props: Partial<Parameters<typeof MarketAssetPickerDialog>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onSelect = vi.fn();
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MarketAssetPickerDialog
        open
        onClose={onClose}
        onSelect={onSelect}
        excludeAssetKeys={new Set()}
        {...props}
      />
    </QueryClientProvider>,
  );
  return { onSelect, onClose };
}

describe("MarketAssetPickerDialog", () => {
  beforeEach(() => {
    listMarketAssets.mockReset();
    pool = [makeMarketAsset(1), makeMarketAsset(2)];
    listMarketAssets.mockImplementation((params: { symbolQ?: string; nameQ?: string }) => {
      const symbolQ = (params.symbolQ ?? "").toLowerCase();
      const nameQ = (params.nameQ ?? "").toLowerCase();
      const items = pool.filter(
        (a) =>
          (!symbolQ || a.symbol.toLowerCase().includes(symbolQ)) &&
          (!nameQ || a.name.toLowerCase().includes(nameQ)),
      );
      return Promise.resolve({ assets: items, syncs: [], total: items.length });
    });
  });

  it("debounces typing: no request for the query within 250ms", async () => {
    vi.useFakeTimers();
    try {
      renderDialog();
      // Flush the initial empty-query request.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });
      listMarketAssets.mockClear();

      fireEvent.change(screen.getByTestId("wizard-holding-search"), { target: { value: "510301" } });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(200);
      });
      expect(listMarketAssets).not.toHaveBeenCalledWith(
        expect.objectContaining({ symbolQ: "510301" }),
      );

      await act(async () => {
        await vi.advanceTimersByTimeAsync(100);
      });
      expect(listMarketAssets).toHaveBeenCalledWith(
        expect.objectContaining({ symbolQ: "510301" }),
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it("hides excluded asset keys from the candidate list", async () => {
    renderDialog({ excludeAssetKeys: new Set([makeMarketAsset(1).asset_key]) });
    expect(await screen.findByRole("button", { name: /目录基金2/ })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /目录基金1/ })).not.toBeInTheDocument();
  });

	it("hides foreign-currency cash but keeps CNY cash", async () => {
		pool = [
			makeMarketAsset(1, { asset_key: "SYS|cash||CNY", instrument_type: "cash", symbol: "CNY", name: "人民币现金", currency: "CNY" }),
			makeMarketAsset(2, { asset_key: "SYS|cash||USD", instrument_type: "cash", symbol: "USD", name: "美元现金", currency: "USD" }),
		];
		renderDialog();
		expect(await screen.findByRole("button", { name: /人民币现金/ })).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: /美元现金/ })).not.toBeInTheDocument();
	});

  it("invokes onSelect with the picked asset", async () => {
    const { onSelect } = renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: /目录基金1/ }));
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ asset_key: makeMarketAsset(1).asset_key }),
    );
  });

  it("shows the identity-conflict hint and sorts by response priority", async () => {
    // The stock identity comes first in API order; the response-provided
    // priority (mutual fund = 0) must reorder it to the top.
    pool = [
      makeMarketAsset(2, {
        asset_key: "CN|cn_exchange_stock|sz|150015",
        instrument_type: "cn_exchange_stock",
        symbol: "150015",
        name: "同码股票",
      }),
      makeMarketAsset(1, {
        asset_key: "CN|cn_mutual_fund||150015",
        instrument_type: "cn_mutual_fund",
        symbol: "150015",
        name: "货币基金",
      }),
    ];
    renderDialog();
    fireEvent.change(screen.getByTestId("wizard-holding-search"), { target: { value: "150015" } });
    expect(
      await screen.findByTestId("picker-identity-conflict-hint"),
    ).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: /150015/ });
    expect(buttons[0]).toHaveTextContent("货币基金");
  });
});
