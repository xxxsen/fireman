import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Dialog } from "./Dialog";

describe("Dialog", () => {
  it("sets aria-modal and closes on Escape", () => {
    const onClose = vi.fn();
    render(
      <Dialog open title="确认操作" onClose={onClose}>
        <p>内容</p>
      </Dialog>,
    );

    expect(screen.getByTestId("dialog")).toHaveAttribute("aria-modal", "true");
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-labelledby");

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("traps Tab focus within the dialog panel", () => {
    const onClose = vi.fn();
    render(
      <>
        <button type="button">Outside</button>
        <Dialog
          open
          title="确认操作"
          onClose={onClose}
          footer={<button type="button">Footer action</button>}
        >
          <button type="button">Content action</button>
        </Dialog>
      </>,
    );

    const panel = screen.getByTestId("dialog");
    const contentBtn = screen.getByRole("button", { name: "Content action" });
    const footerBtn = screen.getByRole("button", { name: "Footer action" });
    const outsideBtn = screen.getByRole("button", { name: "Outside" });

    contentBtn.focus();
    expect(document.activeElement).toBe(contentBtn);

    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(footerBtn);

    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).not.toBe(outsideBtn);
    expect(panel.contains(document.activeElement)).toBe(true);
    expect(document.activeElement).toBe(contentBtn);

    fireEvent.keyDown(document, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(footerBtn);
  });

  it("skips aria-disabled links when trapping Tab focus", () => {
    render(
      <Dialog open title="确认操作" onClose={vi.fn()}>
        <a href="/enabled">Enabled link</a>
        <a href="/disabled" aria-disabled="true">
          Disabled link
        </a>
        <button type="button">Done</button>
      </Dialog>,
    );

    const enabledLink = screen.getByRole("link", { name: "Enabled link" });
    const doneBtn = screen.getByRole("button", { name: "Done" });

    enabledLink.focus();
    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(doneBtn);

    fireEvent.keyDown(document, { key: "Tab" });
    expect(document.activeElement).toBe(enabledLink);
  });
});
