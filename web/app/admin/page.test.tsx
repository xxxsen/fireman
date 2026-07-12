import { render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminOverview } from "@/lib/api/admin";
import AdminOverviewPage from "./page";

const getAdminOverviewMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  getAdminOverview: () => getAdminOverviewMock(),
}));

function makeOverview(overrides: Partial<AdminOverview> = {}): AdminOverview {
  return {
    worker_tasks: {
      active: 2,
      by_status: { pending: 1, running: 1 },
      failed_last_24h: 3,
      completed_last_24h: 12,
      stale_running: 0,
    },
    finalizations: { total_last_24h: 15, failed_last_24h: 2 },
    sync_health: {
      directory_scopes: [
        {
          scope: "cn_all",
          label: "中国市场目录",
          status: "complete",
          last_success_at: Date.now() - 2 * 3600_000,
          active_task_status: "",
          stale: false,
          units: [
            {
              sync_key: "cn_exchange_stock",
              label: "A 股股票",
              last_success_at: Date.now() - 2 * 3600_000,
              active_task_status: "",
              latest_task_failed: false,
              stale: false,
            },
          ],
        },
        {
          scope: "us_all",
          label: "美股市场目录",
          status: "running",
          last_success_at: Date.now() - 8 * 24 * 3600_000,
          active_task_status: "running",
          stale: true,
          units: [
            {
              sync_key: "us_stock",
              label: "美股股票",
              last_success_at: Date.now() - 8 * 24 * 3600_000,
              active_task_status: "running",
              latest_task_failed: false,
              stale: true,
            },
            {
              sync_key: "us_etf",
              label: "美股 ETF",
              last_success_at: Date.now() - 8 * 24 * 3600_000,
              active_task_status: "",
              latest_task_failed: true,
              stale: true,
            },
          ],
        },
      ],
      fx_pairs: [{ pair: "USDCNY", last_success_at: null }],
      history_dimensions: { total: 42, stale_over_7d: 3, never_synced: 1 },
    },
    storage: {
      main_db_bytes: 10 * 1024 * 1024,
      resource_db_bytes: 2 * 1024 * 1024,
      resource_count: 18,
    },
    ...overrides,
  };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminOverviewPage />
    </QueryClientProvider>,
  );
}

describe("AdminOverviewPage", () => {
  beforeEach(() => {
    getAdminOverviewMock.mockReset();
  });

  it("renders stat cards with links to pre-filtered boards", async () => {
    getAdminOverviewMock.mockResolvedValue(makeOverview());
    renderPage();
    await screen.findByTestId("admin-overview");

    const cards = screen.getAllByTestId("stat-card");
    expect(cards).toHaveLength(3);

    const links = screen.getAllByTestId("stat-card-link");
    const hrefs = links.map((l) => l.getAttribute("href"));
    expect(hrefs).toContain("/admin/worker-tasks?status=active");
    expect(hrefs).toContain("/admin/worker-tasks?status=failed");
    expect(hrefs).toContain("/admin/finalizations");

    expect(screen.getByText("活跃任务")).toBeInTheDocument();
    expect(screen.getByText("24h 任务失败")).toBeInTheDocument();
    expect(screen.getByText("2 次失败")).toBeInTheDocument();
  });

  it("links the finalization card to the finalization board", async () => {
    const overview = makeOverview();
    overview.finalizations.failed_last_24h = 2;
    getAdminOverviewMock.mockResolvedValue(overview);
    renderPage();
    await screen.findByTestId("admin-overview");

    const hrefs = screen
      .getAllByTestId("stat-card-link")
      .map((l) => l.getAttribute("href"));
    expect(hrefs).toContain("/admin/finalizations");
  });

  it("shows sync health rows with stale warning and storage sizes", async () => {
    getAdminOverviewMock.mockResolvedValue(makeOverview());
    renderPage();
    const panel = await screen.findByTestId("sync-health-panel");

    const scopes = within(panel).getAllByTestId("sync-health-scope");
    expect(scopes).toHaveLength(2);
    expect(within(scopes[0]).getByText("中国市场目录")).toBeInTheDocument();
    expect(
      within(scopes[0]).getByTestId("scope-health-status-cn_all"),
    ).toHaveTextContent("已同步");
    expect(
      within(scopes[1]).getByTestId("scope-health-status-us_all"),
    ).toHaveTextContent("同步中");
    expect(
      within(scopes[1]).getAllByText("超 7 天未成功").length,
    ).toBeGreaterThan(0);

    // Unit-level details render under the scope.
    const usStock = within(scopes[1]).getByTestId("sync-health-unit-us_stock");
    expect(within(usStock).getByTestId("task-status-badge")).toHaveAttribute(
      "data-status",
      "running",
    );
    const usEtf = within(scopes[1]).getByTestId("sync-health-unit-us_etf");
    expect(within(usEtf).getByText("最近失败")).toBeInTheDocument();

    const fx = within(panel).getByTestId("sync-health-fx");
    expect(within(fx).getByText("USDCNY")).toBeInTheDocument();
    expect(within(fx).getByText("从未成功")).toBeInTheDocument();

    expect(within(panel).getByTestId("history-dimensions")).toHaveTextContent(
      "历史维度 42 个 · 3 个超 7 天未更新 · 1 个从未同步",
    );

    const storage = screen.getByTestId("storage-panel");
    expect(within(storage).getByText("10.0 MB")).toBeInTheDocument();
    expect(within(storage).getByText("2.0 MB")).toBeInTheDocument();
    expect(within(storage).getByText("资源库（18 条）")).toBeInTheDocument();
  });

  it("shows the stale-running hint with a warning tone", async () => {
    const overview = makeOverview();
    overview.worker_tasks.stale_running = 2;
    overview.worker_tasks.failed_last_24h = 0;
    getAdminOverviewMock.mockResolvedValue(overview);
    renderPage();
    await screen.findByTestId("admin-overview");
    expect(screen.getByText("另有 2 个任务心跳滞留")).toBeInTheDocument();
  });

  it("shows an error state with retry when the overview request fails", async () => {
    getAdminOverviewMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.getByTestId("error-state-retry")).toBeInTheDocument();
  });
});
