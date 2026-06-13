import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Drawer } from "./Drawer";

describe("Drawer", () => {
  it("sets aria-modal and closes on Escape", () => {
    const onClose = vi.fn();
    render(
      <Drawer open title="详情" onClose={onClose}>
        <p>内容</p>
      </Drawer>,
    );

    expect(screen.getByTestId("drawer")).toHaveAttribute("aria-modal", "true");
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-labelledby");

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("traps Tab focus within the drawer panel", () => {
    const onClose = vi.fn();
    render(
      <>
        <button type="button">Outside</button>
        <Drawer open title="详情" onClose={onClose}>
          <button type="button">Content action</button>
        </Drawer>
      </>,
    );

    const panel = screen.getByTestId("drawer");
    const closeBtn = screen.getByRole("button", { name: "关闭" });
    const contentBtn = screen.getByRole("button", { name: "Content action" });
    const outsideBtn = screen.getByRole("button", { name: "Outside" });

    closeBtn.focus();
    expect(document.activeElement).toBe(closeBtn);

    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(contentBtn);

    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).not.toBe(outsideBtn);
    expect(panel.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(closeBtn);

    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(contentBtn);
  });
});
