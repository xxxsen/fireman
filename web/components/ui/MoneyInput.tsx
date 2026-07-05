"use client";

import { useState, type ReactNode } from "react";
import {
  formatMoneyInput,
  formatMoneyPlain,
  formatMoneyUnitHint,
  parseMoneyInput,
} from "@/lib/format";

interface MoneyInputProps {
  valueMinor: number;
  onChange: (minor: number) => void;
  currency?: string;
  label?: ReactNode;
  disabled?: boolean;
  /** Plain numeric display without thousand separators. */
  plain?: boolean;
}

function draftFromMinor(minor: number, plain: boolean): string {
  if (plain) return formatMoneyPlain(minor);
  if (minor === 0) return "";
  return String(minor / 100);
}

/** Unit suffix shown after the input; CNY renders as 元 for readability. */
function unitLabelFor(currency: string): string {
  return currency === "CNY" ? "元" : currency;
}

export function MoneyInput({
  valueMinor,
  onChange,
  currency = "CNY",
  label,
  disabled,
  plain = false,
}: MoneyInputProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");

  const displayValue = plain
    ? editing
      ? draft
      : formatMoneyPlain(valueMinor)
    : editing
      ? draft
      : formatMoneyInput(valueMinor);

  // Magnitude hint (e.g. 约 400.00 万) for plain inputs where no thousand
  // separators exist to convey scale. Derived from the committed minor value,
  // which stays in sync with the draft because onChange fires per keystroke.
  const unitHint = plain ? formatMoneyUnitHint(valueMinor / 100) : null;

  return (
    <label className="block">
      {label && <span className="mb-1 block text-sm text-ink">{label}</span>}
      <div className="flex items-center gap-2">
        <input
          type="text"
          inputMode="decimal"
          disabled={disabled}
          data-testid="money-input"
          className="input-base min-w-0 font-mono-numeric"
          value={displayValue}
          onFocus={() => {
            setEditing(true);
            setDraft(draftFromMinor(valueMinor, plain));
          }}
          onBlur={() => {
            setEditing(false);
            const trimmed = draft.trim();
            if (trimmed === "") {
              onChange(0);
              return;
            }
            const minor = parseMoneyInput(trimmed);
            if (minor !== null) onChange(minor);
          }}
          onChange={(e) => {
            const next = e.target.value;
            setDraft(next);
            const minor = parseMoneyInput(next);
            if (minor !== null) onChange(minor);
          }}
        />
        <span
          className="shrink-0 whitespace-nowrap text-sm text-ink-muted"
          data-testid="money-inline-unit"
        >
          {unitLabelFor(currency)}
        </span>
      </div>
      {unitHint && (
        <span className="mt-1 block text-xs text-ink-muted" data-testid="money-unit-hint">
          {unitHint}
        </span>
      )}
    </label>
  );
}
