// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

const mockGet = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/simulations", () => ({
  getScenarioComparison: mockGet,
}));

import { ScenarioComparisonCard } from "./ScenarioComparisonCard";

function renderCard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <ScenarioComparisonCard planId="plan_1" />
    </QueryClientProvider>,
  );
}

describe("ScenarioComparisonCard (td/061 §3.6/§5.4.6)", () => {
  it("runs three scenarios on demand and shows deltas vs baseline", async () => {
    mockGet.mockResolvedValue({
      plan_id: "plan_1",
      profile_id: "system_cma_v2",
      profile_version: 1,
      seed: "42",
      runs: 3000,
      baseline_key: "baseline",
      scenarios: [
        {
          scenario: "conservative",
          forward_return: 0.04,
          volatility: 0.18,
          success_rate: 0.7,
          terminal_p00_minor: 0,
          terminal_p50_minor: 800_000_00,
          terminal_p95_minor: 2_000_000_00,
          real_terminal_p50_minor: 500_000_00,
          max_drawdown_p50: 0.3,
        },
        {
          scenario: "baseline",
          forward_return: 0.06,
          volatility: 0.15,
          success_rate: 0.8,
          terminal_p00_minor: 0,
          terminal_p50_minor: 1_000_000_00,
          terminal_p95_minor: 2_500_000_00,
          real_terminal_p50_minor: 650_000_00,
          max_drawdown_p50: 0.25,
        },
        {
          scenario: "optimistic",
          forward_return: 0.08,
          volatility: 0.13,
          success_rate: 0.9,
          terminal_p00_minor: 0,
          terminal_p50_minor: 1_300_000_00,
          terminal_p95_minor: 3_000_000_00,
          real_terminal_p50_minor: 820_000_00,
          max_drawdown_p50: 0.2,
        },
      ],
    });

    renderCard();
    // The query is disabled until the user clicks.
    expect(mockGet).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "运行情景比较" }));

    expect(await screen.findByText("保守")).toBeInTheDocument();
    expect(screen.getByText("基准")).toBeInTheDocument();
    expect(screen.getByText("乐观")).toBeInTheDocument();
    // Baseline delta column shows — ; optimistic shows a positive delta.
    expect(screen.getByText("+¥30.00w")).toBeInTheDocument();
    // Footnote reports the shared profile and per-scenario run count.
    expect(screen.getByText(/条路径/)).toBeInTheDocument();
    // After the first run the action becomes a re-run.
    expect(screen.getByRole("button", { name: "重新运行" })).toBeInTheDocument();
    expect(mockGet).toHaveBeenCalledWith("plan_1");
  });
});
