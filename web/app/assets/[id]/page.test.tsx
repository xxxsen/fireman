// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const getInstrumentDetailMock = vi.hoisted(() => vi.fn());
const deleteInstrumentMock = vi.hoisted(() => vi.fn());
const updateClassificationMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

const baseInstrument = {
  id: "ins_test",
  code: "sh510300",
  name: "测试ETF",
  market: "CN",
  instrument_type: "cn_exchange_fund",
  asset_class: "equity",
  region: "domestic",
  fee_treatment: "embedded",
  expense_ratio_status: "unavailable",
  quality_status: "available",
  data_source_name: "akshare",
  point_type: "close",
  is_system: false,
  asset_key: "cn:cn_exchange_fund:sh:510300",
  updated_at: 1750000000000,
};

function activeDetail() {
  return {
    instrument: { ...baseInstrument, status: "active" },
    annual_returns: [
      {
        year: 2023,
        annual_return: 0.12,
        is_partial: false,
        in_simulation: true,
        start_date: "2023-01-03",
        end_date: "2023-12-29",
      },
    ],
    simulation_window: {
      selected_years: [2023],
      excluded_years: [],
      complete_year_count: 1,
      daily_observation_count: 242,
      monthly_return_count: 12,
      historical_cagr: 0.12,
      annual_volatility: 0.18,
      max_drawdown: 0.1,
      history_depth: "one_year",
      simulation_eligible: true,
      quality_status: "available",
      inclusion_date: "2026-06-14",
      fee_treatment: "embedded",
      expense_ratio_status: "unavailable",
    },
    trailing_returns: {
      as_of_date: "2026-06-12",
      point_type: "adjusted_close",
      source_name: "akshare",
      periods: {
        "1m": { status: "available", cumulative_return: 0.02, end_date: "2026-06-12", target_start_date: "2026-05-12", start_date: "2026-05-12", actual_days: 31, annualized_return: null },
        "3m": { status: "available", cumulative_return: 0.04, end_date: "2026-06-12", target_start_date: "2026-03-12", start_date: "2026-03-12", actual_days: 92, annualized_return: null },
        "6m": { status: "available", cumulative_return: 0.06, end_date: "2026-06-12", target_start_date: "2025-12-12", start_date: "2025-12-12", actual_days: 182, annualized_return: null },
        "1y": { status: "available", cumulative_return: 0.12, end_date: "2026-06-12", target_start_date: "2025-06-12", start_date: "2025-06-12", actual_days: 365, annualized_return: null },
        "3y": { status: "insufficient_history", cumulative_return: null, end_date: "2026-06-12", target_start_date: "2023-06-12", start_date: null, actual_days: null, annualized_return: null },
        "5y": { status: "insufficient_history", cumulative_return: null, end_date: "2026-06-12", target_start_date: "2021-06-12", start_date: null, actual_days: null, annualized_return: null },
      },
    },
    historical_snapshots: [],
    referencing_plans: [],
  };
}

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "ins_test" }),
  useRouter: () => ({ push: routerPushMock }),
}));

vi.mock("@/lib/api/instruments", () => ({
  getInstrumentDetail: (...args: unknown[]) => getInstrumentDetailMock(...args),
  updateInstrumentClassification: (...args: unknown[]) => updateClassificationMock(...args),
  deleteInstrument: (...args: unknown[]) => deleteInstrumentMock(...args),
  getReturnSeries: vi.fn().mockResolvedValue({
    as_of_date: "2026-06-12",
    range: "3m",
    point_type: "nav",
    source_name: "akshare",
    status: "available",
    points: [
      { date: "2026-03-12", value: 1.0, cumulative_return: 0 },
      { date: "2026-06-12", value: 1.08, cumulative_return: 0.08 },
    ],
  }),
}));

vi.mock("@/components/charts/ReturnSeriesChart", () => ({
  ReturnSeriesChart: () => <div data-testid="return-series-chart" />,
}));

import AssetDetailPage from "./page";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AssetDetailPage />
    </QueryClientProvider>,
  );
}

describe("AssetDetailPage layout (td/078 task-based data)", () => {
  beforeEach(() => {
    getInstrumentDetailMock.mockReset();
    getInstrumentDetailMock.mockResolvedValue(activeDetail());
  });

  it("links to the global market asset instead of offering local refresh", async () => {
    renderPage();
    expect(await screen.findByTestId("market-asset-link")).toHaveAttribute(
      "href",
      `/assets/market/${encodeURIComponent("cn:cn_exchange_fund:sh:510300")}`,
    );
    // The legacy synchronous refresh/retry controls are gone.
    expect(screen.queryByRole("button", { name: "刷新" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "重试抓取" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "强制刷新" })).not.toBeInTheDocument();
    // The info alert explains where history refreshes happen now.
    expect(screen.getByText(/历史数据由全局市场资产统一同步/)).toBeInTheDocument();
  });

  it("hides the market asset link for instruments without an asset_key", async () => {
    getInstrumentDetailMock.mockResolvedValue({
      ...activeDetail(),
      instrument: { ...baseInstrument, status: "active", asset_key: "" },
    });
    renderPage();
    await screen.findByText("基础信息");
    expect(screen.queryByTestId("market-asset-link")).not.toBeInTheDocument();
  });

  it("renders simulation window, annual returns and the return curve", async () => {
    renderPage();
    expect(await screen.findByText("年度收益")).toBeInTheDocument();
    expect(screen.getByText("模拟窗口预览（完整自然年度）")).toBeInTheDocument();
    expect(screen.getByText("2023-01-03 ~ 2023-12-29")).toBeInTheDocument();
    expect(screen.getByText("收益曲线")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "近3月" })).toBeInTheDocument();
    expect(await screen.findByTestId("return-series-chart")).toBeInTheDocument();
  });

  it("switches return curve range when a range button is clicked", async () => {
    const { getReturnSeries } = await import("@/lib/api/instruments");
    renderPage();
    await screen.findByTestId("return-series-chart");
    fireEvent.click(screen.getByRole("button", { name: "近1年" }));
    await waitFor(() => expect(getReturnSeries).toHaveBeenCalledWith("ins_test", "1y"));
  });
});

