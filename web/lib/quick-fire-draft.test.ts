import { describe, expect, it } from "vitest";
import {
  QUICK_FIRE_DEFAULTS,
  QUICK_FIRE_DRAFT_KEY,
  QUICK_FIRE_TRANSFER_KEY,
  clearQuickFireTransfer,
  loadQuickFireDraft,
  quickFireTransferToWizardPatch,
  readQuickFireTransfer,
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

  it("reads transfer without deleting it and clears only on acknowledgement", () => {
    saveQuickFireTransfer(window.sessionStorage, QUICK_FIRE_DEFAULTS);
    const raw = window.sessionStorage.getItem(QUICK_FIRE_TRANSFER_KEY);
    expect(raw).not.toContain("annual_return_rate");
    expect(readQuickFireTransfer(window.sessionStorage)).toEqual(expect.objectContaining({
      current_age: QUICK_FIRE_DEFAULTS.current_age,
      annual_retirement_income_minor: QUICK_FIRE_DEFAULTS.annual_retirement_income_minor,
    }));
    expect(readQuickFireTransfer(window.sessionStorage)).not.toBeNull();
    expect(window.sessionStorage.getItem(QUICK_FIRE_TRANSFER_KEY)).toBe(raw);
    clearQuickFireTransfer(window.sessionStorage);
    expect(readQuickFireTransfer(window.sessionStorage)).toBeNull();
  });

  it("maps every documented wizard field and excludes manual return", () => {
    const input = {
      ...QUICK_FIRE_DEFAULTS,
      current_age: 41,
      planned_fire_age: 49,
      end_age: 91,
      current_assets_minor: 543_210_00,
      annual_savings_minor: 123_400_00,
      annual_savings_growth_rate: 0.03,
      annual_spending_minor: 87_600_00,
      annual_retirement_income_minor: 24_000_00,
      annual_retirement_income_growth_rate: 0.01,
      inflation_rate: 0.025,
      terminal_wealth_floor_minor: 10_000_00,
    };
    saveQuickFireTransfer(window.sessionStorage, input);
    const transfer = readQuickFireTransfer(window.sessionStorage);
    expect(transfer).not.toBeNull();
    const patch = quickFireTransferToWizardPatch(transfer!);
    expect(patch).toEqual({
      currentAge: 41,
      retirementAge: 49,
      fireDurationYears: 42,
      totalAssets: 543_210_00,
      annualSavings: 123_400_00,
      annualSpending: 87_600_00,
      annualRetirementIncome: 24_000_00,
      advanced: {
        annual_savings_growth_rate: 0.03,
        annual_retirement_income_growth_rate: 0.01,
        terminal_wealth_floor_minor: 10_000_00,
        inflation_mode: "fixed_real",
        fixed_inflation_rate: 0.025,
      },
    });
    expect(JSON.stringify(patch)).not.toContain("annual_return_rate");
  });
});
