"use client";

import { useMemo, useState } from "react";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PercentInput } from "@/components/ui/PercentInput";
import { assetClassLabel, formatMoney, regionLabel } from "@/lib/format";
import { computeExpectedAmountMinor } from "@/lib/wizard-allocation";
import type { Instrument } from "@/types/api";
import type { WizardHoldingSelection } from "@/lib/wizard-allocation";

export interface AssetClassHoldingPickerProps {
  assetClass: string;
  classWeight: number;
  totalAssetsMinor: number;
  instruments: Instrument[];
  selected: WizardHoldingSelection[];
  onSelectedChange: (next: WizardHoldingSelection[]) => void;
}

function isSelectableInstrument(inst: Instrument): boolean {
  return (
    !inst.is_system &&
    inst.status === "active" &&
    (inst.quality_status ?? "available") === "available"
  );
}

export function AssetClassHoldingPicker({
  assetClass,
  classWeight,
  totalAssetsMinor,
  instruments,
  selected,
  onSelectedChange,
}: AssetClassHoldingPickerProps) {
  const [filter, setFilter] = useState("");

  const selectedIds = useMemo(() => new Set(selected.map((s) => s.inst.id)), [selected]);

  const searchResults = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return [];
    return instruments
      .filter((i) => isSelectableInstrument(i))
      .filter((i) => i.asset_class === assetClass)
      .filter((i) => !selectedIds.has(i.id))
      .filter(
        (i) =>
          i.code.toLowerCase().includes(q) ||
          i.name.toLowerCase().includes(q) ||
          regionLabel(i.region).toLowerCase().includes(q),
      )
      .slice(0, 20);
  }, [filter, instruments, assetClass, selectedIds]);

  const addInstrument = (inst: Instrument) => {
    onSelectedChange([...selected, { inst, weight: 0, amount: 0 }]);
    setFilter("");
  };

  const updateSelection = (instrumentId: string, patch: Partial<Pick<WizardHoldingSelection, "weight" | "amount">>) => {
    onSelectedChange(
      selected.map((s) => (s.inst.id === instrumentId ? { ...s, ...patch } : s)),
    );
  };

  const removeSelection = (instrumentId: string) => {
    onSelectedChange(selected.filter((s) => s.inst.id !== instrumentId));
  };

  return (
    <section
      className="rounded-lg border border-slate-200 p-4"
      aria-label={`${assetClassLabel(assetClass)}选标`}
    >
      <h3 className="font-medium">
        {assetClassLabel(assetClass)}（场景目标 {(classWeight * 100).toFixed(0)}%）
      </h3>
      <input
        className="mt-3 w-full rounded-md border px-3 py-2 text-sm"
        placeholder={`搜索${assetClassLabel(assetClass)}标的（代码或名称）`}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        aria-label={`${assetClassLabel(assetClass)}搜索`}
      />
      {searchResults.length > 0 && (
        <ul className="mt-2 max-h-40 overflow-y-auto rounded-md border divide-y text-sm">
          {searchResults.map((inst) => (
            <li key={inst.id}>
              <button
                type="button"
                className="w-full px-3 py-2 text-left hover:bg-slate-50"
                onClick={() => addInstrument(inst)}
              >
                <span className="font-medium">{inst.name}</span>
                <span className="ml-2 text-slate-500">{inst.code}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {filter.trim() && searchResults.length === 0 && (
        <p className="mt-2 text-sm text-slate-500">未找到匹配的{assetClassLabel(assetClass)}标的。</p>
      )}
      {selected.length > 0 && (
        <ul className="mt-4 space-y-3">
          {selected.map((s) => {
            const expectedMinor = computeExpectedAmountMinor(totalAssetsMinor, classWeight, s.weight);
            return (
              <li key={s.inst.id} className="rounded-md border p-3 text-sm">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="font-medium">{s.inst.name}</div>
                    <div className="text-xs text-slate-500">{s.inst.code}</div>
                  </div>
                  <button
                    type="button"
                    className="text-xs text-red-700 underline"
                    onClick={() => removeSelection(s.inst.id)}
                  >
                    移除
                  </button>
                </div>
                <div className="mt-3 flex flex-wrap items-end gap-3">
                  <label className="text-xs text-slate-600">
                    组内占比
                    <PercentInput
                      value={s.weight}
                      onChange={(w) => updateSelection(s.inst.id, { weight: w })}
                    />
                  </label>
                  <label className="text-xs text-slate-600">
                    已分配金额
                    <MoneyInput
                      valueMinor={s.amount}
                      onChange={(a) => updateSelection(s.inst.id, { amount: a })}
                    />
                  </label>
                  <div className="text-xs text-slate-600">
                    预期资金
                    <div className="mt-1 font-medium text-slate-900">{formatMoney(expectedMinor)}</div>
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
