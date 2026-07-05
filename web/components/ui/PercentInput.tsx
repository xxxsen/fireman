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
      {label && <span className="mb-1 block text-sm text-ink">{label}</span>}
      <div className="flex items-center gap-1">
        <input
          type="text"
          inputMode="decimal"
          disabled={disabled}
          data-testid="percent-input"
          className="input-base min-w-0 font-mono-numeric"
          value={decimalToPercentString(value)}
          onChange={(e) => {
            const d = percentToDecimal(e.target.value);
            if (d !== null) onChange(d);
          }}
        />
        <span className="shrink-0 text-sm text-ink-muted">%</span>
      </div>
    </label>
  );
}
