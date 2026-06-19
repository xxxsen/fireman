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

async function goToScenarioStep() {
  fireEvent.click(screen.getByRole("button", { name: "下一步" }));
  await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());
}

async function goToInstrumentStep(scenarioId: string, regionEdits?: { equityForeign?: string }) {
  await goToScenarioStep();
  const scenarioLabel = scenarioId === "scn_a" ? "测试场景" : "保守";
  await waitFor(() =>
    expect(screen.getByRole("option", { name: new RegExp(scenarioLabel) })).toBeInTheDocument(),
  );
  fireEvent.change(screen.getByRole("combobox"), { target: { value: scenarioId } });

  if (regionEdits?.equityForeign !== undefined) {
    await waitFor(() => expect(screen.getByText("地区组内权重")).toBeInTheDocument());
    const percentInputs = screen.getAllByTestId("percent-input");
    fireEvent.change(percentInputs[1]!, { target: { value: regionEdits.equityForeign } });
  }

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
  await waitFor(() => expect(screen.getByText(/确认组合/)).toBeInTheDocument());
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

  it("uses a narrow card on the basics step and a wide card on the confirm step", async () => {
    renderWizard();
    expect(screen.getByTestId("wizard-step-card")).toHaveClass("max-w-2xl");
    await goToConfirmStep();
    expect(screen.getByTestId("wizard-step-card")).toHaveClass("w-full");
    expect(screen.getByTestId("wizard-step-card")).not.toHaveClass("max-w-2xl");
  });

  it("uses updated default plan name and financial inputs", async () => {
    renderWizard();

    const today = new Date().toISOString().slice(0, 10);
    expect(screen.getByDisplayValue(`我的 FIRE 计划 (${today})`)).toBeInTheDocument();
    expect(screen.getByLabelText("当前年龄")).toHaveValue(30);
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
    await goToScenarioStep();
    await waitFor(() =>
      expect(screen.getByRole("option", { name: /保守/ })).toBeInTheDocument(),
    );
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "scn_conservative" } });
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
});
