import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ResearchCollectionDetail,
  ResearchCollectionItemView,
  ResearchOptimizationRun,
} from "@/lib/api/research";
import OptimizationDetailPage from "./page";

const getOptimizationMock = vi.hoisted(() => vi.fn());
const getCollectionMock = vi.hoisted(() => vi.fn());
const applyOptimizationMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useParams: () => ({ id: "rc_1", optimizationId: "opt_1" }),
  useRouter: () => ({ push: routerPushMock }),
}));

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  getOptimization: (...args: unknown[]) => getOptimizationMock(...args),
  getCollection: (...args: unknown[]) => getCollectionMock(...args),
  applyOptimization: (...args: unknown[]) => applyOptimizationMock(...args),
}));

function item(
  id: string,
  overrides: Partial<ResearchCollectionItemView> = {},
): ResearchCollectionItemView {
  return {
    id,
    collection_id: "rc_1",
    asset_key: `CN|fund|sh|${id}`,
    enabled: true,
    weight: 0,
    weight_locked: false,
    adjust_policy: "hfq",
    point_type: "adjusted_close",
    asset_class: "equity",
    region: "cn",
    note: "",
    sort_order: 0,
    created_at: 0,
    updated_at: 1234,
    name: `资产${id}`,
    symbol: id,
    market: "CN",
    instrument_type: "cn_exchange_fund",
    instrument_type_label: "场内 ETF / LOF",
    currency: "CNY",
    listing_status: "active",
    is_cash: false,
    ...overrides,
  };
}

function collection(): ResearchCollectionDetail {
  return {
    id: "rc_1",
    name: "测试组合",
    description: "",
    base_currency: "CNY",
    initial_amount_minor: 100000000,
    rebalance_policy: "monthly",
    rebalance_threshold: 0,
    start_policy: "common_intersection",
    window_start: "",
    window_end: "",
    risk_free_rate: 0.02,
    transaction_cost_rate: 0,
    tail_risk_confidence: 0.95,
    tail_risk_horizon_days: 20,
    status: "active",
    created_at: 0,
    updated_at: 1234,
    tags: [],
    items: [
      item("a", { enabled: false, weight: 0 }),
      item("b", { enabled: true, weight: 0.2 }),
      item("c", { enabled: true, weight: 0.8, weight_locked: true }),
    ],
  };
}

function optimization(): ResearchOptimizationRun {
  return {
    id: "opt_1",
    collection_id: "rc_1",
    job_id: "job_1",
    status: "succeeded",
    config: {
      weight_step: 0.05,
      top_k: 20,
      tail_risk: { confidence: 0.95, horizon_days: 20 },
      minimum_cagr: 0.03,
    },
    candidate_count: 10,
    evaluated_count: 10,
    base_currency: "CNY",
    rebalance_policy: "monthly",
    window_start: "2020-01-01",
    window_end: "2026-07-01",
    engine_version: "research_optimizer_v4",
    created_at: 1750000000000,
    result: {
      candidate_count: 10,
      evaluated_count: 10,
      skipped_count: 0,
      best_by_cagr: [
        {
          rank: 1,
          objective: "max_cagr",
          score: 0.08,
          weights: [
            { item_id: "a", asset_key: "CN|fund|sh|a", name: "资产a", weight: 0.6, locked: false },
            { item_id: "b", asset_key: "CN|fund|sh|b", name: "资产b", weight: 0.4, locked: false },
            { item_id: "c", asset_key: "CN|fund|sh|c", name: "资产c", weight: 0, locked: false },
          ],
          summary: {
            cumulative_return: 0.9,
            cagr: 0.08,
            annual_volatility: 0.12,
            max_drawdown: -0.18,
            sharpe: 1.2,
            calmar: 0.44,
            current_drawdown_days: 0,
            max_drawdown_duration_days: 120,
            effective_return_days: 1000,
            risk_free_rate: 0.02,
            contributions: [],
          },
        },
      ],
      best_by_drawdown: [],
      best_by_calmar: [],
      best_by_cvar: [
        {
          rank: 1,
          objective: "min_cvar",
          score: -0.07,
          weights: [
            { item_id: "a", asset_key: "CN|fund|sh|a", name: "资产a", weight: 0.6, locked: false },
            { item_id: "b", asset_key: "CN|fund|sh|b", name: "资产b", weight: 0.4, locked: false },
            { item_id: "c", asset_key: "CN|fund|sh|c", name: "资产c", weight: 0, locked: false },
          ],
          summary: {
            cumulative_return: 0.7,
            cagr: 0.06,
            annual_volatility: 0.1,
            max_drawdown: -0.15,
            current_drawdown_days: 0,
            max_drawdown_duration_days: 80,
            effective_return_days: 1000,
            risk_free_rate: 0.02,
            contributions: [],
            tail_risk: {
              algorithm_version: "empirical_cvar_v1",
              confidence: 0.95,
              horizon_days: 20,
              scenario_count: 981,
              tail_count: 50,
              var_loss: -0.05,
              cvar_loss: 0.07,
              worst_loss: 0.12,
            },
          },
        },
      ],
      cvar_eligible_count: 10,
    },
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <OptimizationDetailPage />
    </QueryClientProvider>,
  );
}

