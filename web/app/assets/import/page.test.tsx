// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MarketAsset } from "@/lib/api/market-assets";

const listMarketAssetsMock = vi.hoisted(() => vi.fn());
const getMarketAssetDetailMock = vi.hoisted(() => vi.fn());
const importFromMarketAssetMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());
const searchParamsGetMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPushMock }),
  useSearchParams: () => ({ get: searchParamsGetMock }),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssetsMock(...args),
  getMarketAssetDetail: (...args: unknown[]) => getMarketAssetDetailMock(...args),
  importFromMarketAsset: (...args: unknown[]) => importFromMarketAssetMock(...args),
}));

import ImportAssetPage from "./page";

const ASSET: MarketAsset = {
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
  last_seen_at: 0,
  source_name: "ak.fund_etf_spot_em",
  source_as_of: "",
  refreshed_at: 0,
  created_at: 0,
  updated_at: 0,
};

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ImportAssetPage />
    </QueryClientProvider>,
  );
}

async function chooseCandidate() {
  fireEvent.change(screen.getByTestId("import-search-input"), { target: { value: "510300" } });
  fireEvent.click(await screen.findByTestId(`import-candidate-${ASSET.asset_key}`));
  await screen.findByTestId("confirm-import");
}

describe("ImportAssetPage", () => {
  beforeEach(() => {
    listMarketAssetsMock.mockReset();
    getMarketAssetDetailMock.mockReset();
    importFromMarketAssetMock.mockReset();
    routerPushMock.mockReset();
    searchParamsGetMock.mockReset();
    searchParamsGetMock.mockReturnValue(null);
    listMarketAssetsMock.mockResolvedValue({ assets: [ASSET], syncs: [], total: 1 });
  });

  it("searches the local directory and lists candidates", async () => {
    renderPage();
    fireEvent.change(screen.getByTestId("import-search-input"), {
      target: { value: "510300" },
    });
    expect(
      await screen.findByTestId(`import-candidate-${ASSET.asset_key}`),
    ).toHaveTextContent("沪深300ETF");
    await waitFor(() =>
      expect(listMarketAssetsMock).toHaveBeenCalledWith(
        expect.objectContaining({ q: "510300", limit: 20 }),
      ),
    );
  });

  it("shows the directory hint when the local search has no hits", async () => {
    listMarketAssetsMock.mockResolvedValue({ assets: [], syncs: [], total: 0 });
    renderPage();
    fireEvent.change(screen.getByTestId("import-search-input"), {
      target: { value: "999999" },
    });
    expect(
      await screen.findByText(/未在本地资产目录中找到匹配资产/),
    ).toBeInTheDocument();
  });

  it("requires asset class and region before importing", async () => {
    renderPage();
    await chooseCandidate();

    const confirm = screen.getByTestId("confirm-import");
    expect(confirm).toBeDisabled();

    fireEvent.change(screen.getByTestId("asset-class-select"), { target: { value: "equity" } });
    expect(confirm).toBeDisabled();

    fireEvent.change(screen.getByTestId("region-select"), { target: { value: "domestic" } });
    expect(confirm).toBeEnabled();
  });

  it("imports the selected asset and navigates to the new instrument", async () => {
    importFromMarketAssetMock.mockResolvedValue({ id: "ins_new" });
    renderPage();
    await chooseCandidate();

    fireEvent.change(screen.getByTestId("asset-class-select"), { target: { value: "equity" } });
    fireEvent.change(screen.getByTestId("region-select"), { target: { value: "domestic" } });
    fireEvent.click(screen.getByTestId("confirm-import"));

    await waitFor(() =>
      expect(importFromMarketAssetMock).toHaveBeenCalledWith({
        asset_key: ASSET.asset_key,
        asset_class: "equity",
        region: "domestic",
      }),
    );
    await waitFor(() => expect(routerPushMock).toHaveBeenCalledWith("/assets/ins_new"));
  });

  it("guides to the market asset detail page when history is empty", async () => {
    const { ApiError } = await import("@/lib/api/client");
    importFromMarketAssetMock.mockRejectedValue(
      new ApiError("market_asset_history_empty", "history empty"),
    );
    renderPage();
    await chooseCandidate();

    fireEvent.change(screen.getByTestId("asset-class-select"), { target: { value: "equity" } });
    fireEvent.change(screen.getByTestId("region-select"), { target: { value: "domestic" } });
    fireEvent.click(screen.getByTestId("confirm-import"));

    expect(await screen.findByTestId("import-error")).toHaveTextContent(
      "该资产还没有本地历史数据",
    );
    expect(screen.getByTestId("go-sync-history")).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent(ASSET.asset_key)}`,
    );
    expect(routerPushMock).not.toHaveBeenCalled();
  });

  it("links to the existing instrument when the asset was already imported", async () => {
    const { ApiError } = await import("@/lib/api/client");
    importFromMarketAssetMock.mockRejectedValue(
      new ApiError("instrument_already_exists", "exists", { instrument_id: "ins_dup" }),
    );
    renderPage();
    await chooseCandidate();

    fireEvent.change(screen.getByTestId("asset-class-select"), { target: { value: "equity" } });
    fireEvent.change(screen.getByTestId("region-select"), { target: { value: "domestic" } });
    fireEvent.click(screen.getByTestId("confirm-import"));

    expect(await screen.findByTestId("import-error")).toHaveTextContent("已录入资产库");
    expect(screen.getByRole("link", { name: "查看已录入的标的" })).toHaveAttribute(
      "href",
      "/assets/ins_dup",
    );
  });

  it("preselects the asset when arriving with an asset_key query param", async () => {
    searchParamsGetMock.mockImplementation((key: string) =>
      key === "asset_key" ? ASSET.asset_key : null,
    );
    getMarketAssetDetailMock.mockResolvedValue({
      asset: ASSET,
      history: {},
      points: [],
      annual_returns: [],
    });
    renderPage();

    expect(await screen.findByTestId("confirm-import")).toBeInTheDocument();
    expect(getMarketAssetDetailMock).toHaveBeenCalledWith(ASSET.asset_key);
    expect(screen.getByText("沪深300ETF")).toBeInTheDocument();
  });

  it("returns to the search stage via 重新选择", async () => {
    renderPage();
    await chooseCandidate();

    fireEvent.click(screen.getByRole("button", { name: "重新选择" }));
    expect(screen.getByTestId("import-search-input")).toBeInTheDocument();
    expect(screen.queryByTestId("confirm-import")).not.toBeInTheDocument();
  });
});
