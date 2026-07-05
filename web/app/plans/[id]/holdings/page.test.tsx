import { vi } from "vitest";

const redirectMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  redirect: redirectMock,
}));

import HoldingsRedirect from "./page";

describe("HoldingsRedirect", () => {
  it("redirects holdings route to rebalance preserving query", async () => {
    await HoldingsRedirect({
      params: Promise.resolve({ id: "plan_1" }),
      searchParams: Promise.resolve({ asset_refreshed: "1" }),
    });
    expect(redirectMock).toHaveBeenCalledWith(
      "/plans/plan_1/rebalance?asset_refreshed=1",
    );
  });

  it("redirects without query when none is present", async () => {
    redirectMock.mockClear();
    await HoldingsRedirect({
      params: Promise.resolve({ id: "plan_1" }),
      searchParams: Promise.resolve({}),
    });
    expect(redirectMock).toHaveBeenCalledWith("/plans/plan_1/rebalance");
  });
});
