import { render, screen } from "@testing-library/react";
import { vi } from "vitest";
import ScenariosPage from "./page";

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
          weights: [{ asset_class: "equity", weight: 1 }],
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

describe("ScenariosPage", () => {
  it("renders global scenario management", () => {
    render(<ScenariosPage />);
    expect(screen.getByRole("heading", { name: "场景配置" })).toBeInTheDocument();
    expect(screen.getByText("均衡")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "新建场景" })).toBeInTheDocument();
  });
});
