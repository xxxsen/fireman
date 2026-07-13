// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FrontierPage } from "./FrontierPage";

const api = vi.hoisted(() => ({
  readiness: vi.fn(), create: vi.fn(), list: vi.fn(), detail: vi.fn(),
  preview: vi.fn(), apply: vi.fn(), plan: vi.fn(), parameters: vi.fn(), simulations: vi.fn(),
}));
const task = vi.hoisted(() => ({ value: { task: null, refetch: vi.fn() } as Record<string, unknown> }));

vi.mock("next/navigation", () => ({ useSearchParams: () => new URLSearchParams() }));
vi.mock("@/hooks/useTaskStatus", () => ({ useTaskStatus: () => task.value }));
vi.mock("@/hooks/useActiveTaskRestore", () => ({
  useActiveTaskRestore: () => ({ task: null, taskId: null, restoring: false, restoreError: null }),
}));
vi.mock("@/lib/api/frontiers", () => ({
  getFrontierReadiness: (...args: unknown[]) => api.readiness(...args),
  createFrontierRun: (...args: unknown[]) => api.create(...args),
  listFrontierRuns: (...args: unknown[]) => api.list(...args),
  getFrontierRun: (...args: unknown[]) => api.detail(...args),
  previewFrontierPoint: (...args: unknown[]) => api.preview(...args),
  applyFrontierPoint: (...args: unknown[]) => api.apply(...args),
}));
vi.mock("@/lib/api/plans", () => ({
  getPlan: (...args: unknown[]) => api.plan(...args),
  getParameters: (...args: unknown[]) => api.parameters(...args),
}));
vi.mock("@/lib/api/simulations", () => ({
  listSimulations: (...args: unknown[]) => api.simulations(...args),
}));

const parameters = {
  plan_id: "plan_1", current_age: 40, retirement_age: 50, end_age: 90,
  total_assets_minor: 100_000_000, annual_savings_minor: 20_000_000,
  annual_savings_growth_rate: 0, annual_spending_minor: 40_000_000,
  annual_retirement_income_minor: 0, annual_retirement_income_growth_rate: 0,
  terminal_wealth_floor_minor: 0, inflation_mode: "fixed_real", fixed_inflation_rate: 0.03,
  inflation_mu: 0.03, inflation_phi: 0.5, inflation_sigma: 0.01,
  withdrawal_type: "fixed_real", withdrawal_rate: 0.04, withdrawal_floor_ratio: 0.7,
  withdrawal_ceiling_ratio: 1.3, withdrawal_tax_rate: 0, taxable_withdrawal_ratio: 0,
  rebalance_frequency: "annual", rebalance_threshold: 0.03, transaction_cost_rate: 0,
  simulation_runs: 3000, student_t_df: 7, return_assumption_mode: "historical_cagr",
  assumption_selection_mode: "follow_global", return_assumption_set_id: "",
  return_assumption_set_version: 0, return_assumption_scenario: "follow_global", updated_at: 1,
};
const source = {
  id: "sim_1", task_id: "sim_task", plan_id: "plan_1", input_hash: "input",
  current_config_hash: "config", result_stale: false, market_snapshot_hash: "market",
  engine_version: "3.5.0", runs: 3000, seed: "42", horizon_months: 600,
  success_count: 2400, failure_count: 600, summary_json: {}, created_at: 1_780_000_000_000,
  task_status: "complete",
};
const readiness = {
  ready: true, issues: [], money_levels: 21, age_points: 11, evaluation_budget: 100,
  path_month_budget: 180_000_000,
  config: {
    frontier_type: "retirement_age_max_spending", target_success_probability: 0.9,
    evaluation_runs: 3000, retirement_age_range: { min: 50, max: 60 },
    search: { min_minor: 4_000_000, max_minor: 80_000_000, step_minor: 4_000_000 },
    money_levels: 20, age_points: 11, per_point_budget: 9,
    evaluation_budget: 100, path_month_budget: 180_000_000,
  },
  source_baseline: { id: "sim_1", runs: 3000, evaluation_runs: 3000 },
};
const evaluation = {
  retirement_age: 50, value_minor: 40_000_000, runs: 3000, success_count: 2750,
  success_probability: 0.9166666667, success_wilson_low: 0.906, success_wilson_high: 0.926,
  terminal_wealth_p50_minor: 100_000_000, max_drawdown_p95: 0.3,
  improved_path_count: 350, regressed_path_count: 0, meets_target: true,
  outcome_hash: "sha256:outcome", snapshot_hash: "snapshot", candidate_config_hash: "candidate",
};
const points = [
  {
    id: "point_boundary", retirement_age: 50, value_minor: 40_000_000,
    status: "boundary_found", applicable: true, evaluation,
    worse_neighbor: { ...evaluation, value_minor: 44_000_000, success_count: 2600, success_wilson_low: 0.89, meets_target: false },
  },
  { id: "point_entire", retirement_age: 51, value_minor: 80_000_000, status: "entire_domain_feasible", applicable: true, evaluation: { ...evaluation, retirement_age: 51, value_minor: 80_000_000 } },
  { id: "point_none", retirement_age: 52, value_minor: 4_000_000, status: "no_feasible_value", applicable: false, evaluation: { ...evaluation, retirement_age: 52, value_minor: 4_000_000, success_count: 100, success_probability: 0.03, success_wilson_low: 0.02, meets_target: false } },
];

