import { render, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import HoldingsRedirectPage from "./page";

const replace = vi.fn();

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ replace }),
  useSearchParams: () => new URLSearchParams("asset_refreshed=1"),
}));

describe("HoldingsRedirectPage", () => {
  it("redirects holdings route to rebalance preserving query", async () => {
    render(<HoldingsRedirectPage />);
    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith("/plans/plan_1/rebalance?asset_refreshed=1"),
    );
  });
});
