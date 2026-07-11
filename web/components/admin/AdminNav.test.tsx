import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AdminNav } from "./AdminNav";

const mockPathname = vi.fn(() => "/admin");

vi.mock("next/navigation", () => ({
  usePathname: () => mockPathname(),
}));

const getOverviewMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  getAdminOverview: () => getOverviewMock(),
}));

function makeOverview(workerTasks: Partial<{ failed_last_24h: number; stale_running: number }>) {
  return {
    worker_tasks: {
      active: 0,
      by_status: {},
      failed_last_24h: 0,
      completed_last_24h: 0,
      stale_running: 0,
      ...workerTasks,
    },
    jobs: { queued: 0, running: 0, failed_last_24h: 0, succeeded_last_24h: 0 },
    callbacks: { total_last_24h: 0, failed_last_24h: 0 },
    sync_health: {
      directory_scopes: [],
      fx_pairs: [],
      history_dimensions: { total: 0, stale_over_7d: 0, never_synced: 0 },
    },
    storage: { main_db_bytes: 0, resource_db_bytes: 0, resource_count: 0 },
  };
}

function renderNav() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminNav />
    </QueryClientProvider>,
  );
}

describe("AdminNav", () => {
  beforeEach(() => {
    getOverviewMock.mockReset();
    getOverviewMock.mockResolvedValue(makeOverview({}));
    mockPathname.mockReturnValue("/admin");
  });

  it("renders all admin tabs with the overview active on /admin", () => {
    renderNav();
    const nav = screen.getByTestId("admin-nav");
    for (const label of ["概览", "市场数据任务", "计算作业", "回调记录", "数据版本", "自动更新管理"]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("link", { name: "概览" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(nav).toBeInTheDocument();
  });

  it("marks the matching tab active by path prefix", () => {
    mockPathname.mockReturnValue("/admin/worker-tasks");
    renderNav();
    expect(screen.getByRole("link", { name: "市场数据任务" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(screen.getByRole("link", { name: "概览" })).not.toHaveAttribute("aria-current");
  });

  it("shows the alert dot when tasks failed in the last 24h", async () => {
    getOverviewMock.mockResolvedValue(makeOverview({ failed_last_24h: 3 }));
    renderNav();
    await waitFor(() =>
      expect(screen.getByTestId("admin-nav-alert-dot")).toBeInTheDocument(),
    );
  });

  it("shows the alert dot when running tasks have stale heartbeats", async () => {
    getOverviewMock.mockResolvedValue(makeOverview({ stale_running: 1 }));
    renderNav();
    await waitFor(() =>
      expect(screen.getByTestId("admin-nav-alert-dot")).toBeInTheDocument(),
    );
  });

  it("hides the alert dot when everything is healthy", async () => {
    renderNav();
    await waitFor(() => expect(getOverviewMock).toHaveBeenCalled());
    expect(screen.queryByTestId("admin-nav-alert-dot")).not.toBeInTheDocument();
  });
});
