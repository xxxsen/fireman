import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminPage, AdminFinalizeRecord } from "@/lib/api/admin";
import AdminFinalizationsPage from "./page";

const replaceMock = vi.hoisted(() => vi.fn());
const searchParamsMock = vi.hoisted(() => ({ value: new URLSearchParams() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
  usePathname: () => "/admin/finalizations",
  useSearchParams: () => searchParamsMock.value,
}));

const listRecordsMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  listAdminFinalizeRecords: (...args: unknown[]) => listRecordsMock(...args),
}));

function makeRecord(
  overrides: Partial<AdminFinalizeRecord> = {},
): AdminFinalizeRecord {
  return {
    id: 12,
    task_id: "wt_1",
    task_type: "asset_history_sync",
    attempt_no: 1,
    result: "retryable_error",
    error_code: "resource_not_found",
    error_message: "gone",
    duration_ms: 45,
    created_at: Date.now() - 60_000,
    ...overrides,
  };
}

function makePage(
  items: AdminFinalizeRecord[],
  total = items.length,
): AdminPage<AdminFinalizeRecord> {
  return { items, total, limit: 20, offset: 0 };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminFinalizationsPage />
    </QueryClientProvider>,
  );
}

describe("AdminFinalizationsPage", () => {
  beforeEach(() => {
    replaceMock.mockReset();
    listRecordsMock.mockReset();
    searchParamsMock.value = new URLSearchParams();
    listRecordsMock.mockResolvedValue(makePage([makeRecord()]));
  });

  it("explains what a finalization record is", async () => {
    renderPage();
    await screen.findByTestId("finalize-record-row");
    expect(
      screen.getByText(/每条记录对应 Go finalizer 的一次业务落库尝试/),
    ).toBeInTheDocument();
  });

  it("renders record rows with result badge and a task link", async () => {
    renderPage();
    const row = await screen.findByTestId("finalize-record-row");
    expect(within(row).getByTestId("finalize-result-badge")).toHaveAttribute(
      "data-result",
      "retryable_error",
    );
    expect(within(row).getByText("历史同步")).toBeInTheDocument();
    expect(within(row).getByRole("link")).toHaveAttribute(
      "href",
      "/admin/worker-tasks?task_id=wt_1",
    );
    expect(within(row).getByText("45ms")).toBeInTheDocument();
  });

  it("writes the result filter into the URL", async () => {
    renderPage();
    await screen.findByTestId("finalize-record-row");
    fireEvent.change(screen.getByTestId("admin-filter-result"), {
      target: { value: "permanent_error" },
    });
    expect(replaceMock).toHaveBeenCalledWith(
      "/admin/finalizations?result=permanent_error",
      {
        scroll: false,
      },
    );
  });

  it("debounces the task id search before writing it into the URL", async () => {
    renderPage();
    await screen.findByTestId("finalize-record-row");
    fireEvent.change(screen.getByTestId("admin-filter-search"), {
      target: { value: " wt_42 " },
    });
    await waitFor(
      () =>
        expect(replaceMock).toHaveBeenCalledWith(
          "/admin/finalizations?task_id=wt_42",
          {
            scroll: false,
          },
        ),
      { timeout: 2000 },
    );
  });

  it("passes URL filters to the API call", async () => {
    searchParamsMock.value = new URLSearchParams(
      "result=success&task_type=fx_rate_sync&task_id=wt_9&offset=20",
    );
    renderPage();
    await waitFor(() =>
      expect(listRecordsMock).toHaveBeenCalledWith(
        expect.objectContaining({
          result: "success",
          taskType: "fx_rate_sync",
          taskId: "wt_9",
          limit: 20,
          offset: 20,
        }),
      ),
    );
  });

  it("shows the empty state when no record matches", async () => {
    listRecordsMock.mockResolvedValue(makePage([]));
    renderPage();
    expect(await screen.findByText("没有匹配的终结记录")).toBeInTheDocument();
  });

  it("shows the error state when the list request fails", async () => {
    listRecordsMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });
});
