import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminAutoUpdateRule, AdminPage } from "@/lib/api/admin";
import AutoUpdatesPage from "./page";

const listMock = vi.hoisted(() => vi.fn());
const createMock = vi.hoisted(() => vi.fn());
const updateMock = vi.hoisted(() => vi.fn());
const unitsMock = vi.hoisted(() => vi.fn());
const searchParamsMock = vi.hoisted(() => ({ value: new URLSearchParams() }));

vi.mock("next/navigation", () => ({
  useSearchParams: () => searchParamsMock.value,
}));

vi.mock("@/lib/api/admin", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/admin")>()),
  listAdminAutoUpdates: (...args: unknown[]) => listMock(...args),
  createAdminDirectoryAutoUpdate: (...args: unknown[]) => createMock(...args),
  updateAdminAutoUpdate: (...args: unknown[]) => updateMock(...args),
  listAdminAutoUpdateDirectoryUnits: () => unitsMock(),
}));

function rule(overrides: Partial<AdminAutoUpdateRule> = {}): AdminAutoUpdateRule {
  return {
    id: "aur_cn_stock",
    target_type: "directory_unit",
    sync_key: "cn_exchange_stock",
    asset_key: "",
    adjust_policy: "",
    point_type: "",
    enabled: true,
    interval_hours: 24,
    next_run_at: 1_800_000_000_000,
    last_task_id: "",
    last_error_code: "",
    last_error_message: "",
    version: 1,
    created_at: 1_700_000_000_000,
    updated_at: 1_700_000_000_000,
    target_label: "cn_exchange_stock",
    ...overrides,
  };
}

function page(
  items: AdminAutoUpdateRule[],
  overrides: Partial<AdminPage<AdminAutoUpdateRule>> = {},
): AdminPage<AdminAutoUpdateRule> {
  return { items, total: items.length, limit: 100, offset: 0, ...overrides };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><AutoUpdatesPage /></QueryClientProvider>);
}

