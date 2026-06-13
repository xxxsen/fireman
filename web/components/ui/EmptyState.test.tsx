import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  it("renders title and primary action link", () => {
    render(
      <EmptyState
        title="还没有 FIRE 计划"
        description="创建第一个计划"
        action={{ label: "新建计划", href: "/plans/new" }}
      />,
    );
    expect(screen.getByText("还没有 FIRE 计划")).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-action")).toHaveAttribute("href", "/plans/new");
  });
});
