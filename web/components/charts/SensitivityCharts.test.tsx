// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { vi } from "vitest";

vi.mock("echarts-for-react", () => ({
  default: ({ option }: { option: { series?: { name: string }[] } }) => (
    <div data-testid="echart">{option.series?.map((s) => s.name).join(",")}</div>
  ),
}));

import {
  ParameterCurvesChart,
  SensitivityHeatmap,
  TornadoChart,
} from "./SensitivityCharts";

describe("SensitivityCharts", () => {
  it("renders tornado, curves and heatmap", () => {
    render(
      <>
        <TornadoChart
          items={[{ parameter_name: "withdrawal_rate", low_success: 0.4, high_success: 0.6 }]}
        />
        <ParameterCurvesChart
          curves={[
            {
              parameter_name: "retirement_age",
              points: [
                { label: "50", success_probability: 0.5 },
                { label: "55", success_probability: 0.6 },
              ],
            },
          ]}
        />
        <SensitivityHeatmap
          heatmap={[
            [
              { spending_label: "低", return_label: "低", success_probability: 0.3 },
              { spending_label: "高", return_label: "低", success_probability: 0.2 },
            ],
            [
              { spending_label: "低", return_label: "高", success_probability: 0.7 },
              { spending_label: "高", return_label: "高", success_probability: 0.6 },
            ],
          ]}
        />
      </>,
    );

    const charts = screen.getAllByTestId("echart");
    expect(charts.length).toBeGreaterThanOrEqual(3);
    expect(screen.getByText("低扰动,高扰动")).toBeInTheDocument();
    expect(screen.getByText("retirement_age")).toBeInTheDocument();
  });
});
