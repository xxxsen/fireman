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
  AllocationSettings: () => <div>目标配置内容</div>,
}));
vi.mock("../scenarios/page", () => ({
  ScenariosContent: () => <div>场景内容</div>,
}));
vi.mock("../parameters/page", () => ({
  ParametersContent: () => <div>参数内容</div>,
}));
vi.mock("../analysis/page", () => ({
  AnalysisContent: () => <div>模拟内容</div>,
}));

describe("PlanSettingsPage", () => {
  it("renders segmented settings navigation and synchronizes section URL", () => {
    render(<PlanSettingsPage />);

    expect(screen.getByText("目标配置内容")).toBeInTheDocument();
    expect(screen.getByText("场景内容")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "FIRE 参数" }));
    expect(confirmLeave).toHaveBeenCalled();
    expect(routerReplace).toHaveBeenCalledWith(
      "/plans/plan_1/settings?section=fire-params",
    );
  });
});
