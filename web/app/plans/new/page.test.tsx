// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import NewPlanWizardPage from "./page";

const routerPush = vi.fn();
const listInstruments = vi.hoisted(() => vi.fn());
const searchInstruments = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPush }),
}));

const createPlanWizard = vi.fn();
const createSimulation = vi.fn();

vi.mock("@/lib/api/plans", () => ({
  createPlanWizard: (...args: unknown[]) => createPlanWizard(...args),
  createPlan: vi.fn(),
  updateParameters: vi.fn(),
}));

vi.mock("@/lib/api/holdings", () => ({ updateHoldings: vi.fn() }));
vi.mock("@/lib/api/simulations", () => ({
  createSimulation: (...args: unknown[]) => createSimulation(...args),
}));
const conservativeScenario = {
  id: "scn_conservative",
  name: "保守",
  weights: [
    { asset_class: "equity", weight: 0.45 },
    { asset_class: "bond", weight: 0.45 },
    { asset_class: "cash", weight: 0.1 },
  ],
  is_builtin: true,
  created_at: 0,
  updated_at: 0,
};

const singleClassScenario = {
  id: "scn_a",
  name: "测试场景",
  weights: [
    { asset_class: "equity", weight: 1 },
    { asset_class: "bond", weight: 0 },
    { asset_class: "cash", weight: 0 },
  ],
  is_builtin: true,
  created_at: 0,
  updated_at: 0,
};

vi.mock("@/lib/api/allocation", () => ({
  listScenarios: () =>
    Promise.resolve({
      scenarios: [singleClassScenario, conservativeScenario],
    }),
}));

vi.mock("@/lib/api/instruments", () => ({
  listInstruments: (...args: unknown[]) => listInstruments(...args),
  searchInstruments: (...args: unknown[]) => searchInstruments(...args),
}));

interface SearchParams {
  q?: string;
  assetClass?: string;
  region?: string;
  excludeIds?: string[];
  cursor?: number;
  limit?: number;
}

let searchPool: (typeof defaultInstruments)[number][] = [];

function filterSearchPool(params: SearchParams) {
  const q = (params.q ?? "").toLowerCase();
  const exclude = new Set(params.excludeIds ?? []);
  const items = searchPool.filter(
    (i) =>
      (!params.assetClass || i.asset_class === params.assetClass) &&
      (!params.region || i.region === params.region) &&
      i.status === "active" &&
      !i.is_system &&
      !exclude.has(i.id) &&
      (!q || i.code.toLowerCase().includes(q) || i.name.toLowerCase().includes(q)),
  );
  const cursor = params.cursor ?? 0;
  const limit = params.limit ?? 10;
  const page = items.slice(cursor, cursor + limit);
  const next = cursor + page.length < items.length ? cursor + page.length : null;
  return Promise.resolve({ instruments: page, next_cursor: next, total: items.length });
}

const defaultInstruments = [
  {
    id: "ins_equity_domestic",
    code: "T1",
    name: "测试权益基金",
    market: "CN",
    instrument_type: "fund",
    asset_class: "equity",
    region: "domestic",
    currency: "CNY",
    quality_status: "available",
    simulation_eligible: true,
    status: "active",
    is_system: false,
  },
  {
    id: "ins_equity_foreign",
    code: "F1",
    name: "测试国外权益基金",
    market: "CN",
    instrument_type: "fund",
    asset_class: "equity",
    region: "foreign",
    currency: "CNY",
    quality_status: "available",
    simulation_eligible: true,
    status: "active",
    is_system: false,
  },
  {
    id: "ins_bond",
    code: "B1",
    name: "测试债券基金",
    market: "CN",
    instrument_type: "fund",
    asset_class: "bond",
    region: "domestic",
    currency: "CNY",
    quality_status: "available",
    simulation_eligible: true,
    status: "active",
    is_system: false,
  },
];

function renderWizard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <NewPlanWizardPage />
    </QueryClientProvider>,
  );
}

function getScenarioSelect() {
  return screen.getByRole("combobox", { name: "配置模板" });
}

