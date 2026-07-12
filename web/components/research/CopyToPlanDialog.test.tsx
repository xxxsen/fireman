import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/client";
import type {
  ResearchCollectionDetail,
  ResearchPlanReplacementPreview,
} from "@/lib/api/research";
import { CopyToPlanDialog } from "./CopyToPlanDialog";

const listPlansMock = vi.hoisted(() => vi.fn());
const previewMock = vi.hoisted(() => vi.fn());
const applyMock = vi.hoisted(() => vi.fn());
const updateItemMock = vi.hoisted(() => vi.fn());
const pushMock = vi.hoisted(() => vi.fn());

vi.mock("next/navigation", () => ({ useRouter: () => ({ push: pushMock }) }));
vi.mock("@/lib/api/plans", () => ({ listPlans: () => listPlansMock() }));
vi.mock("@/lib/api/research", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/research")>()),
  previewPlanReplacement: (...args: unknown[]) => previewMock(...args),
  applyPlanReplacement: (...args: unknown[]) => applyMock(...args),
  updateCollectionItem: (...args: unknown[]) => updateItemMock(...args),
}));

const detail = {
  id: "rc_1",
  name: "退休组合",
  base_currency: "CNY",
  items: [{
    id: "item_1", asset_key: "ASSET_1", name: "资产一", symbol: "A1",
    asset_class: "equity", region: "domestic",
  }],
} as ResearchCollectionDetail;

const preview: ResearchPlanReplacementPreview = {
  plan_id: "plan_1",
  plan_name: "人生计划",
  collection_id: "rc_1",
  base_currency: "CNY",
  target_total_assets_minor: 100_000_000,
  expected_config_version: 7,
  replacement_hash: "replacement-hash",
  before_holding_count: 2,
  after_holding_count: 1,
  existing_holdings_will_change: true,
  rounding_adjustment_minor: 0,
  allocation: {
    asset_class_targets: [
      { asset_class: "equity", weight: 1 },
      { asset_class: "bond", weight: 0 },
      { asset_class: "cash", weight: 0 },
    ],
    region_targets: [],
  },
  holdings: [{
    asset_key: "ASSET_1", name: "资产一", symbol: "A1", weight: 1,
    asset_class: "equity", region: "domestic", weight_within_group: 1,
    current_amount_minor: 100_000_000,
  }],
  removed_holdings: [{ asset_key: "OLD", name: "旧资产", symbol: "OLD" }],
  warnings: ["现有目标配置和全部持仓将被完整替换"],
};

function renderDialog() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <CopyToPlanDialog open onClose={vi.fn()} detail={detail} />
    </QueryClientProvider>,
  );
}

describe("CopyToPlanDialog", () => {
  beforeEach(() => {
    listPlansMock.mockReset();
    previewMock.mockReset();
    applyMock.mockReset();
    updateItemMock.mockReset();
    pushMock.mockReset();
    listPlansMock.mockResolvedValue([{
      id: "plan_1", name: "人生计划", base_currency: "CNY",
      valuation_date: "2026-07-12", config_version: 7,
    }]);
    previewMock.mockResolvedValue(preview);
    applyMock.mockResolvedValue({
      plan_id: "plan_1", collection_id: "rc_1", config_version: 8,
      holding_count: 1, portfolio_snapshot_id: "psnap_1",
    });
  });

  it("previews before applying and sends the frozen version and hash", async () => {
    renderDialog();
    fireEvent.click(await screen.findByRole("radio", { name: /人生计划/ }));
    fireEvent.click(screen.getByTestId("preview-plan-replacement"));
    expect(await screen.findByTestId("replacement-preview")).toBeInTheDocument();
    expect(screen.getByText(/完整替换计划当前的目标配置和全部持仓/)).toBeInTheDocument();
    expect(screen.getByText("旧资产")).toBeInTheDocument();
    expect(applyMock).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("apply-plan-replacement"));
    await waitFor(() => expect(applyMock).toHaveBeenCalledWith("rc_1", {
      plan_id: "plan_1",
      expected_config_version: 7,
      expected_replacement_hash: "replacement-hash",
      mode: "replace_all",
    }));
    expect(await screen.findByTestId("apply-result")).toHaveTextContent("计划配置版本为 8");
    fireEvent.click(screen.getByTestId("goto-plan-overview"));
    expect(pushMock).toHaveBeenCalledWith("/plans/plan_1/overview?source=research");
  });

  it("discards a stale preview after a plan version conflict", async () => {
    applyMock.mockRejectedValueOnce(new ApiError(
      "plan_config_conflict", "plan configuration version mismatch", undefined, 409,
    ));
    renderDialog();
    fireEvent.click(await screen.findByRole("radio", { name: /人生计划/ }));
    fireEvent.click(screen.getByTestId("preview-plan-replacement"));
    fireEvent.click(await screen.findByTestId("apply-plan-replacement"));
    expect(await screen.findByRole("alert")).toHaveTextContent("重新生成预览");
    expect(screen.queryByTestId("replacement-preview")).not.toBeInTheDocument();
  });
});
