// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

const listAssumptionProfiles = vi.hoisted(() => vi.fn());
const getAssumptionProfile = vi.hoisted(() => vi.fn());
const saveAssumptionProfile = vi.hoisted(() => vi.fn());
const activateAssumptionProfile = vi.hoisted(() => vi.fn());
const setAssumptionPreferences = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/assumptions", () => ({
  listAssumptionProfiles: (...a: unknown[]) => listAssumptionProfiles(...a),
  getAssumptionProfile: (...a: unknown[]) => getAssumptionProfile(...a),
  saveAssumptionProfile: (...a: unknown[]) => saveAssumptionProfile(...a),
  activateAssumptionProfile: (...a: unknown[]) => activateAssumptionProfile(...a),
  setAssumptionPreferences: (...a: unknown[]) => setAssumptionPreferences(...a),
}));

import AssumptionsPage from "./page";

const systemProfile = {
  id: "system_cma_v1",
  version: 1,
  owner_scope: "system" as const,
  name: "系统默认（CMA v1）",
  status: "active" as const,
  prior_strength_years: 20,
  correlation_strength_months: 36,
  student_t_df: 7,
  scenarios: {
    conservative: { return_shift_log: -0.015, return_shift_log_fx: 0, volatility_multiplier: 1.15 },
    baseline: { return_shift_log: 0, return_shift_log_fx: 0, volatility_multiplier: 1 },
    optimistic: { return_shift_log: 0.015, return_shift_log_fx: 0, volatility_multiplier: 0.9 },
  },
  return_priors: [
    {
      asset_class: "equity",
      region: "domestic",
      valuation_currency: "CNY",
      annual_geometric_return: 0.06,
      annual_volatility_floor: 0.12,
      annual_volatility_ceiling: 0.35,
      source_url: "https://example.com",
      published_at: "2026-06-20",
      reviewed_at: "2026-06-20",
    },
  ],
  correlation_priors: [
    { factor_a: "asset:equity:domestic", factor_b: "asset:bond:domestic", rho: 0.15 },
  ],
};

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AssumptionsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  listAssumptionProfiles.mockResolvedValue({
    profiles: [
      {
        id: "system_cma_v1",
        version: 1,
        owner_scope: "system",
        name: "系统默认（CMA v1）",
        status: "active",
        content_hash: "abc",
        created_at: 0,
        updated_at: 0,
      },
    ],
    preferences: {
      default_profile_id: "system_cma_v1",
      default_profile_version: 1,
      default_scenario: "baseline",
    },
    scenarios: ["conservative", "baseline", "optimistic"],
  });
  getAssumptionProfile.mockResolvedValue({ profile: systemProfile });
  saveAssumptionProfile.mockResolvedValue({
    profile: { ...systemProfile, id: "user_cma_x", owner_scope: "user", status: "draft" },
  });
  activateAssumptionProfile.mockResolvedValue({ activated: true });
  setAssumptionPreferences.mockResolvedValue({
    preferences: {
      default_profile_id: "system_cma_v1",
      default_profile_version: 1,
      default_scenario: "conservative",
    },
  });
});

describe("AssumptionsPage", () => {
  it("shows the system profile, its priors and the correlation matrix", async () => {
    renderPage();
    expect(await screen.findByRole("heading", { name: "模拟假设" })).toBeInTheDocument();
    // system profile listed
    expect(await screen.findByText("系统默认（CMA v1）")).toBeInTheDocument();
    // detail: return prior + correlation matrix rendered
    await waitFor(() => expect(getAssumptionProfile).toHaveBeenCalled());
    expect(await screen.findByText("收益先验（费用后·基准币种·名义几何）")).toBeInTheDocument();
    expect(await screen.findByText("相关性先验矩阵")).toBeInTheDocument();
  });

  it("copies the system profile into a custom draft", async () => {
    renderPage();
    const copyBtn = await screen.findByRole("button", { name: "复制为自定义" });
    fireEvent.click(copyBtn);
    await waitFor(() => expect(saveAssumptionProfile).toHaveBeenCalled());
    const arg = saveAssumptionProfile.mock.calls[0][0];
    expect(arg.profile.owner_scope).toBe("user");
    expect(arg.profile.status).toBe("draft");
    expect(arg.profile.id).not.toBe("system_cma_v1");
  });

  it("saves the global default selection", async () => {
    renderPage();
    const saveBtn = await screen.findByRole("button", { name: "保存默认" });
    const scenarioSelect = (await screen.findByTestId("default-scenario-select")) as HTMLSelectElement;
    fireEvent.change(scenarioSelect, { target: { value: "conservative" } });
    fireEvent.click(saveBtn);
    await waitFor(() => expect(setAssumptionPreferences).toHaveBeenCalled());
    expect(setAssumptionPreferences.mock.calls[0][0].default_scenario).toBe("conservative");
  });
});
