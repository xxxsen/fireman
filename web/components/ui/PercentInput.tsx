"use client";

import { useEffect, useRef, useState } from "react";
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
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const lastEmitted = useRef(value);

  useEffect(() => {
    lastEmitted.current = value;
  }, [value]);

  const emitIfChanged = (next: number | null) => {
    if (next === null || Object.is(next, value) || Object.is(next, lastEmitted.current)) return;
    lastEmitted.current = next;
    onChange(next);
  };

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
          value={editing ? draft : decimalToPercentString(value)}
          onFocus={() => {
            setEditing(true);
            setDraft(decimalToPercentString(value));
            lastEmitted.current = value;
          }}
          onBlur={() => {
            emitIfChanged(percentToDecimal(draft));
            setEditing(false);
          }}
          onChange={(e) => {
            const next = e.target.value;
            setDraft(next);
            emitIfChanged(percentToDecimal(next));
          }}
        />
        <span className="shrink-0 text-sm text-ink-muted">%</span>
      </div>
    </label>
  );
}
