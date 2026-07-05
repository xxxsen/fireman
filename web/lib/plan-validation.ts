/**
 * Shared client-side validation for core plan parameters, mirroring the
 * backend rules `0 < current_age <= retirement_age < end_age <= 120` and
 * "total_assets / annual_spending must be > 0" so invalid values are rejected
 * with a Chinese message before submit instead of a server-side English 400.
 *
 * Used by both the new-plan wizard (which edits a FIRE duration) and the plan
 * settings parameters form (which edits the end age directly); the messages
 * follow whichever field the surface actually exposes.
 */

export interface ValidationResult {
  ok: boolean;
  message?: string;
}

const ok: ValidationResult = { ok: true };

function fail(message: string): ValidationResult {
  return { ok: false, message };
}

export function validateAges(input: {
  currentAge: number;
  retirementAge: number;
  /** Wizard surface: end age is retirementAge + fireDurationYears. */
  fireDurationYears?: number;
  /** Parameters surface: end age edited directly. */
  endAge?: number;
}): ValidationResult {
  const { currentAge, retirementAge, fireDurationYears, endAge } = input;
  if (!Number.isInteger(currentAge) || currentAge <= 0) {
    return fail("当前年龄需为大于 0 的整数。");
  }
  if (!Number.isInteger(retirementAge)) {
    return fail("退休年龄需为整数。");
  }
  if (retirementAge < currentAge) {
    return fail("退休年龄不能小于当前年龄。");
  }
  if (fireDurationYears !== undefined) {
    if (!Number.isInteger(fireDurationYears)) {
      return fail("预计 FIRE 时长需为整数（年）。");
    }
    if (fireDurationYears < 1) {
      return fail("预计 FIRE 时长至少为 1 年。");
    }
    if (retirementAge + fireDurationYears > 120) {
      return fail("退休年龄加 FIRE 时长不能超过 120 岁。");
    }
    return ok;
  }
  if (!Number.isInteger(endAge)) {
    return fail("规划终止年龄需为整数。");
  }
  if ((endAge as number) <= retirementAge) {
    return fail("规划终止年龄需大于退休年龄。");
  }
  if ((endAge as number) > 120) {
    return fail("规划终止年龄不能超过 120 岁。");
  }
  return ok;
}

export function validatePositiveMoneyFields(input: {
  totalAssetsMinor: number;
  annualSpendingMinor: number;
  annualSavingsMinor: number;
}): ValidationResult {
  if (input.totalAssetsMinor <= 0) {
    return fail("基准规模需大于 0。");
  }
  if (input.annualSpendingMinor <= 0) {
    return fail("当前年支出需大于 0。");
  }
  if (input.annualSavingsMinor < 0) {
    return fail("年储蓄不能为负数。");
  }
  return ok;
}
