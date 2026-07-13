"use client";

import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { HelpLabel } from "@/components/ui/HelpLabel";
import type { QuickFireInput } from "@/lib/api/quick-fire";

export function validateQuickFireInput(input: QuickFireInput): Record<string, string> {
  const errors: Record<string, string> = {};
  if (!Number.isInteger(input.current_age) || input.current_age < 18 || input.current_age > 120) errors.current_age = "年龄需在 18 到 120 岁之间。";
  if (!Number.isInteger(input.planned_fire_age) || input.planned_fire_age < input.current_age || input.planned_fire_age >= input.end_age) errors.planned_fire_age = "计划 FIRE 年龄需不早于当前年龄且早于目标年龄。";
  if (!Number.isInteger(input.end_age) || input.end_age <= input.planned_fire_age || input.end_age > 120) errors.end_age = "目标年龄需大于计划 FIRE 年龄且不超过 120 岁。";
  const checkAmount = (key: keyof QuickFireInput, min: number, max: number, label: string) => {
    const value = input[key] as number;
    if (!Number.isInteger(value) || value < min || value > max) {
      errors[String(key)] = `${label}需为 ${min === 0 ? "不小于 0" : "大于 0"} 且不超过 ${max} 的整数。`;
    }
  };
  checkAmount("current_assets_minor", 0, 999_999_999_999_00, "当前资产");
  checkAmount("annual_savings_minor", 0, 99_999_999_999_00, "年净储蓄");
  checkAmount("annual_spending_minor", 1, 99_999_999_999_00, "当前年支出");
  checkAmount("annual_retirement_income_minor", 0, 99_999_999_999_00, "稳定收入");
  checkAmount("terminal_wealth_floor_minor", 0, 999_999_999_999_00, "期末最低资产");
  const checkRate = (key: keyof QuickFireInput, min: number, max: number, label: string) => {
    const value = input[key] as number;
    if (!Number.isFinite(value) || value < min || value > max) errors[String(key)] = `${label}需在 ${min * 100}% 到 ${max * 100}% 之间。`;
  };
  checkRate("annual_return_rate", -0.99, 1, "年化收益率");
  checkRate("inflation_rate", -0.02, 0.2, "通胀率");
  checkRate("annual_savings_growth_rate", -0.5, 0.5, "储蓄增长率");
  checkRate("annual_retirement_income_growth_rate", -0.5, 0.5, "稳定收入增长率");
  return errors;
}

export function QuickFireForm({
  input,
  errors,
  onChange,
}: {
  input: QuickFireInput;
  errors: Record<string, string>;
  onChange: <K extends keyof QuickFireInput>(key: K, value: QuickFireInput[K]) => void;
}) {
  const numeric = <K extends "current_age" | "planned_fire_age" | "end_age">(key: K, label: string) => (
    <label className="block text-sm" htmlFor={`quick-fire-${key}`} key={key}>
      <span className="mb-1 block text-ink">{label}</span>
      <div className="flex items-center gap-2"><input id={`quick-fire-${key}`} className="input-base" type="number" value={input[key]} onChange={(e) => onChange(key, Number(e.target.value))} /><span className="text-ink-muted">岁</span></div>
      {errors[key] && <span className="mt-1 block text-xs text-danger">{errors[key]}</span>}
    </label>
  );
  return (
    <section aria-labelledby="quick-fire-input-title">
      <h1 id="quick-fire-input-title" className="text-xl font-semibold text-ink">FIRE 快算</h1>
      <div className="mt-4 grid gap-4 sm:grid-cols-2">
        {numeric("current_age", "当前年龄")}
        {numeric("planned_fire_age", "计划 FIRE 年龄")}
        {numeric("end_age", "目标年龄")}
        <MoneyField label="当前可投资资产" termKey="current_investable_assets" field="current_assets_minor" input={input} errors={errors} onChange={onChange} />
        <MoneyField label="FIRE 前年净储蓄" termKey="annual_savings_wizard" field="annual_savings_minor" input={input} errors={errors} onChange={onChange} />
        <MoneyField label="退休首年支出（当前购买力）" termKey="retirement_spending" field="annual_spending_minor" input={input} errors={errors} onChange={onChange} />
        <MoneyField label="退休后税后稳定年收入" termKey="stable_retirement_income" field="annual_retirement_income_minor" input={input} errors={errors} onChange={onChange} />
        <PercentField label="名义几何年化收益" termKey="geometric_annual_return" field="annual_return_rate" input={input} errors={errors} onChange={onChange} />
        <PercentField label="固定通胀率" termKey="fixed_inflation" field="inflation_rate" input={input} errors={errors} onChange={onChange} />
      </div>
      <details className="mt-5 border-t border-line pt-4">
        <summary className="cursor-pointer text-sm font-medium text-ink">更多假设</summary>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <PercentField label="储蓄增长率" termKey="savings_growth" field="annual_savings_growth_rate" input={input} errors={errors} onChange={onChange} />
          <PercentField label="稳定收入年增长率" termKey="retirement_income_growth" field="annual_retirement_income_growth_rate" input={input} errors={errors} onChange={onChange} />
          <MoneyField label="期末最低名义资产" termKey="terminal_wealth_floor" field="terminal_wealth_floor_minor" input={input} errors={errors} onChange={onChange} />
        </div>
      </details>
    </section>
  );
}

function MoneyField<K extends "current_assets_minor" | "annual_savings_minor" | "annual_spending_minor" | "annual_retirement_income_minor" | "terminal_wealth_floor_minor">({
  label, termKey, field, input, errors, onChange,
}: { label: string; termKey: string; field: K; input: QuickFireInput; errors: Record<string, string>; onChange: <T extends keyof QuickFireInput>(key: T, value: QuickFireInput[T]) => void }) {
  return <div><MoneyInput label={<HelpLabel label={label} termKey={termKey} />} ariaLabel={label} valueMinor={input[field]} onChange={(value) => onChange(field, value)} />{errors[field] && <span className="mt-1 block text-xs text-danger">{errors[field]}</span>}</div>;
}

function PercentField<K extends "annual_return_rate" | "inflation_rate" | "annual_savings_growth_rate" | "annual_retirement_income_growth_rate">({
  label, termKey, field, input, errors, onChange,
}: { label: string; termKey: string; field: K; input: QuickFireInput; errors: Record<string, string>; onChange: <T extends keyof QuickFireInput>(key: T, value: QuickFireInput[T]) => void }) {
  return <div><PercentInput label={<HelpLabel label={label} termKey={termKey} />} ariaLabel={label} value={input[field]} onChange={(value) => onChange(field, value)} />{errors[field] && <span className="mt-1 block text-xs text-danger">{errors[field]}</span>}</div>;
}