describe("AssetDetailPage historical snapshots", () => {
  beforeEach(() => {
    getInstrumentDetailMock.mockReset();
    getInstrumentDetailMock.mockResolvedValue({
      ...activeDetail(),
      historical_snapshots: [
        {
          id: "snap_hist_1",
          plan_id: "plan_1",
          inclusion_date: "2025-06-01",
          complete_year_count: 6,
          monthly_return_count: 72,
          history_depth: "five_plus_years",
          metrics_version: "monthly_log_return_v1",
          warnings: ["完整年度样本较少，估计不稳定"],
          created_at: Date.parse("2025-06-01T08:00:00.000Z"),
        },
      ],
    });
  });

  it("shows historical snapshot metrics and warnings", async () => {
    renderPage();
    expect(await screen.findByText("历史计划快照")).toBeInTheDocument();
    expect(screen.getByText(/72 月收益观测/)).toBeInTheDocument();
    expect(screen.getByText(/monthly_log_return_v1/)).toBeInTheDocument();
    expect(screen.getByText("完整年度样本较少，估计不稳定")).toBeInTheDocument();
  });
});

describe("AssetDetailPage classification editing", () => {
  beforeEach(() => {
    getInstrumentDetailMock.mockReset();
    updateClassificationMock.mockReset();
    getInstrumentDetailMock.mockResolvedValue(activeDetail());
  });

  it("edits classification and shows the frozen-plan notice", async () => {
    updateClassificationMock.mockResolvedValue({
      instrument: { ...baseInstrument, status: "active", asset_class: "bond", region: "foreign" },
      referencing_plan_count: 2,
      classification_sync_scope: "future_only",
    });
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "编辑大类和地区" }));
    expect(screen.getByTestId("classification-editor")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("资产大类"), { target: { value: "bond" } });
    fireEvent.change(screen.getByLabelText("资产地区"), { target: { value: "foreign" } });
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() =>
      expect(updateClassificationMock).toHaveBeenCalledWith("ins_test", {
        asset_class: "bond",
        region: "foreign",
        expected_updated_at: 1750000000000,
      }),
    );
    expect(await screen.findByText(/已关联 2 个计划保持原配置/)).toBeInTheDocument();
  });

  it("keeps input and offers reload on version conflict", async () => {
    const { ApiError } = await import("@/lib/api/client");
    updateClassificationMock.mockRejectedValue(
      new ApiError("instrument_version_conflict", "conflict"),
    );
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "编辑大类和地区" }));
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    expect(await screen.findByText(/请刷新后确认分类再保存/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重新加载" })).toBeInTheDocument();
    // Editor stays open so the user keeps their selections.
    expect(screen.getByTestId("classification-editor")).toBeInTheDocument();
  });

  it("hides the edit entry for system instruments", async () => {
    getInstrumentDetailMock.mockResolvedValue({
      ...activeDetail(),
      instrument: { ...baseInstrument, status: "active", is_system: true },
    });
    renderPage();
    await screen.findByText("基础信息");
    expect(screen.queryByRole("button", { name: "编辑大类和地区" })).not.toBeInTheDocument();
  });

  it("opens the editor and focuses the asset-class select from the top entry", async () => {
    renderPage();
    const entry = await screen.findByRole("button", { name: "编辑大类和地区" });
    fireEvent.click(entry);
    const select = screen.getByLabelText("资产大类");
    expect(screen.getByTestId("classification-editor")).toBeInTheDocument();
    await waitFor(() => expect(select).toHaveFocus());
  });
});

describe("AssetDetailPage delete", () => {
  beforeEach(() => {
    deleteInstrumentMock.mockReset();
    routerPushMock.mockReset();
    getInstrumentDetailMock.mockReset();
    deleteInstrumentMock.mockResolvedValue({ deleted: true });
    getInstrumentDetailMock.mockResolvedValue(activeDetail());
  });

  it("invalidates instruments cache and navigates to the library after delete", async () => {
    const invalidateSpy = vi.spyOn(QueryClient.prototype, "invalidateQueries");
    const removeSpy = vi.spyOn(QueryClient.prototype, "removeQueries");

    renderPage();
    expect(await screen.findByRole("button", { name: "删除" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    fireEvent.click(screen.getByTestId("confirm-dialog-confirm"));

    await waitFor(() => expect(deleteInstrumentMock).toHaveBeenCalledWith("ins_test"));
    await waitFor(() =>
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["instruments"] }),
    );
    expect(removeSpy).toHaveBeenCalledWith({ queryKey: ["instrument-detail", "ins_test"] });
    expect(routerPushMock).toHaveBeenCalledWith("/assets/library");

    invalidateSpy.mockRestore();
    removeSpy.mockRestore();
  });

  it("disables delete while the instrument is referenced by plans", async () => {
    getInstrumentDetailMock.mockResolvedValue({
      ...activeDetail(),
      referencing_plans: [
        { plan_id: "plan_1", plan_name: "养老计划", snapshot_inclusion_date: "2026-01-01" },
      ],
    });
    renderPage();
    expect(await screen.findByRole("button", { name: "删除" })).toBeDisabled();
  });
});
