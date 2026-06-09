"use client";

import { formatMoneyInput, parseMoneyInput } from "@/lib/format";

interface MoneyInputProps {
  valueMinor: number;
  onChange: (minor: number) => void;
  currency?: string;
  label?: string;
  disabled?: boolean;
}

export function MoneyInput({
  valueMinor,
  onChange,
  currency = "CNY",
  label,
  disabled,
}: MoneyInputProps) {
  return (
    <label className="block">
      {label && <span className="mb-1 block text-sm text-slate-600">{label}</span>}
      <div className="flex items-center gap-2">
        <span className="text-sm text-slate-500">{currency}</span>
        <input
          type="text"
          inputMode="decimal"
          disabled={disabled}
          data-testid="money-input"
          className="w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
          value={formatMoneyInput(valueMinor)}
          onChange={(e) => {
            const minor = parseMoneyInput(e.target.value);
            if (minor !== null) onChange(minor);
          }}
        />
      </div>
    </label>
  );
}
