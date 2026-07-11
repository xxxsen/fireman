import { describe, expect, it } from "vitest";
import {
  QUICK_FIRE_DEFAULTS,
  QUICK_FIRE_DRAFT_KEY,
  QUICK_FIRE_TRANSFER_KEY,
  consumeQuickFireTransfer,
  loadQuickFireDraft,
  saveQuickFireDraft,
  saveQuickFireTransfer,
} from "./quick-fire-draft";

describe("quick-fire draft storage", () => {
  it("restores a valid versioned draft", () => {
    saveQuickFireDraft(window.localStorage, { ...QUICK_FIRE_DEFAULTS, current_age: 42 });
    expect(loadQuickFireDraft(window.localStorage).current_age).toBe(42);
  });

  it("drops malformed, unknown-version, and out-of-range drafts", () => {
    for (const raw of [
      "not-json",
      JSON.stringify({ version: 2, engine_version: "quick_fire_v1", inputs: QUICK_FIRE_DEFAULTS }),
      JSON.stringify({ version: 1, engine_version: "quick_fire_v1", inputs: { ...QUICK_FIRE_DEFAULTS, annual_return_rate: 2 } }),
    ]) {
      window.localStorage.setItem(QUICK_FIRE_DRAFT_KEY, raw);
      expect(loadQuickFireDraft(window.localStorage)).toEqual(QUICK_FIRE_DEFAULTS);
      expect(window.localStorage.getItem(QUICK_FIRE_DRAFT_KEY)).toBeNull();
    }
  });

  it("transfers only allowed fields and consumes the payload once", () => {
    saveQuickFireTransfer(window.sessionStorage, QUICK_FIRE_DEFAULTS);
    const raw = window.sessionStorage.getItem(QUICK_FIRE_TRANSFER_KEY);
    expect(raw).not.toContain("annual_return_rate");
    expect(consumeQuickFireTransfer(window.sessionStorage)).toEqual(expect.objectContaining({
      current_age: QUICK_FIRE_DEFAULTS.current_age,
      annual_retirement_income_minor: QUICK_FIRE_DEFAULTS.annual_retirement_income_minor,
    }));
    expect(consumeQuickFireTransfer(window.sessionStorage)).toBeNull();
  });
});
