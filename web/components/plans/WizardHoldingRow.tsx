"use client";

import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { formatMoney } from "@/lib/format";
import type { WizardHoldingSelection } from "@/lib/wizard-allocation";

export interface WizardHoldingRowProps {
  selection: WizardHoldingSelection;
  expectedMinor: number;
  onWeightChange: (weight: number) => void;
  onAmountChange: (amount: number) => void;
  onRemove: () => void;
  ariaLabel: string;
}

export function WizardHoldingRow({
  selection,
  expectedMinor,
  onWeightChange,
  onAmountChange,
  onRemove,
  ariaLabel,
}: WizardHoldingRowProps) {
  const { inst } = selection;
  return (
    <li
      className="rounded border border-line bg-surface-muted px-3 py-2 text-sm"
      aria-label={ariaLabel}
    >
      <div className="flex items-center gap-2">
        <div className="min-w-0 flex-1 truncate">
          <span className="font-medium">{inst.name}</span>
          <span className="ml-2 text-xs text-ink-muted">{inst.code}</span>
        </div>
        <button
          type="button"
          className="shrink-0 px-1 text-lg leading-none text-danger"
          aria-label={`移除 ${inst.name}`}
          onClick={onRemove}
        >
          ×
        </button>
      </div>
      <div className="mt-2 grid grid-cols-1 gap-x-3 gap-y-2 sm:grid-cols-3">
        <div className="min-w-0">
          <PercentInput label="组内占比" value={selection.weight} onChange={onWeightChange} />
        </div>
        <div className="min-w-0">
          <MoneyInput label="已分配金额" valueMinor={selection.amount} onChange={onAmountChange} />
        </div>
        <div className="min-w-0">
          <span className="mb-1 block text-sm text-ink">预期资金</span>
          <p className="py-2 font-mono-numeric font-medium text-ink">
            {formatMoney(expectedMinor)}
          </p>
        </div>
      </div>
    </li>
  );
}
