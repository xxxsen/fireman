// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";
import NewPlanWizardPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

const createPlanWizard = vi.fn();
const createSimulation = vi.fn();

let wizardJobCallbacks: {
  onComplete?: () => void;
  onFailed?: (message: string) => void;
  onCanceled?: () => void;
} = {};

vi.mock("@/lib/api/plans", () => ({
  createPlanWizard: (...args: unknown[]) => createPlanWizard(...args),
  createPlan: vi.fn(),
  updateParameters: vi.fn(),
}));

vi.mock("@/lib/api/holdings", () => ({ updateHoldings: vi.fn() }));
vi.mock("@/lib/api/simulations", () => ({
  createSimulation: (...args: unknown[]) => createSimulation(...args),
}));
vi.mock("@/hooks/useJobStatus", () => ({
  useJobStatus: (jobId: string | null, options?: typeof wizardJobCallbacks) => {
    wizardJobCallbacks = options ?? {};
    if (!jobId) {
      return { job: null, progress: 0, error: null };
    }
    return { job: { status: "running", progress_current: 10, progress_total: 100 }, progress: 0.1, error: null };
  },
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
  listInstruments: () =>
    Promise.resolve({
      instruments: [
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
          status: "active",
          is_system: false,
        },
      ],
    }),
}));

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

describe("NewPlanWizardPage", () => {
  beforeEach(() => {
    createPlanWizard.mockReset();
    createSimulation.mockReset();
    createPlanWizard.mockResolvedValue({ id: "plan_new", config_version: 1 });
    createSimulation.mockResolvedValue({ job_id: "job_1", run_id: "run_1", status: "queued" });
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
    expect(screen.getByText("未找到匹配的权益标的。")).toBeInTheDocument();
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
    expect(await screen.findByText("¥1,000,000.00")).toBeInTheDocument();
  });

  it("calls wizard once with region_targets on finish", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/10,000/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "创建并启动模拟" }));
    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    const body = createPlanWizard.mock.calls[0]![0] as {
      region_targets: unknown[];
      apply_unallocated_to_cash: boolean;
    };
    expect(body.region_targets).toHaveLength(6);
    expect(createSimulation).toHaveBeenCalledWith("plan_new", { runs: 10000 });
  });

  it("retries simulation without recreating plan after first failure", async () => {
    renderWizard();
    await goToInstrumentStep("scn_a");
    await selectEquityInstrument();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/10,000/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "创建并启动模拟" }));

    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(createSimulation).toHaveBeenCalledTimes(1));

    await act(async () => {
      wizardJobCallbacks.onFailed?.("首次模拟失败");
    });

    expect(await screen.findByText("首次模拟失败")).toBeInTheDocument();
    createSimulation.mockResolvedValue({ job_id: "job_retry", run_id: "run_retry", status: "queued" });
    fireEvent.click(screen.getByRole("button", { name: "重新启动模拟" }));

    await waitFor(() => expect(createSimulation).toHaveBeenCalledTimes(2));
    expect(createPlanWizard).toHaveBeenCalledTimes(1);
    expect(createSimulation).toHaveBeenLastCalledWith("plan_new", { runs: 10000 });
  });
});
