// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";
import { AssetClassHoldingPicker } from "./AssetClassHoldingPicker";

const resolveImport = vi.fn();
const importAsync = vi.fn();
const getFetchStatus = vi.fn();
const getInstrument = vi.fn();

vi.mock("@/lib/api/instruments", () => ({
  resolveImport: (...args: unknown[]) => resolveImport(...args),
  importAsync: (...args: unknown[]) => importAsync(...args),
  getFetchStatus: (...args: unknown[]) => getFetchStatus(...args),
  getInstrument: (...args: unknown[]) => getInstrument(...args),
}));

const baseInstrument = {
  id: "ins_1",
  code: "T1",
  name: "资料库基金",
  market: "CN",
  instrument_type: "cn_mutual_fund",
  asset_class: "equity",
  region: "domestic",
  currency: "CNY",
  provider: "akshare",
  is_system: false,
  status: "active",
  simulation_eligible: true,
  expense_ratio_status: "unknown",
  fee_treatment: "net",
  data_stale: false,
  created_at: 0,
  updated_at: 0,
};

function renderPicker(instruments = [baseInstrument]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const onSelectedChange = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <AssetClassHoldingPicker
        assetClass="equity"
        classWeight={1}
        regionWeight={1}
        region="domestic"
        totalAssetsMinor={1_000_000}
        instruments={instruments}
        selected={[]}
        onSelectedChange={onSelectedChange}
      />
    </QueryClientProvider>,
  );
  return { onSelectedChange, client };
}

describe("AssetClassHoldingPicker", () => {
  beforeEach(() => {
    resolveImport.mockReset();
    importAsync.mockReset();
    getFetchStatus.mockReset();
    getInstrument.mockReset();
  });

  it("queries AKShare when library has no exact code match", async () => {
    resolveImport.mockResolvedValueOnce({
      ambiguous: false,
      resolved: {
        code: "270042",
        provider_symbol: "270042",
        name: "广发纳指100ETF联接（QDII）人民币A",
        exchange: "",
        instrument_kind: "mutual_fund",
        ticket_id: "ticket_1",
        is_importable: true,
      },
    });
    renderPicker();

    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });

    await waitFor(() => {
      expect(resolveImport).toHaveBeenCalledWith({
        market: "CN",
        instrument_type: "cn_exchange_fund",
        code: "270042",
      });
    });

    expect(await screen.findByTestId("wizard-external-results")).toBeInTheDocument();
    expect(screen.getByText(/资料库未收录/)).toBeInTheDocument();
  });

  it("imports external candidate and adds instrument to selection", async () => {
    resolveImport.mockResolvedValueOnce({
      ambiguous: false,
      resolved: {
        code: "270042",
        provider_symbol: "270042",
        name: "广发纳指100ETF联接（QDII）人民币A",
        exchange: "",
        instrument_kind: "mutual_fund",
        ticket_id: "ticket_1",
        is_importable: true,
      },
    });
    importAsync.mockResolvedValueOnce({ instrument_id: "ins_new", job_id: "job_1", status: "queued" });
    getFetchStatus.mockResolvedValueOnce({ instrument_status: "active", progress_current: 1, progress_total: 1 });
    getInstrument.mockResolvedValueOnce({
      ...baseInstrument,
      id: "ins_new",
      code: "270042",
      name: "广发纳指100ETF联接（QDII）人民币A",
    });

    const { onSelectedChange } = renderPicker();

    fireEvent.change(screen.getByTestId("wizard-holding-search"), {
      target: { value: "270042" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /点击录入并添加/ }));

    await waitFor(() => {
      expect(importAsync).toHaveBeenCalledWith({
        ticket_id: "ticket_1",
        asset_class: "equity",
        region: "domestic",
      });
    });
    await waitFor(() => {
      expect(onSelectedChange).toHaveBeenCalled();
    });
  });
});
