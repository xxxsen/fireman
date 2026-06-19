import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi, beforeEach } from "vitest";
import AssetsPage from "./page";

const deleteInstrumentMock = vi.hoisted(() => vi.fn());

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
      referencing_plan_count: 0,
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
      referencing_plan_count: 0,
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
      referencing_plan_count: 0,
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

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
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
  };
});

vi.mock("@/lib/api/instruments", () => ({
  listInstruments: vi.fn(),
  deleteInstrument: (...args: unknown[]) => deleteInstrumentMock(...args),
}));

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AssetsPage />
    </QueryClientProvider>,
  );
}

describe("AssetsPage", () => {
  beforeEach(() => {
    mockState.instruments = defaultInstruments.map((i) => ({ ...i }));
    mockState.isLoading = false;
    mockState.isError = false;
    mockState.error = null;
    mockState.isFetching = false;
    mockState.keepCachedData = false;
    mockState.refetch.mockClear();
    deleteInstrumentMock.mockReset();
    deleteInstrumentMock.mockResolvedValue({ deleted: true });
    window.confirm = vi.fn(() => true);
  });

  it("shows short-history simulation label for one-year instruments", () => {
    mockState.instruments = defaultInstruments.map((i) =>
      i.id === "inst_1"
        ? {
            ...i,
            simulation_eligible: true,
            history_depth: "one_year",
            complete_year_count: 1,
          }
        : { ...i },
    );
    renderPage();
    expect(screen.getAllByText("可用于模拟·历史样本有限").length).toBeGreaterThan(0);
  });

  it("renders instruments in table and mobile cards", () => {
    renderPage();
    expect(screen.getByRole("heading", { name: "资产资料库" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "510300" })).toHaveAttribute("href", "/assets/inst_1");
    expect(screen.getByTestId("instrument-cards")).toBeInTheDocument();
    expect(screen.getAllByTestId("instrument-card")).toHaveLength(3);
  });

  it("filters by search query", () => {
    renderPage();
    fireEvent.change(screen.getByTestId("assets-search"), { target: { value: "510300" } });
    expect(screen.getByRole("link", { name: "510300" })).toBeInTheDocument();
    expect(screen.queryByText("抓取失败示例")).not.toBeInTheDocument();
  });

  it("filters by status", () => {
    renderPage();
    fireEvent.change(screen.getByTestId("assets-status-filter"), {
      target: { value: "fetch_failed" },
    });
    expect(screen.getAllByText("抓取失败示例").length).toBeGreaterThan(0);
    expect(screen.queryByRole("link", { name: "510300" })).not.toBeInTheDocument();
  });

  it("shows filter-empty state", () => {
    renderPage();
    fireEvent.change(screen.getByTestId("assets-search"), { target: { value: "不存在" } });
    expect(screen.getByText("没有匹配的标的")).toBeInTheDocument();
  });

  it("shows loading skeleton without data", () => {
    mockState.isLoading = true;
    renderPage();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  it("shows error state with retry", () => {
    mockState.isError = true;
    mockState.error = new Error("boom");
    renderPage();
    fireEvent.click(screen.getByTestId("error-state-retry"));
    expect(mockState.refetch).toHaveBeenCalled();
  });

  it("shows library empty state", () => {
    mockState.instruments = [];
    renderPage();
    expect(screen.getByText("资料库为空")).toBeInTheDocument();
  });

  it("has single primary action to import", () => {
    renderPage();
    const primary = screen.getByTestId("page-header-primary");
    expect(primary).toHaveTextContent("录入资产");
    expect(primary).toHaveAttribute("href", "/assets/import");
  });

  it("shows context action for fetch_failed", () => {
    renderPage();
    const rows = screen.getAllByRole("row");
    const failedRow = rows.find((row) => within(row).queryByText("抓取失败示例"));
    expect(failedRow).toBeTruthy();
    expect(within(failedRow!).getByRole("link", { name: "查看并重试" })).toHaveAttribute(
      "href",
      "/assets/inst_2",
    );
  });

  it("shows context action for pending_fetch", () => {
    renderPage();
    const rows = screen.getAllByRole("row");
    const pendingRow = rows.find((row) => within(row).queryByText("抓取中示例"));
    expect(pendingRow).toBeTruthy();
    expect(within(pendingRow!).getByRole("link", { name: "查看进度" })).toHaveAttribute(
      "href",
      "/assets/inst_3",
    );
  });

  it("mobile pending_fetch card has no nested anchor links", () => {
    renderPage();
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
    renderPage();
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
    renderPage();
    expect(screen.getByTestId("error-state-back")).toHaveAttribute("href", "/");
  });

  it("keeps cached instruments visible when background refresh fails", () => {
    mockState.keepCachedData = true;
    mockState.isError = true;
    mockState.error = new Error("network");
    renderPage();
    expect(screen.getByRole("link", { name: "510300" })).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("shows delete button for deletable instruments", () => {
    renderPage();
    expect(screen.getAllByTestId("instrument-delete-inst_1")[0]).toBeEnabled();
  });

  it("disables delete when instrument is referenced by plans", () => {
    mockState.instruments = defaultInstruments.map((i) =>
      i.id === "inst_1" ? { ...i, referencing_plan_count: 2 } : { ...i },
    );
    renderPage();
    expect(screen.getAllByTestId("instrument-delete-inst_1")[0]).toBeDisabled();
    expect(screen.getAllByText("已被计划引用，无法删除").length).toBeGreaterThan(0);
  });

  it("calls deleteInstrument after confirm", async () => {
    renderPage();
    fireEvent.click(screen.getAllByTestId("instrument-delete-inst_1")[0]!);
    fireEvent.click(screen.getByTestId("confirm-dialog-confirm"));
    await waitFor(() => expect(deleteInstrumentMock).toHaveBeenCalledWith("inst_1"));
  });
});
