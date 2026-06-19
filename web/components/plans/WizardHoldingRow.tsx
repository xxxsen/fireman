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
      className="grid grid-cols-[1fr_auto] items-center gap-x-2 gap-y-1 rounded border border-line bg-surface-muted px-2 py-1.5 text-sm"
      aria-label={ariaLabel}
    >
      <div className="min-w-0 truncate">
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
      <div className="col-span-2 grid grid-cols-3 gap-2">
        <div>
          <span className="sr-only">组内占比</span>
          <PercentInput value={selection.weight} onChange={onWeightChange} className="text-xs" />
        </div>
        <div>
          <span className="sr-only">已分配金额</span>
          <MoneyInput valueMinor={selection.amount} onChange={onAmountChange} />
        </div>
        <div className="flex flex-col justify-center text-xs text-ink-muted">
          <span className="sr-only">预期资金</span>
          <span className="font-medium text-ink">{formatMoney(expectedMinor)}</span>
        </div>
      </div>
    </li>
  );
}
