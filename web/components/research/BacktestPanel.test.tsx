import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ResearchCollectionDetail,
  ResearchOptimizationReadiness,
  ResearchReadiness,
  ResearchRunView,
} from "@/lib/api/research";
import {
  BacktestPanel,
  optimizationDisabledReason,
  runDisabledReason,
} from "./BacktestPanel";

const createBacktestMock = vi.hoisted(() => vi.fn());
const getOptimizationReadinessMock = vi.hoisted(() => vi.fn());
const getLatestOptimizationMock = vi.hoisted(() => vi.fn());
const createOptimizationMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPushMock }),
}));

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  createBacktest: (...args: unknown[]) => createBacktestMock(...args),
  getOptimizationReadiness: (...args: unknown[]) => getOptimizationReadinessMock(...args),
  getLatestOptimization: (...args: unknown[]) => getLatestOptimizationMock(...args),
  createOptimization: (...args: unknown[]) => createOptimizationMock(...args),
}));

function detail(): ResearchCollectionDetail {
  return {
    id: "rc_1",
    name: "测试集合",
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
    status: "active",
    created_at: 0,
    updated_at: 0,
    tags: [],
    items: [],
  };
}

function readiness(overrides: Partial<ResearchReadiness> = {}): ResearchReadiness {
  return {
    ready: true,
    weight_sum: 1,
    common_start: "2018-01-01",
    common_end: "2026-07-01",
    window_start: "2018-01-01",
    window_end: "2026-07-01",
    blocking_reasons: [],
    warnings: [],
    assets: [],
    data_dependencies: {
      asset_count: 2,
      fx_pairs: [],
      stale_asset_count: 0,
      missing_history_count: 0,
    },
    ...overrides,
  };
}

function optReadiness(
  overrides: Partial<ResearchOptimizationReadiness> = {},
): ResearchOptimizationReadiness {
  return {
    ready: true,
    enabled_count: 2,
    locked_count: 0,
    tunable_count: 2,
    locked_weight_sum: 0,
    candidate_count: 10,
    blocking_reasons: [],
    warnings: [],
    ...overrides,
  };
}

function issue(reason: string, message = reason) {
  return { reason, message };
}

function run(overrides: Partial<ResearchRunView> = {}): ResearchRunView {
  return {
    id: "rbr_1",
    collection_id: "rc_1",
    job_id: "job_1",
    input_hash: "h",
    source_hash: "s",
    engine_version: "research_backtest_v1",
    base_currency: "CNY",
    rebalance_policy: "monthly",
    window_start: "2018-01-01",
    window_end: "2026-07-01",
    status: "succeeded",
    created_at: 1750000000000,
    summary: {
      cumulative_return: 0.8,
      cagr: 0.07,
      annual_volatility: 0.15,
      max_drawdown: -0.25,
      current_drawdown_days: 0,
      max_drawdown_duration_days: 200,
      effective_return_days: 2000,
      risk_free_rate: 0.02,
      contributions: [],
    },
    ...overrides,
  };
}

function renderPanel(r?: ResearchReadiness, latestRuns: ResearchRunView[] = []) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <BacktestPanel detail={detail()} readiness={r} latestRuns={latestRuns} />
    </QueryClientProvider>,
  );
}

describe("runDisabledReason", () => {
  it("returns null when ready", () => {
    expect(runDisabledReason(readiness())).toBeNull();
  });

  it("is checking while readiness is missing", () => {
    expect(runDisabledReason(undefined)).toContain("检查");
  });

  it("prioritizes weight problems with the gap", () => {
    const r = readiness({
      ready: false,
      weight_sum: 0.8,
      blocking_reasons: [issue("weight_sum_invalid"), issue("history_missing")],
    });
    expect(runDisabledReason(r)).toBe("权重合计 80%，差 20% 才能运行");
  });

  it("reports no enabled assets", () => {
    const r = readiness({
      ready: false,
      weight_sum: 0,
      blocking_reasons: [issue("no_enabled_assets")],
    });
    expect(runDisabledReason(r)).toBe("集合没有启用的资产");
  });

  it("reports missing history before syncing", () => {
    const r = readiness({
      ready: false,
      blocking_reasons: [issue("history_missing"), issue("history_syncing")],
    });
    expect(runDisabledReason(r)).toContain("缺历史");
  });

  it("reports active sync tasks", () => {
    const r = readiness({ ready: false, blocking_reasons: [issue("history_syncing")] });
    expect(runDisabledReason(r)).toContain("同步任务进行中");
  });

  it("reports fx problems", () => {
    const r = readiness({ ready: false, blocking_reasons: [issue("fx_missing")] });
    expect(runDisabledReason(r)).toContain("汇率");
  });

  it("reports a short common window with the limiting asset", () => {
    const r = readiness({
      ready: false,
      blocking_reasons: [issue("common_window_too_short")],
      assets: [
        {
          item_id: "a",
          asset_key: "CN|x",
          name: "新发基金",
          currency: "CNY",
          is_cash: false,
          enabled: true,
          weight: 0.5,
          adjust_policy: "qfq",
          point_type: "adjusted_close",
          listing_status: "active",
          has_history: true,
          point_count: 10,
          stale: false,
          limits_common_start: true,
        },
      ],
    });
    expect(runDisabledReason(r)).toBe("共同历史区间不足（受限于 新发基金）");
  });
});

