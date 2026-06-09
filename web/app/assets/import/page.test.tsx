import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ImportAssetPage from "./page";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

describe("ImportAssetPage", () => {
  it("only exposes market, type and code inputs in search stage", () => {
    render(<ImportAssetPage />);
    expect(screen.getByText(/查询标的/)).toBeInTheDocument();
    expect(screen.getByText("市场")).toBeInTheDocument();
    expect(screen.getByText("标的类型")).toBeInTheDocument();
    expect(screen.getByText("代码")).toBeInTheDocument();
    expect(screen.queryByLabelText(/名称/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/年化收益/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/波动率/)).not.toBeInTheDocument();
  });
});
