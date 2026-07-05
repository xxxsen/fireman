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
  // The page runs one useJobStatus hook per job kind (sim/stress/sensitivity).
  // Only the hook that currently holds a job id gets its callbacks captured, so
  // tests fire terminal callbacks against the panel that actually started a job.
  useJobStatus: (jobId: string | null, options?: typeof jobStatusCallbacks) => {
    if (jobId) {
      jobStatusCallbacks = options ?? {};
    }
    return useJobStatusMock(jobId, options);
  },
}));

vi.mock("@/components/charts/WealthPathChart", () => ({
  WealthPathChart: () => <div data-testid="wealth-chart" />,
}));

import { AnalysisContent as AnalysisPage } from "./AnalysisContent";

function renderAnalysis() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={qc}>
      <AnalysisPage />
    </QueryClientProvider>,
  );
  return { qc, view };
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
    expect((await screen.findAllByText(/成功率 0%/)).length).toBeGreaterThanOrEqual(1);
    expect(await screen.findByText(/P00/)).toBeInTheDocument();
    expect(screen.getByTestId("wealth-chart")).toBeInTheDocument();
  });

  it("drops the terminal_quantiles Pxx grid but keeps ordered representative paths", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({
      simulations: [
        {
          ...defaultSimulations.simulations[0],
          summary_json: {
            success_probability: 0.5,
            // Distinct minor amount that would only appear in the removed grid.
            terminal_quantiles: { p50: 3_000_000_00 },
            monthly_wealth_quantiles: [{ month_offset: 0, p50_minor: 100 }],
          },
        },
      ],
    });

    renderAnalysis();
    await screen.findAllByText(/成功率 50%/);

    // The interpolated terminal-quantile grid is gone.
    expect(screen.queryByText("¥3,000,000.00")).toBeNull();
    // The representative-path section explains it is the sole Pxx source.
    expect(
      await screen.findByText(/每项为期末资产最接近对应分位数的实际模拟路径/),
    ).toBeInTheDocument();
    // Representative path buttons remain, and render in business order P00→P50
    // regardless of the order returned by listPaths.
    const repButtons = screen
      .getAllByRole("button", { name: /^P\d{2} ·/ })
      .map((b) => b.textContent?.slice(0, 3));
    expect(repButtons).toEqual(["P00", "P50"]);
    // The wealth path chart (P25-P75/P50 band) is unaffected.
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
      if (jobId) {
        jobStatusCallbacks = options ?? {};
      }
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
      if (jobId) {
        jobStatusCallbacks = options ?? {};
      }
      if (jobId === "job_stress") {
        return { job: { status: "failed" }, progress: 0, error: "压力测试失败" };
      }
      return { job: null, progress: 0, error: null };
    });

    renderAnalysis();
    const stressBtn = await screen.findByRole("button", { name: "运行压力测试" });
    await waitFor(() => expect(stressBtn).not.toBeDisabled());
    fireEvent.click(stressBtn);
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
      if (jobId) {
        jobStatusCallbacks = options ?? {};
      }
      if (jobId === "job_sens") {
        return { job: { status: "failed" }, progress: 0, error: "敏感性测试失败" };
      }
      return { job: null, progress: 0, error: null };
    });

    renderAnalysis();
    const sensBtn = await screen.findByRole("button", { name: "运行敏感性测试" });
    await waitFor(() => expect(sensBtn).not.toBeDisabled());
    fireEvent.click(sensBtn);
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
      (await screen.findAllByText(/仅有 1 个完整自然年度，收益与风险估计的不确定性较高/)).length,
    ).toBeGreaterThanOrEqual(1);
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

  it("defaults to the latest run and queries attached analysis by run id", async () => {
    renderAnalysis();
    await screen.findAllByText(/成功率 0%/);
    await waitFor(() =>
      expect(listStressTestsMock).toHaveBeenCalledWith("plan_1", "run_1"),
    );
    expect(listSensitivityTestsMock).toHaveBeenCalledWith("plan_1", "run_1");
  });

  it("switches attached analysis queries when a historical run is selected", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({
      simulations: [
        { ...defaultSimulations.simulations[0] },
        {
          ...defaultSimulations.simulations[0],
          id: "run_0",
          job_id: "job_0",
          created_at: -1000,
        },
      ],
    });
    renderAnalysis();
    const select = await screen.findByTestId("simulation-history-select");
    fireEvent.change(select, { target: { value: "run_0" } });
    await waitFor(() =>
      expect(listStressTestsMock).toHaveBeenCalledWith("plan_1", "run_0"),
    );
    expect(listSensitivityTestsMock).toHaveBeenCalledWith("plan_1", "run_0");
  });

  it("disables attached analysis when no simulation exists", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({ simulations: [] });
    renderAnalysis();
    expect(await screen.findByRole("button", { name: "运行压力测试" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "运行敏感性测试" })).toBeDisabled();
    expect(screen.getAllByText("请先运行 Monte Carlo 模拟").length).toBeGreaterThanOrEqual(1);
  });

  it("disables attached analysis when the selected run is not completed", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({
      simulations: [
        {
          ...defaultSimulations.simulations[0],
          summary_json: undefined,
        },
      ],
    });
    renderAnalysis();
    expect(await screen.findByRole("button", { name: "运行压力测试" })).toBeDisabled();
    expect(screen.getAllByText(/当前模拟尚未完成/).length).toBeGreaterThanOrEqual(1);
  });

  it("first run shows results after the job completes without manual refresh", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({ simulations: [] });
    createSimulation.mockReset();
    createSimulation.mockResolvedValue({ job_id: "job_new", run_id: "run_new", status: "queued" });

    renderAnalysis();

    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());

    // Pending run selected, but results are not shown until the summary lands.
    expect(screen.queryByText("模拟结果")).not.toBeInTheDocument();

    // Backend now reports the completed run for the next refetch.
    listSimulationsMock.mockResolvedValue({
      simulations: [
        {
          ...defaultSimulations.simulations[0],
          id: "run_new",
          job_id: "job_new",
          summary_json: { success_probability: 0.9, terminal_quantiles: { p50: 100 } },
        },
      ],
    });

    await act(async () => {
      jobStatusCallbacks.onComplete?.();
    });

    expect(await screen.findByText("模拟结果")).toBeInTheDocument();
    expect(screen.getAllByText(/成功率 90%/).length).toBeGreaterThanOrEqual(1);
  });

  it("rebuilds the sim job from a persisted pending run after refresh", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({
      simulations: [
        {
          ...defaultSimulations.simulations[0],
          id: "run_pending",
          job_id: "job_resume",
          summary_json: {},
        },
      ],
    });

    renderAnalysis();

    // The page adopts the persisted job and shows progress + cancel again.
    await waitFor(() =>
      expect(useJobStatusMock).toHaveBeenCalledWith("job_resume", expect.anything()),
    );
    expect(await screen.findByText(/running… 40%/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "取消" })).toBeInTheDocument();
  });

  it("does not re-adopt an older failed run when the newest run already succeeded", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({
      simulations: [
        // Newest first (created_at DESC): a completed run with a summary.
        { ...defaultSimulations.simulations[0] },
        // Older run that failed before persisting a summary: it must stay
        // settled instead of resurfacing its failure on every page visit.
        {
          ...defaultSimulations.simulations[0],
          id: "run_old_failed",
          job_id: "job_old_failed",
          summary_json: {},
          created_at: -1000,
        },
      ],
    });

    renderAnalysis();
    await screen.findAllByText(/成功率 0%/);

    expect(useJobStatusMock).not.toHaveBeenCalledWith(
      "job_old_failed",
      expect.anything(),
    );
    expect(screen.getByRole("button", { name: "运行模拟" })).not.toBeDisabled();
    expect(screen.queryByRole("button", { name: "重试" })).not.toBeInTheDocument();
  });

  it("re-attaches running stress and sensitivity jobs from persisted lists", async () => {
    const baseView = {
      plan_id: "plan_1",
      input_hash: "",
      current_config_hash: "",
      result_stale: false,
      simulation_run_id: "run_1",
      created_at: 0,
    };
    listStressTestsMock.mockReset();
    listStressTestsMock.mockResolvedValue({
      stress_tests: [
        { ...baseView, job_id: "job_stress_done", status: "succeeded" },
        { ...baseView, job_id: "job_stress_running", status: "running" },
      ],
    });
    listSensitivityTestsMock.mockReset();
    listSensitivityTestsMock.mockResolvedValue({
      sensitivity_tests: [{ ...baseView, job_id: "job_sens_queued", status: "queued" }],
    });

    renderAnalysis();

    await waitFor(() =>
      expect(useJobStatusMock).toHaveBeenCalledWith(
        "job_stress_running",
        expect.anything(),
      ),
    );
    await waitFor(() =>
      expect(useJobStatusMock).toHaveBeenCalledWith("job_sens_queued", expect.anything()),
    );
    // Terminal records are never adopted.
    expect(useJobStatusMock).not.toHaveBeenCalledWith(
      "job_stress_done",
      expect.anything(),
    );
  });

  it("keeps the freshly started job when the list still contains an older unfinished run", async () => {
    listSimulationsMock.mockReset();
    listSimulationsMock.mockResolvedValue({ simulations: [] });
    createSimulation.mockReset();
    createSimulation.mockResolvedValue({
      job_id: "job_new",
      run_id: "run_new",
      status: "queued",
    });

    const { qc } = renderAnalysis();

    fireEvent.click(await screen.findByRole("button", { name: "运行模拟" }));
    await waitFor(() => expect(createSimulation).toHaveBeenCalled());
    await waitFor(() =>
      expect(useJobStatusMock).toHaveBeenCalledWith("job_new", expect.anything()),
    );

    // A stale refetch still lists an older unfinished run; it must not steal
    // the slot from the job the user just started.
    listSimulationsMock.mockResolvedValue({
      simulations: [
        {
          ...defaultSimulations.simulations[0],
          id: "run_old",
          job_id: "job_old",
          summary_json: {},
        },
      ],
    });
    await act(async () => {
      await qc.invalidateQueries({ queryKey: ["simulations", "plan_1"] });
    });

    expect(useJobStatusMock).not.toHaveBeenCalledWith("job_old", expect.anything());
    const lastRenderJobIds = useJobStatusMock.mock.calls.slice(-3).map((c) => c[0]);
    expect(lastRenderJobIds).toContain("job_new");
  });
});
