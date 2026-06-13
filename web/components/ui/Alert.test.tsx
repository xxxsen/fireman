import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Alert } from "./Alert";

describe("Alert", () => {
  it("renders children with alert role", () => {
    render(<Alert>保存成功</Alert>);
    expect(screen.getByRole("alert")).toHaveTextContent("保存成功");
  });

  it("renders optional title", () => {
    render(
      <Alert variant="warning" title="注意">
        请确认后再提交
      </Alert>,
    );
    expect(screen.getByText("注意")).toBeInTheDocument();
    expect(screen.getByText("请确认后再提交")).toBeInTheDocument();
  });
});
