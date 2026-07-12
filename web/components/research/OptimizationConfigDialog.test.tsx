import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { OptimizationConfigDialog } from "./OptimizationConfigDialog";

const getReadinessMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  getOptimizationReadiness: (...args: unknown[]) => getReadinessMock(...args),
}));

function renderDialog(onSubmit = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <OptimizationConfigDialog
        open
        onClose={() => undefined}
        pending={false}
        onSubmit={onSubmit}
        collectionId="rc_1"
        defaultConfidence={0.95}
        defaultHorizonDays={20}
      />
    </QueryClientProvider>,
  );
  return onSubmit;
}

describe("OptimizationConfigDialog", () => {
  beforeEach(() => {
    getReadinessMock.mockReset();
    getReadinessMock.mockResolvedValue({
      ready: true,
      candidate_count: 42,
      enabled_count: 2,
      locked_count: 0,
      tunable_count: 2,
      locked_weight_sum: 0,
      blocking_reasons: [],
      warnings: [],
      tail_risk: {
        confidence: 0.95,
        horizon_days: 20,
        effective_return_count: 252,
        scenario_count: 233,
        minimum_scenario_count: 100,
      },
    });
  });

  it("queries readiness with the selected CVaR spec", async () => {
    renderDialog();
    await waitFor(() =>
      expect(getReadinessMock).toHaveBeenCalledWith("rc_1", {
        weightStep: 0.05,
        confidence: 0.95,
        horizonDays: 20,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "99%" }));
    fireEvent.click(screen.getByRole("button", { name: "1 日" }));
    await waitFor(() =>
      expect(getReadinessMock).toHaveBeenLastCalledWith("rc_1", {
        weightStep: 0.05,
        confidence: 0.99,
        horizonDays: 1,
      }),
    );
  });

  it("keeps decimal input editable and submits minimum CAGR only as a CVaR filter", async () => {
    const onSubmit = renderDialog();
    await screen.findByText("233 / 最少 100");
    fireEvent.click(screen.getByLabelText("限制最低历史年化收益"));
    const input = screen.getByTestId("minimum-cagr-input");
    fireEvent.change(input, { target: { value: "3." } });
    expect(input).toHaveValue("3.");
    fireEvent.change(input, { target: { value: "3.25" } });
    fireEvent.blur(input);
    fireEvent.click(screen.getByTestId("start-optimization"));

    expect(onSubmit).toHaveBeenCalledWith({
      weight_step: 0.05,
      top_k: 20,
      tail_risk: { confidence: 0.95, horizon_days: 20 },
      minimum_cagr: 0.0325,
    });
    expect(getReadinessMock).toHaveBeenCalledTimes(1);
  });

  it("keeps the previous readiness summary visible while a new spec loads", async () => {
    getReadinessMock
      .mockResolvedValueOnce({
        ready: true,
        candidate_count: 42,
        enabled_count: 2,
        locked_count: 0,
        tunable_count: 2,
        locked_weight_sum: 0,
        blocking_reasons: [],
        warnings: [],
        tail_risk: {
          confidence: 0.95,
          horizon_days: 20,
          effective_return_count: 252,
          scenario_count: 233,
          minimum_scenario_count: 100,
        },
      })
      .mockImplementationOnce(() => new Promise(() => undefined));
    renderDialog();
    await screen.findByText("233 / 最少 100");
    fireEvent.click(screen.getByRole("button", { name: "99%" }));
    expect(screen.getByText("233 / 最少 100")).toBeInTheDocument();
    expect(await screen.findByText("更新中…")).toBeInTheDocument();
  });

  it("shows candidate performance warning inside the dialog without blocking submission", async () => {
    getReadinessMock.mockResolvedValue({
      ready: true,
      candidate_count: 20001,
      enabled_count: 6,
      locked_count: 0,
      tunable_count: 6,
      locked_weight_sum: 0,
      blocking_reasons: [],
      warnings: [
        {
          reason: "candidate_count_exceeds_recommendation",
          message: "当前候选数量 20001，推荐控制在 20000 以内；超过推荐数量后，模拟耗时和内存占用会急剧增加",
        },
      ],
    });

    renderDialog();

    await waitFor(() =>
      expect(screen.getByTestId("candidate-count")).toHaveTextContent("20,001"),
    );
    expect(
      screen.getByText(/当前候选数量 20001，推荐控制在 20000 以内/),
    ).toBeInTheDocument();
    expect(screen.getByText(/模拟耗时和内存占用会急剧增加/)).toBeInTheDocument();
    expect(screen.getByTestId("start-optimization")).toBeEnabled();
  });
});
