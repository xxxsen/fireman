// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MarketAssetDetail } from "@/lib/api/market-assets";
import MarketAssetDetailPage from "./page";

const getMarketAssetDetailMock = vi.hoisted(() => vi.fn());
const useWorkerTaskPollingMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useParams: () => ({ assetKey: encodeURIComponent("CN|cn_mutual_fund||007194") }),
}));

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  getMarketAssetDetail: (...args: unknown[]) => getMarketAssetDetailMock(...args),
}));

vi.mock("@/hooks/useWorkerTaskPolling", () => ({
  useWorkerTaskPolling: (...args: unknown[]) => useWorkerTaskPollingMock(...args),
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
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MarketAssetDetailPage />
    </QueryClientProvider>,
  );
}

describe("MarketAssetDetailPage failed-task error display", () => {
  beforeEach(() => {
    getMarketAssetDetailMock.mockReset();
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
