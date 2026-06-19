// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import ScenariosPage from "./page";

const updateScenario = vi.fn();

const regionTargets = [
  { asset_class: "equity", region: "domestic", weight_within_class: 1 },
  { asset_class: "equity", region: "foreign", weight_within_class: 0 },
  { asset_class: "bond", region: "domestic", weight_within_class: 1 },
  { asset_class: "bond", region: "foreign", weight_within_class: 0 },
  { asset_class: "cash", region: "domestic", weight_within_class: 1 },
  { asset_class: "cash", region: "foreign", weight_within_class: 0 },
];

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: {
      scenarios: [
        {
          id: "scn_1",
          name: "均衡",
          description: "内置模板",
          is_builtin: true,
          plan_count: 2,
          weights: [
            { asset_class: "equity", weight: 0.7 },
            { asset_class: "bond", weight: 0.3 },
            { asset_class: "cash", weight: 0 },
          ],
          region_targets: regionTargets,
          created_at: 0,
          updated_at: 0,
        },
        {
          id: "scn_2",
          name: "自定义",
          description: "我的方案",
          is_builtin: false,
          plan_count: 0,
          weights: [
            { asset_class: "equity", weight: 0.6 },
            { asset_class: "bond", weight: 0.4 },
            { asset_class: "cash", weight: 0 },
          ],
          region_targets: regionTargets,
          created_at: 0,
          updated_at: 0,
        },
      ],
    },
    isLoading: false,
  }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@/lib/api/allocation", () => ({
  createScenario: vi.fn(),
  deleteScenario: vi.fn(),
  listScenarios: vi.fn(),
  updateScenario: (...args: unknown[]) => updateScenario(...args),
}));

describe("ScenariosPage", () => {
  it("renders global scenario management", () => {
    render(<ScenariosPage />);
    expect(screen.getByRole("heading", { name: "场景配置" })).toBeInTheDocument();
    expect(screen.getByText("均衡")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "新建场景" })).toBeInTheDocument();
  });

  it("edits only asset-class weights (no region inputs) in scenario modal", () => {
    render(<ScenariosPage />);
    fireEvent.click(screen.getByRole("button", { name: "编辑场景" }));
    expect(screen.getByText("大类权重")).toBeInTheDocument();
    expect(screen.queryByText("地区组内权重")).not.toBeInTheDocument();
    // Only the three asset-class weight inputs remain; region inputs are removed.
    expect(screen.getAllByTestId("percent-input")).toHaveLength(3);
  });

  it("hides edit/delete for builtin scenario, shows badge and copy only", () => {
    render(<ScenariosPage />);
    // builtin badge appears inline with the title
    expect(screen.getByText("内置")).toBeInTheDocument();
    // builtin (plan_count 2) has copy but no delete; custom (plan_count 0) has all three
    expect(screen.getAllByRole("button", { name: "复制场景" })).toHaveLength(2);
    expect(screen.getAllByRole("button", { name: "编辑场景" })).toHaveLength(1);
    expect(screen.getAllByRole("button", { name: "删除场景" })).toHaveLength(1);
  });

  it("shows reference text only when plan_count > 0 and hides normal weight pass text", () => {
    render(<ScenariosPage />);
    expect(screen.getByText("2 个计划使用")).toBeInTheDocument();
    expect(screen.queryByText(/0 个计划使用/)).not.toBeInTheDocument();
    expect(screen.queryByText(/，通过/)).not.toBeInTheDocument();
  });
});
