// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

interface CapturedAxis {
  name?: string;
  min?: number;
  max?: number;
}
interface CapturedOption {
  xAxis?: CapturedAxis;
  yAxis?: CapturedAxis;
  tooltip?: {
    formatter?: (params: Array<{ axisValue?: string; name?: string; value: number }>) => string;
  };
}

let capturedOption: CapturedOption | null = null;
let capturedHeight: number | undefined;

vi.mock("echarts-for-react", () => ({
  default: ({ option, style }: { option: CapturedOption; style?: { height?: number } }) => {
    capturedOption = option;
    capturedHeight = style?.height;
    return <div data-testid="echart" />;
  },
}));

import { ParameterCurvesChart, SensitivityHeatmap } from "./SensitivityCharts";

describe("SensitivityCharts", () => {
  beforeEach(() => {
    capturedOption = null;
    capturedHeight = undefined;
  });

  it("renders parameter curves with labeled axes, a fixed 0-1 range and a taller height", () => {
    render(
      <ParameterCurvesChart
        curves={[
          {
            parameter_name: "通胀率",
            points: [
              { label: "-10%", success_probability: 0.8 },
              { label: "基准", success_probability: 0.9 },
              { label: "+10%", success_probability: 0.85 },
            ],
          },
        ]}
      />,
    );

    expect(capturedHeight).toBe(280);
    expect(capturedOption?.yAxis?.min).toBe(0);
    expect(capturedOption?.yAxis?.max).toBe(1);
    expect(capturedOption?.yAxis?.name).toBe("成功率");
    expect(capturedOption?.xAxis?.name).toBe("参数扰动");

    const html = capturedOption?.tooltip?.formatter?.([{ axisValue: "+10%", value: 0.85 }]) ?? "";
    expect(html).toContain("成功率：85%");
    expect(html).toContain("相对基准：-5%");
  });

  it("renders the heatmap with named axes", () => {
    render(
      <SensitivityHeatmap
        heatmap={[[{ spending_label: "低", return_label: "低", success_probability: 0.5 }]]}
      />,
    );
    expect(capturedOption?.xAxis?.name).toBe("支出扰动");
    expect(capturedOption?.yAxis?.name).toBe("收益扰动");
  });
});
