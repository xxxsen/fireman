import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ErrorState } from "./ErrorState";

describe("ErrorState", () => {
  it("renders message and retry button", () => {
    const onRetry = vi.fn();
    render(<ErrorState message="无法连接后端" onRetry={onRetry} />);
    expect(screen.getByText("无法连接后端")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("error-state-retry"));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("shows back link when provided", () => {
    render(<ErrorState message="失败" backHref="/" backLabel="返回首页" />);
    expect(screen.getByTestId("error-state-back")).toHaveAttribute("href", "/");
  });
});
