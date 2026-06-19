// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

const useJobStatusMock = vi.hoisted(() => vi.fn());
const createSimulation = vi.hoisted(() => vi.fn());
const createStressTest = vi.hoisted(() => vi.fn());
const createSensitivityTest = vi.hoisted(() => vi.fn());
const getParametersMock = vi.hoisted(() => vi.fn());
const listSimulationsMock = vi.hoisted(() => vi.fn());
const listStressTestsMock = vi.hoisted(() => vi.fn());
const listSensitivityTestsMock = vi.hoisted(() => vi.fn());

let jobStatusCallbacks: {
  onComplete?: () => void;
  onFailed?: (message: string) => void;
  onCanceled?: () => void;
} = {};

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

const defaultParameters = {
  parameters: { simulation_runs: 20000, plan_id: "plan_1" },
  cash_flows: [],
};

vi.mock("@/lib/api/plans", () => ({
  getParameters: (...args: unknown[]) => getParametersMock(...args),
  getPlan: () =>
    Promise.resolve({
      id: "plan_1",
      name: "测试计划",
      valuation_date: "2026-06-14",
      base_currency: "CNY",
      status: "active",
      config_version: 1,
      created_at: 0,
      updated_at: 0,
    }),
}));

const getHoldingsMock = vi.hoisted(() => vi.fn());

const defaultHoldings = {
  holdings: [
    {
      id: "hold_1",
      plan_id: "plan_1",
      instrument_id: "inst_short",
      enabled: true,
      asset_class: "equity",
      region: "domestic",
      weight_within_group: 1,
      current_amount_minor: 1_000_000_00,
      simulation_snapshot_id: "snap_1",
      snapshot_history_depth: "one_year",
      snapshot_warnings: ["仅有 1 个完整自然年度，收益与风险估计的不确定性较高"],
      instrument_code: "SHORT01",
      instrument_name: "短历史基金",
      sort_order: 1,
    },
  ],
};

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: (...args: unknown[]) => getHoldingsMock(...args),
}));

const defaultSimulations = {
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
        model_warnings: [
          "短历史基金（SHORT01）仅有 1 个完整自然年度，收益与风险估计的不确定性较高",
        ],
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
};

