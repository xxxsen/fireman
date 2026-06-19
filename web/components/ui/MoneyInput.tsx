"use client";

import { useState, type ReactNode } from "react";
import {
  formatMoneyInlineUnit,
  formatMoneyInput,
  formatMoneyPlain,
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

  const rawForUnit = editing ? draft : draftFromMinor(valueMinor, plain);
  const inlineUnit = plain ? formatMoneyInlineUnit(currency, rawForUnit) : currency;

  return (
    <label className="block">
      {label && <span className="mb-1 block text-sm text-ink-muted">{label}</span>}
      <div className="flex items-center gap-2">
        <span className="text-sm text-ink-muted whitespace-nowrap" data-testid="money-inline-unit">
          {inlineUnit}
        </span>
        <input
          type="text"
          inputMode="decimal"
          disabled={disabled}
          data-testid="money-input"
          className="input-base font-mono-numeric"
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
      </div>
    </label>
  );
}
