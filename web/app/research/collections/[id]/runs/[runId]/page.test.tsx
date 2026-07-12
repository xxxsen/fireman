import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchRunDetail, ResearchRunPoint } from "@/lib/api/research";
import ResearchRunDetailPage from "./page";

const getRunMock = vi.hoisted(() => vi.fn());
const getRunPointsMock = vi.hoisted(() => vi.fn());
const getCollectionMock = vi.hoisted(() => vi.fn());
const listRunsMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "rc_1", runId: "rbr_1" }),
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  getRun: (...args: unknown[]) => getRunMock(...args),
  getRunPoints: (...args: unknown[]) => getRunPointsMock(...args),
  getCollection: (...args: unknown[]) => getCollectionMock(...args),
  listRuns: (...args: unknown[]) => listRunsMock(...args),
}));

vi.mock("echarts-for-react", () => ({
  default: () => <div data-testid="echarts" />,
}));

function points(): ResearchRunPoint[] {
  const out: ResearchRunPoint[] = [];
  for (let i = 0; i < 30; i++) {
    const date = `2024-01-${String(i + 1).padStart(2, "0")}`;
    const nav = 1 + i * 0.001;
    out.push({
      date,
      nav,
      cumulative_return: nav - 1,
      period_return: 0.001,
      drawdown: 0,
    });
  }
  return out;
}

function runDetail(overrides: Partial<ResearchRunDetail> = {}): ResearchRunDetail {
  return {
    id: "rbr_1",
    collection_id: "rc_1",
    job_id: "job_1",
    input_hash: "a".repeat(64),
    source_hash: "b".repeat(64),
    engine_version: "research_backtest_v1",
    base_currency: "CNY",
    rebalance_policy: "monthly",
    window_start: "2024-01-01",
    window_end: "2024-01-30",
    status: "succeeded",
    created_at: 1750000000000,
    completed_at: 1750000050000,
    summary: {
      cumulative_return: 0.029,
      cagr: 0.35,
      annual_volatility: 0.001,
      max_drawdown: -0.01,
      sharpe: 1.2,
      calmar: 3.5,
      best_year: { year: 2024, return: 0.029 },
      worst_year: { year: 2024, return: 0.029 },
      positive_month_ratio: 1,
      current_drawdown_days: 0,
      max_drawdown_duration_days: 3,
      effective_return_days: 29,
      risk_free_rate: 0.02,
      contributions: [
        {
          asset_key: "CN|a",
          name: "资产A",
          target_weight: 1,
          end_weight: 1,
          cumulative_contribution: 0.029,
          risk_contribution: 1,
          drawdown_contribution: -0.01,
        },
      ],
      correlations: { asset_keys: ["CN|a"], matrix: [[1]] },
    },
    years: [
      {
        run_id: "rbr_1",
        year: 2024,
        annual_return: 0.029,
        volatility: 0.001,
        max_drawdown: -0.01,
        start_nav: 1,
        end_nav: 1.029,
        is_partial: true,
      },
    ],
    months: [{ run_id: "rbr_1", year: 2024, month: 1, monthly_return: 0.029 }],
    input_snapshot: {
      assets: [
        {
          asset_key: "CN|a",
          name: "资产A",
          adjust_policy: "hfq",
          point_type: "adjusted_close",
          source_name: "eastmoney",
          points_hash: "c".repeat(16),
        },
      ],
    },
    data_quality: {
      common_start_policy: "max_asset_start",
      common_end_policy: "min_asset_end",
      forward_fill_days_max: 7,
      common_start: "2024-01-01",
      common_end: "2024-01-30",
      window_start: "2024-01-01",
      window_end: "2024-01-30",
      assets: [],
      fx: [],
    },
    ...overrides,
  } as ResearchRunDetail;
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ResearchRunDetailPage />
    </QueryClientProvider>,
  );
}

