import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useUnsavedChanges } from "./useUnsavedChanges";

describe("useUnsavedChanges", () => {
  it("confirmLeave returns false when dirty", () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);
    const { result } = renderHook(() => useUnsavedChanges());
    act(() => result.current.markDirty());
    expect(result.current.confirmLeave()).toBe(false);
    expect(confirmSpy).toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it("confirmLeave returns true when clean", () => {
    const { result } = renderHook(() => useUnsavedChanges());
    expect(result.current.confirmLeave()).toBe(true);
  });
});
