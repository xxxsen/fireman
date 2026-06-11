// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import AssetRefreshPage from "./page";

const mockSearchParams = vi.hoisted(() => {
  let params = new URLSearchParams();
  return {
    set: (next: URLSearchParams) => {
      params = next;
    },
    get: () => params,
  };
});

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => mockSearchParams.get(),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () =>
    Promise.resolve({
      id: "plan_1",
      name: "测试计划",
      config_version: 1,
      base_currency: "CNY",
    }),
}));

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: () =>
    Promise.resolve({
      holdings: [
        {
          id: "h1",
          instrument_id: "i1",
          instrument_name: "基金A",
          enabled: true,
          current_amount_minor: 50_000_00,
        },
      ],
    }),
  getTargets: () =>
    Promise.resolve({
      asset_class_targets: [{ asset_class: "equity", weight: 1 }],
      holdings: [],
    }),
}));

vi.mock("@/lib/api/asset-refresh", () => ({
  submitAssetRefresh: vi.fn(),
}));

describe("AssetRefreshPage", () => {
  beforeEach(() => {
    mockSearchParams.set(new URLSearchParams());
  });

  it("shows wizard steps and holdings table", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <AssetRefreshPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("更新账户资产")).toBeInTheDocument();
    expect(screen.getByText("1. 说明")).toBeInTheDocument();
  });

  it("reason=scale skips to step 2 and preselects sync scale", async () => {
    mockSearchParams.set(new URLSearchParams("reason=scale"));
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={queryClient}>
        <AssetRefreshPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText("录入当前资产")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    const syncCheckbox = screen.getByRole("checkbox", {
      name: /同步计划基准规模/,
    }) as HTMLInputElement;
    expect(syncCheckbox.checked).toBe(true);
  });
});
