import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { PlanContextBar } from "./PlanContextBar";

const { mockState } = vi.hoisted(() => ({
  mockState: {
    plans: undefined as
      | { id: string; name: string }[]
      | undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: mockState.plans,
    isLoading: mockState.isLoading,
    isError: mockState.isError,
    refetch: mockState.refetch,
  }),
}));

vi.mock("@/hooks/usePlanEdit", () => ({
  usePlanEdit: () => ({ confirmLeave: () => true }),
}));

describe("PlanContextBar", () => {
  beforeEach(() => {
    mockState.plans = [
      { id: "plan_1", name: "测试计划" },
      { id: "plan_2", name: "其他计划" },
    ];
    mockState.isLoading = false;
    mockState.isError = false;
    mockState.refetch.mockClear();
  });

  it("shows read-only current plan name without dropdown or new plan link", () => {
    render(<PlanContextBar currentPlanId="plan_1" />);

    expect(screen.getByTestId("plan-context-name")).toHaveTextContent("测试计划");
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "新建计划" })).not.toBeInTheDocument();
  });

  it("shows retry when plan query fails", () => {
    mockState.isError = true;
    mockState.plans = undefined;
    render(<PlanContextBar currentPlanId="plan_1" />);

    expect(screen.getByTestId("plan-context-error")).toHaveTextContent("计划加载失败");
    fireEvent.click(screen.getByTestId("plan-context-retry"));
    expect(mockState.refetch).toHaveBeenCalledOnce();
  });
});
