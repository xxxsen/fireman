// @vitest-environment jsdom
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("@/lib/api/plans", () => ({
  getParameters: () =>
    Promise.resolve({
      parameters: { simulation_runs: 20000, plan_id: "plan_1" },
      cash_flows: [],
    }),
}));

vi.mock("@/lib/api/simulations", () => ({
  listSimulations: () =>
    Promise.resolve({
      simulations: [
        {
          id: "run_1",
          job_id: "job_1",
          plan_id: "plan_1",
          success_count: 0,
          failure_count: 100,
          summary_json: {
            success_probability: 0,
            terminal_quantiles: { p50: 0 },
            monthly_wealth_quantiles: [{ month_offset: 0, p50_minor: 100 }],
          },
          runs: 100,
          seed: "42",
          horizon_months: 120,
          input_hash: "",
          current_config_hash: "",
          result_stale: false,
          market_snapshot_hash: "",
          engine_version: "v1",
          created_at: 0,
        },
      ],
    }),
  getJob: () =>
    Promise.resolve({
      id: "job_1",
      status: "succeeded",
      progress_current: 100,
      progress_total: 100,
      created_at: 0,
    }),
  listPaths: () =>
    Promise.resolve({
      paths: [
        { path_no: 1, path_seed: "1", representative_percentile: "p00", terminal_wealth_minor: 0, succeeded: false, max_drawdown: 0.5, run_id: "run_1" },
        { path_no: 2, path_seed: "2", representative_percentile: "p50", terminal_wealth_minor: 0, succeeded: false, max_drawdown: 0.4, run_id: "run_1" },
      ],
    }),
  createSimulation: vi.fn(),
  cancelJob: vi.fn(),
}));

vi.mock("@/lib/api/analysis", () => ({
  listStressTests: () => Promise.resolve({ stress_tests: [] }),
  listSensitivityTests: () => Promise.resolve({ sensitivity_tests: [] }),
  createStressTest: vi.fn(),
  createSensitivityTest: vi.fn(),
  getStressTest: vi.fn(),
  getSensitivityTest: vi.fn(),
}));

vi.mock("@/hooks/useJobStatus", () => ({
  useJobStatus: () => ({ job: null, progress: 0, error: null }),
}));

vi.mock("@/components/charts/WealthPathChart", () => ({
  WealthPathChart: () => <div data-testid="wealth-chart" />,
}));

import AnalysisPage from "./page";

describe("AnalysisPage zero success", () => {
  it("shows 0% success and representative paths", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <AnalysisPage />
      </QueryClientProvider>,
    );
    expect(await screen.findByText(/成功率 0%/)).toBeInTheDocument();
    expect(await screen.findByText(/P00/)).toBeInTheDocument();
    expect(screen.getByTestId("wealth-chart")).toBeInTheDocument();
  });

  it("initializes simulation runs from plan parameters", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <AnalysisPage />
      </QueryClientProvider>,
    );
    const input = await screen.findByLabelText("模拟次数");
    await waitFor(() => expect(input).toHaveValue(20000));
  });
});