describe("ResearchRunDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getCollectionMock.mockResolvedValue({ id: "rc_1", name: "测试集合", items: [], tags: [] });
    getRunPointsMock.mockResolvedValue({ points: points(), total: 30 });
    listRunsMock.mockResolvedValue({ runs: [] });
  });

  it("shows a progress panel and polls while the run is active", async () => {
    getRunMock
      .mockResolvedValueOnce(
        runDetail({
          status: "running",
          summary: undefined,
          job: {
            status: "running",
            phase: "computing",
            progress_current: 2,
            progress_total: 5,
          },
        }),
      )
      .mockResolvedValue(runDetail());
    renderPage();
    expect(await screen.findByText(/回测计算中/)).toBeInTheDocument();
    expect(screen.getByText(/2\/5/)).toBeInTheDocument();
    // The query polls every 2s while active; the second response flips to succeeded.
    await waitFor(
      () => expect(screen.getByTestId("metric-cards")).toBeInTheDocument(),
      { timeout: 5000 },
    );
    expect(getRunMock.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("renders metric cards, charts, tables and export entries when succeeded", async () => {
    getRunMock.mockResolvedValue(runDetail());
    renderPage();
    expect(await screen.findByTestId("metric-cards")).toBeInTheDocument();
    expect(screen.getByText("累计收益")).toBeInTheDocument();
    expect(screen.getByText("35%")).toBeInTheDocument();
    expect(screen.getByText("1.20")).toBeInTheDocument();
    expect(screen.getByText("该历史回测版本未计算 CVaR")).toBeInTheDocument();

    await waitFor(() => expect(screen.getAllByTestId("echarts").length).toBeGreaterThan(0));

    expect(screen.getByText("年度收益表")).toBeInTheDocument();
    expect(screen.getByText("月度收益热力图")).toBeInTheDocument();
    expect(screen.getByText("滚动指标")).toBeInTheDocument();
    expect(screen.getByText("资产贡献")).toBeInTheDocument();
    expect(screen.getByText("累计收益贡献")).toHaveAttribute(
      "title",
      "各资产链接贡献之和等于组合累计收益",
    );
    expect(screen.getByText("风险贡献")).toHaveAttribute(
      "title",
      "组合方差非零时，各资产风险贡献之和等于 100%",
    );
    expect(screen.getByText("回撤期贡献")).toHaveAttribute(
      "title",
      "各资产峰值到谷底的链接贡献之和等于最大回撤",
    );
    expect(screen.getByText("相关性矩阵")).toBeInTheDocument();
    expect(screen.getByText("数据质量")).toBeInTheDocument();

    const csv = screen.getByTestId("export-csv");
    expect(csv).toHaveAttribute("href", expect.stringContaining("/runs/rbr_1/export.csv"));
    expect(screen.getByTestId("export-json")).toBeInTheDocument();
    expect(screen.getByTestId("compare-run")).toBeInTheDocument();
    expect(screen.getByTestId("clone-params")).toBeInTheDocument();
  });

  it("renders frozen VaR and CVaR metrics from a v3 summary", async () => {
    const detail = runDetail();
    getRunMock.mockResolvedValue(runDetail({
      engine_version: "research_backtest_v3",
      summary: {
        ...detail.summary!,
        tail_risk: {
          algorithm_version: "empirical_cvar_v1",
          confidence: 0.95,
          horizon_days: 20,
          scenario_count: 233,
          tail_count: 12,
          var_loss: 0.05,
          cvar_loss: 0.08,
          worst_loss: 0.13,
        },
      },
    }));
    renderPage();
    expect(await screen.findByText("20 日 95% VaR")).toBeInTheDocument();
    expect(screen.getByText("20 日 95% CVaR")).toBeInTheDocument();
    expect(screen.getByText("最差 20 日损失")).toBeInTheDocument();
    expect(screen.queryByText("该历史回测版本未计算 CVaR")).not.toBeInTheDocument();
  });

  it("renders data quality when historical run stores fx as null", async () => {
    getRunMock.mockResolvedValue(
      runDetail({
        data_quality: {
          common_start_policy: "max_asset_start",
          common_end_policy: "min_asset_end",
          forward_fill_days_max: 7,
          common_start: "2024-01-01",
          common_end: "2024-01-30",
          window_start: "2024-01-01",
          window_end: "2024-01-30",
          assets: [
            {
              asset_key: "CN|a",
              name: "资产A",
              currency: "CNY",
              raw_start: "2024-01-01",
              raw_end: "2024-01-30",
              raw_point_count: 30,
              usable_start: "2024-01-01",
              usable_end: "2024-01-30",
              fill_count: 0,
              max_fill_gap_days: 0,
              fill_tolerance_days: 7,
              fill_gap_exceeded: false,
            },
          ],
          fx: null,
        },
      }),
    );
    renderPage();
    const dataQuality = await screen.findByTestId("data-quality");
    expect(within(dataQuality).getByText("资产A")).toBeInTheDocument();
    expect(within(dataQuality).queryByText(/^汇率/)).not.toBeInTheDocument();
  });

  it("shows the failure state with the job error", async () => {
    getRunMock.mockResolvedValue(
      runDetail({
        status: "failed",
        summary: undefined,
        job: {
          status: "failed",
          phase: "",
          progress_current: 0,
          progress_total: 0,
          error_code: "source_hash_mismatch",
          error_message: "数据已变化，请重新运行",
        },
      }),
    );
    renderPage();
    expect(await screen.findByText("回测失败")).toBeInTheDocument();
    expect(screen.getByText("数据已变化，请重新运行")).toBeInTheDocument();
  });
});
