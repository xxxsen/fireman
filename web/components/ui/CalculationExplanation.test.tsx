import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CalculationExplanation } from "./CalculationExplanation";

describe("CalculationExplanation", () => {
  it("shows the conclusion first and reveals calculation details", () => {
    render(
      <CalculationExplanation
        summary="结论摘要"
        answer="回答什么"
        changed="改变内容"
        fixed="冻结内容"
        data="来源数据"
        criterion="判定方法"
        uncertainty="限制"
        nextStep="下一步"
        audit="engine=v1"
      />,
    );

    expect(screen.getByText("结论摘要")).toBeVisible();
    expect(screen.getByText("回答什么")).not.toBeVisible();
    expect(screen.getByRole("heading", { name: "这次到底计算了什么" })).toBeVisible();
    fireEvent.click(screen.getByText("展开详细口径"));
    expect(screen.getByText("回答什么")).toBeVisible();
    expect(screen.getByText("冻结内容")).toBeVisible();
    expect(screen.getByText("高级计算详情")).toBeVisible();
  });
});