describe("optimizationDisabledReason", () => {
  it("does not inherit the normal backtest weight-sum block", () => {
    expect(
      optimizationDisabledReason(
        readiness({
          ready: false,
          weight_sum: 0,
          blocking_reasons: [issue("weight_sum_invalid")],
        }),
        optReadiness({
          ready: true,
          enabled_count: 4,
          tunable_count: 4,
          candidate_count: 8855,
        }),
      ),
    ).toBeNull();
  });
});

describe("BacktestPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getOptimizationReadinessMock.mockResolvedValue(optReadiness());
    getLatestOptimizationMock.mockResolvedValue(null);
  });

  it("disables the run button with the reason when blocked", () => {
    renderPanel(
      readiness({ ready: false, blocking_reasons: [issue("fx_missing")] }),
    );
    expect(screen.getByTestId("run-backtest")).toBeDisabled();
    expect(screen.getByTestId("run-disabled-reason")).toHaveTextContent("汇率");
  });

  it("shows both run and optimization disabled reasons simultaneously", async () => {
    getOptimizationReadinessMock.mockResolvedValue(
      optReadiness({
        ready: false,
        blocking_reasons: [issue("too_many_enabled_assets")],
        enabled_count: 12,
      }),
    );
    renderPanel(
      readiness({ ready: false, blocking_reasons: [issue("fx_missing")] }),
    );
    expect(screen.getByTestId("run-disabled-reason")).toHaveTextContent("汇率");
    await waitFor(() =>
      expect(screen.getByTestId("opt-disabled-reason")).toHaveTextContent("超过上限"),
    );
  });

  it("runs a backtest and navigates to the run page", async () => {
    createBacktestMock.mockResolvedValue({ run: run({ status: "queued" }), reused: false });
    renderPanel(readiness());
    const btn = screen.getByTestId("run-backtest");
    expect(btn).toBeEnabled();
    fireEvent.click(btn);
    await waitFor(() => expect(createBacktestMock).toHaveBeenCalledWith("rc_1"));
    await waitFor(() =>
      expect(routerPushMock).toHaveBeenCalledWith("/research/collections/rc_1/runs/rbr_1"),
    );
  });

  it("notes reuse when an identical successful run exists", async () => {
    createBacktestMock.mockResolvedValue({ run: run(), reused: true });
    renderPanel(readiness());
    fireEvent.click(screen.getByTestId("run-backtest"));
    expect(
      await screen.findByText("输入未变化，已复用此前成功的回测结果。"),
    ).toBeInTheDocument();
  });

  it("shows the latest run with metrics and window", () => {
    renderPanel(readiness(), [run()]);
    const latest = screen.getByTestId("latest-run");
    expect(latest).toHaveTextContent("2018-01-01 ~ 2026-07-01");
    expect(latest).toHaveTextContent("CAGR 7%");
    expect(screen.getByTestId("backtest-window")).toHaveTextContent(
      "2018-01-01 ~ 2026-07-01",
    );
  });

  it("enables the find-optimal button when optimization readiness is ready", async () => {
    renderPanel(readiness());
    await waitFor(() =>
      expect(screen.getByTestId("find-optimal")).toBeEnabled(),
    );
  });

  it("disables the find-optimal button when optimization is not ready", async () => {
    getOptimizationReadinessMock.mockResolvedValue(
      optReadiness({
        ready: false,
        blocking_reasons: [issue("too_many_enabled_assets")],
        enabled_count: 12,
      }),
    );
    renderPanel(readiness());
    await waitFor(() =>
      expect(screen.getByTestId("opt-disabled-reason")).toHaveTextContent("超过上限"),
    );
    expect(screen.getByTestId("find-optimal")).toBeDisabled();
  });
});
