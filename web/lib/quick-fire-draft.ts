import type { QuickFireInput } from "@/lib/api/quick-fire";

export const QUICK_FIRE_DRAFT_KEY = "fireman.quick-fire.draft.v1";
export const QUICK_FIRE_TRANSFER_KEY = "fireman.plan-wizard.quick-fire-transfer.v1";

export type QuickFireTransferInput = Omit<QuickFireInput, "annual_return_rate">;

export interface QuickFireWizardPatch {
  currentAge: number;
  retirementAge: number;
  fireDurationYears: number;
  totalAssets: number;
  annualSavings: number;
  annualSpending: number;
  annualRetirementIncome: number;
  advanced: {
    annual_savings_growth_rate: number;
    annual_retirement_income_growth_rate: number;
    terminal_wealth_floor_minor: number;
    inflation_mode: "fixed_real";
    fixed_inflation_rate: number;
  };
}

export const QUICK_FIRE_DEFAULTS: QuickFireInput = {
  base_currency: "CNY",
  current_age: 35,
  planned_fire_age: 45,
  end_age: 90,
  current_assets_minor: 300_0000_00,
  annual_savings_minor: 12_0000_00,
  annual_savings_growth_rate: 0,
  annual_spending_minor: 12_0000_00,
  annual_retirement_income_minor: 3_0000_00,
  annual_retirement_income_growth_rate: 0,
  annual_return_rate: 0.04,
  inflation_rate: 0.02,
  terminal_wealth_floor_minor: 0,
};

const numericKeys: (keyof QuickFireInput)[] = [
  "current_age",
  "planned_fire_age",
  "end_age",
  "current_assets_minor",
  "annual_savings_minor",
  "annual_savings_growth_rate",
  "annual_spending_minor",
  "annual_retirement_income_minor",
  "annual_retirement_income_growth_rate",
  "annual_return_rate",
  "inflation_rate",
  "terminal_wealth_floor_minor",
];

export function isQuickFireInput(value: unknown): value is QuickFireInput {
  if (!value || typeof value !== "object") return false;
  const input = value as Record<string, unknown>;
  if (input.base_currency !== "CNY") return false;
  if (!numericKeys.every((key) => typeof input[key] === "number" && Number.isFinite(input[key]))) return false;
  const typed = input as unknown as QuickFireInput;
  return (
    Number.isInteger(typed.current_age) && typed.current_age >= 18 && typed.current_age <= 120 &&
    Number.isInteger(typed.planned_fire_age) && typed.planned_fire_age >= typed.current_age && typed.planned_fire_age < typed.end_age &&
    Number.isInteger(typed.end_age) && typed.end_age > typed.planned_fire_age && typed.end_age <= 120 &&
    typed.current_assets_minor >= 0 && typed.current_assets_minor <= 999_999_999_999_00 &&
    typed.annual_savings_minor >= 0 && typed.annual_savings_minor <= 99_999_999_999_00 &&
    typed.annual_spending_minor > 0 && typed.annual_spending_minor <= 99_999_999_999_00 &&
    typed.annual_retirement_income_minor >= 0 && typed.annual_retirement_income_minor <= 99_999_999_999_00 &&
    typed.terminal_wealth_floor_minor >= 0 && typed.terminal_wealth_floor_minor <= 999_999_999_999_00 &&
    typed.annual_savings_growth_rate >= -0.5 && typed.annual_savings_growth_rate <= 0.5 &&
    typed.annual_retirement_income_growth_rate >= -0.5 && typed.annual_retirement_income_growth_rate <= 0.5 &&
    typed.annual_return_rate >= -0.99 && typed.annual_return_rate <= 1 &&
    typed.inflation_rate >= -0.02 && typed.inflation_rate <= 0.2
  );
}

export function loadQuickFireDraft(storage: Storage | null): QuickFireInput {
  if (!storage) return QUICK_FIRE_DEFAULTS;
  try {
    const raw = storage.getItem(QUICK_FIRE_DRAFT_KEY);
    if (!raw) return QUICK_FIRE_DEFAULTS;
    const parsed = JSON.parse(raw) as { version?: unknown; engine_version?: unknown; inputs?: unknown };
    if (parsed.version !== 1 || parsed.engine_version !== "quick_fire_v1" || !isQuickFireInput(parsed.inputs)) {
      storage.removeItem(QUICK_FIRE_DRAFT_KEY);
      return QUICK_FIRE_DEFAULTS;
    }
    return parsed.inputs;
  } catch {
    storage.removeItem(QUICK_FIRE_DRAFT_KEY);
    return QUICK_FIRE_DEFAULTS;
  }
}

export function saveQuickFireDraft(storage: Storage | null, input: QuickFireInput): void {
  storage?.setItem(QUICK_FIRE_DRAFT_KEY, JSON.stringify({ version: 1, engine_version: "quick_fire_v1", inputs: input }));
}

export function clearQuickFireDraft(storage: Storage | null): void {
  storage?.removeItem(QUICK_FIRE_DRAFT_KEY);
}

export function saveQuickFireTransfer(storage: Storage | null, input: QuickFireInput): void {
  const { annual_return_rate, ...transfer } = input;
  void annual_return_rate;
  storage?.setItem(QUICK_FIRE_TRANSFER_KEY, JSON.stringify({ version: 1, engine_version: "quick_fire_v1", inputs: transfer }));
}

export function readQuickFireTransfer(storage: Storage | null): QuickFireTransferInput | null {
  if (!storage) return null;
  const raw = storage.getItem(QUICK_FIRE_TRANSFER_KEY);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as { version?: unknown; engine_version?: unknown; inputs?: unknown };
    if (parsed.version !== 1 || parsed.engine_version !== "quick_fire_v1" || !parsed.inputs || typeof parsed.inputs !== "object") return null;
    const candidate = parsed.inputs as Record<string, unknown>;
    const input = { ...candidate, annual_return_rate: 0 };
    if (!isQuickFireInput(input)) return null;
    return {
      base_currency: input.base_currency,
      current_age: input.current_age,
      planned_fire_age: input.planned_fire_age,
      end_age: input.end_age,
      current_assets_minor: input.current_assets_minor,
      annual_savings_minor: input.annual_savings_minor,
      annual_savings_growth_rate: input.annual_savings_growth_rate,
      annual_spending_minor: input.annual_spending_minor,
      annual_retirement_income_minor: input.annual_retirement_income_minor,
      annual_retirement_income_growth_rate: input.annual_retirement_income_growth_rate,
      inflation_rate: input.inflation_rate,
      terminal_wealth_floor_minor: input.terminal_wealth_floor_minor,
    };
  } catch {
    return null;
  }
}

export function clearQuickFireTransfer(storage: Storage | null): void {
  storage?.removeItem(QUICK_FIRE_TRANSFER_KEY);
}

export function quickFireTransferToWizardPatch(input: QuickFireTransferInput): QuickFireWizardPatch {
  return {
    currentAge: input.current_age,
    retirementAge: input.planned_fire_age,
    fireDurationYears: input.end_age - input.planned_fire_age,
    totalAssets: input.current_assets_minor,
    annualSavings: input.annual_savings_minor,
    annualSpending: input.annual_spending_minor,
    annualRetirementIncome: input.annual_retirement_income_minor,
    advanced: {
      annual_savings_growth_rate: input.annual_savings_growth_rate,
      annual_retirement_income_growth_rate: input.annual_retirement_income_growth_rate,
      terminal_wealth_floor_minor: input.terminal_wealth_floor_minor,
      inflation_mode: "fixed_real",
      fixed_inflation_rate: input.inflation_rate,
    },
  };
}
