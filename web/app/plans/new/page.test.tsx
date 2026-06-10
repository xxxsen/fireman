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

vi.mock("@/lib/api/allocation", () => ({
  listScenarios: () =>
    Promise.resolve({
      scenarios: [
        {
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
        },
      ],
    }),
}));

vi.mock("@/lib/api/instruments", () => ({
  listInstruments: () =>
    Promise.resolve({
      instruments: [
        {
          id: "ins_1",
          code: "T1",
          name: "测试基金",
          market: "CN",
          instrument_type: "fund",
          asset_class: "equity",
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

describe("NewPlanWizardPage", () => {
  beforeEach(() => {
    createPlanWizard.mockReset();
    createSimulation.mockReset();
    createPlanWizard.mockResolvedValue({ id: "plan_new", config_version: 1 });
    createSimulation.mockResolvedValue({ job_id: "job_1", run_id: "run_1", status: "queued" });
  });

  it("does not call create until final step", async () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(createPlanWizard).not.toHaveBeenCalled());
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "scn_a" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(createPlanWizard).not.toHaveBeenCalled());
  });

  it("calls wizard once with 10000 runs on finish", async () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByRole("option", { name: /测试场景/ })).toBeInTheDocument());
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "scn_a" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/测试基金/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.change(screen.getByTestId("percent-input"), { target: { value: "100" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/10,000/)).toBeInTheDocument());
    const gapBoxes = screen.getAllByRole("checkbox");
    fireEvent.click(gapBoxes[gapBoxes.length - 1]!);
    fireEvent.click(screen.getByRole("button", { name: "创建并启动模拟" }));
    await waitFor(() => expect(createPlanWizard).toHaveBeenCalledTimes(1));
    expect(createSimulation).toHaveBeenCalledWith("plan_new", { runs: 10000 });
  });

  it("retries simulation without recreating plan after first failure", async () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByRole("option", { name: /测试场景/ })).toBeInTheDocument());
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "scn_a" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/测试基金/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.change(screen.getByTestId("percent-input"), { target: { value: "100" } });
    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    await waitFor(() => expect(screen.getByText(/10,000/)).toBeInTheDocument());
    const gapBoxes = screen.getAllByRole("checkbox");
    fireEvent.click(gapBoxes[gapBoxes.length - 1]!);
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