describe("AutoUpdatesPage", () => {
  let directoryRules: AdminAutoUpdateRule[];

  beforeEach(() => {
    directoryRules = [];
    listMock.mockReset();
    createMock.mockReset();
    updateMock.mockReset();
    unitsMock.mockReset();
    searchParamsMock.value = new URLSearchParams();
    listMock.mockImplementation((params: { targetType: string }) =>
      Promise.resolve(page(params.targetType === "directory_unit" ? directoryRules : [])),
    );
    unitsMock.mockResolvedValue([
      { sync_key: "cn_exchange_stock", scope: "cn_all", label: "A 股股票" },
      { sync_key: "cn_exchange_fund", scope: "cn_all", label: "场内基金（ETF/LOF）" },
      { sync_key: "cn_mutual_fund", scope: "cn_all", label: "场外基金" },
      { sync_key: "hk_stock", scope: "hk_all", label: "港股股票" },
      { sync_key: "hk_etf", scope: "hk_all", label: "港股 ETF" },
      { sync_key: "us_stock", scope: "us_all", label: "美股股票" },
      { sync_key: "us_etf", scope: "us_all", label: "美股 ETF" },
    ]);
    createMock.mockImplementation((body: { sync_key: string; interval_hours: number }) => {
      const created = rule({ sync_key: body.sync_key, interval_hours: body.interval_hours });
      directoryRules = [created];
      return Promise.resolve(created);
    });
    updateMock.mockImplementation((_id: string, body: { enabled: boolean; interval_hours: number; version: number }) => {
      const updated = rule({ enabled: body.enabled, interval_hours: body.interval_hours, version: body.version + 1 });
      directoryRules = [updated];
      return Promise.resolve(updated);
    });
  });

  it("always lists every directory unit", async () => {
    renderPage();
    expect(await screen.findByText("A 股股票")).toBeInTheDocument();
    expect(screen.getByText("场外基金")).toBeInTheDocument();
    expect(screen.getByText("港股 ETF")).toBeInTheDocument();
    expect(screen.getByText("美股 ETF")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "启用" })).toHaveLength(7);
  });

  it("enables an unconfigured directory with the selected interval and updates the row", async () => {
    renderPage();
    const row = await screen.findByTestId("directory-rule-cn_exchange_stock");
    fireEvent.change(within(row).getByLabelText("A 股股票更新周期"), { target: { value: "6" } });
    fireEvent.click(within(row).getByRole("button", { name: "启用" }));
    expect(within(row).getByRole("button", { name: "启用中…" })).toBeDisabled();
    await waitFor(() => expect(createMock).toHaveBeenCalledWith({ sync_key: "cn_exchange_stock", interval_hours: 6 }));
    await waitFor(() => expect(within(row).getByText("等待执行")).toBeInTheDocument());
    expect(within(row).getByLabelText("cn_exchange_stock更新周期")).toHaveValue("6");
  });

  it("loads the persisted interval instead of falling back to 24 hours", async () => {
    directoryRules = [rule({ interval_hours: 6 })];
    renderPage();
    const row = await screen.findByTestId("directory-rule-cn_exchange_stock");
    await within(row).findByText("等待执行");
    expect(within(row).getByLabelText("cn_exchange_stock更新周期")).toHaveValue("6");
    const select = within(row).getByLabelText("cn_exchange_stock更新周期");
    const options = Array.from(select.querySelectorAll("option"));
    expect(options.find((o) => o.value === "24")?.textContent).toBe("1 天");
    expect(options.find((o) => o.value === "168")?.textContent).toBe("7 天");
    expect(options.find((o) => o.value === "1")?.textContent).toBe("1 小时");
  });

  it("keeps an edited interval visible when a version conflict occurs", async () => {
    directoryRules = [rule()];
    updateMock.mockRejectedValueOnce(new Error("配置已被其他页面修改，请刷新后重试"));
    renderPage();
    const row = await screen.findByTestId("directory-rule-cn_exchange_stock");
    await within(row).findByText("等待执行");
    fireEvent.change(within(row).getByLabelText("cn_exchange_stock更新周期"), { target: { value: "12" } });
    fireEvent.click(within(row).getByRole("button", { name: "保存" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("配置已被其他页面修改");
    expect(within(row).getByLabelText("cn_exchange_stock更新周期")).toHaveValue("12");
  });

  it("uses the asset query passed from the asset detail page", async () => {
    searchParamsMock.value = new URLSearchParams("q=US%7Cus_stock%7Cnasdaq%7CAAPL");
    renderPage();
    await waitFor(() => expect(listMock).toHaveBeenCalledWith(expect.objectContaining({
      targetType: "asset_history",
      q: "US|us_stock|nasdaq|AAPL",
    })));
  });

  it("shows the fixed backward-adjusted history policy", async () => {
    const historyRule = rule({
      id: "aur_601088",
      target_type: "asset_history",
      sync_key: "",
      asset_key: "CN|cn_exchange_stock|sh|601088",
      adjust_policy: "hfq",
      point_type: "adjusted_close",
      target_label: "中国神华",
    });
    listMock.mockImplementation((params: { targetType: string }) =>
      Promise.resolve(page(params.targetType === "asset_history" ? [historyRule] : [])),
    );
    renderPage();
    expect(await screen.findByText("中国神华")).toBeInTheDocument();
    expect(screen.getByText("后复权 · 复权收盘价")).toBeInTheDocument();
  });

  it("paginates asset history rules in pages of 50", async () => {
    listMock.mockImplementation((params: { targetType: string; limit?: number; offset?: number }) => {
      if (params.targetType === "directory_unit") return Promise.resolve(page([]));
      const offset = params.offset ?? 0;
      const count = offset === 100 ? 1 : 50;
      const items = Array.from({ length: count }, (_, index) => rule({
        id: `history-${offset + index}`,
        target_type: "asset_history",
        asset_key: `ASSET-${offset + index}`,
        sync_key: "",
        target_label: `资产 ${offset + index}`,
      }));
      return Promise.resolve(page(items, { total: 101, limit: 50, offset }));
    });
    renderPage();
    expect(await screen.findByText("第 1 / 3 页")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("admin-page-next"));
    expect(await screen.findByText("第 2 / 3 页")).toBeInTheDocument();
    await waitFor(() => expect(listMock).toHaveBeenCalledWith(expect.objectContaining({
      targetType: "asset_history", limit: 50, offset: 50,
    })));
    fireEvent.click(screen.getByTestId("admin-page-next"));
    expect(await screen.findByText("第 3 / 3 页")).toBeInTheDocument();
    expect(await screen.findByText("资产 100")).toBeInTheDocument();
  });

  it("debounces search for 300ms and resets pagination", async () => {
    renderPage();
    await waitFor(() => expect(listMock).toHaveBeenCalledWith(expect.objectContaining({
      targetType: "asset_history", q: "", offset: 0,
    })));
    listMock.mockClear();
    fireEvent.change(screen.getByPlaceholderText("搜索资产代码或名称"), {
      target: { value: "601088" },
    });
    expect(listMock).not.toHaveBeenCalledWith(expect.objectContaining({ q: "601088" }));
    await waitFor(() => expect(listMock).toHaveBeenCalledWith(expect.objectContaining({
      targetType: "asset_history", q: "601088", limit: 50, offset: 0,
    })), { timeout: 1000 });
  });
});
