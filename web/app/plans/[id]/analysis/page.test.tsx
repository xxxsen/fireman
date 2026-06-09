// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

const useJobStatusMock = vi.hoisted(() => vi.fn());
const createSimulation = vi.hoisted(() => vi.fn());
const createStressTest = vi.hoisted(() => vi.fn());
const createSensitivityTest = vi.hoisted(() => vi.fn());

let jobStatusCallbacks: {
  onComplete?: () => void;
  onFailed?: (message: string) => void;
  onCanceled?: () => void;
} = {};

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
        {
          path_no: 1,
          path_seed: "1",
          representative_percentile: "p00",
          terminal_wealth_minor: 0,
          succeeded: false,
          max_drawdown: 0.5,
          run_id: "run_1",
        },
        {
          path_no: 2,
          path_seed: "2",
          representative_percentile: "p50",
          terminal_wealth_minor: 0,
          succeeded: false,
          max_drawdown: 0.4,
          run_id: "run_1",
        },
      ],
    }),
  createSimulation,
  cancelJob: vi.fn(),
}));

vi.mock("@/lib/api/analysis", () => ({
  listStressTests: () => Promise.resolve({ stress_tests: [] }),
  listSensitivityTests: () => Promise.resolve({ sensitivity_tests: [] }),
  createStressTest,
  createSensitivityTest,
  getStressTest: vi.fn(),
  getSensitivityTest: vi.fn(),
}));

vi.mock("@/hooks/useJobStatus", () => ({
  useJobStatus: (jobId: string | null, options?: typeof jobStatusCallbacks) => {
    jobStatusCallbacks = options ?? {};
    return useJobStatusMock(jobId, options);
  },
}));

vi.mock("@/components/charts/WealthPathChart", () => ({
  WealthPathChart: () => <div data-testid="wealth-chart" />,
}));

import AnalysisPage from "./page";

function renderAnalysis() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AnalysisPage />
    </QueryClientProvider>,
  );
}

describe("AnalysisPage zero success", () => {
  beforeEach(() => {
    useJobStatusMock.mockReset();
    createSimulation.mockReset();
    createStressTest.mockReset();
    createSensitivityTest.mockReset();
    useJobStatusMock.mockImplementation((jobId) => {
      if (!jobId) {
        return { job: null, progress: 0, error: null };
      }
      return {
        job: { status: "running", progress_current: 40, progress_total: 100 },
        progress: 0.4,
        error: null,
      };
    });
    createSimulation.mockResolvedValue({ job_id: "job_sim_busy", run_id: "run_busy", status: "queued" });
    createStressTest.mockResolvedValue({ job_id: "job_stress", status: "queued" });
    createSensitivityTest.mockResolvedValue({ job_id: "job_sens", status: "queued" });
  });

  it("shows 0% success and representative paths", async () => {
    renderAnalysis();
    expect(await screen.findByText(/成功率 0%/)).toBeInTheDocument();
    expect(await screen.findByText(/P00/)).toBeInTheDocument();
    expect(screen.getByTestId("wealth-chart")).toBeInTheDocument();
  });

  it("initializes simulation runs from plan parameters", async () => {
    renderAnalysis();
    const input = await screen.findByLabelText("模拟次数");
    await waitFor(() => expect(input).toHaveValue(20000));
  });

  it("disables stress and sensitivity while simulation job is busy", async () => {
    renderAnalysis();
    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());

    expect(screen.getByRole("button", { name: "运行压力测试" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "运行敏感性测试" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "运行模拟" })).toBeDisabled();
  });

  it("clears active job and keeps error after failed terminal state", async () => {
    useJobStatusMock.mockImplementation((jobId, options) => {
      jobStatusCallbacks = options ?? {};
      if (jobId === "job_sim_busy") {
        return { job: { status: "failed" }, progress: 0, error: "模拟引擎错误" };
      }
      return { job: null, progress: 0, error: null };
    });

    renderAnalysis();
    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());

    await act(async () => {
      jobStatusCallbacks.onFailed?.("模拟引擎错误");
    });

    expect(screen.getByText("模拟引擎错误")).toBeInTheDocument();
    expect(screen.queryByText(/连接中/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行压力测试" })).not.toBeDisabled();
  });

  it("clears active job after canceled terminal state", async () => {
    renderAnalysis();
    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());

    await act(async () => {
      jobStatusCallbacks.onCanceled?.();
    });

    expect(screen.queryByText(/连接中/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行模拟" })).not.toBeDisabled();
  });
});