// Scenario + region targets now live on the merged 计划目标 step (step 0).
async function selectScenario(scenarioId: string, regionEdits?: { equityForeign?: string }) {
  const scenarioLabel = scenarioId === "scn_a" ? "测试场景" : "保守";
  await waitFor(() =>
    expect(screen.getByRole("option", { name: new RegExp(scenarioLabel) })).toBeInTheDocument(),
  );
  fireEvent.change(getScenarioSelect(), { target: { value: scenarioId } });

  if (regionEdits?.equityForeign !== undefined) {
    await waitFor(() => expect(screen.getByText("地区组内权重")).toBeInTheDocument());
    const percentInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(percentInputs[1]!, { target: { value: regionEdits.equityForeign } });
  }
}

async function goToInstrumentStep(scenarioId: string, regionEdits?: { equityForeign?: string }) {
  await selectScenario(scenarioId, regionEdits);
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  await waitFor(() =>
    expect(screen.getByText(/按大类分标签页搜索并添加标的/)).toBeInTheDocument(),
  );
}

async function selectEquityInstrument() {
  const search = await screen.findByLabelText("权益国内搜索");
  fireEvent.change(search, { target: { value: "T1" } });
  fireEvent.click(await screen.findByRole("button", { name: /测试权益基金/ }));
}

async function goToConfirmStep(scenarioId = "scn_a") {
  await goToInstrumentStep(scenarioId);
  await selectEquityInstrument();
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  await waitFor(() => expect(screen.getByText(/已选标的：/)).toBeInTheDocument());
}