describe("OptimizationDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getOptimizationMock.mockResolvedValue(optimization());
    getCollectionMock.mockResolvedValue(collection());
    applyOptimizationMock.mockResolvedValue(collection());
  });

  it("renders metric help in Chinese", async () => {
    renderPage();
    expect(await screen.findByText("夏普比率")).toBeInTheDocument();
    expect(screen.getByText("卡玛比率")).toBeInTheDocument();
    fireEvent.mouseEnter(screen.getByLabelText("夏普比率说明"));
    expect(await screen.findByText(/单位波动风险/)).toBeInTheDocument();
  });

  it("does not mislabel the current streaming optimizer as a pre-cost version", async () => {
    const current = optimization();
    current.engine_version = "research_optimizer_v6";
    getOptimizationMock.mockResolvedValue(current);

    renderPage();

    await screen.findByText("夏普比率");
    expect(screen.queryByText(/未计研究交易成本/)).not.toBeInTheDocument();
  });

  it("applies a result to the collection and navigates back", async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId("apply-result-cagr-1"));
    expect(await screen.findByText("目标组合：测试组合")).toBeInTheDocument();
    expect(screen.getByText("应用后会同步当前组合的启用、锁定、权重、回测区间和尾部风险口径。")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("confirm-dialog-confirm"));

    await waitFor(() => expect(applyOptimizationMock).toHaveBeenCalledTimes(1));
    expect(applyOptimizationMock).toHaveBeenCalledWith("opt_1", {
      objective: "max_cagr",
      rank: 1,
      expected_collection_updated_at: 1234,
    });
    await waitFor(() =>
      expect(routerPushMock).toHaveBeenCalledWith("/research/collections/rc_1?optimized_applied=1"),
    );
  });

  it("renders the CVaR ranking with frozen loss metrics", async () => {
    renderPage();
    expect(await screen.findByText("20 日 / 95%")).toBeInTheDocument();
    fireEvent.mouseEnter(screen.getByLabelText("最低尾部损失说明"));
    expect(await screen.findByText(/最差 5% 场景的平均损失/)).toBeInTheDocument();
    expect(screen.getByText(/不保证年化收益更高/)).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("tab-cvar"));
    expect(await screen.findByTestId("result-table-cvar")).toBeInTheDocument();
    expect(screen.getAllByText("7%").length).toBeGreaterThan(0);
    expect(screen.getByText("VaR loss")).toBeInTheDocument();
    expect(screen.getByText("CVaR loss")).toBeInTheDocument();
    expect(screen.getByText("-5%").closest("td")).toHaveClass("text-positive");
    expect(screen.getAllByText("7%").some((cell) => cell.closest("td")?.classList.contains("text-danger"))).toBe(true);
    fireEvent.mouseEnter(screen.getByText("-5%"));
    expect(await screen.findByText(/981 个场景/)).toBeInTheDocument();
  });

  it("shows an interrupted optimization retry", async () => {
    getOptimizationMock.mockResolvedValue({
      ...optimization(),
      status: "queued",
      result: undefined,
      evaluated_count: 0,
      job: {
        status: "queued",
        phase: "retrying",
        progress_current: 0,
        progress_total: 10,
        retry_count: 1,
      },
    });

    renderPage();

    expect(await screen.findByText("任务中断后自动重试（1/1），等待执行…")).toBeInTheDocument();
  });

  it("explains a worker interruption terminal failure", async () => {
    getOptimizationMock.mockResolvedValue({
      ...optimization(),
      status: "failed",
      result: undefined,
      error_code: "worker_interrupted",
      error_message: "执行进程中断，自动重试次数已用尽，请重新运行",
      job: {
        status: "failed",
        phase: "",
        progress_current: 0,
        progress_total: 10,
        retry_count: 1,
        error_code: "worker_interrupted",
      },
    });

    renderPage();

    expect(await screen.findByText("调优失败")).toBeInTheDocument();
    expect(screen.getByText("执行进程中断，自动重试仍未完成。请重新发起调优。")).toBeInTheDocument();
  });

});
