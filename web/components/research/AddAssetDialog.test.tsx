import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchAssetView } from "@/lib/api/research";
import { AddAssetDialog } from "./AddAssetDialog";

const listResearchAssetsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  listResearchAssets: (...args: unknown[]) => listResearchAssetsMock(...args),
}));

function asset(overrides: Partial<ResearchAssetView> = {}): ResearchAssetView {
  return {
    asset_key: "CN|fund|sh|510300",
    market: "CN",
    instrument_type: "cn_exchange_fund",
    instrument_type_label: "场内 ETF / LOF",
    region_code: "sh",
    symbol: "510300",
    name: "沪深300ETF",
    exchange: "SSE",
    instrument_kind: "fund",
    currency: "CNY",
    active: true,
    listing_status: "active",
    is_cash: false,
    has_history: true,
    adjust_policy: "qfq",
    point_type: "adjusted_close",
    point_count: 1000,
    stale: false,
    fx_available: true,
    backtest_ready: true,
    quality_badges: ["normal"],
    ...overrides,
  };
}

function renderDialog(props: Partial<Parameters<typeof AddAssetDialog>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AddAssetDialog
        open
        onClose={vi.fn()}
        existingAssetKeys={new Set()}
        onAdd={vi.fn()}
        addPending={false}
        {...props}
      />
    </QueryClientProvider>,
  );
}

describe("AddAssetDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listResearchAssetsMock.mockResolvedValue({ assets: [asset()], total: 1 });
  });

  it("searches assets and renders results inside the fixed results container", async () => {
    renderDialog();
    const results = await screen.findByTestId("add-asset-results");
    expect(results).toHaveClass("min-h-0", "flex-1", "overflow-y-auto");
    expect(listResearchAssetsMock).toHaveBeenCalledWith({ q: "", limit: 20 });
    expect(await screen.findByText("沪深300ETF")).toBeInTheDocument();
  });

  it("keeps loading and empty states inside the same results container", async () => {
    listResearchAssetsMock.mockResolvedValueOnce({ assets: [], total: 0 });
    renderDialog();
    const results = await screen.findByTestId("add-asset-results");
    expect(await screen.findByText("无匹配资产。")).toBeInTheDocument();
    expect(results).toContainElement(screen.getByText("无匹配资产。"));
  });

  it("lets users type and add a returned asset", async () => {
    const onAdd = vi.fn();
    renderDialog({ onAdd });
    fireEvent.change(screen.getByTestId("add-asset-search"), { target: { value: "510300" } });
    await waitFor(() => expect(listResearchAssetsMock).toHaveBeenLastCalledWith({ q: "510300", limit: 20 }));
    fireEvent.click(await screen.findByTestId("add-CN|fund|sh|510300"));
    expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ asset_key: "CN|fund|sh|510300" }));
  });

  it("marks existing assets as already added", async () => {
    renderDialog({ existingAssetKeys: new Set(["CN|fund|sh|510300"]) });
    const button = await screen.findByTestId("add-CN|fund|sh|510300");
    expect(button).toBeDisabled();
    expect(button).toHaveTextContent("已加入");
  });
});
