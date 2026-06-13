import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Button } from "./Button";

describe("Button", () => {
  it("renders primary variant", () => {
    render(<Button>保存</Button>);
    expect(screen.getByRole("button", { name: "保存" })).toBeInTheDocument();
  });

  it("shows pending label and disables interaction", () => {
    render(<Button pending>提交</Button>);
    const btn = screen.getByRole("button");
    expect(btn).toHaveTextContent("处理中…");
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
  });

  it("respects disabled state", () => {
    render(
      <Button disabled variant="secondary">
        取消
      </Button>,
    );
    expect(screen.getByRole("button", { name: "取消" })).toBeDisabled();
  });

  it("renders as link when href is provided", () => {
    render(<Button href="/plans/new">新建</Button>);
    expect(screen.getByRole("link", { name: "新建" })).toHaveAttribute("href", "/plans/new");
  });

  it("disabled link is not tab focusable", () => {
    render(
      <>
        <button type="button">Before</button>
        <Button href="/plans/new" disabled>
          新建
        </Button>
        <button type="button">After</button>
      </>,
    );
    const link = screen.getByRole("link", { name: "新建" });
    expect(link).toHaveAttribute("tabindex", "-1");
    expect(link).toHaveAttribute("aria-disabled", "true");
  });

  it("calls onClick for button variant", () => {
    const onClick = vi.fn();
    render(<Button onClick={onClick}>点击</Button>);
    fireEvent.click(screen.getByRole("button"));
    expect(onClick).toHaveBeenCalledOnce();
  });
});
