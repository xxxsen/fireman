import { validateAges, validatePositiveMoneyFields } from "./plan-validation";

describe("validateAges", () => {
  it("accepts the boundary chain current == retirement < end == 120", () => {
    expect(
      validateAges({ currentAge: 35, retirementAge: 35, endAge: 120 }).ok,
    ).toBe(true);
    expect(
      validateAges({ currentAge: 35, retirementAge: 35, fireDurationYears: 85 }).ok,
    ).toBe(true);
  });

  it.each([
    [{ currentAge: 0, retirementAge: 35, endAge: 80 }, "当前年龄需为大于 0 的整数。"],
    [{ currentAge: -3, retirementAge: 35, endAge: 80 }, "当前年龄需为大于 0 的整数。"],
    [{ currentAge: 35.5, retirementAge: 40, endAge: 80 }, "当前年龄需为大于 0 的整数。"],
    [{ currentAge: 35, retirementAge: 40.2, endAge: 80 }, "退休年龄需为整数。"],
    [{ currentAge: 40, retirementAge: 35, endAge: 80 }, "退休年龄不能小于当前年龄。"],
    [{ currentAge: 35, retirementAge: 40, endAge: 40 }, "规划终止年龄需大于退休年龄。"],
    [{ currentAge: 35, retirementAge: 40, endAge: 39 }, "规划终止年龄需大于退休年龄。"],
    [{ currentAge: 35, retirementAge: 40, endAge: 121 }, "规划终止年龄不能超过 120 岁。"],
    [{ currentAge: 35, retirementAge: 40, endAge: 80.5 }, "规划终止年龄需为整数。"],
  ])("rejects end-age surface %j", (input, message) => {
    const res = validateAges(input);
    expect(res.ok).toBe(false);
    expect(res.message).toBe(message);
  });

  it.each([
    [
      { currentAge: 35, retirementAge: 40, fireDurationYears: 0 },
      "预计 FIRE 时长至少为 1 年。",
    ],
    [
      { currentAge: 35, retirementAge: 40, fireDurationYears: 30.5 },
      "预计 FIRE 时长需为整数（年）。",
    ],
    [
      { currentAge: 35, retirementAge: 40, fireDurationYears: 81 },
      "退休年龄加 FIRE 时长不能超过 120 岁。",
    ],
  ])("rejects duration surface %j", (input, message) => {
    const res = validateAges(input);
    expect(res.ok).toBe(false);
    expect(res.message).toBe(message);
  });
});

describe("validatePositiveMoneyFields", () => {
  it("accepts positive assets/spending and zero savings", () => {
    expect(
      validatePositiveMoneyFields({
        totalAssetsMinor: 1,
        annualSpendingMinor: 1,
        annualSavingsMinor: 0,
      }).ok,
    ).toBe(true);
  });

  it.each([
    [
      { totalAssetsMinor: 0, annualSpendingMinor: 100, annualSavingsMinor: 0 },
      "基准规模需大于 0。",
    ],
    [
      { totalAssetsMinor: -1, annualSpendingMinor: 100, annualSavingsMinor: 0 },
      "基准规模需大于 0。",
    ],
    [
      { totalAssetsMinor: 100, annualSpendingMinor: 0, annualSavingsMinor: 0 },
      "当前年支出需大于 0。",
    ],
    [
      { totalAssetsMinor: 100, annualSpendingMinor: -5, annualSavingsMinor: 0 },
      "当前年支出需大于 0。",
    ],
    [
      { totalAssetsMinor: 100, annualSpendingMinor: 100, annualSavingsMinor: -1 },
      "年储蓄不能为负数。",
    ],
  ])("rejects %j", (input, message) => {
    const res = validatePositiveMoneyFields(input);
    expect(res.ok).toBe(false);
    expect(res.message).toBe(message);
  });
});
