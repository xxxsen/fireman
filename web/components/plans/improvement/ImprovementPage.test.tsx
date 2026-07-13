// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ImprovementPage } from "./ImprovementPage";

const api = vi.hoisted(() => ({
  readiness: vi.fn(),
  list: vi.fn(),
  detail: vi.fn(),
  create: vi.fn(),
  preview: vi.fn(),
  apply: vi.fn(),
  plan: vi.fn(),
  getSimulation: vi.fn(),
  createSimulation: vi.fn(),
  cancelTask: vi.fn(),
}));

const taskState = vi.hoisted(() => ({
  value: { task: null as { status: string; phase?: string } | null, progress: 0, error: null as string | null },
}));
const restoreState = vi.hoisted(() => ({
  value: {
    task: null,
    taskId: null as string | null,
    restoring: false,
    restoreError: null as Error | null,
    retryRestore: vi.fn(),
  },
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock("@/hooks/useTaskStatus", () => ({
  useTaskStatus: () => taskState.value,
}));
vi.mock("@/hooks/useActiveTaskRestore", () => ({
  useActiveTaskRestore: () => restoreState.value,
}));

vi.mock("@/lib/api/improvements", () => ({
  getImprovementReadiness: (...args: unknown[]) => api.readiness(...args),
  listImprovementRuns: (...args: unknown[]) => api.list(...args),
  getImprovementRun: (...args: unknown[]) => api.detail(...args),
  createImprovementRun: (...args: unknown[]) => api.create(...args),
  previewImprovementProposal: (...args: unknown[]) => api.preview(...args),
  applyImprovementProposal: (...args: unknown[]) => api.apply(...args),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: (...args: unknown[]) => api.plan(...args),
}));

vi.mock("@/lib/api/simulations", () => ({
  getSimulation: (...args: unknown[]) => api.getSimulation(...args),
  createSimulation: (...args: unknown[]) => api.createSimulation(...args),
  cancelTask: (...args: unknown[]) => api.cancelTask(...args),
}));

const readiness = {
  ready: true,
  source_run: {
    id: "sim_1",
    engine_version: "3.5.0",
    runs: 1000,
    success_probability: 0.35,
    success_wilson_low: 0.32,
    success_wilson_high: 0.38,
    created_at: 1_780_000_000_000,
  },
  current_parameters: {
    retirement_age: 50,
    end_age: 90,
    annual_savings_minor: 20_000_000,
    annual_spending_minor: 40_000_000,
    annual_retirement_income_minor: 0,
  },
  blocking_reasons: [],
  warnings: [],
};

const baseEvaluation = {
  adjustments: {
    delay_years: 0,
    savings_increase_minor: 0,
    spending_reduction_minor: 0,
    retirement_income_increase_minor: 0,
  },
  runs: 1000,
  success_count: 350,
  success_probability: 0.35,
  success_wilson_low: 0.32,
  success_wilson_high: 0.38,
  terminal_p50_minor: 10_000_000,
  max_drawdown_p95: 0.4,
  improved_path_count: 0,
  regressed_path_count: 0,
  unchanged_success_count: 350,
  unchanged_failure_count: 650,
  meets_target: false,
  candidate_config_hash: "config_1",
  candidate_snapshot_hash: "snapshot_1",
};

const proposal = {
  id: "proposal_1",
  recipe: "pure_savings_increase",
  delay_years: 0,
  savings_increase_minor: 10_000_000,
  spending_reduction_minor: 0,
  retirement_income_increase_minor: 0,
  result_retirement_age: 50,
  result_annual_savings_minor: 30_000_000,
  result_annual_spending_minor: 40_000_000,
  result_annual_retirement_income_minor: 0,
  success_probability: 0.93,
  success_wilson_low: 0.91,
  success_wilson_high: 0.94,
  terminal_p50_minor: 100_000_000,
  max_drawdown_p95: 0.3,
  improved_path_count: 580,
  regressed_path_count: 0,
  candidate_config_hash: "config_2",
  candidate_snapshot_hash: "snapshot_2",
};

function run(status: string, result?: Record<string, unknown>) {
  return {
    id: "run_1",
    task_id: "task_1",
    plan_id: "plan_1",
    source_simulation_run_id: "sim_1",
    input_hash: "input_1",
    algorithm_version: "fire_plan_improver_v1",
    source_engine_version: "3.5.0",
    source_config_hash: "config_1",
    source_market_hash: "market_1",
    config: { target_success_probability: 0.9 },
    status,
    progress_current: status === "complete" ? 4 : 1,
    progress_total: 4,
    phase: status === "running" ? "搜索方案" : "",
    attempt_count: 1,
    created_at: 1_780_000_000_000,
    completed_at: status === "complete" ? 1_780_000_001_000 : undefined,
    result_stale: false,
    result,
  };
}

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <ImprovementPage planId="plan_1" />
    </QueryClientProvider>,
  );
}

describe("ImprovementPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    taskState.value = { task: null, progress: 0, error: null };
    restoreState.value = {
      task: null,
      taskId: null,
      restoring: false,
      restoreError: null,
      retryRestore: vi.fn(),
    };
    api.readiness.mockResolvedValue(readiness);
    api.list.mockResolvedValue({ runs: [], total: 0, limit: 20, offset: 0 });
    api.detail.mockResolvedValue(run("pending"));
    api.create.mockResolvedValue({ run_id: "run_1", task_id: "task_1", status: "pending", reused: false });
    api.plan.mockResolvedValue({ id: "plan_1", config_version: 3 });
  });

  it("accepts a decimal target and omits disabled levers from the request", async () => {
    renderPage();
    const target = await screen.findByLabelText("目标成功率");
    fireEvent.change(target, { target: { value: "90.5" } });
    fireEvent.click(screen.getByRole("button", { name: "开始分析" }));

    await waitFor(() => expect(api.create).toHaveBeenCalledTimes(1));
    expect(api.create).toHaveBeenCalledWith(
      "plan_1",
      expect.objectContaining({
        simulation_run_id: "sim_1",
        target_success_probability: 0.905,
        retirement_income_increase: null,
      }),
    );
  });

  it("restores an active task after entering the page again", async () => {
    api.list.mockResolvedValue({
      runs: [{ id: "run_1", status: "running", task_id: "task_1" }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    api.detail.mockResolvedValue(run("running"));
    taskState.value = { task: { status: "running", phase: "搜索方案" }, progress: 0.4, error: null };
    renderPage();

    expect(await screen.findByText("正在分析")).toBeInTheDocument();
    expect(screen.getByText(/搜索方案 · 40%/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "开始分析" })).toBeDisabled();
  });

  it("previews and applies only a completed feasible proposal", async () => {
    const result = {
      algorithm_version: "fire_plan_improver_v1",
      target_probability: 0.9,
      baseline: baseEvaluation,
      target_reached: true,
      proposals: [proposal],
      recipes: [{ recipe: proposal.recipe, status: "feasible", proposal_id: proposal.id }],
      evaluations: [{ ...baseEvaluation, ...proposal, adjustments: baseEvaluation.adjustments, meets_target: true }],
      evaluated_count: 4,
      warnings: [],
    };
    api.list.mockResolvedValue({
      runs: [{ id: "run_1", status: "complete", task_id: "task_1" }],
      total: 1,
      limit: 20,
      offset: 0,
    });
    api.detail.mockResolvedValue(run("complete", result));
    const preview = {
      run_id: "run_1",
      proposal_id: "proposal_1",
      expected_plan_config_version: 3,
      before: { retirement_age: 50, annual_savings_minor: 20_000_000, annual_spending_minor: 40_000_000, annual_retirement_income_minor: 0 },
      after: { retirement_age: 50, annual_savings_minor: 30_000_000, annual_spending_minor: 40_000_000, annual_retirement_income_minor: 0 },
      unchanged: ["持仓与权重", "收益与风险假设"],
      source_run_id: "sim_1",
      algorithm_version: "fire_plan_improver_v1",
      target_probability: 0.9,
      success_probability: 0.93,
      success_wilson_low: 0.91,
      success_wilson_high: 0.94,
      retirement_income_delayed: false,
      current_config_hash: "config_1",
      current_market_hash: "market_1",
      preview_hash: "preview_1",
      preview_expires_at: 1_900_000_000_000,
    };
    api.preview.mockResolvedValue(preview);
    api.apply.mockResolvedValue({ application: {}, plan: {}, parameters: {} });
    renderPage();

    const actions = await screen.findAllByRole("button", { name: "查看并应用" });
    fireEvent.click(actions[0]);
    expect(await screen.findByRole("dialog", { name: "查看并应用改善方案" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "应用到计划" }));

    await waitFor(() => expect(api.apply).toHaveBeenCalledWith(preview));
    expect(await screen.findByText("改善方案已应用")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行验证模拟" })).toBeInTheDocument();
  });
});
