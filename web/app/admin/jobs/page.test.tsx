import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminJobItem, AdminPage } from "@/lib/api/admin";
import AdminJobsPage from "./page";

const replaceMock = vi.hoisted(() => vi.fn());
const searchParamsMock = vi.hoisted(() => ({ value: new URLSearchParams() }));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: replaceMock }),
  usePathname: () => "/admin/jobs",
  useSearchParams: () => searchParamsMock.value,
}));

const listJobsMock = vi.hoisted(() => vi.fn());
const listPlansMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  listAdminJobs: (...args: unknown[]) => listJobsMock(...args),
}));

vi.mock("@/lib/api/plans", () => ({
  listPlans: () => listPlansMock(),
}));

function makeJob(overrides: Partial<AdminJobItem> = {}): AdminJobItem {
  return {
    id: "job_1",
    plan_id: "plan_1",
    plan_name: "主计划",
    type: "simulation",
    status: "succeeded",
    phase: "",
    progress_current: 0,
    progress_total: 0,
    error_code: "",
    error_message: "",
    created_at: Date.now() - 3600_000,
    started_at: Date.now() - 3590_000,
    finished_at: Date.now() - 3580_000,
    duration_ms: 10_000,
    ...overrides,
  };
}

function makePage(items: AdminJobItem[], total = items.length): AdminPage<AdminJobItem> {
  return { items, total, limit: 20, offset: 0 };
}

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <AdminJobsPage />
    </QueryClientProvider>,
  );
}

describe("AdminJobsPage", () => {
  beforeEach(() => {
    replaceMock.mockReset();
    listJobsMock.mockReset();
    listPlansMock.mockReset();
    searchParamsMock.value = new URLSearchParams();
    listJobsMock.mockResolvedValue(makePage([makeJob()]));
    listPlansMock.mockResolvedValue([{ id: "plan_1", name: "主计划" }]);
  });

  it("renders job rows with status badge and a plan link", async () => {
    renderPage();
    const row = await screen.findByTestId("job-row");
    expect(within(row).getByTestId("job-status-badge")).toHaveAttribute(
      "data-status",
      "succeeded",
    );
    expect(within(row).getByText("模拟")).toBeInTheDocument();
    expect(within(row).getByRole("link", { name: "主计划" })).toHaveAttribute(
      "href",
      "/plans/plan_1",
    );
  });

  it("labels jobs without a plan as system jobs", async () => {
    listJobsMock.mockResolvedValue(makePage([makeJob({ plan_id: "", plan_name: "" })]));
    renderPage();
    const row = await screen.findByTestId("job-row");
    expect(within(row).getByText("系统作业")).toBeInTheDocument();
  });

  it("shows phase and progress bar for a running job", async () => {
    listJobsMock.mockResolvedValue(
      makePage([
        makeJob({
          status: "running",
          phase: "mc_paths",
          progress_current: 4200,
          progress_total: 10_000,
          finished_at: null,
          duration_ms: null,
        }),
      ]),
    );
    renderPage();
    const progress = await screen.findByTestId("job-progress");
    expect(progress).toHaveTextContent("mc_paths");
    expect(progress).toHaveTextContent("4200 / 10000");
  });

  it("expands a failed row inline to reveal the error", async () => {
    listJobsMock.mockResolvedValue(
      makePage([
        makeJob({
          status: "failed",
          error_code: "sim_engine_error",
          error_message: "matrix not positive definite",
        }),
      ]),
    );
    renderPage();
    const row = await screen.findByTestId("job-row");
    expect(screen.queryByTestId("job-error-row")).not.toBeInTheDocument();

    fireEvent.click(row);
    const errorRow = screen.getByTestId("job-error-row");
    expect(errorRow).toHaveTextContent("sim_engine_error");
    expect(errorRow).toHaveTextContent("matrix not positive definite");

    fireEvent.click(row);
    expect(screen.queryByTestId("job-error-row")).not.toBeInTheDocument();
  });

  it("populates the plan filter from listPlans and writes plan_id to the URL", async () => {
    renderPage();
    await screen.findByTestId("job-row");

    const planSelect = screen.getByTestId("admin-filter-plan");
    await waitFor(() =>
      expect(within(planSelect).getByRole("option", { name: "主计划" })).toBeInTheDocument(),
    );

    fireEvent.change(planSelect, { target: { value: "plan_1" } });
    expect(replaceMock).toHaveBeenCalledWith("/admin/jobs?plan_id=plan_1", {
      scroll: false,
    });
  });

  it("passes URL filters to the API call", async () => {
    searchParamsMock.value = new URLSearchParams("type=stress&status=failed");
    renderPage();
    await waitFor(() =>
      expect(listJobsMock).toHaveBeenCalledWith(
        expect.objectContaining({ type: "stress", status: "failed", limit: 20, offset: 0 }),
      ),
    );
  });

  it("shows the empty state when no job matches", async () => {
    listJobsMock.mockResolvedValue(makePage([]));
    renderPage();
    expect(await screen.findByText("没有匹配的计算作业")).toBeInTheDocument();
  });

  it("shows the error state when the list request fails", async () => {
    listJobsMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
  });
});
