import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AppShell } from "./AppShell";

const mockPathname = vi.fn(() => "/");

vi.mock("next/navigation", () => ({
  usePathname: () => mockPathname(),
}));

vi.mock("@/lib/unsavedGuard", () => ({
  confirmLeaveIfDirty: () => true,
}));

describe("AppShell", () => {
  it("highlights 计划 on home, new plan, and plan detail routes", () => {
    for (const pathname of ["/", "/plans/new", "/plans/plan_1/overview"]) {
      mockPathname.mockReturnValue(pathname);
      const { unmount } = render(
        <AppShell>
          <div>content</div>
        </AppShell>,
      );

      for (const link of screen.getAllByRole("link", { name: "计划" })) {
        expect(link).toHaveAttribute("aria-current", "page");
      }

      unmount();
    }
  });

  it("does not highlight 计划 on assets or settings", () => {
    for (const pathname of ["/assets", "/settings"]) {
      mockPathname.mockReturnValue(pathname);
      const { unmount } = render(
        <AppShell>
          <div>content</div>
        </AppShell>,
      );

      for (const link of screen.getAllByRole("link", { name: "计划" })) {
        expect(link).not.toHaveAttribute("aria-current", "page");
      }

      unmount();
    }
  });
});
