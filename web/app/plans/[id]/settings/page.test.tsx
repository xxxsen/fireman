import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import PlanSettingsPage from "./page";

const routerReplace = vi.fn();
const confirmLeave = vi.fn(() => true);

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ replace: routerReplace }),
  useSearchParams: () => new URLSearchParams("section=scenarios"),
}));

vi.mock("@/hooks/usePlanEdit", () => ({
  usePlanEdit: () => ({ confirmLeave, markClean: vi.fn() }),
}));
vi.mock("@/components/plans/AllocationSettings", () => ({
  PlanTargetsContent: () => <div>目标配置内容</div>,
  AllocationSettings: () => <div>目标配置内容</div>,
}));
vi.mock("@/components/plans/settings/ParametersContent", () => ({
  ParametersContent: () => <div>参数内容</div>,
}));
vi.mock("@/components/plans/settings/AnalysisContent", () => ({
  AnalysisContent: () => <div>模拟内容</div>,
}));

describe("PlanSettingsPage", () => {
  it("rewrites legacy scenarios section to plan-targets", () => {
    render(<PlanSettingsPage />);
    expect(routerReplace).toHaveBeenCalledWith(
      "/plans/plan_1/settings?section=plan-targets",
    );
  });

  it("renders segmented settings navigation", () => {
    vi.mocked(routerReplace).mockClear();
    render(<PlanSettingsPage />);

    expect(screen.getByText("目标配置内容")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "FIRE 参数" }));
    expect(confirmLeave).toHaveBeenCalled();
  });
});
