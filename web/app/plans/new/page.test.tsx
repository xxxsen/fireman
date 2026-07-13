// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { vi } from "vitest";
import NewPlanWizardPage from "./page";
import { QUICK_FIRE_TRANSFER_KEY } from "@/lib/quick-fire-draft";

const routerPush = vi.fn();
const listMarketAssets = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPush }),
}));

const createPlanWizard = vi.fn();
const createSimulation = vi.fn();

function percentInputByLabel(label: string): HTMLInputElement | null {
  for (const node of screen.queryAllByText(label)) {
    const input = node.closest("label")?.querySelector<HTMLInputElement>(
      'input[data-testid="percent-input"]',
    );
    if (input) return input;
  }
  return null;
}

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

vi.mock("@/lib/api/market-assets", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/market-assets")>()),
  listMarketAssets: (...args: unknown[]) => listMarketAssets(...args),
}));

interface ListParams {
  symbolQ?: string;
  nameQ?: string;
  market?: string;
  instrumentTypes?: string[];
  limit?: number;
  offset?: number;
}

let searchPool: (typeof defaultAssets)[number][] = [];

function filterSearchPool(params: ListParams) {
  const symbolQ = (params.symbolQ ?? "").toLowerCase();
  const nameQ = (params.nameQ ?? "").toLowerCase();
  const items = searchPool.filter(
    (a) =>
      (!symbolQ || a.symbol.toLowerCase().includes(symbolQ)) &&
      (!nameQ || a.name.toLowerCase().includes(nameQ)),
  );
  const offset = params.offset ?? 0;
  const limit = params.limit ?? 10;
  const page = items.slice(offset, offset + limit);
  return Promise.resolve({ assets: page, syncs: [], total: items.length });
}

function makeDirectoryAsset(symbol: string, name: string, hasHistory = true) {
  return {
    asset_key: `CN|cn_exchange_fund|sh|${symbol}`,
    market: "CN",
    instrument_type: "cn_exchange_fund",
    region_code: "sh",
    symbol,
    name,
    exchange: "SH",
    instrument_kind: "etf",
    currency: "CNY",
    active: true,
    listing_status: "active",
    last_seen_at: 0,
    source_name: "ak.fund_etf_spot_em",
    source_as_of: "",
    refreshed_at: 0,
    created_at: 0,
    updated_at: 0,
    has_history: hasHistory,
    history_data_as_of: hasHistory ? "2026-07-01" : undefined,
    history_source_name: hasHistory ? "ak.fund_etf_hist_em" : undefined,
  };
}

const defaultAssets = [
  makeDirectoryAsset("T1", "测试权益基金"),
  makeDirectoryAsset("F1", "测试国外权益基金"),
  makeDirectoryAsset("B1", "测试债券基金"),
];

