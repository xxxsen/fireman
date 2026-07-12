// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MarketAssetDetail } from "@/lib/api/market-assets";
import MarketAssetDetailPage from "./page";

const getMarketAssetDetailMock = vi.hoisted(() => vi.fn());
const setMarketAssetHistoryAutoUpdateMock = vi.hoisted(() => vi.fn());
const useWorkerTaskPollingMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useParams: () => ({
    assetKey: encodeURIComponent("CN|cn_mutual_fund||007194"),
  }),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  getMarketAssetDetail: (...args: unknown[]) =>
    getMarketAssetDetailMock(...args),
  setMarketAssetHistoryAutoUpdate: (...args: unknown[]) =>
    setMarketAssetHistoryAutoUpdateMock(...args),
}));

vi.mock("@/hooks/useWorkerTaskPolling", () => ({
  useWorkerTaskPolling: (...args: unknown[]) =>
    useWorkerTaskPollingMock(...args),
}));

// ECharts does not render in jsdom; the stub exposes what the page passed in
// so range filtering and cumulative-return re-zeroing are observable.
vi.mock("@/components/charts/ReturnSeriesChart", () => ({
  ReturnSeriesChart: ({
    points,
  }: {
    points: { date: string; cumulative_return: number }[];
  }) => (
    <div
      data-testid="return-chart"
      data-count={points.length}
      data-first-date={points[0]?.date}
      data-first-cr={points[0]?.cumulative_return}
    />
  ),
}));

const LONG_ERROR =
  "market_provider_unavailable: history fetch failed: " +
  "ak.fund_open_fund_info_em:累计净值走势: unsupported fund classification; " +
  "ak.fund_open_fund_info_em:单位净值走势: unsupported fund classification; " +
  "this diagnostic keeps going long enough to overflow any inline container";

function makeDetail(): MarketAssetDetail {
  return {
    asset: {
      asset_key: "CN|cn_mutual_fund||007194",
      market: "CN",
      instrument_type: "cn_mutual_fund",
      region_code: "",
      symbol: "007194",
      name: "长城短债A",
      exchange: "",
      instrument_kind: "",
      currency: "CNY",
      active: true,
      listing_status: "active",
      last_seen_at: 1751000000000,
      source_name: "ak.fund_name_em",
      source_as_of: "2026-07-01",
      refreshed_at: 1751000000000,
      created_at: 0,
      updated_at: 0,
    },
    history: {
      adjust_policy: "none",
      point_type: "nav",
      data_as_of: "",
      point_count: 0,
      source_name: "",
      last_success_at: null,
      last_success_task_id: "",
      task: {
        id: "wt_1",
        type: "asset_history_sync",
        status: "failed",
        error_code: "market_provider_unavailable",
        error_message: LONG_ERROR,
        created_at: 1751000000000,
      },
      can_switch_source: false,
    },
    points: [],
    annual_returns: [],
  };
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MarketAssetDetailPage />
    </QueryClientProvider>,
  );
}

/** Monthly points on the 1st, ascending; last one is 2026-07-01 when count=24 starting 2024-08. */
function monthlyPoints(startYear: number, startMonth: number, count: number) {
  return Array.from({ length: count }, (_, i) => {
    const d = new Date(startYear, startMonth - 1 + i, 1);
    const mm = String(d.getMonth() + 1).padStart(2, "0");
    return { date: `${d.getFullYear()}-${mm}-01`, value: 100 + i };
  });
}

function makeDetailWithHistory(): MarketAssetDetail {
  const detail = makeDetail();
  const points = monthlyPoints(2024, 8, 24);
  return {
    ...detail,
    history: {
      ...detail.history,
      task: null,
      point_count: points.length,
      data_as_of: points[points.length - 1]!.date,
      source_name: "ak.fund_etf_hist_em",
    },
    points,
  };
}

