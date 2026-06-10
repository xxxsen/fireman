import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ImportAssetPage from "./page";

const resolveImportMock = vi.fn();
const importAsyncMock = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

vi.mock("@/lib/api/instruments", () => ({
  resolveImport: (...args: unknown[]) => resolveImportMock(...args),
  importAsync: (...args: unknown[]) => importAsyncMock(...args),
  candidateIdentity: (candidate: {
    candidate_id?: string;
    ticket_id?: string;
    code: string;
    provider_symbol: string;
    instrument_kind: string;
    exchange: string;
  }) =>
    candidate.candidate_id ??
    candidate.ticket_id ??
    `${candidate.code}|${candidate.provider_symbol}|${candidate.instrument_kind}|${candidate.exchange}`,
  isSameCandidate: (
    a: { candidate_id?: string; ticket_id?: string; code: string; provider_symbol: string; instrument_kind: string; exchange: string } | null,
    b: { candidate_id?: string; ticket_id?: string; code: string; provider_symbol: string; instrument_kind: string; exchange: string } | null,
  ) => {
    if (!a || !b) return false;
    const id = (c: typeof a) =>
      c.candidate_id ??
      c.ticket_id ??
      `${c.code}|${c.provider_symbol}|${c.instrument_kind}|${c.exchange}`;
    return id(a) === id(b);
  },
}));

describe("ImportAssetPage", () => {
  it("exposes CN, HK and US markets", () => {
    render(<ImportAssetPage />);
    expect(screen.getByRole("option", { name: "中国市场" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "香港市场" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "美国市场" })).toBeInTheDocument();
  });

  it("only exposes market, type and code inputs in search stage", () => {
    render(<ImportAssetPage />);
    expect(screen.getByRole("heading", { name: "1. 解析标的" })).toBeInTheDocument();
    expect(screen.getByText("市场")).toBeInTheDocument();
    expect(screen.getByText("标的类型")).toBeInTheDocument();
    expect(screen.getByText("代码")).toBeInTheDocument();
  });

  it("shows disambiguate stage when resolve is ambiguous", async () => {
    resolveImportMock.mockResolvedValueOnce({
      ambiguous: true,
      candidates: [
        {
          code: "sh000510",
          provider_symbol: "sh000510",
          name: "中证A500",
          exchange: "SH",
          instrument_kind: "index_etf",
          candidate_id: "tkt_etf",
          is_importable: true,
          ticket_id: "tkt_etf",
        },
        {
          code: "sz000510",
          provider_symbol: "sz000510",
          name: "新金路",
          exchange: "SZ",
          instrument_kind: "stock",
          candidate_id: "sz000510|sz000510|stock|SZ",
          is_importable: false,
        },
      ],
    });
    render(<ImportAssetPage />);
    fireEvent.change(screen.getByPlaceholderText(/510300/), { target: { value: "000510" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("2. 选择真实标的")).toBeInTheDocument();
    expect(screen.getByText("sh000510")).toBeInTheDocument();
    expect(screen.getByText("sz000510")).toBeInTheDocument();
    expect(screen.getByTestId("candidate-tkt_etf")).toHaveAttribute("data-compatible", "true");
    expect(screen.getByTestId("candidate-sz000510|sz000510|stock|SZ")).toHaveAttribute(
      "data-compatible",
      "false",
    );
    expect(screen.getByTestId("candidate-sz000510|sz000510|stock|SZ").querySelector("input")).toBeDisabled();
  });

  it("disables etf candidates when instrument type is cn_exchange_stock", async () => {
    resolveImportMock.mockResolvedValueOnce({
      ambiguous: true,
      candidates: [
        {
          code: "sh000510",
          provider_symbol: "sh000510",
          name: "中证A500",
          exchange: "SH",
          instrument_kind: "index_etf",
          candidate_id: "sh000510|sh000510|index_etf|SH",
          is_importable: false,
        },
        {
          code: "sz000510",
          provider_symbol: "sz000510",
          name: "新金路",
          exchange: "SZ",
          instrument_kind: "stock",
          candidate_id: "tkt_stock",
          is_importable: true,
          ticket_id: "tkt_stock",
        },
      ],
    });
    render(<ImportAssetPage />);
    const selects = screen.getAllByRole("combobox");
    fireEvent.change(selects[1], { target: { value: "cn_exchange_stock" } });
    fireEvent.change(screen.getByPlaceholderText(/600519/), { target: { value: "000510" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("2. 选择真实标的")).toBeInTheDocument();
    expect(screen.getByTestId("candidate-sh000510|sh000510|index_etf|SH")).toHaveAttribute(
      "data-compatible",
      "false",
    );
    expect(screen.getByTestId("candidate-tkt_stock")).toHaveAttribute("data-compatible", "true");
    expect(screen.getByTestId("candidate-sh000510|sh000510|index_etf|SH").querySelector("input")).toBeDisabled();
  });

  it("goes to confirm on unambiguous resolve", async () => {
    resolveImportMock.mockResolvedValueOnce({
      ambiguous: false,
      resolved: {
        code: "sh510300",
        provider_symbol: "sh510300",
        name: "沪深300ETF",
        exchange: "SH",
        instrument_kind: "etf",
        candidate_id: "tkt_test",
        ticket_id: "tkt_test",
      },
    });
    render(<ImportAssetPage />);
    fireEvent.change(screen.getByPlaceholderText(/510300/), { target: { value: "510300" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("3. 确认并开始抓取")).toBeInTheDocument();
    expect(screen.getByText("沪深300ETF")).toBeInTheDocument();
  });

  it("selects ETF vs LOF precisely when code is duplicated", async () => {
    resolveImportMock.mockResolvedValueOnce({
      ambiguous: true,
      candidates: [
        {
          code: "sz150001",
          provider_symbol: "sz150001",
          name: "测试ETF",
          exchange: "SZ",
          instrument_kind: "etf",
          candidate_id: "tkt_etf",
          is_importable: true,
          ticket_id: "tkt_etf",
        },
        {
          code: "sz150001",
          provider_symbol: "sz150001",
          name: "测试LOF",
          exchange: "SZ",
          instrument_kind: "lof",
          candidate_id: "tkt_lof",
          is_importable: true,
          ticket_id: "tkt_lof",
        },
      ],
    });
    importAsyncMock.mockResolvedValueOnce({
      instrument_id: "ins_test",
      job_id: "job_test",
      status: "pending_fetch",
    });

    render(<ImportAssetPage />);
    fireEvent.change(screen.getByPlaceholderText(/510300/), { target: { value: "150001" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("2. 选择真实标的")).toBeInTheDocument();

    const etfRow = screen.getByTestId("candidate-tkt_etf");
    const lofRow = screen.getByTestId("candidate-tkt_lof");
    expect(etfRow).toBeInTheDocument();
    expect(lofRow).toBeInTheDocument();
    expect(screen.getAllByText("sz150001")).toHaveLength(2);

    const etfRadio = etfRow.querySelector("input") as HTMLInputElement;
    const lofRadio = lofRow.querySelector("input") as HTMLInputElement;
    expect(etfRadio.checked).toBe(true);
    expect(lofRadio.checked).toBe(false);

    fireEvent.click(lofRadio);
    expect(etfRadio.checked).toBe(false);
    expect(lofRadio.checked).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "下一步" }));
    expect(await screen.findByText("3. 确认并开始抓取")).toBeInTheDocument();
    expect(screen.getByText("测试LOF")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("confirm-import"));
    expect(importAsyncMock).toHaveBeenCalledWith({ ticket_id: "tkt_lof" });
  });
});
