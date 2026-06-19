// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const listPlansMock = vi.hoisted(() => vi.fn());
const downloadBackupMock = vi.hoisted(() => vi.fn());
const restoreBackupMock = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/plans", () => ({
  listPlans: (...args: unknown[]) => listPlansMock(...args),
}));

vi.mock("@/lib/api/system", () => ({
  downloadBackup: (...args: unknown[]) => downloadBackupMock(...args),
  restoreBackup: (...args: unknown[]) => restoreBackupMock(...args),
  exportPlanJsonUrl: (id: string) => `/json/${id}`,
  exportTargetsCsvUrl: (id: string) => `/targets/${id}`,
  exportRebalanceCsvUrl: (id: string) => `/rebalance/${id}`,
}));

import SettingsPage from "./page";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SettingsPage />
    </QueryClientProvider>,
  );
}

describe("SettingsPage", () => {
  beforeEach(() => {
    listPlansMock.mockReset();
    downloadBackupMock.mockReset();
    restoreBackupMock.mockReset();
    listPlansMock.mockResolvedValue([]);
  });

  it("renders header and backup sections", async () => {
    renderPage();
    expect(screen.getByRole("heading", { name: "设置" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "下载备份" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "恢复备份" })).toBeInTheDocument();
  });

  it("confirms before restoring an uploaded backup", async () => {
    restoreBackupMock.mockResolvedValue({ restored: true, restart_required: true });
    renderPage();

    const file = new File(["db"], "backup.db", { type: "application/octet-stream" });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });

    expect(screen.getByRole("dialog")).toHaveTextContent("恢复将替换当前数据库");
    expect(restoreBackupMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("confirm-dialog-confirm"));
    await waitFor(() => expect(restoreBackupMock).toHaveBeenCalledWith(file));
    expect(await screen.findByText(/备份已恢复/)).toBeInTheDocument();
  });

  it("cancels restore without calling the API", () => {
    renderPage();
    const file = new File(["db"], "backup.db", { type: "application/octet-stream" });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [file] } });

    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(restoreBackupMock).not.toHaveBeenCalled();
  });

  it("shows fallback when plans fail to load", async () => {
    listPlansMock.mockRejectedValue(new Error("boom"));
    renderPage();
    expect(await screen.findByText(/无法加载计划列表/)).toBeInTheDocument();
  });
});
