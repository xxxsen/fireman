import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import AssetsPage from "./page";

const { defaultInstruments, mockState } = vi.hoisted(() => {
  const defaultInstruments = [
    {
      id: "inst_1",
      code: "510300",
      name: "沪深300ETF",
      market: "SH",
      instrument_type: "etf",
      asset_class: "equity",
      region: "domestic",
      currency: "CNY",
      provider: "akshare",
      is_system: false,
      expense_ratio_status: "unknown",
      fee_treatment: "deduct",
      status: "active",
      quality_status: "available",
      data_source_name: "akshare",
      data_stale: false,
      created_at: 0,
      updated_at: 0,
    },
    {
      id: "inst_2",
      code: "999999",
      name: "抓取失败示例",
      market: "SH",
      instrument_type: "etf",
      asset_class: "equity",
      region: "domestic",
      currency: "CNY",
      provider: "akshare",
      is_system: false,
      expense_ratio_status: "unknown",
      fee_treatment: "deduct",
      status: "fetch_failed",
      data_stale: false,
      created_at: 0,
      updated_at: 0,
    },
    {
      id: "inst_3",
      code: "888888",
      name: "抓取中示例",
      market: "SH",
      instrument_type: "etf",
      asset_class: "equity",
      region: "domestic",
      currency: "CNY",
      provider: "akshare",
      is_system: false,
      expense_ratio_status: "unknown",
      fee_treatment: "deduct",
      status: "pending_fetch",
      data_stale: false,
      created_at: 0,
      updated_at: 0,
    },
  ];
  return {
    defaultInstruments,
    mockState: {
      instruments: defaultInstruments.map((i) => ({ ...i })),
      isLoading: false,
      isError: false,
      error: null as Error | null,
      isFetching: false,
      refetch: vi.fn(),
      keepCachedData: false,
    },
  };
});

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data:
      mockState.isLoading && !mockState.keepCachedData
        ? undefined
        : mockState.isError && !mockState.keepCachedData
          ? undefined
          : { instruments: mockState.instruments },
    isLoading: mockState.isLoading,
    isError: mockState.isError,
    error: mockState.error,
    isFetching: mockState.isFetching,
    refetch: mockState.refetch,
  }),
}));

describe("AssetsPage", () => {
  beforeEach(() => {
    mockState.instruments = defaultInstruments.map((i) => ({ ...i }));
    mockState.isLoading = false;
    mockState.isError = false;
    mockState.error = null;
    mockState.isFetching = false;
    mockState.keepCachedData = false;
    mockState.refetch.mockClear();
  });

  it("renders instruments in table and mobile cards", () => {
    render(<AssetsPage />);
    expect(screen.getByRole("heading", { name: "资产资料库" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "510300" })).toHaveAttribute("href", "/assets/inst_1");
    expect(screen.getByTestId("instrument-cards")).toBeInTheDocument();
    expect(screen.getAllByTestId("instrument-card")).toHaveLength(3);
  });

  it("filters by search query", () => {
    render(<AssetsPage />);
    fireEvent.change(screen.getByTestId("assets-search"), { target: { value: "510300" } });
    expect(screen.getByRole("link", { name: "510300" })).toBeInTheDocument();
    expect(screen.queryByText("抓取失败示例")).not.toBeInTheDocument();
  });

  it("filters by status", () => {
    render(<AssetsPage />);
    fireEvent.change(screen.getByTestId("assets-status-filter"), {
      target: { value: "fetch_failed" },
    });
    expect(screen.getAllByText("抓取失败示例").length).toBeGreaterThan(0);
    expect(screen.queryByRole("link", { name: "510300" })).not.toBeInTheDocument();
  });

  it("shows filter-empty state", () => {
    render(<AssetsPage />);
    fireEvent.change(screen.getByTestId("assets-search"), { target: { value: "不存在" } });
    expect(screen.getByText("没有匹配的标的")).toBeInTheDocument();
  });

  it("shows loading skeleton without data", () => {
    mockState.isLoading = true;
    render(<AssetsPage />);
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  it("shows error state with retry", () => {
    mockState.isError = true;
    mockState.error = new Error("boom");
    render(<AssetsPage />);
    fireEvent.click(screen.getByTestId("error-state-retry"));
    expect(mockState.refetch).toHaveBeenCalled();
  });

  it("shows library empty state", () => {
    mockState.instruments = [];
    render(<AssetsPage />);
    expect(screen.getByText("资料库为空")).toBeInTheDocument();
  });

  it("has single primary action to import", () => {
    render(<AssetsPage />);
    const primary = screen.getByTestId("page-header-primary");
    expect(primary).toHaveTextContent("录入资产");
    expect(primary).toHaveAttribute("href", "/assets/import");
  });

  it("shows context action for fetch_failed", () => {
    render(<AssetsPage />);
    const rows = screen.getAllByRole("row");
    const failedRow = rows.find((row) => within(row).queryByText("抓取失败示例"));
    expect(failedRow).toBeTruthy();
    expect(within(failedRow!).getByRole("link", { name: "查看并重试" })).toHaveAttribute(
      "href",
      "/assets/inst_2",
    );
  });

  it("shows context action for pending_fetch", () => {
    render(<AssetsPage />);
    const rows = screen.getAllByRole("row");
    const pendingRow = rows.find((row) => within(row).queryByText("抓取中示例"));
    expect(pendingRow).toBeTruthy();
    expect(within(pendingRow!).getByRole("link", { name: "查看进度" })).toHaveAttribute(
      "href",
      "/assets/inst_3",
    );
  });

  it("mobile pending_fetch card has no nested anchor links", () => {
    render(<AssetsPage />);
    const cards = screen.getAllByTestId("instrument-card");
    const pendingCard = cards.find((card) => within(card).queryByText("抓取中示例"));
    expect(pendingCard).toBeTruthy();
    expect(within(pendingCard!).getByRole("link", { name: "查看进度" })).toHaveAttribute(
      "href",
      "/assets/inst_3",
    );
    expect(within(pendingCard!).queryAllByRole("link")).toHaveLength(1);
  });

  it("mobile fetch_failed card has no nested anchor links", () => {
    render(<AssetsPage />);
    const cards = screen.getAllByTestId("instrument-card");
    const failedCard = cards.find((card) => within(card).queryByText("抓取失败示例"));
    expect(failedCard).toBeTruthy();
    expect(within(failedCard!).getByRole("link", { name: "查看并重试" })).toHaveAttribute(
      "href",
      "/assets/inst_2",
    );
    expect(within(failedCard!).queryAllByRole("link")).toHaveLength(1);
  });

  it("shows back link on error state", () => {
    mockState.isError = true;
    mockState.error = new Error("boom");
    render(<AssetsPage />);
    expect(screen.getByTestId("error-state-back")).toHaveAttribute("href", "/");
  });

  it("keeps cached instruments visible when background refresh fails", () => {
    mockState.keepCachedData = true;
    mockState.isError = true;
    mockState.error = new Error("network");
    render(<AssetsPage />);
    expect(screen.getByRole("link", { name: "510300" })).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });
});
