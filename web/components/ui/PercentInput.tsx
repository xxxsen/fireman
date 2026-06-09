"use client";

import { decimalToPercentString, percentToDecimal } from "@/lib/percent";

interface PercentInputProps {
  value: number;
  onChange: (decimal: number) => void;
  label?: string;
  disabled?: boolean;
  className?: string;
}

export function PercentInput({
  value,
  onChange,
  label,
  disabled,
  className,
}: PercentInputProps) {
  return (
    <label className={`block ${className ?? ""}`}>
      {label && <span className="mb-1 block text-sm text-slate-600">{label}</span>}
      <div className="flex items-center gap-1">
        <input
          type="text"
          inputMode="decimal"
          disabled={disabled}
          data-testid="percent-input"
          className="w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
          value={decimalToPercentString(value)}
          onChange={(e) => {
            const d = percentToDecimal(e.target.value);
            if (d !== null) onChange(d);
          }}
        />
        <span className="text-sm text-slate-500">%</span>
      </div>
    </label>
  );
}
