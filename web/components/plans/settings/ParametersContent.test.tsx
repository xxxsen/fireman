// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "plan_1" }),
}));

const updatePlanSettings = vi.fn();

function percentInputByLabel(label: string): HTMLInputElement | null {
  for (const node of screen.queryAllByText(label)) {
    const input = node.closest("label")?.querySelector<HTMLInputElement>(
      'input[data-testid="percent-input"]',
    );
    if (input) return input;
  }
  return null;
}

vi.mock("@/hooks/usePlanResultStale", () => ({
  usePlanResultStale: () => ({ stale: false }),
}));

const markDirty = vi.fn();
const markClean = vi.fn();

vi.mock("@/hooks/usePlanEdit", () => ({
  usePlanEdit: () => ({
    dirty: true,
    markDirty,
    markClean,
  }),
}));

vi.mock("@/lib/api/plans", () => ({
  getPlan: () =>
    Promise.resolve({
      id: "plan_1",
      name: "测试计划",
      config_version: 1,
      base_currency: "CNY",
      valuation_date: "2026-06-09",
      status: "active",
      created_at: 0,
      updated_at: 0,
    }),
  getParameters: () =>
    Promise.resolve({
      parameters: {
        plan_id: "plan_1",
        current_age: 30,
        retirement_age: 55,
        end_age: 90,
        total_assets_minor: 1_000_000_00,
        annual_savings_minor: 200_000_00,
        annual_savings_growth_rate: 0,
        annual_spending_minor: 400_000_00,
		annual_retirement_income_minor: 0,
		annual_retirement_income_growth_rate: 0,
        terminal_wealth_floor_minor: 0,
        inflation_mode: "fixed_real",
        fixed_inflation_rate: 0.03,
        inflation_mu: 0.03,
        inflation_phi: 0.5,
        inflation_sigma: 0.01,
        withdrawal_type: "fixed_real",
        withdrawal_rate: 0.04,
        withdrawal_floor_ratio: 0.7,
        withdrawal_ceiling_ratio: 1.3,
        withdrawal_tax_rate: 0,
        taxable_withdrawal_ratio: 0,
        rebalance_frequency: "annual",
        rebalance_threshold: 0.03,
        transaction_cost_rate: 0,
        simulation_runs: 10000,
        student_t_df: 7,
        return_assumption_mode: "blended_prior",
        assumption_selection_mode: "follow_global",
        return_assumption_set_id: "",
        return_assumption_set_version: 0,
        return_assumption_scenario: "baseline",
        updated_at: 0,
      },
      effective_assumption: {
        profile_id: "system_cma_v3",
        profile_version: 1,
        content_hash: "1234567890abcdef",
        scenario: "baseline",
      },
    }),
  updatePlanSettings: (...args: unknown[]) => updatePlanSettings(...args),
}));

vi.mock("@/lib/api/assumptions", () => ({
  listAssumptionProfiles: () =>
    Promise.resolve({
      profiles: [
        {
          id: "system_cma_v3",
          version: 1,
          owner_scope: "system",
          name: "系统默认（CMA v3）",
          status: "active",
          content_hash: "h",
          created_at: 0,
          updated_at: 0,
          eligible_for_global_default: true,
        },
      ],
      preferences: {
        default_profile_id: "system_cma_v3",
        default_profile_version: 1,
        default_scenario: "baseline",
      },
      scenarios: ["conservative", "baseline", "optimistic"],
    }),
}));

const holdingsSumMinor = vi.hoisted(() => ({ value: 1_000_000_00 }));
const getHoldingsMock = vi.hoisted(() =>
  vi.fn(() =>
    Promise.resolve({
      holdings: [
        {
          id: "h1",
          plan_id: "plan_1",
          instrument_id: "ins_1",
          enabled: true,
          asset_class: "equity",
          region: "domestic",
          weight_within_group: 1,
          current_amount_minor: holdingsSumMinor.value,
          simulation_snapshot_id: "snap_1",
          simulation_snapshot_created_at: Date.parse("2026-06-09T08:00:00.000Z"),
          snapshot_complete_year_count: 8,
          snapshot_monthly_return_count: 96,
          snapshot_history_depth: "five_plus_years",
          snapshot_metrics_version: "monthly_log_return_v1",
          snapshot_warnings: ["仅有 1 个完整自然年度，收益与风险估计的不确定性较高"],
          instrument_code: "T1",
          instrument_name: "测试权益基金",
          sort_order: 1,
        },
      ],
    }),
  ),
);

