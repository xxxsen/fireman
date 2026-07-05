// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { vi } from "vitest";

const mockGetHoldings = vi.hoisted(() => vi.fn());
const mockGetOverrides = vi.hoisted(() => vi.fn());
const mockSetOverride = vi.hoisted(() => vi.fn());
const mockDeleteOverride = vi.hoisted(() => vi.fn());

vi.mock("@/lib/api/holdings", () => ({ getHoldings: mockGetHoldings }));
vi.mock("@/lib/api/simulations", () => ({
  getReturnOverrides: mockGetOverrides,
  setReturnOverride: mockSetOverride,
  deleteReturnOverride: mockDeleteOverride,
}));

import { ReturnOverridesCard } from "./ReturnOverridesCard";

function renderCard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <ReturnOverridesCard planId="plan_1" />
    </QueryClientProvider>,
  );
}

const holding = {
  id: "h1",
  plan_id: "plan_1",
  asset_key: "ins_bond",
  enabled: true,
  asset_class: "bond",
  region: "domestic",
  weight_within_group: 1,
  current_amount_minor: 1000,
  simulation_snapshot_id: "s1",
  sort_order: 1,
  instrument_name: "到期债券",
  instrument_code: "B001",
};

describe("ReturnOverridesCard", () => {
  beforeEach(() => {
    mockGetHoldings.mockReset();
    mockGetOverrides.mockReset();
    mockSetOverride.mockReset();
    mockDeleteOverride.mockReset();
    mockGetHoldings.mockResolvedValue({ holdings: [holding] });
  });

  it("does not fetch until expanded, then lists existing overrides", async () => {
    mockGetOverrides.mockResolvedValue({
      overrides: [
        {
          asset_key: "ins_bond",
          forward_return: 0.032,
          annual_volatility: null,
          reason: "持有至到期",
          expires_at: "2030-12-31",
          expired: false,
          created_at: 1,
          updated_at: 1,
        },
      ],
    });

    renderCard();
    expect(mockGetOverrides).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: /展开/ }));

    expect(await screen.findByText("持有至到期")).toBeInTheDocument();
    // The instrument name appears in both the override row and the select option.
    expect(screen.getAllByText("到期债券").length).toBeGreaterThan(0);
    expect(screen.getByText("3.2%")).toBeInTheDocument();
    expect(mockGetHoldings).toHaveBeenCalledWith("plan_1");
  });

  it("validates and submits a new override with decimal forward return", async () => {
    mockGetOverrides.mockResolvedValue({ overrides: [] });
    mockSetOverride.mockResolvedValue({});

    renderCard();
    fireEvent.click(screen.getByRole("button", { name: /展开/ }));
    await screen.findByText("选择标的…");

    // Missing instrument => validation error, no API call.
    fireEvent.click(screen.getByRole("button", { name: "保存覆盖" }));
    expect(await screen.findByText("请选择要覆盖的标的。")).toBeInTheDocument();
    expect(mockSetOverride).not.toHaveBeenCalled();

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "ins_bond" } });
    const numberInputs = screen.getAllByRole("textbox");
    // First textbox is the forward-return percent field.
    fireEvent.change(numberInputs[0], { target: { value: "3.2" } });
    fireEvent.change(screen.getByPlaceholderText(/持有至到期/), {
      target: { value: "锁定到期收益率" },
    });
    // Date inputs aren't exposed as textboxes, so target it directly.
    const dateInput = document.querySelector('input[type="date"]') as HTMLInputElement;
    fireEvent.change(dateInput, { target: { value: "2030-12-31" } });

    fireEvent.click(screen.getByRole("button", { name: "保存覆盖" }));

    await waitFor(() =>
      expect(mockSetOverride).toHaveBeenCalledWith("plan_1", "ins_bond", {
        forward_return: 0.032,
        annual_volatility: null,
        reason: "锁定到期收益率",
        expires_at: "2030-12-31",
      }),
    );
  });
});