describe("MarketAssetDetailPage history range shortcuts", () => {
  beforeEach(() => {
    getMarketAssetDetailMock.mockReset();
    setMarketAssetHistoryAutoUpdateMock.mockReset();
    useWorkerTaskPollingMock.mockReset();
    useWorkerTaskPollingMock.mockReturnValue({ task: null, pollError: null });
    getMarketAssetDetailMock.mockResolvedValue(makeDetailWithHistory());
  });

  it("renders every range shortcut", async () => {
    renderPage();
    await screen.findByTestId("return-chart");
    for (const key of ["7d", "1m", "3m", "6m", "1y", "3y", "5y", "all"]) {
      expect(screen.getByTestId(`history-range-${key}`)).toBeInTheDocument();
    }
    expect(screen.getByTestId("history-range-1y")).toHaveTextContent("近 1 年");
    expect(screen.getByTestId("history-range-all")).toHaveTextContent("全部");
  });

  it("defaults to 近 1 年 when coverage exceeds one year", async () => {
    renderPage();
    const chart = await screen.findByTestId("return-chart");
    // 2024-08-01..2026-07-01 monthly; the 1y window 2025-07-01.. holds 13 points.
    expect(chart).toHaveAttribute("data-count", "13");
    expect(chart).toHaveAttribute("data-first-date", "2025-07-01");
    expect(screen.getByTestId("history-range-1y")).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByTestId("history-range-summary")).toHaveTextContent(
      "当前区间 近 1 年 · 13 / 24 个点",
    );
  });

  it("re-zeroes cumulative return on the first visible point after switching", async () => {
    renderPage();
    await screen.findByTestId("return-chart");
    fireEvent.click(screen.getByTestId("history-range-1m"));
    const chart = screen.getByTestId("return-chart");
    expect(chart).toHaveAttribute("data-count", "2");
    expect(chart).toHaveAttribute("data-first-date", "2026-06-01");
    // Base is the range's own first point, not the full-series first point.
    expect(chart).toHaveAttribute("data-first-cr", "0");
  });

  it("returns to the full series when 全部 is clicked", async () => {
    renderPage();
    await screen.findByTestId("return-chart");
    fireEvent.click(screen.getByTestId("history-range-all"));
    const chart = screen.getByTestId("return-chart");
    expect(chart).toHaveAttribute("data-count", "24");
    expect(chart).toHaveAttribute("data-first-date", "2024-08-01");
    expect(screen.getByTestId("history-range-all")).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    // And back to a shorter window.
    fireEvent.click(screen.getByTestId("history-range-1y"));
    expect(screen.getByTestId("return-chart")).toHaveAttribute(
      "data-count",
      "13",
    );
  });

  it("disables ranges without enough points and titles the reason", async () => {
    renderPage();
    await screen.findByTestId("return-chart");
    // Monthly data leaves at most 1 point in the last 7 days.
    const btn = screen.getByTestId("history-range-7d");
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("title", "该区间历史数据不足");
    expect(screen.getByTestId("history-range-3y")).toBeEnabled();
  });

  it("hides the range control when there is no history", async () => {
    getMarketAssetDetailMock.mockResolvedValue(makeDetail());
    renderPage();
    await screen.findByTestId("history-current-task");
    expect(screen.queryByTestId("history-range-all")).not.toBeInTheDocument();
  });
});

describe("MarketAssetDetailPage mutual fund fee identity", () => {
  beforeEach(() => {
    getMarketAssetDetailMock.mockReset();
    useWorkerTaskPollingMock.mockReset();
    useWorkerTaskPollingMock.mockReturnValue({ task: null, pollError: null });
  });

  it("explains shared NAV history without claiming transaction fees are included", async () => {
    const detail = makeDetailWithHistory();
    detail.asset.symbol = "000157";
    detail.asset.name = "富国全球科技互联网股票(QDII)A(后端)";
    detail.asset.canonical_symbol = "100055";
    detail.asset.fee_mode = "back_end";
    getMarketAssetDetailMock.mockResolvedValue(detail);

    renderPage();

    expect(await screen.findByTestId("fund-fee-mode")).toHaveTextContent(
      "后端收费",
    );
    expect(screen.getByTestId("canonical-fund-symbol")).toHaveTextContent(
      "100055",
    );
    expect(screen.getByTestId("fund-fee-history-note")).toHaveTextContent(
      "回测仅使用净值序列，不包含申购、赎回或后端申购费用",
    );
  });
});

