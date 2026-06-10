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
        },
        {
          code: "sz000510",
          provider_symbol: "sz000510",
          name: "新金路",
          exchange: "SZ",
          instrument_kind: "stock",
        },
      ],
    });
    render(<ImportAssetPage />);
    fireEvent.change(screen.getByPlaceholderText(/510300/), { target: { value: "000510" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("2. 选择真实标的")).toBeInTheDocument();
    expect(screen.getByText("sh000510")).toBeInTheDocument();
    expect(screen.getByText("sz000510")).toBeInTheDocument();
    expect(screen.getByTestId("candidate-sh000510")).toHaveAttribute("data-compatible", "true");
    expect(screen.getByTestId("candidate-sz000510")).toHaveAttribute("data-compatible", "false");
    expect(screen.getByTestId("candidate-sz000510").querySelector("input")).toBeDisabled();
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
        },
        {
          code: "sz000510",
          provider_symbol: "sz000510",
          name: "新金路",
          exchange: "SZ",
          instrument_kind: "stock",
        },
      ],
    });
    render(<ImportAssetPage />);
    const selects = screen.getAllByRole("combobox");
    fireEvent.change(selects[1], { target: { value: "cn_exchange_stock" } });
    fireEvent.change(screen.getByPlaceholderText(/600519/), { target: { value: "000510" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("2. 选择真实标的")).toBeInTheDocument();
    expect(screen.getByTestId("candidate-sh000510")).toHaveAttribute("data-compatible", "false");
    expect(screen.getByTestId("candidate-sz000510")).toHaveAttribute("data-compatible", "true");
    expect(screen.getByTestId("candidate-sh000510").querySelector("input")).toBeDisabled();
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
      },
    });
    render(<ImportAssetPage />);
    fireEvent.change(screen.getByPlaceholderText(/510300/), { target: { value: "510300" } });
    fireEvent.click(screen.getByTestId("resolve-button"));
    expect(await screen.findByText("3. 确认并开始抓取")).toBeInTheDocument();
    expect(screen.getByText("沪深300ETF")).toBeInTheDocument();
  });
});
