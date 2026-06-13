import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import HomePage from "./page";

const { defaultPlans, mockState } = vi.hoisted(() => {
  const defaultPlans = [
    {
      id: "plan_1",
      name: "测试计划",
      base_currency: "CNY",
      valuation_date: "2026-01-01",
      status: "active",
      config_version: 1,
      config_hash: "",
      created_at: 0,
      updated_at: 0,
      rebalance_actionable_count: 2,
      holdings_gap_minor: 500_000,
    },
  ];
  return {
    defaultPlans,
    mockState: {
      plans: defaultPlans.map((p) => ({ ...p })),
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
          : mockState.plans,
    isLoading: mockState.isLoading,
    isError: mockState.isError,
    error: mockState.error,
    isFetching: mockState.isFetching,
    refetch: mockState.refetch,
  }),
}));

describe("HomePage", () => {
  beforeEach(() => {
    mockState.plans = defaultPlans.map((p) => ({ ...p }));
    mockState.isLoading = false;
    mockState.isError = false;
    mockState.error = null;
    mockState.isFetching = false;
    mockState.keepCachedData = false;
    mockState.refetch.mockClear();
  });

  it("renders plan cards with metrics", () => {
    render(<HomePage />);
    expect(screen.getByRole("heading", { name: /我的 FIRE 计划/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /测试计划/ })).toHaveAttribute(
      "href",
      "/plans/plan_1/overview",
    );
    expect(screen.getByText("2 个标的")).toBeInTheDocument();
    expect(screen.getByText(/规模超出/)).toBeInTheDocument();
    expect(screen.getByText(/¥5,000\.00/)).toBeInTheDocument();
    expect(screen.queryByText("查看详情 →")).not.toBeInTheDocument();
  });

  it("shows aligned scale status when within tolerance", () => {
    mockState.plans[0]!.holdings_gap_minor = 50;
    render(<HomePage />);
    expect(screen.getByText("规模一致")).toBeInTheDocument();
    expect(screen.queryByText("规模超出")).not.toBeInTheDocument();
    expect(screen.queryByText("规模缺口")).not.toBeInTheDocument();
  });

  it("clamps long plan names in card layout", () => {
    mockState.plans[0]!.name =
      "非常非常非常非常非常非常非常非常非常非常非常非常长的 FIRE 计划名称";
    render(<HomePage />);
    const heading = screen.getByRole("heading", {
      name: /非常非常非常/,
    });
    expect(heading).toHaveClass("line-clamp-2");
  });

  it("keeps cached plans visible when background refresh fails", () => {
    mockState.keepCachedData = true;
    mockState.isError = true;
    mockState.error = new Error("network");
    render(<HomePage />);
    expect(screen.getByRole("link", { name: /测试计划/ })).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("shows skeleton while loading", () => {
    mockState.isLoading = true;
    render(<HomePage />);
    expect(screen.getAllByTestId("plan-card-skeleton").length).toBeGreaterThan(0);
  });

  it("shows error state with retry on failure", () => {
    mockState.isError = true;
    mockState.error = new Error("network");
    render(<HomePage />);
    expect(screen.getByTestId("error-state")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("error-state-retry"));
    expect(mockState.refetch).toHaveBeenCalled();
  });

  it("shows empty state when no plans", () => {
    mockState.plans = [];
    render(<HomePage />);
    expect(screen.getByTestId("empty-state")).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-action")).toHaveAttribute("href", "/plans/new");
  });

  it("has a single primary action in page header", () => {
    render(<HomePage />);
    expect(screen.getAllByTestId("page-header-primary")).toHaveLength(1);
    expect(screen.getByTestId("page-header-primary")).toHaveTextContent("新建计划");
  });
});
