import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ResearchCollectionListItem, ResearchRunView } from "@/lib/api/research";
import ResearchHomePage from "./page";

const listCollectionsMock = vi.hoisted(() => vi.fn());
const listRecentRunsMock = vi.hoisted(() => vi.fn());
const deleteCollectionMock = vi.hoisted(() => vi.fn());
const createCollectionMock = vi.hoisted(() => vi.fn());
const updateCollectionMock = vi.hoisted(() => vi.fn());
const routerPushMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: routerPushMock }),
}));

vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  listCollections: (...args: unknown[]) => listCollectionsMock(...args),
  listRecentRuns: (...args: unknown[]) => listRecentRunsMock(...args),
  deleteCollection: (...args: unknown[]) => deleteCollectionMock(...args),
  createCollection: (...args: unknown[]) => createCollectionMock(...args),
  updateCollection: (...args: unknown[]) => updateCollectionMock(...args),
}));

vi.mock("@/lib/api/plans", () => ({
  listPlans: () => Promise.resolve([]),
}));

function collection(
  overrides: Partial<ResearchCollectionListItem> = {},
): ResearchCollectionListItem {
  return {
    id: "rc_1",
    name: "中美宽基",
    description: "",
    base_currency: "CNY",
    initial_amount_minor: 100000000,
    rebalance_policy: "monthly",
    rebalance_threshold: 0,
    start_policy: "common_intersection",
    window_start: "",
    window_end: "",
    risk_free_rate: 0,
    transaction_cost_rate: 0,
    status: "active",
    created_at: 1750000000000,
    updated_at: 1750000000000,
    tags: [],
    enabled_assets: 3,
    total_assets: 4,
    weight_sum: 1,
    weight_valid: true,
    latest_run: null,
    latest_run_summary: null,
    ...overrides,
  };
}

function run(overrides: Partial<ResearchRunView> = {}): ResearchRunView {
  return {
    id: "rbr_1",
    collection_id: "rc_1",
    task_id: "job_1",
    input_hash: "h",
    source_hash: "s",
    engine_version: "research_backtest_v1",
    base_currency: "CNY",
    rebalance_policy: "monthly",
    window_start: "2020-01-01",
    window_end: "2026-06-30",
    status: "complete",
    created_at: 1750000000000,
    ...overrides,
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ResearchHomePage />
    </QueryClientProvider>,
  );
}

describe("ResearchHomePage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listCollectionsMock.mockResolvedValue({ collections: [collection()] });
    listRecentRunsMock.mockResolvedValue({ runs: [] });
  });

  it("renders the collection list with weight badge and links", async () => {
    renderPage();
    expect(await screen.findByTestId("collection-table")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "中美宽基" })).toHaveAttribute(
      "href",
      "/research/collections/rc_1",
    );
    expect(screen.getByText("权重 100%")).toBeInTheDocument();
    expect(screen.getByText("尚未回测")).toBeInTheDocument();
  });

  it("shows weight warning and latest run metrics", async () => {
    listCollectionsMock.mockResolvedValue({
      collections: [
        collection({
          weight_valid: false,
          weight_sum: 0.8,
          latest_run: run(),
          latest_run_summary: {
            cumulative_return: 0.5,
            cagr: 0.08,
            annual_volatility: 0.15,
            max_drawdown: -0.2,
            current_drawdown_days: 0,
            max_drawdown_duration_days: 100,
            effective_return_days: 900,
            risk_free_rate: 0,
            contributions: [],
          },
        }),
      ],
    });
    renderPage();
    expect(await screen.findByText("权重 80%")).toBeInTheDocument();
    expect(screen.getByText("8%")).toBeInTheDocument();
    expect(screen.getByText("-20%")).toBeInTheDocument();
  });

  it("renders the empty state with a create entry", async () => {
    listCollectionsMock.mockResolvedValue({ collections: [] });
    renderPage();
    expect(await screen.findByTestId("empty-state")).toBeInTheDocument();
    expect(screen.getByText("还没有研究集合")).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-action")).toHaveAttribute(
      "href",
      "/research/collections/new",
    );
  });

  it("renders recent runs with detail links", async () => {
    listRecentRunsMock.mockResolvedValue({ runs: [run()] });
    renderPage();
    expect(await screen.findByTestId("recent-runs")).toBeInTheDocument();
    expect(
      await screen.findByRole("link", { name: /2020-01-01 ~ 2026-06-30/ }),
    ).toHaveAttribute("href", "/research/collections/rc_1/runs/rbr_1");
  });

  it("archives a collection after confirmation", async () => {
    deleteCollectionMock.mockResolvedValue({ archived: true });
    renderPage();
    fireEvent.click(await screen.findByTestId("archive-rc_1"));
    fireEvent.click(await screen.findByTestId("confirm-dialog-confirm"));
    await waitFor(() => expect(deleteCollectionMock).toHaveBeenCalledWith("rc_1", false));
  });

  it("separates archived collections with restore and hard delete", async () => {
    listCollectionsMock.mockResolvedValue({
      collections: [
        collection(),
        collection({ id: "rc_2", name: "旧组合", status: "archived" }),
      ],
    });
    updateCollectionMock.mockResolvedValue({});
    deleteCollectionMock.mockResolvedValue({ deleted: true });
    renderPage();
    await screen.findByTestId("collection-table");
    // The archived collection stays out of the main table.
    expect(screen.queryByRole("link", { name: "旧组合" })).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("archived-toggle"));
    expect(screen.getByTestId("archived-rc_2")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("restore-rc_2"));
    await waitFor(() =>
      expect(updateCollectionMock).toHaveBeenCalledWith("rc_2", { status: "active" }),
    );

    fireEvent.click(screen.getByTestId("hard-delete-rc_2"));
    fireEvent.click(await screen.findByTestId("confirm-dialog-confirm"));
    await waitFor(() => expect(deleteCollectionMock).toHaveBeenCalledWith("rc_2", true));
  });

  it("imports a collection from a JSON file", async () => {
    createCollectionMock.mockResolvedValue({ id: "rc_new" });
    renderPage();
    await screen.findByTestId("collection-table");
    const input = screen.getByTestId("import-json-input");
    const file = new File(
      [JSON.stringify({ name: "导入集合", items: [{ asset_key: "CN|a", weight: 1 }] })],
      "collection.json",
      { type: "application/json" },
    );
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() => expect(createCollectionMock).toHaveBeenCalled());
    expect(createCollectionMock.mock.calls[0]![0]).toMatchObject({ name: "导入集合" });
    await waitFor(() =>
      expect(routerPushMock).toHaveBeenCalledWith("/research/collections/rc_new"),
    );
  });

  it("surfaces import errors for invalid files", async () => {
    renderPage();
    await screen.findByTestId("collection-table");
    const file = new File(["{broken"], "bad.json", { type: "application/json" });
    fireEvent.change(screen.getByTestId("import-json-input"), {
      target: { files: [file] },
    });
    expect(await screen.findByText(/导入失败/)).toBeInTheDocument();
    expect(createCollectionMock).not.toHaveBeenCalled();
  });

  it("has copy-from-plan entry", async () => {
    renderPage();
    await screen.findByTestId("collection-table");
    fireEvent.click(screen.getByTestId("copy-from-plan-entry"));
    expect(await screen.findByTestId("dialog")).toBeInTheDocument();
    expect(screen.getByText("从计划复制", { selector: "h2" })).toBeInTheDocument();
  });
});