vi.mock("@/lib/api/simulations", () => ({
  listSimulations: (...args: unknown[]) => listSimulationsMock(...args),
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
  listStressTests: (...args: unknown[]) => listStressTestsMock(...args),
  listSensitivityTests: (...args: unknown[]) => listSensitivityTestsMock(...args),
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

import { AnalysisContent as AnalysisPage } from "./page";

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
    getHoldingsMock.mockReset();
    getHoldingsMock.mockResolvedValue(defaultHoldings);
    useJobStatusMock.mockReset();
    createSimulation.mockReset();
    createStressTest.mockReset();
    createSensitivityTest.mockReset();
    getParametersMock.mockReset();
    getParametersMock.mockResolvedValue(defaultParameters);
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue(defaultSimulations);
    listStressTestsMock.mockReset();
    listStressTestsMock.mockResolvedValue({ stress_tests: [] });
    listSensitivityTestsMock.mockReset();
    listSensitivityTestsMock.mockResolvedValue({ sensitivity_tests: [] });
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

    fireEvent.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalledTimes(2));
    expect(createStressTest).not.toHaveBeenCalled();
    expect(createSensitivityTest).not.toHaveBeenCalled();
  });

  it("shows stress failure only in stress panel and retries stress test", async () => {
    useJobStatusMock.mockImplementation((jobId, options) => {
      jobStatusCallbacks = options ?? {};
      if (jobId === "job_stress") {
        return { job: { status: "failed" }, progress: 0, error: "压力测试失败" };
      }
      return { job: null, progress: 0, error: null };
    });

    renderAnalysis();
    fireEvent.click(await screen.findByRole("button", { name: "运行压力测试" }));
    await waitFor(() => expect(createStressTest).toHaveBeenCalled());

    await act(async () => {
      jobStatusCallbacks.onFailed?.("压力测试失败");
    });

    expect(screen.getByText("压力测试失败")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "运行模拟" })).not.toBeDisabled();

    const retryButtons = screen.getAllByRole("button", { name: "重试" });
    fireEvent.click(retryButtons[retryButtons.length - 1]!);
    await waitFor(() => expect(createStressTest).toHaveBeenCalledTimes(2));
    expect(createSimulation).not.toHaveBeenCalled();
    expect(createSensitivityTest).not.toHaveBeenCalled();
  });

  it("shows sensitivity failure only in sensitivity panel and retries sensitivity test", async () => {
    useJobStatusMock.mockImplementation((jobId, options) => {
      jobStatusCallbacks = options ?? {};
      if (jobId === "job_sens") {
        return { job: { status: "failed" }, progress: 0, error: "敏感性测试失败" };
      }
      return { job: null, progress: 0, error: null };
    });

    renderAnalysis();
    fireEvent.click(await screen.findByRole("button", { name: "运行敏感性测试" }));
    await waitFor(() => expect(createSensitivityTest).toHaveBeenCalled());

    await act(async () => {
      jobStatusCallbacks.onFailed?.("敏感性测试失败");
    });

    expect(screen.getByText("敏感性测试失败")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "运行模拟" })).not.toBeDisabled();

    const retryButtons = screen.getAllByRole("button", { name: "重试" });
    fireEvent.click(retryButtons[retryButtons.length - 1]!);
    await waitFor(() => expect(createSensitivityTest).toHaveBeenCalledTimes(2));
    expect(createSimulation).not.toHaveBeenCalled();
    expect(createStressTest).not.toHaveBeenCalled();
  });

  it("shows model_warnings with visible asset names", async () => {
    renderAnalysis();
    expect(
      await screen.findByText(/短历史基金（SHORT01）仅有 1 个完整自然年度/),
    ).toBeInTheDocument();
  });

  it("shows short-history warning before running simulation", async () => {
    renderAnalysis();
    expect(
      await screen.findByText(/以下持仓历史样本有限/),
    ).toBeInTheDocument();
    expect(
      await screen.findByText(/仅有 1 个完整自然年度，收益与风险估计的不确定性较高/),
    ).toBeInTheDocument();
  });

  it("keeps frozen snapshot warnings from holdings after library refresh", async () => {
    getHoldingsMock.mockImplementation(() =>
      Promise.resolve({
        holdings: [
          {
            id: "hold_1",
            plan_id: "plan_1",
            instrument_id: "inst_short",
            enabled: true,
            asset_class: "equity",
            region: "domestic",
            weight_within_group: 1,
            current_amount_minor: 1_000_000_00,
            simulation_snapshot_id: "snap_frozen",
            snapshot_history_depth: "five_plus_years",
            snapshot_complete_year_count: 8,
            snapshot_warnings: ["冻结快照提示：完整年度样本较少，估计不稳定"],
            instrument_code: "SHORT01",
            instrument_name: "短历史基金",
            sort_order: 1,
          },
        ],
      }),
    );

    renderAnalysis();
    expect(
      await screen.findByText(/冻结快照提示：完整年度样本较少，估计不稳定/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/历史样本有限：仅 1 年/)).not.toBeInTheDocument();
  });

  it("clears active job after canceled terminal state without cross-panel retry", async () => {
    renderAnalysis();
    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());

    await act(async () => {
      jobStatusCallbacks.onCanceled?.();
    });

    expect(screen.queryByText(/连接中/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行模拟" })).not.toBeDisabled();
    expect(screen.queryByRole("button", { name: "重试" })).not.toBeInTheDocument();
  });

  it("hides run button until parameters load and uses configured runs", async () => {
    let resolveParams: (v: unknown) => void = () => {};
    getParametersMock.mockReset();
    getParametersMock.mockReturnValue(
      new Promise((res) => {
        resolveParams = res;
      }),
    );

    renderAnalysis();

    expect(screen.queryByRole("button", { name: "运行模拟" })).not.toBeInTheDocument();

    await act(async () => {
      resolveParams(defaultParameters);
    });

    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());
    expect(createSimulation.mock.calls[0]?.[1]).toMatchObject({ runs: 20000 });
  });

  it("shows page-level error state when parameters load fails", async () => {
    getParametersMock.mockReset();
    getParametersMock.mockRejectedValue(new Error("params boom"));
    renderAnalysis();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.getByTestId("error-state-retry")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "运行模拟" })).not.toBeInTheDocument();
  });

  it("shows page-level error state when holdings load fails", async () => {
    getHoldingsMock.mockReset();
    getHoldingsMock.mockRejectedValue(new Error("holdings boom"));
    renderAnalysis();

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "运行模拟" })).not.toBeInTheDocument();
  });

  it("shows module error (not silent empty) when simulations list fails", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockRejectedValue(new Error("sims boom"));
    renderAnalysis();

    expect(await screen.findByText(/无法加载历史模拟结果/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行模拟" })).toBeInTheDocument();
  });

  it("shows module error when stress list fails", async () => {
    listStressTestsMock.mockReset();
    listStressTestsMock.mockRejectedValue(new Error("stress boom"));
    renderAnalysis();

    expect(await screen.findByText(/无法加载压力测试结果/)).toBeInTheDocument();
  });

  it("shows module error when sensitivity list fails", async () => {
    listSensitivityTestsMock.mockReset();
    listSensitivityTestsMock.mockRejectedValue(new Error("sens boom"));
    renderAnalysis();

    expect(await screen.findByText(/无法加载敏感性测试结果/)).toBeInTheDocument();
  });
});
