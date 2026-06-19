// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const useJobStatusMock = vi.hoisted(() => vi.fn());
const getInstrumentDetailMock = vi.hoisted(() => vi.fn());
const getFetchStatusMock = vi.hoisted(() => vi.fn());
const deleteInstrumentMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

let jobStatusCallbacks: {
  onComplete?: () => void;
  onFailed?: (message: string) => void;
  onCanceled?: () => void;
} = {};

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
};

function pendingDetail() {
  return {
    instrument: { ...baseInstrument, status: "pending_fetch" },
    annual_returns: [],
    simulation_window: {},
    historical_snapshots: [],
    referencing_plans: [],
  };
}

function failedDetail() {
  return {
    instrument: { ...baseInstrument, status: "fetch_failed" },
    annual_returns: [],
    simulation_window: {},
    historical_snapshots: [],
    referencing_plans: [],
  };
}

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
  getFetchStatus: (...args: unknown[]) => getFetchStatusMock(...args),
  retryFetch: vi.fn(),
  refreshInstrument: vi.fn(),
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

vi.mock("@/hooks/useJobStatus", () => ({
  useJobStatus: (jobId: string | null, options?: typeof jobStatusCallbacks) => {
    jobStatusCallbacks = options ?? {};
    return useJobStatusMock(jobId, options);
  },
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

describe("AssetDetailPage job terminal states", () => {
  beforeEach(() => {
    useJobStatusMock.mockReset();
    getInstrumentDetailMock.mockReset();
    getFetchStatusMock.mockReset();
    jobStatusCallbacks = {};

    getInstrumentDetailMock.mockResolvedValue(failedDetail());
    getFetchStatusMock.mockResolvedValue({
      job_id: "job_test",
      instrument_status: "fetch_failed",
    });
    useJobStatusMock.mockImplementation((jobId) => {
      if (!jobId) {
        return { job: null, progress: 0, error: null, loading: false };
      }
      return {
        job: {
          id: "job_test",
          status: "failed",
          error_message: "fetch_failed",
          progress_current: 0,
          progress_total: 1,
          phase: "",
        },
        progress: 0,
        error: "fetch_failed",
        loading: false,
      };
    });
  });

  it("shows retry button when instrument is fetch_failed", async () => {
    renderPage();
    expect(await screen.findByText("历史数据抓取失败")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试抓取" })).toBeInTheDocument();
  });

  it("shows fetch status query error", async () => {
    getFetchStatusMock.mockRejectedValue(new Error("网络错误"));
    renderPage();
    expect(await screen.findByText("抓取状态查询失败")).toBeInTheDocument();
    expect(screen.getByText("网络错误")).toBeInTheDocument();
  });

  it("refetches and shows failure banner after pending_fetch job fails", async () => {
    getInstrumentDetailMock
      .mockResolvedValueOnce(pendingDetail())
      .mockResolvedValue(failedDetail());
    getFetchStatusMock.mockResolvedValue({
      job_id: "job_pending",
      instrument_status: "pending_fetch",
    });
    useJobStatusMock.mockImplementation((jobId, options) => {
      jobStatusCallbacks = options ?? {};
      if (jobId === "job_pending") {
        return {
          job: { id: "job_pending", status: "running", progress_current: 0, progress_total: 1, phase: "fetching" },
          progress: 0.1,
          error: null,
          loading: false,
        };
      }
      return { job: null, progress: 0, error: null, loading: false };
    });

    renderPage();
    expect(await screen.findByText("历史数据抓取中")).toBeInTheDocument();

    await act(async () => {
      jobStatusCallbacks.onFailed?.("provider_timeout");
    });

    await waitFor(() => expect(getInstrumentDetailMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("历史数据抓取失败")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "重试抓取" })).toBeInTheDocument();
    expect(screen.queryByText("历史数据抓取中")).not.toBeInTheDocument();
  });

  it("refetches and shows annual returns after pending_fetch job succeeds", async () => {
    getInstrumentDetailMock
      .mockResolvedValueOnce(pendingDetail())
      .mockResolvedValue(activeDetail());
    getFetchStatusMock.mockResolvedValue({
      job_id: "job_pending",
      instrument_status: "pending_fetch",
    });
    useJobStatusMock.mockImplementation((jobId, options) => {
      jobStatusCallbacks = options ?? {};
      if (jobId === "job_pending") {
        return {
          job: { id: "job_pending", status: "running", progress_current: 0, progress_total: 1, phase: "fetching" },
          progress: 0.5,
          error: null,
          loading: false,
        };
      }
      return { job: null, progress: 0, error: null, loading: false };
    });

    renderPage();
    expect(await screen.findByText("历史数据抓取中")).toBeInTheDocument();

    await act(async () => {
      jobStatusCallbacks.onComplete?.();
    });

    await waitFor(() => expect(getInstrumentDetailMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("年度收益")).toBeInTheDocument();
    expect(screen.getByText("模拟窗口预览（完整自然年度）")).toBeInTheDocument();
    expect(screen.getByText("2023-01-03 ~ 2023-12-29")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "刷新 AKShare 数据" })).toBeInTheDocument();
    expect(screen.queryByText("历史数据抓取中")).not.toBeInTheDocument();
  });

  it("opens centered fetch status dialog", async () => {
    getInstrumentDetailMock.mockResolvedValue(pendingDetail());
    getFetchStatusMock.mockResolvedValue({
      job_id: "job_pending",
      instrument_status: "pending_fetch",
    });
    useJobStatusMock.mockReturnValue({
      job: { id: "job_pending", status: "running", progress_current: 1, progress_total: 2, phase: "fetching" },
      progress: 0.5,
      error: null,
      loading: false,
    });

    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "查看抓取状态" }));
    expect(screen.getByTestId("dialog")).toBeInTheDocument();
    expect(screen.getByTestId("fetch-status-job-status")).toHaveTextContent("running");
    fireEvent.click(screen.getByRole("button", { name: "关闭" }));
    expect(screen.queryByTestId("dialog")).not.toBeInTheDocument();
  });

  it("shows canceled notice instead of permanent fetching banner", async () => {
    getInstrumentDetailMock
      .mockResolvedValueOnce(pendingDetail())
      .mockResolvedValue(failedDetail());
    getFetchStatusMock.mockResolvedValue({
      job_id: "job_pending",
      instrument_status: "fetch_failed",
      error_code: "fetch_canceled",
    });
    useJobStatusMock.mockImplementation((jobId, options) => {
      jobStatusCallbacks = options ?? {};
      if (jobId === "job_pending") {
        return {
          job: { id: "job_pending", status: "canceled", progress_current: 0, progress_total: 1, phase: "" },
          progress: 0,
          error: null,
          loading: false,
        };
      }
      return { job: null, progress: 0, error: null, loading: false };
    });

    renderPage();
    expect(await screen.findByText("历史数据抓取中")).toBeInTheDocument();

    await act(async () => {
      jobStatusCallbacks.onCanceled?.();
    });

    await waitFor(() => expect(getInstrumentDetailMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByText("历史数据抓取已取消")).toBeInTheDocument();
    expect(screen.queryByText("历史数据抓取中")).not.toBeInTheDocument();
  });
});