describe("NewPlanWizardPage", () => {
  beforeEach(() => {
    createPlanWizard.mockReset();
    createSimulation.mockReset();
    routerPush.mockReset();
    listInstruments.mockReset();
    listInstruments.mockResolvedValue({ instruments: defaultInstruments });
    searchInstruments.mockReset();
    searchPool = defaultInstruments;
    searchInstruments.mockImplementation((params: SearchParams) => filterSearchPool(params));
    createPlanWizard.mockResolvedValue({ id: "plan_new", config_version: 1 });
    createSimulation.mockResolvedValue({ job_id: "job_1", run_id: "run_1", status: "queued" });
  });

  it("shows short-history warning on confirm step for one-year instruments", async () => {
    searchPool = [
      {
        id: "ins_short",
        code: "SHORT01",
        name: "短历史基金",
        market: "CN",
        instrument_type: "fund",
        asset_class: "equity",
        region: "domestic",
        currency: "CNY",
        quality_status: "available",
        simulation_eligible: true,
        history_depth: "one_year",
        status: "active",
        is_system: false,
      },
    ] as unknown as typeof defaultInstruments;

    renderWizard();
    await goToInstrumentStep("scn_a");
    const search = await screen.findByLabelText("权益国内搜索");
    fireEvent.change(search, { target: { value: "SHORT" } });
    fireEvent.click(await screen.findByRole("button", { name: /短历史基金/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));

    const warning = await screen.findByTestId("wizard-short-history");
    expect(warning).toHaveTextContent("短历史基金（SHORT01）历史样本有限");
    expect(warning).toHaveTextContent("模拟长期估计不确定性较高");
  });

  it("shows confirm checklist on final step", async () => {
    renderWizard();
    await goToConfirmStep();

    expect(screen.getByText("组内权重：通过")).toBeInTheDocument();
    expect(screen.getByText("全组合目标权重：通过")).toBeInTheDocument();
    expect(screen.getByText(/已选标的：1 个/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "创建计划" })).toBeInTheDocument();
  });

  it("uses one full-width step card across all steps", async () => {
    renderWizard();
    const card = screen.getByTestId("wizard-step-card");
    expect(card).toHaveClass("w-full");
    expect(card).not.toHaveClass("max-w-2xl");
    await goToConfirmStep();
    expect(screen.getByTestId("wizard-step-card")).toHaveClass("w-full");
    expect(screen.getByTestId("wizard-step-card")).not.toHaveClass("max-w-2xl");
  });

  it("uses updated default plan name and financial inputs", async () => {
    renderWizard();

    const today = new Date().toISOString().slice(0, 10);
    expect(screen.getByDisplayValue(`我的 FIRE 计划 (${today})`)).toBeInTheDocument();
    expect(screen.getByLabelText("当前年龄")).toHaveValue(35);
    expect(screen.getByLabelText("退休年龄")).toHaveValue(35);
    expect(screen.getByDisplayValue("4000000")).toBeInTheDocument();
    expect(screen.getByDisplayValue("120000")).toBeInTheDocument();
    expect(screen.getByDisplayValue("100000")).toBeInTheDocument();
    expect(screen.getByText("预计 FIRE 时长")).toBeInTheDocument();
  });

  it("does not call create until final step", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await waitFor(() => expect(createPlanWizard).not.toHaveBeenCalled());
  });

  it("shows tabs for equity and bond without cash container", async () => {
    renderWizard();
    await goToInstrumentStep("scn_conservative");

    expect(screen.getByRole("tab", { name: /权益/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /债券/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("现金/其他选标")).not.toBeInTheDocument();

    const equitySearch = screen.getByLabelText("权益国内搜索");
    fireEvent.change(equitySearch, { target: { value: "B1" } });
    expect(screen.queryByRole("button", { name: /测试债券基金/ })).not.toBeInTheDocument();
    expect(await screen.findByText("未找到匹配的权益标的。")).toBeInTheDocument();
  });

  it("auto-complements region allocation when editing domestic", async () => {
    renderWizard();
    await waitFor(() =>
      expect(screen.getByRole("option", { name: /保守/ })).toBeInTheDocument(),
    );
    fireEvent.change(getScenarioSelect(), { target: { value: "scn_conservative" } });
    await waitFor(() => expect(screen.getByText("地区组内权重")).toBeInTheDocument());

    const percentInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(percentInputs[0]!, { target: { value: "70" } });
    expect(percentInputs[1]).toHaveValue("30");
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() =>
      expect(screen.getByText(/按大类分标签页搜索并添加标的/)).toBeInTheDocument(),
    );
  });

  it("shows domestic and foreign sub-containers when equity foreign > 0", async () => {
    renderWizard();
    await goToInstrumentStep("scn_conservative", { equityForeign: "30" });

    expect(screen.getByLabelText("国内（占权益 70%）搜索")).toBeInTheDocument();
    expect(screen.getByLabelText("国外（占权益 30%）搜索")).toBeInTheDocument();

    const domesticSearch = screen.getByLabelText("国内（占权益 70%）搜索");
    fireEvent.change(domesticSearch, { target: { value: "F1" } });
    expect(screen.queryByRole("button", { name: /测试国外权益基金/ })).not.toBeInTheDocument();

    const foreignSearch = screen.getByLabelText("国外（占权益 30%）搜索");
    fireEvent.change(foreignSearch, { target: { value: "T1" } });
    expect(screen.queryByRole("button", { name: /测试权益基金/ })).not.toBeInTheDocument();
  });

  it("defaults weight to 100% when selecting first instrument", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    expect(await screen.findByText("¥4,000,000.00")).toBeInTheDocument();
  });

  it("creates without simulation by default and navigates to overview", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/10,000/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "创建计划" }));
    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const body = createPlanWizard.mock.calls[0]![0] as {
      region_targets: unknown[];
      apply_unallocated_to_cash: boolean;
    };
    expect(body.region_targets).toHaveLength(6);
    expect(createSimulation).not.toHaveBeenCalled();
    expect(routerPush).toHaveBeenCalledWith("/plans/plan_new/overview");
  });

  it("starts optional simulation and navigates with its job id", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/10,000/)).toBeInTheDocument());
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /创建后运行 FIRE 模拟/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "创建并运行模拟" }));

    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(createSimulation).toHaveBeenCalledTimes(1));
    expect(createSimulation).toHaveBeenCalledWith("plan_new", { runs: 10000 });
    expect(routerPush).toHaveBeenCalledWith(
      "/plans/plan_new/overview?job_id=job_1",
    );
  });

  it("still enters the created plan when optional simulation fails to start", async () => {
    createSimulation.mockRejectedValueOnce(new Error("启动失败"));
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await screen.findByRole("button", { name: "创建计划" });
    fireEvent.click(screen.getByRole("checkbox", { name: /创建后运行 FIRE 模拟/ }));
    fireEvent.click(screen.getByRole("button", { name: "创建并运行模拟" }));

    await waitFor(() =>
      expect(routerPush).toHaveBeenCalledWith(
        "/plans/plan_new/overview?simulation_error=1",
      ),
    );
    expect(createPlanWizard).toHaveBeenCalledTimes(1);
  });

  it("renders the three merged steps in the progress bar", () => {
    renderWizard();
    expect(screen.getByText("1. 计划目标")).toBeInTheDocument();
    expect(screen.getByText("2. 建立持仓")).toBeInTheDocument();
    expect(screen.getByText("3. 确认组合")).toBeInTheDocument();
    expect(screen.queryByText(/^4\. /)).not.toBeInTheDocument();
    expect(screen.queryByText("1. 计划基础")).not.toBeInTheDocument();
  });

  it("blocks leaving 计划目标 until a scenario is chosen", async () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("请选择配置模板。")).toBeInTheDocument();
    expect(screen.queryByText(/按大类分标签页搜索并添加标的/)).not.toBeInTheDocument();
  });

  it("preserves the draft when navigating back to 计划目标 and forward", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();

    fireEvent.click(screen.getByRole("button", { name: "上一步" }));
    expect(getScenarioSelect()).toHaveValue("scn_a");

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() =>
      expect(screen.getByText(/按大类分标签页搜索并添加标的/)).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/已选标的：1 个/)).toBeInTheDocument();
  });

  it("fire duration: one editable input with preset回填", async () => {
    renderWizard();
    const durationInputs = screen.getAllByLabelText("预计 FIRE 时长（年）");
    expect(durationInputs).toHaveLength(1);
    const input = durationInputs[0]!;

    fireEvent.change(input, { target: { value: "37" } });
    expect(input).toHaveValue(37);
    // Custom value is not a preset, so the suggestion select shows its placeholder.
    const preset = screen.getByLabelText("常用 FIRE 时长预设");
    expect(preset).toHaveValue("");

    fireEvent.change(preset, { target: { value: "40" } });
    expect(screen.getByLabelText("预计 FIRE 时长（年）")).toHaveValue(40);
    expect(screen.getByLabelText("常用 FIRE 时长预设")).toHaveValue("40");
  });

  it("keeps a custom fire duration after switching scenario", async () => {
    renderWizard();
    fireEvent.change(screen.getByLabelText("预计 FIRE 时长（年）"), { target: { value: "37" } });
    await selectScenario("scn_a");
    expect(screen.getByLabelText("预计 FIRE 时长（年）")).toHaveValue(37);
  });

  it("advanced FIRE params start collapsed and submit current defaults", async () => {
    renderWizard();
    expect(screen.getByTestId("wizard-advanced-params")).not.toHaveAttribute("open");

    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "创建计划" }));

    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const body = createPlanWizard.mock.calls[0]![0] as {
      parameters: Record<string, unknown>;
    };
    expect(body.parameters).toMatchObject({
      inflation_mode: "fixed_real",
      fixed_inflation_rate: 0.03,
      withdrawal_type: "fixed_real",
      withdrawal_rate: 0.04,
      withdrawal_floor_ratio: 0.7,
      withdrawal_ceiling_ratio: 1.3,
      withdrawal_tax_rate: 0,
      taxable_withdrawal_ratio: 0,
    });
  });

  it("removes a now-disabled region holding and excludes it from the payload", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument(); // domestic T1

    // Back to 计划目标 and flip equity to 国内 0% / 国外 100%.
    fireEvent.click(screen.getByRole("button", { name: "上一步" }));
    const percentInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(percentInputs[0]!, { target: { value: "0" } });

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() =>
      expect(screen.getByTestId("wizard-removed-by-targets")).toHaveTextContent(
        "测试权益基金（T1）",
      ),
    );
    // Domestic picker is gone; foreign picker is present.
    expect(screen.queryByLabelText("权益国内搜索")).not.toBeInTheDocument();
    expect(screen.getByLabelText("权益国外搜索")).toBeInTheDocument();

    // Pick a foreign equity, finish, and assert the payload omits T1.
    const foreignSearch = screen.getByLabelText("权益国外搜索");
    fireEvent.change(foreignSearch, { target: { value: "F1" } });
    fireEvent.click(await screen.findByRole("button", { name: /测试国外权益基金/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "创建计划" }));

    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const body = createPlanWizard.mock.calls[0]![0] as {
      holdings: { instrument_id: string }[];
    };
    const ids = body.holdings.map((h) => h.instrument_id);
    expect(ids).toContain("ins_equity_foreign");
    expect(ids).not.toContain("ins_equity_domestic");
  });

  it("blocks advancing when advanced params are out of range", async () => {
    renderWizard();
    const advancedInputs = screen.getAllByTestId("percent-input");
    // Default fixed_real layout: [固定通胀率, 有效提取税率, 应税提取比例].
    fireEvent.change(advancedInputs[1]!, { target: { value: "100" } });
    fireEvent.change(advancedInputs[2]!, { target: { value: "100" } });
    expect(screen.getByTestId("wizard-advanced-errors")).toHaveTextContent(
      "有效提取税率 × 应税提取比例需小于 1。",
    );

    await selectScenario("scn_a");
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/高级 FIRE 参数无效/)).toBeInTheDocument();
    expect(screen.queryByText(/按大类分标签页搜索并添加标的/)).not.toBeInTheDocument();
  });

  it("requires confirmation when fixed inflation exceeds 15%", async () => {
    renderWizard();
    const advancedInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(advancedInputs[0]!, { target: { value: "18" } });

    await selectScenario("scn_a");
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText(/固定通胀率超过 15%，请/)).toBeInTheDocument();
    expect(screen.queryByText(/按大类分标签页搜索并添加标的/)).not.toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("checkbox", { name: /确认固定通胀率超过 15%/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() =>
      expect(screen.getByText(/按大类分标签页搜索并添加标的/)).toBeInTheDocument(),
    );
  });

  it("sends end_age = retirement_age + custom duration", async () => {
    renderWizard();
    fireEvent.change(screen.getByLabelText("预计 FIRE 时长（年）"), {
      target: { value: "37" },
    });
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "创建计划" }));

    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const body = createPlanWizard.mock.calls[0]![0] as {
      parameters: { retirement_age: number; end_age: number };
    };
    expect(body.parameters.retirement_age).toBe(35);
    expect(body.parameters.end_age).toBe(72);
  });

  it("summarizes advanced params on the confirm step", async () => {
    renderWizard();
    await goToConfirmStep();
    const summary = screen.getByTestId("wizard-advanced-summary");
    expect(summary).toHaveTextContent("使用默认值");
    expect(summary).toHaveTextContent("固定 3%");
    expect(summary).toHaveTextContent("固定实际支出");
  });

  it("marks advanced summary as 已自定义 after editing", async () => {
    renderWizard();
    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "guardrail" } });
    await goToConfirmStep();
    const summary = screen.getByTestId("wizard-advanced-summary");
    expect(summary).toHaveTextContent("已自定义");
    expect(summary).toHaveTextContent("动态提取（护栏）");
  });

  it("persists guardrail withdrawal and random inflation from advanced panel", async () => {
    renderWizard();
    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "guardrail" } });
    fireEvent.change(screen.getByLabelText("通胀模式"), { target: { value: "random_ar1" } });

    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    fireEvent.click(await screen.findByRole("button", { name: "创建计划" }));

    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const body = createPlanWizard.mock.calls[0]![0] as {
      parameters: Record<string, unknown>;
    };
    expect(body.parameters).toMatchObject({
      withdrawal_type: "guardrail",
      inflation_mode: "random_ar1",
    });
    expect(typeof body.parameters.inflation_mu).toBe("number");
    expect(typeof body.parameters.withdrawal_rate).toBe("number");
  });
});