function completeRun(status = "complete") {
  return {
    id: "run_1", task_id: "task_1", plan_id: "plan_1", source_simulation_run_id: "sim_1",
    input_hash: "hash", algorithm_version: "fire_frontier_v1",
    frontier_type: "retirement_age_max_spending", source_engine_version: "3.5.0",
    source_config_hash: "config", source_market_hash: "market", evaluation_runs: 3000,
    config: readiness.config, status, progress_current: 8, progress_total: 100,
    phase: status === "canceled" ? "searching" : "complete", attempt_count: 1, created_at: 1,
    source_available: false, current_plan_changed: true,
    frozen_basis: {
      base_currency: "CNY", current_age: 40, retirement_age: 50, end_age: 90,
      total_assets_minor: 100_000_000, annual_savings_minor: 20_000_000,
      annual_savings_growth_rate: 0, annual_spending_minor: 40_000_000,
      annual_retirement_income_minor: 0, annual_retirement_income_growth_rate: 0,
      inflation_mode: "fixed_real", withdrawal_type: "fixed_real", rebalance_frequency: "annual",
      asset_count: 2, random_factor_model: "multivariate_student_t",
      return_assumption_mode: "blended_prior", return_assumption_scenario: "baseline",
      source_simulation_runs: 3000, seed: "42", asset_scaling_basis: "source_amount_proportions",
    },
    result: status === "complete" ? {
      algorithm_version: "fire_frontier_v1", frontier_type: "retirement_age_max_spending",
      target_probability: 0.9, evaluation_runs: 3000, baseline: { ...evaluation, success_count: 2400 },
      points, evaluations: [evaluation], distinct_evaluations: 8, actual_path_months: 14_400_000,
      evaluation_budget: 100, path_month_budget: 180_000_000,
      discrete_connection_note: "连线仅为视觉连接，不代表中间年龄或金额已计算。",
    } : undefined,
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}><FrontierPage planId="plan_1" /></QueryClientProvider>);
}