describe("MarketAssetDetailPage failed-task error display", () => {
  beforeEach(() => {
    getMarketAssetDetailMock.mockReset();
    setMarketAssetHistoryAutoUpdateMock.mockReset();
    useWorkerTaskPollingMock.mockReset();
    useWorkerTaskPollingMock.mockReturnValue({ task: null, pollError: null });
    getMarketAssetDetailMock.mockResolvedValue(makeDetail());
  });

  it("constrains the current-task error so it can truncate instead of overflowing", async () => {
    renderPage();
    const dd = await screen.findByTestId("history-current-task");
    // The flex container must allow its children to shrink (min-w-0).
    expect(dd.className).toContain("min-w-0");

    const triggers = screen.getAllByTestId("task-error-inline");
    expect(triggers.length).toBeGreaterThan(0);
    for (const trigger of triggers) {
      // Tooltip wrapper constrains width; the text itself truncates.
      const wrapper = trigger.parentElement as HTMLElement;
      expect(wrapper.className).toContain("min-w-0");
      expect(wrapper.className).toContain("max-w-full");
      expect(wrapper.className).toContain("overflow-hidden");
      expect(trigger.className).toContain("truncate");
      expect(trigger.className).toContain("min-w-0");
      expect(trigger.className).toContain("max-w-full");
    }
  });

  it("shows the full error text in a tooltip on hover", async () => {
    renderPage();
    await screen.findByTestId("history-current-task");
    const trigger = screen.getAllByTestId("task-error-inline")[0];
    fireEvent.mouseOver(trigger);
    const tooltip = await screen.findAllByTestId("task-error-tooltip");
    expect(tooltip[0].textContent).toContain("unsupported fund classification");
    expect(tooltip[0].textContent).toContain("market_provider_unavailable");
  });
});

describe("MarketAssetDetailPage automatic update", () => {
  beforeEach(() => {
    getMarketAssetDetailMock.mockReset();
    setMarketAssetHistoryAutoUpdateMock.mockReset();
    useWorkerTaskPollingMock.mockReset();
    useWorkerTaskPollingMock.mockReturnValue({ task: null, pollError: null });
  });

  it("enables the current history dimension and renders the returned interval", async () => {
    const initial = makeDetailWithHistory();
    const updatedRule = {
      id: "aur_history",
      target_type: "asset_history" as const,
      sync_key: "",
      asset_key: initial.asset.asset_key,
      adjust_policy: initial.history.adjust_policy,
      point_type: initial.history.point_type,
      enabled: true,
      interval_hours: 24,
      next_run_at: Date.now(),
      last_task_id: "",
      last_error_code: "",
      last_error_message: "",
      version: 1,
      created_at: Date.now(),
      updated_at: Date.now(),
    };
    getMarketAssetDetailMock
      .mockResolvedValueOnce(initial)
      .mockResolvedValue({
        ...initial,
        history: { ...initial.history, auto_update: updatedRule },
      });
    setMarketAssetHistoryAutoUpdateMock.mockResolvedValue(updatedRule);
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "启用每日自动更新" }),
    );
    await waitFor(() =>
      expect(setMarketAssetHistoryAutoUpdateMock).toHaveBeenCalledWith({
        asset_key: initial.asset.asset_key,
        adjust_policy: initial.history.adjust_policy,
        point_type: initial.history.point_type,
        enabled: true,
      }),
    );
    expect(
      await screen.findByRole("button", { name: "自动更新：每 1 天" }),
    ).toBeInTheDocument();
  });

  it("pauses an enabled rule without changing its history dimension", async () => {
    const initial = makeDetailWithHistory();
    const enabledRule = {
      id: "aur_history",
      target_type: "asset_history" as const,
      sync_key: "",
      asset_key: initial.asset.asset_key,
      adjust_policy: initial.history.adjust_policy,
      point_type: initial.history.point_type,
      enabled: true,
      interval_hours: 6,
      next_run_at: Date.now(),
      last_task_id: "",
      last_error_code: "",
      last_error_message: "",
      version: 2,
      created_at: Date.now(),
      updated_at: Date.now(),
    };
    const disabledRule = {
      ...enabledRule,
      enabled: false,
      next_run_at: null,
      version: 3,
    };
    const enabledDetail = {
      ...initial,
      history: { ...initial.history, auto_update: enabledRule },
    };
    getMarketAssetDetailMock
      .mockResolvedValueOnce(enabledDetail)
      .mockResolvedValue({
        ...initial,
        history: { ...initial.history, auto_update: disabledRule },
      });
    setMarketAssetHistoryAutoUpdateMock.mockResolvedValue(disabledRule);
    renderPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "自动更新：每 6 小时" }),
    );
    await waitFor(() =>
      expect(setMarketAssetHistoryAutoUpdateMock).toHaveBeenCalledWith({
        asset_key: initial.asset.asset_key,
        adjust_policy: initial.history.adjust_policy,
        point_type: initial.history.point_type,
        enabled: false,
      }),
    );
    expect(
      await screen.findByRole("button", { name: "启用每日自动更新" }),
    ).toBeInTheDocument();
  });
});
