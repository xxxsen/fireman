// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import ScenariosPage from "./page";

const updateScenario = vi.fn();

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

  it("allows editing cash region weights in scenario modal", () => {
    render(<ScenariosPage />);
    fireEvent.click(screen.getByRole("button", { name: "编辑" }));
    expect(screen.getAllByText("现金/其他").length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByTestId("percent-input").length).toBeGreaterThan(0);
  });
});