function renderWizard({ strict = false }: { strict?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const page = (
    <QueryClientProvider client={qc}>
      <NewPlanWizardPage />
    </QueryClientProvider>
  );
  return render(strict ? <StrictMode>{page}</StrictMode> : page);
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
    listMarketAssets.mockReset();
    searchPool = defaultAssets;
    listMarketAssets.mockImplementation((params: ListParams) => filterSearchPool(params));
    createPlanWizard.mockResolvedValue({ id: "plan_new", config_version: 1 });
    createSimulation.mockResolvedValue({ task_id: "job_1", run_id: "run_1", status: "pending" });
    window.sessionStorage.clear();
    window.history.replaceState({}, "", "/plans/new");
  });

  it("shows missing-history hint on confirm step for unsynced assets", async () => {
    searchPool = [makeDirectoryAsset("SHORT01", "未同步历史基金", false)];

    renderWizard();
    await goToInstrumentStep("scn_a");
    const search = await screen.findByLabelText("权益国内搜索");
    fireEvent.change(search, { target: { value: "SHORT" } });
    fireEvent.click(await screen.findByRole("button", { name: /未同步历史基金/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));

    const warning = await screen.findByTestId("wizard-missing-history");
    expect(warning).toHaveTextContent("未同步历史基金（SHORT01）尚未同步历史数据");
    expect(warning).toHaveTextContent("一键同步缺失历史");
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

    // Classification is user-driven now: a symbol miss shows the directory hint.
    const equitySearch = screen.getByLabelText("权益国内搜索");
    fireEvent.change(equitySearch, { target: { value: "X9" } });
    expect(
      await screen.findByText(/未在本地资产目录中找到匹配标的/),
    ).toBeInTheDocument();
  });

  it("keeps one plan-wide owner per asset_key across class pickers", async () => {
    const shared = makeDirectoryAsset("159007", "深红利ETF");
    searchPool = [shared, makeDirectoryAsset("B1", "测试债券基金")];
    const consoleError = vi.spyOn(console, "error");

    renderWizard();
    await goToInstrumentStep("scn_conservative");

    // 权益 owns the shared asset first.
    const equitySearch = screen.getByLabelText("权益国内搜索");
    fireEvent.change(equitySearch, { target: { value: "159007" } });
    fireEvent.click(await screen.findByRole("button", { name: /深红利ETF/ }));

    // 债券 candidates no longer offer the asset owned by 权益.
    fireEvent.click(screen.getByRole("tab", { name: /债券/ }));
    const bondSearch = screen.getByLabelText("债券国内搜索");
    fireEvent.change(bondSearch, { target: { value: "159007" } });
    expect(
      await screen.findByText(/未在本地资产目录中找到匹配标的/),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /深红利ETF/ })).not.toBeInTheDocument();

    // Pick a distinct bond so group weights pass, then confirm.
    fireEvent.change(bondSearch, { target: { value: "B1" } });
    fireEvent.click(await screen.findByRole("button", { name: /测试债券基金/ }));
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/已选标的：/)).toBeInTheDocument());

    // The confirm table renders the shared asset exactly once and React never
    // reports a duplicate row key.
    const table = screen.getByRole("table", { name: "组合确认明细" });
    expect(within(table).getAllByText("159007")).toHaveLength(1);
    expect(
      consoleError.mock.calls.some((args) =>
        args.some(
          (a) =>
            typeof a === "string" &&
            a.includes("Encountered two children with the same key"),
        ),
      ),
    ).toBe(false);

    // The created plan carries the asset_key exactly once.
    fireEvent.click(screen.getByRole("button", { name: "创建计划" }));
    await waitFor(() => expect(createPlanWizard).toHaveBeenCalled());
    const req = createPlanWizard.mock.calls[0]![0] as {
      holdings: { asset_key: string }[];
    };
    expect(req.holdings.filter((h) => h.asset_key === shared.asset_key)).toHaveLength(1);
    consoleError.mockRestore();
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

    // Picking in the foreign sub-container classifies the asset as foreign.
    const foreignSearch = screen.getByLabelText("国外（占权益 30%）搜索");
    fireEvent.change(foreignSearch, { target: { value: "F1" } });
    fireEvent.click(await screen.findByRole("button", { name: /测试国外权益基金/ }));
    expect(await screen.findByLabelText("测试国外权益基金 F1")).toBeInTheDocument();
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
      "/plans/plan_new/overview?task_id=job_1",
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

  it("imports every quick-fire field once under StrictMode and submits the mapped payload", async () => {
    window.history.replaceState({}, "", "/plans/new?source=quick-fire");
    window.sessionStorage.setItem(QUICK_FIRE_TRANSFER_KEY, JSON.stringify({
      version: 1,
      engine_version: "quick_fire_v1",
      inputs: {
        base_currency: "CNY",
        current_age: 41,
        planned_fire_age: 49,
        end_age: 91,
        current_assets_minor: 543_210_00,
        annual_savings_minor: 123_400_00,
        annual_savings_growth_rate: 0.03,
        annual_spending_minor: 87_600_00,
        annual_retirement_income_minor: 24_000_00,
        annual_retirement_income_growth_rate: 0.01,
        inflation_rate: 0.025,
        terminal_wealth_floor_minor: 10_000_00,
        annual_return_rate: 0.071,
      },
    }));

    const first = renderWizard({ strict: true });
    expect(await screen.findByRole("status")).toHaveTextContent("已从 FIRE 快算带入现金流参数");
    expect(screen.getByLabelText("当前年龄")).toHaveValue(41);
    expect(screen.getByLabelText("退休年龄")).toHaveValue(49);
    expect(screen.getByLabelText("预计 FIRE 时长（年）")).toHaveValue(42);
    const importedMoney = screen.getAllByTestId("money-input");
    expect(importedMoney[0]).toHaveValue("543210");
    expect(importedMoney[1]).toHaveValue("87600");
    expect(importedMoney[2]).toHaveValue("123400");
    expect(importedMoney[3]).toHaveValue("24000");
    fireEvent.click(screen.getByText(/高级 FIRE 参数/));
    expect(screen.getByRole("textbox", { name: "固定通胀率" })).toHaveValue("2.5");
    expect(screen.getByRole("textbox", { name: "储蓄增长率" })).toHaveValue("3");
    expect(screen.getByRole("textbox", { name: "稳定收入年增长率" })).toHaveValue("1");
    expect(screen.getAllByTestId("money-input")[4]).toHaveValue("10,000.00");
    expect(window.sessionStorage.getItem(QUICK_FIRE_TRANSFER_KEY)).toBeNull();

    await goToConfirmStep();
    fireEvent.click(screen.getByRole("button", { name: "创建计划" }));
    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const payload = createPlanWizard.mock.calls[0]![0];
    expect(payload.parameters).toEqual(expect.objectContaining({
      current_age: 41,
      retirement_age: 49,
      end_age: 91,
      total_assets_minor: 543_210_00,
      annual_savings_minor: 123_400_00,
      annual_savings_growth_rate: 0.03,
      annual_spending_minor: 87_600_00,
      annual_retirement_income_minor: 24_000_00,
      annual_retirement_income_growth_rate: 0.01,
      inflation_mode: "fixed_real",
      fixed_inflation_rate: 0.025,
      terminal_wealth_floor_minor: 10_000_00,
    }));
    expect(JSON.stringify(payload)).not.toContain("0.071");
    first.unmount();

    renderWizard();
    expect(screen.queryByText("已从 FIRE 快算带入现金流参数；完整模拟的收益率将根据后续选择的资产和模拟假设生成。")).not.toBeInTheDocument();
    expect(screen.getByLabelText("当前年龄")).toHaveValue(35);
  });

  it("warns and uses defaults when the quick-fire transfer is malformed", async () => {
    window.history.replaceState({}, "", "/plans/new?source=quick-fire");
    window.sessionStorage.setItem(QUICK_FIRE_TRANSFER_KEY, "not-json");
    renderWizard();

    expect(await screen.findByRole("alert")).toHaveTextContent("FIRE 快算参数未能读取");
    expect(window.sessionStorage.getItem(QUICK_FIRE_TRANSFER_KEY)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
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

  it("fire duration: one editable input with preset chips", async () => {
    renderWizard();
    const durationInputs = screen.getAllByLabelText("预计 FIRE 时长（年）");
    expect(durationInputs).toHaveLength(1);
    const input = durationInputs[0]!;

    // Default 30 matches a preset chip, which shows as pressed.
    expect(screen.getByRole("button", { name: "30 年", pressed: true })).toBeInTheDocument();

    fireEvent.change(input, { target: { value: "37" } });
    expect(input).toHaveValue(37);
    // Custom value is not a preset, so no chip is pressed.
    expect(screen.queryByRole("button", { pressed: true })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "40 年" }));
    expect(screen.getByLabelText("预计 FIRE 时长（年）")).toHaveValue(40);
    expect(screen.getByRole("button", { name: "40 年", pressed: true })).toBeInTheDocument();
  });

  it("blocks leaving goal step when ages or duration are invalid", async () => {
    renderWizard();
    await selectScenario("scn_a");

    fireEvent.change(screen.getByLabelText("退休年龄"), { target: { value: "30" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("退休年龄不能小于当前年龄。")).toBeInTheDocument();
    expect(screen.queryByText(/按大类分标签页搜索并添加标的/)).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("退休年龄"), { target: { value: "40" } });
    fireEvent.change(screen.getByLabelText("预计 FIRE 时长（年）"), { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("预计 FIRE 时长至少为 1 年。")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("预计 FIRE 时长（年）"), { target: { value: "90" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(
      await screen.findByText("退休年龄加 FIRE 时长不能超过 120 岁。"),
    ).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("当前年龄"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("预计 FIRE 时长（年）"), { target: { value: "30" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("当前年龄需为大于 0 的整数。")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("当前年龄"), { target: { value: "35" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() =>
      expect(screen.getByText(/按大类分标签页搜索并添加标的/)).toBeInTheDocument(),
    );
  });

  it("blocks leaving goal step when base amounts are invalid", async () => {
    renderWizard();
    await selectScenario("scn_a");

    // Money inputs on the goal step in DOM order: 基准规模, 当前年支出, 年储蓄.
    const [totalAssets, annualSpending, annualSavings] = screen.getAllByTestId("money-input");

    fireEvent.change(totalAssets!, { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("基准规模需大于 0。")).toBeInTheDocument();

    fireEvent.change(totalAssets!, { target: { value: "4000000" } });
    fireEvent.change(annualSpending!, { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("当前年支出需大于 0。")).toBeInTheDocument();

    fireEvent.change(annualSpending!, { target: { value: "120000" } });
    fireEvent.change(annualSavings!, { target: { value: "-1" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("年储蓄不能为负数。")).toBeInTheDocument();

    fireEvent.change(annualSavings!, { target: { value: "100000" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() =>
      expect(screen.getByText(/按大类分标签页搜索并添加标的/)).toBeInTheDocument(),
    );
  });

  it("lets step content fill the card width and keeps a single panel border", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    // Step content must fill the card: no max-width cap on the step wrapper.
    expect(screen.getByTestId("wizard-holdings-step").className).not.toMatch(/max-w-/);

    // The tab panel draws the only border; the top-level picker inside is
    // borderless so panel width and content width stay in sync.
    const panel = screen.getByRole("tabpanel");
    expect(panel).toHaveClass("border");
    const picker = screen.getByRole("region", { name: "权益选标" });
    expect(picker).not.toHaveClass("border");
    expect(picker.className).not.toMatch(/max-w-/);

    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await screen.findByText(/已选标的：/);
    expect(screen.getByTestId("wizard-confirm-step").className).not.toMatch(/max-w-/);
  });

  it("shows visible labels for selected holding weight, amount and expected funds", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();

    const row = await screen.findByLabelText("测试权益基金 T1");
    expect(within(row).getByText("组内占比")).toBeInTheDocument();
    expect(within(row).getByText("已分配金额")).toBeInTheDocument();
    expect(within(row).getByText("预期资金")).toBeInTheDocument();
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
      holdings: { asset_key: string; asset_class: string; region: string }[];
    };
    const ids = body.holdings.map((h) => h.asset_key);
    expect(ids).toContain("CN|cn_exchange_fund|sh|F1");
    expect(ids).not.toContain("CN|cn_exchange_fund|sh|T1");
    const foreignHolding = body.holdings.find(
      (h) => h.asset_key === "CN|cn_exchange_fund|sh|F1",
    );
    expect(foreignHolding).toMatchObject({ asset_class: "equity", region: "foreign" });
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

  it("drops inactive fixed-inflation and withdrawal-rate validation", async () => {
    renderWizard();
    fireEvent.change(percentInputByLabel("固定通胀率")!, { target: { value: "25" } });
    expect(screen.getByTestId("wizard-advanced-errors")).toHaveTextContent("固定通胀率需在 -2% 到 20% 之间。");

    fireEvent.change(screen.getByLabelText("通胀模式"), { target: { value: "random_ar1" } });
    expect(percentInputByLabel("固定通胀率")).not.toBeInTheDocument();
    expect(screen.queryByText("固定通胀率需在 -2% 到 20% 之间。")).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: /确认固定通胀率超过 15%/ })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("提取策略"), { target: { value: "guardrail" } });
    expect(percentInputByLabel("提取率")).not.toBeInTheDocument();
    expect(percentInputByLabel("护栏下限比例")).toBeInTheDocument();
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
