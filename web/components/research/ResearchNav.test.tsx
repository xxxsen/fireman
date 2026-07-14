import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ResearchNav } from "./ResearchNav";

const mockPathname = vi.fn(() => "/research");
const mockConfirmLeave = vi.fn(() => true);

vi.mock("next/navigation", () => ({ usePathname: () => mockPathname() }));
vi.mock("@/lib/unsavedGuard", () => ({
  confirmLeaveIfDirty: () => mockConfirmLeave(),
}));

describe("ResearchNav", () => {
  beforeEach(() => {
    mockPathname.mockReturnValue("/research");
    mockConfirmLeave.mockReturnValue(true);
  });

  it("renders peer entries and highlights portfolio research routes", () => {
    for (const pathname of ["/research", "/research/collections/rc_1/runs/run_1"]) {
      mockPathname.mockReturnValue(pathname);
      const { unmount } = render(<ResearchNav />);
      expect(screen.getByRole("link", { name: "组合研究" })).toHaveAttribute(
        "aria-current",
        "page",
      );
      expect(screen.getByRole("link", { name: "单资产实验" })).not.toHaveAttribute(
        "aria-current",
      );
      unmount();
    }
  });

  it("highlights single-asset list and result routes", () => {
    for (const pathname of [
      "/research/investment-paths",
      "/research/investment-paths/runs/run_1",
    ]) {
      mockPathname.mockReturnValue(pathname);
      const { unmount } = render(<ResearchNav />);
      expect(screen.getByRole("link", { name: "单资产实验" })).toHaveAttribute(
        "aria-current",
        "page",
      );
      expect(screen.getByRole("link", { name: "组合研究" })).not.toHaveAttribute(
        "aria-current",
      );
      unmount();
    }
  });

  it("honors the unsaved-changes guard", () => {
    mockConfirmLeave.mockReturnValue(false);
    render(<ResearchNav />);
    expect(fireEvent.click(screen.getByRole("link", { name: "单资产实验" }))).toBe(false);
    expect(mockConfirmLeave).toHaveBeenCalled();
  });
});
