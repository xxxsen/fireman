// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { vi } from "vitest";
import type { AllocationBar } from "@/types/api";

let capturedOption: {
  tooltip?: { formatter?: (p: unknown) => string };
  xAxis?: { data?: string[] };
} | null = null;

vi.mock("echarts-for-react", () => ({
  default: ({ option }: { option: typeof capturedOption }) => {
    capturedOption = option;
    return <div data-testid="echart" />;
  },
}));

import { AllocationBarChart } from "./AllocationBarChart";

const bars: AllocationBar[] = [
  {
    asset_class: "equity",
    target_weight: 0.6,
    current_weight: 0.55,
    target_amount_minor: 600_000_00,
    current_amount_minor: 550_000_00,
    holdings: [
      {
        instrument_name: "沪深300ETF",
        instrument_code: "510300",
        current_amount_minor: 550_000_00,
        target_amount_minor: 600_000_00,
        current_weight: 0.55,
        target_weight: 0.6,
      },
    ],
  },
  {
    asset_class: "cash",
    target_weight: 0.1,
    current_weight: 0.05,
    target_amount_minor: 0,
    current_amount_minor: 0,
    holdings: [],
  },
];

describe("AllocationBarChart", () => {
  it("orders categories and shows class + holding details in tooltip", () => {
    render(<AllocationBarChart bars={bars} currency="CNY" />);
    expect(capturedOption?.xAxis?.data).toEqual(["权益", "现金/其他"]);

    const tip = capturedOption?.tooltip?.formatter?.([{ dataIndex: 0 }]) ?? "";
    expect(tip).toContain("权益");
    expect(tip).toContain("目标比例 60%");
    expect(tip).toContain("当前比例 55%");
    expect(tip).toContain("目标金额 ¥60.00 万元");
    expect(tip).toContain("沪深300ETF（510300）");
  });

  it("shows empty-detail placeholder for bars without holdings", () => {
    render(<AllocationBarChart bars={bars} currency="CNY" />);
    const tip = capturedOption?.tooltip?.formatter?.([{ dataIndex: 1 }]) ?? "";
    expect(tip).toContain("暂无资产明细");
  });
});
