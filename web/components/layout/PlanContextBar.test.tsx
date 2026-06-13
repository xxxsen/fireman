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

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe("PlanContextBar", () => {
  beforeEach(() => {
    mockState.plans = [{ id: "plan_1", name: "测试计划" }];
    mockState.isLoading = false;
    mockState.isError = false;
    mockState.refetch.mockClear();
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