describe("FrontierPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    task.value = { task: null, refetch: vi.fn() };
    api.plan.mockResolvedValue({ id: "plan_1", config_version: 7, base_currency: "CNY" });
    api.parameters.mockResolvedValue({ parameters, effective_assumption: {} });
    api.simulations.mockResolvedValue({ simulations: [source] });
    api.list.mockResolvedValue({ runs: [], total: 0, limit: 20, offset: 0 });
    api.readiness.mockResolvedValue(readiness);
    api.create.mockResolvedValue({ run_id: "run_1", task_id: "task_1", status: "pending", reused: false });
  });

  it("shows four fixed definitions and gates creation through readiness", async () => {
    renderPage();
    for (const label of [
      "不同退休年龄可承受多少支出", "不同退休年龄至少需要储蓄多少",
      "按当前方案需要多少资产", "停止新增储蓄后需要多少资产",
    ]) {
      expect(await screen.findByRole("button", { name: `选择「${label}」` })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: `了解「${label}」的作用和计算逻辑` })).toBeInTheDocument();
    }

    const assetHelp = screen.getByRole("button", { name: "了解「按当前方案需要多少资产」的作用和计算逻辑" });
    fireEvent.mouseEnter(assetHelp);
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent("今天至少需要多少起始资产");
    expect(tooltip).toHaveTextContent("只改变起始总资产");
    expect(tooltip).toHaveTextContent("同一批路径重新运行正式模拟");

    fireEvent.click(screen.getByRole("button", { name: "检查可运行性" }));
    expect(await screen.findByText(/最多评估 100 次/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "开始计算前沿" }));
    await waitFor(() => expect(api.create).toHaveBeenCalledTimes(1));
    expect(api.readiness).toHaveBeenCalledTimes(2);
    expect(api.create).toHaveBeenCalledWith("plan_1", expect.objectContaining({
      source_simulation_run_id: "sim_1", frontier_type: "retirement_age_max_spending",
    }));
  });

  it("defaults to a 95% target and 10,000 evaluation paths when the source supports them", async () => {
    api.simulations.mockResolvedValue({ simulations: [{ ...source, runs: 10_000 }] });
    renderPage();

    expect(await screen.findByRole("textbox", { name: "目标成功率（%）" })).toHaveValue("95");
    expect(screen.getByRole("spinbutton", { name: "评估路径数" })).toHaveValue(10_000);

    fireEvent.click(screen.getByRole("button", { name: "检查可运行性" }));
    await waitFor(() => expect(api.readiness).toHaveBeenCalledWith("plan_1", expect.objectContaining({
      target_success_probability: 0.95,
      evaluation_runs: 10_000,
    })));
  });

  it("disables creation for a readiness budget failure", async () => {
    api.readiness.mockResolvedValue({ ready: false, issues: [{ code: "frontier_budget_exceeded", message: "预算超过 256" }] });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "检查可运行性" }));
    expect(await screen.findByText("预算超过 256")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "开始计算前沿" })).toBeDisabled();
  });

  it("renders frozen statuses, evidence, disclosure and applies an eligible point", async () => {
    api.list.mockResolvedValue({ runs: [{ id: "run_1", task_id: "task_1", status: "complete", frontier_type: "retirement_age_max_spending", target_probability: 0.9, created_at: 1 }], total: 1 });
    api.detail.mockResolvedValue(completeRun());
    const preview = {
      run_id: "run_1", point_id: "point_boundary", expected_plan_config_version: 7,
      before: { retirement_age: 50, annual_savings_minor: 20_000_000, annual_spending_minor: 44_000_000 },
      after: { retirement_age: 50, annual_savings_minor: 20_000_000, annual_spending_minor: 40_000_000 },
      unchanged: ["持仓与权重"], source_run_id: "sim_1", algorithm_version: "fire_frontier_v1",
      target_probability: 0.9, runs: 3000, success_probability: 0.916,
      success_wilson_low: 0.906, success_wilson_high: 0.926,
      improved_path_count: 350, regressed_path_count: 0, current_config_hash: "config",
      current_market_hash: "market", preview_hash: "preview", preview_expires_at: 1_900_000_000_000,
    };
    api.preview.mockResolvedValue(preview);
    api.apply.mockResolvedValue({ application: { id: "app" }, plan: {}, parameters: {} });
    renderPage();

    expect(await screen.findByText("找到离散边界")).toBeInTheDocument();
    expect(screen.getByText("整个搜索域均达标")).toBeInTheDocument();
    expect(screen.getByText("搜索域内无达标值")).toBeInTheDocument();
    expect(screen.getByText("最大可承受支出高于或等于搜索上限")).toBeInTheDocument();
    expect(screen.getByText("最大可承受支出低于搜索下限")).toBeInTheDocument();
    expect(screen.getByText("源模拟已清理")).toBeInTheDocument();
    expect(screen.getByText("当前计划已变化")).toBeInTheDocument();
    expect(screen.getByText(/连线仅为视觉连接/)).toBeInTheDocument();
    expect(screen.getByText(/空心点表示搜索域内没有达标值/)).toBeInTheDocument();
    expect(screen.getByText(/Wilson 区间只反映有限模拟路径/)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "这次到底计算了什么" })).toBeInTheDocument();
    expect(screen.getByText(/源共 3,000 条，固定使用前 3,000 条 · seed 42/)).toBeInTheDocument();
    expect(screen.getByText(/这里展示的是 run 创建时的冻结值/)).toBeInTheDocument();
    expect(screen.getAllByText("目标 Wilson 下界")).toHaveLength(3);
    expect(screen.queryByText(/安全金额|95% 确信会成功|保证支出/)).not.toBeInTheDocument();

    // The stale badge disables apply. Return the frozen run to current state and refetch.
    api.detail.mockResolvedValue({ ...completeRun(), current_plan_changed: false });
    cleanup();
    renderPage();
    const actions = await screen.findAllByRole("button", { name: "预览应用" });
    fireEvent.click(actions[0]);
    expect(await screen.findByRole("dialog", { name: "应用 FIRE 达标前沿点" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认应用" }));
    await waitFor(() => expect(api.apply).toHaveBeenCalledWith(preview));
  });

  it("explains the required-current-assets result from its frozen source data", async () => {
    const assetEvaluation = { ...evaluation, retirement_age: 0, value_minor: 120_000_000 };
    const assetPoint = {
      id: "point_assets", retirement_age: 0, value_minor: 120_000_000,
      status: "boundary_found", applicable: true, evaluation: assetEvaluation,
      worse_neighbor: { ...assetEvaluation, value_minor: 116_000_000, success_wilson_low: 0.89, meets_target: false },
      source_current_assets_minor: 100_000_000, gap_minor: 20_000_000, achieved: false,
    };
    const run = completeRun();
    api.list.mockResolvedValue({ runs: [{ id: "run_1", task_id: "task_1", status: "complete", frontier_type: "required_current_assets", target_probability: 0.9, created_at: 1 }], total: 1 });
    api.detail.mockResolvedValue({
      ...run,
      frontier_type: "required_current_assets",
      config: { ...run.config, frontier_type: "required_current_assets", retirement_age_range: null },
      result: { ...run.result, frontier_type: "required_current_assets", points: [assetPoint] },
    });

    renderPage();

    expect(await screen.findByText(/最低达标当前资产为/)).toHaveTextContent("低一个 step");
    expect(screen.getByText(/只改变起始总资产，并按源模拟中各启用持仓的金额比例同比缩放/)).toBeInTheDocument();
    expect(screen.getByText(/2 个启用持仓 · 按源金额比例/)).toBeInTheDocument();
    expect(screen.getByText(/储蓄.*支出.*退休收入/)).toBeInTheDocument();
    expect(screen.getByText(/当前资产.*缺口/)).toBeInTheDocument();
  });

  it("marks cancellation without rendering partial points", async () => {
    api.list.mockResolvedValue({ runs: [{ id: "run_1", task_id: "task_1", status: "canceled", frontier_type: "required_current_assets", target_probability: 0.9, created_at: 1 }], total: 1 });
    api.detail.mockResolvedValue(completeRun("canceled"));
    renderPage();
    expect(await screen.findByText("任务已取消，没有保存或显示部分前沿。")).toBeInTheDocument();
    expect(screen.queryByText("找到离散边界")).not.toBeInTheDocument();
  });
});
