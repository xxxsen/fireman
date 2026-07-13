import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ChartFrame } from "./ChartFrame";

describe("ChartFrame", () => {
  it("renders visible axis semantics, interpretation and a data fallback", () => {
    render(
      <ChartFrame
        title="资产走势"
        xAxis="年龄"
        yAxis="资产金额"
        unit="元"
        interpretation="曲线用于比较名义资产与所需资本。"
        dataTable={<table aria-label="资产数据" />}
      >
        <div data-testid="chart" />
      </ChartFrame>,
    );

    expect(screen.getByText("横轴：年龄 · 纵轴：资产金额（元）")).toBeVisible();
    expect(screen.getByText(/如何解读/)).toHaveTextContent("名义资产");
    fireEvent.click(screen.getByText("查看数据表"));
    expect(screen.getByRole("table", { name: "资产数据" })).toBeVisible();
  });
});
