// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import { PlanTargetsContent } from "./AllocationSettings";
import { applyScenario } from "@/lib/api/allocation";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
}));

vi.mock("@/hooks/usePlanEdit", () => ({
  usePlanEdit: () => ({ markDirty: vi.fn(), markClean: vi.fn() }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () => Promise.resolve({ id: "plan_1", config_version: 1 }),
  getParameters: () =>
    Promise.resolve({
      parameters: { selected_scenario_id: "scn_1" },
      cash_flows: [],
    }),
}));

vi.mock("@/lib/api/allocation", () => ({
  listScenarios: () =>
    Promise.resolve({
      scenarios: [
        {
          id: "scn_1",
          name: "均衡",
          description: "",
          is_builtin: true,
          plan_count: 2,
          weights: [
            { asset_class: "equity", weight: 0.7 },
            { asset_class: "bond", weight: 0.3 },
            { asset_class: "cash", weight: 0 },
          ],
          region_targets: [
            { asset_class: "equity", region: "domestic", weight_within_class: 1 },
            { asset_class: "equity", region: "foreign", weight_within_class: 0 },
            { asset_class: "bond", region: "domestic", weight_within_class: 1 },
            { asset_class: "bond", region: "foreign", weight_within_class: 0 },
            { asset_class: "cash", region: "domestic", weight_within_class: 1 },
            { asset_class: "cash", region: "foreign", weight_within_class: 0 },
          ],
          created_at: 0,
          updated_at: 0,
        },
        {
          id: "scn_2",
          name: "保守",
          description: "",
          is_builtin: false,
          plan_count: 0,
          weights: [
            { asset_class: "equity", weight: 0.4 },
            { asset_class: "bond", weight: 0.6 },
            { asset_class: "cash", weight: 0 },
          ],
          region_targets: [
            { asset_class: "equity", region: "domestic", weight_within_class: 0.6 },
            { asset_class: "equity", region: "foreign", weight_within_class: 0.4 },
            { asset_class: "bond", region: "domestic", weight_within_class: 1 },
            { asset_class: "bond", region: "foreign", weight_within_class: 0 },
            { asset_class: "cash", region: "domestic", weight_within_class: 1 },
            { asset_class: "cash", region: "foreign", weight_within_class: 0 },
          ],
          created_at: 0,
          updated_at: 0,
        },
      ],
    }),
  getAllocation: () =>
    Promise.resolve({
      asset_class_targets: [{ asset_class: "equity", weight: 0.7 }],
      region_targets: [{ asset_class: "equity", region: "domestic", weight_within_class: 1 }],
    }),
  applyScenario: vi.fn(),
}));

function renderContent() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <PlanTargetsContent />
    </QueryClientProvider>,
  );
}

describe("PlanTargetsContent", () => {
  it("shows read-only weights and scenario switch without percent inputs", async () => {
    renderContent();
    expect(await screen.findByText("当前计划目标配置")).toBeInTheDocument();
    expect(screen.getByText("大类目标权重（只读）")).toBeInTheDocument();
    expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "前往场景配置" })).toHaveAttribute(
      "href",
      "/scenarios",
    );
  });

  it("shows save bar only after scenario selection changes", async () => {
    renderContent();
    await screen.findByTestId("plan-targets-scenario-select");
    expect(screen.queryByText("有未保存的修改")).not.toBeInTheDocument();

    fireEvent.change(screen.getByTestId("plan-targets-scenario-select"), {
      target: { value: "scn_2" },
    });
    expect(screen.getByText("有未保存的修改")).toBeInTheDocument();
    expect(screen.getByTestId("plan-targets-preview-note")).toHaveTextContent(/大类与地区组内权重均随所选场景预览/);
  });

  it("calls applyScenario when saving scenario switch", async () => {
    vi.mocked(applyScenario).mockResolvedValue({
      scenario_id: "scn_2",
      before: [],
      after: [],
      applied: true,
      config_version: 2,
    });
    renderContent();
    await screen.findByTestId("plan-targets-scenario-select");
    fireEvent.change(screen.getByTestId("plan-targets-scenario-select"), {
      target: { value: "scn_2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() =>
      expect(applyScenario).toHaveBeenCalledWith("plan_1", {
        scenario_id: "scn_2",
        config_version: 1,
        dry_run: false,
      }),
    );
  });
});
