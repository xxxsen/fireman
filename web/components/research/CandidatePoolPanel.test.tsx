import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchAssetView } from "@/lib/api/research";
import { CandidatePoolPanel } from "./CandidatePoolPanel";

const createCollectionMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPushMock }),
}));

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  createCollection: (...args: unknown[]) => createCollectionMock(...args),
}));

function candidate(key: string): ResearchAssetView {
  return {
    asset_key: key,
    market: "cn",
    instrument_type: "cn_exchange_fund",
    instrument_type_label: "场内 ETF / LOF",
    region_code: "sh",
    symbol: key,
    name: `资产${key}`,
    exchange: "SSE",
    instrument_kind: "index_etf",
    currency: "CNY",
    active: true,
    listing_status: "active",
    is_cash: false,
    has_history: true,
    adjust_policy: "qfq",
    point_type: "adjusted_close",
    point_count: 100,
    stale: false,
    fx_available: true,
    backtest_ready: true,
    quality_badges: ["normal"],
  };
}

describe("CandidatePoolPanel", () => {
  beforeEach(() => vi.clearAllMocks());

  it("creates a collection whose equal weights sum to exactly 1", async () => {
    createCollectionMock.mockResolvedValue({ id: "rc_x" });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    // 6 candidates: naive per-item rounding of 1/6 would sum to 1.000002.
    const candidates = ["a", "b", "c", "d", "e", "f"].map(candidate);
    render(
      <QueryClientProvider client={client}>
        <CandidatePoolPanel
          candidates={candidates}
          onRemove={() => {}}
          onClear={() => {}}
          onCompare={() => {}}
        />
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByTestId("create-collection-from-pool"));
    await waitFor(() => expect(createCollectionMock).toHaveBeenCalled());
    const body = createCollectionMock.mock.calls[0]![0] as {
      items: { weight: number }[];
    };
    expect(body.items).toHaveLength(6);
    const sum = body.items.reduce((s, it) => s + it.weight, 0);
    expect(sum).toBe(1);
  });
});