vi.mock("@/lib/api/holdings", () => ({
  getHoldings: (...args: unknown[]) => getHoldingsMock(...args),
}));

const defaultAllocation = {
  asset_class_targets: [
    { asset_class: "equity", weight: 1 },
    { asset_class: "bond", weight: 0 },
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
};
const getAllocationMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/allocation", () => ({
  getAllocation: (...args: unknown[]) => getAllocationMock(...args),
  listScenarios: () => Promise.resolve({ scenarios: [] }),
}));

import { ParametersContent as ParametersPage } from "./ParametersContent";

describe("ParametersPage strategy enums", () => {
  beforeEach(() => {
    holdingsSumMinor.value = 1_000_000_00;
    getHoldingsMock.mockClear();
    getHoldingsMock.mockImplementation(() =>
      Promise.resolve({
        holdings: [
          {
            id: "h1",
            plan_id: "plan_1",
            instrument_id: "ins_1",
            enabled: true,
            asset_class: "equity",
            region: "domestic",
            weight_within_group: 1,
            current_amount_minor: holdingsSumMinor.value,
            simulation_snapshot_id: "snap_1",
            simulation_snapshot_created_at: Date.parse("2026-06-09T08:00:00.000Z"),
            snapshot_complete_year_count: 8,
            snapshot_monthly_return_count: 96,
            snapshot_history_depth: "five_plus_years",
            snapshot_metrics_version: "monthly_log_return_v1",
            snapshot_warnings: ["仅有 1 个完整自然年度，收益与风险估计的不确定性较高"],
            instrument_code: "T1",
            instrument_name: "测试权益基金",
            sort_order: 1,
          },
        ],
      }),
    );
    updatePlanSettings.mockReset();
    updatePlanSettings.mockResolvedValue({});
    getAllocationMock.mockReset();
    getAllocationMock.mockResolvedValue(defaultAllocation);
  });

  it("shows error state (not a false scale-gap prompt) when holdings load fails", async () => {
    getHoldingsMock.mockReset();
    getHoldingsMock.mockRejectedValue(new Error("boom"));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.queryByText(/规模缺口/)).not.toBeInTheDocument();
    expect(screen.queryByText("资金与现金流")).not.toBeInTheDocument();
  });

  it("shows error state when allocation load fails", async () => {
    getAllocationMock.mockReset();
    getAllocationMock.mockRejectedValue(new Error("boom"));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });

  it("shows holdings simulation snapshot fields", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("持仓模拟数据");
    expect(await screen.findByText("测试权益基金（T1）")).toBeInTheDocument();
    expect(await screen.findByText("历史样本充足")).toBeInTheDocument();
    expect(await screen.findByText("8")).toBeInTheDocument();
    expect(await screen.findByText("96")).toBeInTheDocument();
    expect(await screen.findByText("monthly_log_return_v1")).toBeInTheDocument();
    expect(
      await screen.findByText("仅有 1 个完整自然年度，收益与风险估计的不确定性较高"),
    ).toBeInTheDocument();
  });

  it("keeps frozen snapshot fields after instrument library refresh", async () => {
    getHoldingsMock.mockImplementation(() =>
      Promise.resolve({
        holdings: [
          {
            id: "h1",
            plan_id: "plan_1",
            instrument_id: "ins_1",
            enabled: true,
            asset_class: "equity",
            region: "domestic",
            weight_within_group: 1,
            current_amount_minor: holdingsSumMinor.value,
            simulation_snapshot_id: "snap_frozen",
            simulation_snapshot_created_at: Date.parse("2026-06-09T08:00:00.000Z"),
            snapshot_complete_year_count: 8,
            snapshot_monthly_return_count: 96,
            snapshot_history_depth: "five_plus_years",
            snapshot_metrics_version: "monthly_log_return_v1",
            snapshot_warnings: [],
            instrument_code: "T1",
            instrument_name: "测试权益基金",
            sort_order: 1,
          },
        ],
      }),
    );

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("持仓模拟数据");
    expect(await screen.findByText("8")).toBeInTheDocument();
    expect(await screen.findByText("历史样本充足")).toBeInTheDocument();
    expect(await screen.findByText("monthly_log_return_v1")).toBeInTheDocument();
    expect(getHoldingsMock).toHaveBeenCalled();
  });

  it("sends fixed_portfolio and random_ar1 on save", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("提取与通胀");
    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "fixed_portfolio" } });
    fireEvent.change(screen.getByLabelText("通胀模式"), { target: { value: "random_ar1" } });
    expect(screen.getByLabelText("提取策略")).toHaveValue("fixed_portfolio");
    expect(screen.getByLabelText("通胀模式")).toHaveValue("random_ar1");
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updatePlanSettings).toHaveBeenCalledTimes(1));
    const req = updatePlanSettings.mock.calls[0]![1] as {
      parameters: { withdrawal_type: string; inflation_mode: string };
    };
    expect(req.parameters.withdrawal_type).toBe("fixed_portfolio");
    expect(req.parameters.inflation_mode).toBe("random_ar1");
  });

  it("saves name + allocation + parameters with exactly one request", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    const nameInput = await screen.findByTestId("plan-name-input");
    fireEvent.change(nameInput, { target: { value: "合并保存计划" } });

    const classHeading = await screen.findByRole("heading", { name: /大类目标权重/ });
    const classInputs = within(classHeading.parentElement as HTMLElement).getAllByTestId(
      "percent-input",
    );
    expect(classInputs).toHaveLength(3);
    fireEvent.change(classInputs[0]!, { target: { value: "60" } });
    fireEvent.change(classInputs[1]!, { target: { value: "40" } });

    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "fixed_portfolio" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updatePlanSettings).toHaveBeenCalledTimes(1));
    const req = updatePlanSettings.mock.calls[0]![1] as {
      config_version: number;
      plan?: { name: string };
      allocation?: { asset_class_targets: { asset_class: string; weight: number }[] };
      parameters: { withdrawal_type: string };
    };
    expect(req.config_version).toBe(1);
    expect(req.plan).toEqual({ name: "合并保存计划" });
    expect(req.allocation?.asset_class_targets).toEqual([
      { asset_class: "equity", weight: 0.6 },
      { asset_class: "bond", weight: 0.4 },
      { asset_class: "cash", weight: 0 },
    ]);
    expect(req.parameters.withdrawal_type).toBe("fixed_portfolio");
  });

  it("omits plan and allocation patches when only parameters changed", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("提取与通胀");
    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "fixed_portfolio" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updatePlanSettings).toHaveBeenCalledTimes(1));
    const req = updatePlanSettings.mock.calls[0]![1] as Record<string, unknown>;
    expect(req.plan).toBeUndefined();
    expect(req.allocation).toBeUndefined();
  });

  it("requires confirmation before switching the return-assumption mode", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage showAllocation={false} showStale={false} />
      </QueryClientProvider>,
    );

    await screen.findByText("收益率假设");
    fireEvent.change(screen.getByLabelText("收益假设来源"), {
      target: { value: "historical_cagr" },
    });
    expect(screen.getByText(/历史收益不代表未来收益/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(screen.getByText(/切换收益假设来源需先勾选确认/)).toBeInTheDocument();
    expect(updatePlanSettings).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("checkbox", { name: /我确认切换收益假设来源/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updatePlanSettings).toHaveBeenCalledTimes(1));
    const req = updatePlanSettings.mock.calls[0]![1] as {
      parameters: { return_assumption_mode: string };
    };
    expect(req.parameters.return_assumption_mode).toBe("historical_cagr");
  });

  it("blocks save with a Chinese message when core params are invalid (no request)", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("资金与现金流");
    // End age not greater than retirement age (55 in fixture).
    fireEvent.change(screen.getByLabelText("规划终止年龄"), { target: { value: "50" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(await screen.findByText("规划终止年龄需大于退休年龄。")).toBeInTheDocument();
    expect(updatePlanSettings).not.toHaveBeenCalled();

    // Fix the age, then break the budget: zero annual spending.
    fireEvent.change(screen.getByLabelText("规划终止年龄"), { target: { value: "90" } });
    fireEvent.change(screen.getByLabelText(/退休后首年支出/), { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));
    expect(await screen.findByText("当前年支出需大于 0。")).toBeInTheDocument();
    expect(updatePlanSettings).not.toHaveBeenCalled();
  });

	it("blocks transaction cost outside [0%, 100%)", async () => {
		const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
		render(
			<QueryClientProvider client={qc}>
				<ParametersPage />
			</QueryClientProvider>,
		);
		const label = (await screen.findByText("交易成本率")).closest("label");
		const input = within(label as HTMLElement).getByTestId("percent-input");
		for (const invalid of ["-1", "100", "120"]) {
			fireEvent.change(input, { target: { value: invalid } });
			expect(screen.getAllByText("交易成本率必须大于等于 0% 且小于 100%。").length).toBeGreaterThan(0);
			fireEvent.click(screen.getByRole("button", { name: "保存" }));
			expect(updatePlanSettings).not.toHaveBeenCalled();
		}
	});

  it("blocks save when the plan name is cleared (backend would silently ignore it)", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    const nameInput = await screen.findByTestId("plan-name-input");
    fireEvent.change(nameInput, { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(await screen.findByText("计划名称不能为空。")).toBeInTheDocument();
    expect(updatePlanSettings).not.toHaveBeenCalled();
  });

  it("renders the plan baseline help inline with its label", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("资金与现金流");
    const label = screen.getByText("计划基准规模").closest("label");
    expect(label).not.toBeNull();
    expect(label!.querySelector('[data-testid="metric-help-trigger"]')).not.toBeNull();
  });

  it("shows scale gap wording when plan baseline exceeds holdings (§6.4)", async () => {
    holdingsSumMinor.value = 400_000_00;
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("资金与现金流");
    expect(screen.getAllByText(/规模缺口/).length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText(/未分配差额/)).not.toBeInTheDocument();
  });

  it("sends max seed as string without precision loss", async () => {
    const maxSeed = "9223372036854775807";
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage />
      </QueryClientProvider>,
    );

    await screen.findByText("模拟设置");
    const seedInput = screen.getByRole("textbox", { name: /随机种子/ });
    fireEvent.change(seedInput, { target: { value: maxSeed } });
    expect(seedInput).toHaveValue(maxSeed);
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(updatePlanSettings).toHaveBeenCalledTimes(1));
    const req = updatePlanSettings.mock.calls[0]![1] as {
      parameters: { seed: unknown };
    };
    expect(typeof req.parameters.seed).toBe("string");
    expect(req.parameters.seed).toBe(maxSeed);
    expect(req.parameters.seed).not.toBe(Number(maxSeed));
  });

  it("shows save bar after plan name edit in FIRE params section", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage showAllocation={false} showStale={false} />
      </QueryClientProvider>,
    );

    const nameInput = await screen.findByTestId("plan-name-input");
    expect(screen.queryByText("有未保存的修改")).not.toBeInTheDocument();
    fireEvent.change(nameInput, { target: { value: "新计划名称" } });
    expect(screen.getByText("有未保存的修改")).toBeInTheDocument();
  });

  it("does not show save bar when focusing text, money, or percent fields without edits", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage showAllocation={false} showStale={false} />
      </QueryClientProvider>,
    );

    const nameInput = await screen.findByTestId("plan-name-input");
    await screen.findByText("资金与现金流");
    const fields = [
      nameInput,
      screen.getAllByTestId("money-input")[0]!,
      screen.getAllByTestId("percent-input")[0]!,
    ];
    for (const field of fields) {
      fireEvent.focus(field);
      fireEvent.blur(field);
    }
    expect(screen.queryByText("有未保存的修改")).not.toBeInTheDocument();
    expect(updatePlanSettings).not.toHaveBeenCalled();
  });

  it("shows and validates only fields active in the selected modes", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage showAllocation={false} showStale={false} />
      </QueryClientProvider>,
    );

    await screen.findByText("提取与通胀");
    expect(percentInputByLabel("固定通胀率")).toBeInTheDocument();
    expect(percentInputByLabel("通胀均值 μ")).not.toBeInTheDocument();
    expect(percentInputByLabel("提取率")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("通胀模式"), { target: { value: "random_ar1" } });
    expect(percentInputByLabel("固定通胀率")).not.toBeInTheDocument();
    expect(percentInputByLabel("通胀均值 μ")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/提取策略/), { target: { value: "fixed_portfolio" } });
    expect(percentInputByLabel("提取率")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/提取策略/), { target: { value: "guardrail" } });
    expect(percentInputByLabel("提取率")).not.toBeInTheDocument();
    expect(percentInputByLabel("护栏下限比例")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /查看「护栏提取率」说明/ })).toHaveLength(2);
  });

  it("keeps follow-global and baseline as distinct scenario choices", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ParametersPage showAllocation={false} showStale={false} />
      </QueryClientProvider>,
    );

    const select = await screen.findByLabelText("假设情景");
    expect(within(select).getByRole("option", { name: "跟随全局默认" })).toHaveValue("follow_global");
    expect(within(select).getByRole("option", { name: "基准" })).toHaveValue("baseline");
    expect(screen.getByTestId("effective-assumption")).toHaveTextContent(
      "system_cma_v3@1 / baseline / 12345678",
    );
  });
});
