import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminDataVersion, AdminOverview, AdminPage } from "@/lib/api/admin";
import AdminDataVersionsPage from "./page";

const replaceMock = vi.hoisted(() => vi.fn());
const searchParamsMock = vi.hoisted(() => ({ value: new URLSearchParams() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
  usePathname: () => "/admin/data-versions",
  useSearchParams: () => searchParamsMock.value,
}));

const listVersionsMock = vi.hoisted(() => vi.fn());
const getOverviewMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  listAdminDataVersions: (...args: unknown[]) => listVersionsMock(...args),
  getAdminOverview: () => getOverviewMock(),
}));

function makeOverview(): AdminOverview {
  return {
    worker_tasks: {
      active: 0,
      by_status: {},
      failed_last_24h: 0,
      completed_last_24h: 0,
      stale_running: 0,
    },
    jobs: { queued: 0, running: 0, failed_last_24h: 0, succeeded_last_24h: 0 },
    callbacks: { total_last_24h: 0, failed_last_24h: 0 },
    sync_health: {
      directory_scopes: [
        {
          scope: "cn_all",
          label: "中国市场目录",
          status: "complete",
          last_success_at: Date.now() - 3600_000,
          active_task_status: "",
          stale: false,
          units: [
            {
              sync_key: "cn_exchange_stock",
              label: "A 股股票",
              last_success_at: Date.now() - 3600_000,
              active_task_status: "",
              latest_task_failed: false,
              stale: false,
            },
          ],
        },
      ],
      fx_pairs: [{ pair: "USDCNY", last_success_at: Date.now() - 7200_000 }],
      history_dimensions: { total: 10, stale_over_7d: 0, never_synced: 0 },
    },
    storage: { main_db_bytes: 0, resource_db_bytes: 0, resource_count: 0 },
  };
}

function makeVersion(overrides: Partial<AdminDataVersion> = {}): AdminDataVersion {
  return {
    version_key: "asset_directory|cn_exchange_stock",
    version_no: 812,
    task_id: "wt_1",
    updated_at: Date.now() - 3600_000,
    ...overrides,
  };
}

function makePage(
  items: AdminDataVersion[],
  total = items.length,
): AdminPage<AdminDataVersion> {
  return { items, total, limit: 20, offset: 0 };
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminDataVersionsPage />
    </QueryClientProvider>,
  );
}

describe("AdminDataVersionsPage", () => {
  beforeEach(() => {
    replaceMock.mockReset();
    listVersionsMock.mockReset();
    getOverviewMock.mockReset();
    searchParamsMock.value = new URLSearchParams();
    getOverviewMock.mockResolvedValue(makeOverview());
    listVersionsMock.mockResolvedValue(makePage([makeVersion()]));
  });

  it("renders the sync health panel above the version table", async () => {
    renderPage();
    expect(await screen.findByTestId("sync-health-panel")).toBeInTheDocument();
    const row = await screen.findByTestId("data-version-row");
    expect(within(row).getByText("asset_directory|cn_exchange_stock")).toBeInTheDocument();
    expect(within(row).getByText("812")).toBeInTheDocument();
    expect(within(row).getByRole("link")).toHaveAttribute(
      "href",
      "/admin/worker-tasks?task_id=wt_1",
    );
  });

  it("writes the prefix filter into the URL", async () => {
    renderPage();
    await screen.findByTestId("data-version-row");
    fireEvent.change(screen.getByTestId("admin-filter-prefix"), {
      target: { value: "asset_history" },
    });
    expect(replaceMock).toHaveBeenCalledWith(
      "/admin/data-versions?prefix=asset_history",
      { scroll: false },
    );
  });

  it("passes the prefix from the URL to the API call", async () => {
    searchParamsMock.value = new URLSearchParams("prefix=fx_rate&offset=20");
    renderPage();
    await waitFor(() =>
      expect(listVersionsMock).toHaveBeenCalledWith(
        expect.objectContaining({ prefix: "fx_rate", limit: 20, offset: 20 }),
      ),
    );
  });

  it("shows a guiding empty state before the first sync", async () => {
    listVersionsMock.mockResolvedValue(makePage([]));
    renderPage();
    expect(await screen.findByText("尚无数据版本记录")).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-action")).toHaveAttribute("href", "/assets");
  });

  it("keeps the version table alive when only the overview fails", async () => {
    getOverviewMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByTestId("data-version-row")).toBeInTheDocument();
    expect(screen.getByText(/同步健康数据加载失败/)).toBeInTheDocument();
  });
});