describe("AssetDetailPage historical snapshots", () => {
  beforeEach(() => {
    getInstrumentDetailMock.mockReset();
    getFetchStatusMock.mockReset();
    useJobStatusMock.mockReset();
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
    useJobStatusMock.mockReturnValue({ job: null, progress: 0, error: null, loading: false });
  });

  it("shows historical snapshot metrics and warnings", async () => {
    renderPage();
    expect(await screen.findByText("历史计划快照")).toBeInTheDocument();
    expect(screen.getByText(/72 月收益观测/)).toBeInTheDocument();
    expect(screen.getByText(/monthly_log_return_v1/)).toBeInTheDocument();
    expect(screen.getByText("完整年度样本较少，估计不稳定")).toBeInTheDocument();
  });
});

describe("AssetDetailPage layout and return curve", () => {
  beforeEach(() => {
    getInstrumentDetailMock.mockReset();
    getFetchStatusMock.mockReset();
    useJobStatusMock.mockReset();
    getInstrumentDetailMock.mockResolvedValue(activeDetail());
    useJobStatusMock.mockReturnValue({ job: null, progress: 0, error: null, loading: false });
  });

  it("shows top-right action buttons and return curve with range tabs", async () => {
    renderPage();
    expect(await screen.findByRole("button", { name: "刷新 AKShare 数据" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "强制刷新" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();

    expect(screen.getByText("收益曲线")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "近3月" })).toBeInTheDocument();
    expect(await screen.findByTestId("return-series-chart")).toBeInTheDocument();
  });

  it("switches return curve range when a tab is clicked", async () => {
    const { getReturnSeries } = await import("@/lib/api/instruments");
    renderPage();
    await screen.findByTestId("return-series-chart");
    fireEvent.click(screen.getByRole("tab", { name: "近1年" }));
    await waitFor(() =>
      expect(getReturnSeries).toHaveBeenCalledWith("ins_test", "1y"),
    );
  });
});

describe("AssetDetailPage delete", () => {
  beforeEach(() => {
    deleteInstrumentMock.mockReset();
    routerPushMock.mockReset();
    getInstrumentDetailMock.mockReset();
    getFetchStatusMock.mockReset();
    useJobStatusMock.mockReset();
    deleteInstrumentMock.mockResolvedValue({ deleted: true });
    getInstrumentDetailMock.mockResolvedValue(activeDetail());
    useJobStatusMock.mockReturnValue({ job: null, progress: 0, error: null, loading: false });
  });

  it("invalidates instruments cache and navigates home after delete", async () => {
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
    expect(routerPushMock).toHaveBeenCalledWith("/assets");

    invalidateSpy.mockRestore();
    removeSpy.mockRestore();
  });
});
