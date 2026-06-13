"use client";

import { useState } from "react";
import { formatMoneyInput, parseMoneyInput } from "@/lib/format";

interface MoneyInputProps {
  valueMinor: number;
  onChange: (minor: number) => void;
  currency?: string;
  label?: string;
  disabled?: boolean;
}

function draftFromMinor(minor: number): string {
  if (minor === 0) return "";
  return String(minor / 100);
}

export function MoneyInput({
  valueMinor,
  onChange,
  currency = "CNY",
  label,
  disabled,
}: MoneyInputProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");

  return (
    <label className="block">
      {label && <span className="mb-1 block text-sm text-ink-muted">{label}</span>}
      <div className="flex items-center gap-2">
        <span className="text-sm text-ink-muted">{currency}</span>
        <input
          type="text"
          inputMode="decimal"
          disabled={disabled}
          data-testid="money-input"
          className="input-base font-mono-numeric"
          value={editing ? draft : formatMoneyInput(valueMinor)}
          onFocus={() => {
            setEditing(true);
            setDraft(draftFromMinor(valueMinor));
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
