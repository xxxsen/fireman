import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ConfirmDialog } from "./ConfirmDialog";

describe("ConfirmDialog", () => {
  it("renders title, description and triggers confirm", () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <ConfirmDialog
        open
        title="删除场景"
        description="此操作不可撤销。"
        confirmLabel="删除场景"
        variant="danger"
        onConfirm={onConfirm}
        onClose={onClose}
      />,
    );

    expect(screen.getByRole("dialog")).toHaveTextContent("删除场景");
    expect(screen.getByText("此操作不可撤销。")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("confirm-dialog-confirm"));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("calls onClose from cancel button", () => {
    const onClose = vi.fn();
    render(
      <ConfirmDialog open title="确认" onConfirm={vi.fn()} onClose={onClose} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("disables actions and shows pending label while pending", () => {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <ConfirmDialog
        open
        title="确认"
        pending
        onConfirm={onConfirm}
        onClose={onClose}
      />,
    );

    const confirm = screen.getByTestId("confirm-dialog-confirm");
    expect(confirm).toHaveTextContent("处理中…");
    expect(confirm).toBeDisabled();

    const cancel = screen.getByRole("button", { name: "取消" });
    expect(cancel).toBeDisabled();

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).not.toHaveBeenCalled();
  });

  it("renders error message", () => {
    render(
      <ConfirmDialog
        open
        title="确认"
        error="删除失败"
        onConfirm={vi.fn()}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("删除失败");
  });

  it("renders nothing when closed", () => {
    render(<ConfirmDialog open={false} title="确认" onConfirm={vi.fn()} onClose={vi.fn()} />);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
});
