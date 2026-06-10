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
      historical_cagr: 0.12,
      annual_volatility: 0.18,
      max_drawdown: -0.1,
      observation_count: 242,
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
    expect(screen.getByText("模拟窗口预览（最近完整年度）")).toBeInTheDocument();
    expect(screen.getByText("2023-01-03 ~ 2023-12-29")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "刷新 AKShare 数据" })).toBeInTheDocument();
    expect(screen.queryByText("历史数据抓取中")).not.toBeInTheDocument();
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
    window.confirm = vi.fn(() => true);

    renderPage();
    expect(await screen.findByRole("button", { name: "删除" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "删除" }));

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
