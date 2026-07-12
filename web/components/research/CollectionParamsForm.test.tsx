import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ResearchCollectionDetail } from "@/lib/api/research";
import { CollectionParamsForm } from "./CollectionParamsForm";

const updateCollectionMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  updateCollection: (...args: unknown[]) => updateCollectionMock(...args),
}));

function detail(): ResearchCollectionDetail {
  return {
    id: "rc_1",
    name: "组合",
    description: "",
    base_currency: "CNY",
    initial_amount_minor: 100000000,
    rebalance_policy: "monthly",
    rebalance_threshold: 0,
    start_policy: "common_intersection",
    window_start: "",
    window_end: "",
    risk_free_rate: 0.02,
    transaction_cost_rate: 0,
    tail_risk_confidence: 0.95,
    tail_risk_horizon_days: 20,
    status: "active",
    created_at: 1,
    updated_at: 1,
    tags: [],
    items: [],
  };
}

describe("CollectionParamsForm", () => {
  it("persists the selected tail-risk spec", async () => {
    const current = detail();
    updateCollectionMock.mockResolvedValue({
      ...current,
      tail_risk_confidence: 0.99,
      tail_risk_horizon_days: 1,
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <CollectionParamsForm detail={current} onSaved={vi.fn()} />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "99%" }));
    fireEvent.click(screen.getByRole("button", { name: "1 日" }));
    fireEvent.click(screen.getByTestId("save-params"));

    await waitFor(() => expect(updateCollectionMock).toHaveBeenCalledTimes(1));
    expect(updateCollectionMock.mock.calls[0]?.[1]).toMatchObject({
      tail_risk_confidence: 0.99,
      tail_risk_horizon_days: 1,
    });
  });
});
