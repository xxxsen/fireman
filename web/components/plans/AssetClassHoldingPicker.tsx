"use client";

import { useMemo, useState } from "react";
import { WizardHoldingRow } from "@/components/plans/WizardHoldingRow";
import { assetClassLabel, regionLabel } from "@/lib/format";
import {
  addInstrumentToGroup,
  computeExpectedAmountMinor,
  removeInstrumentFromGroup,
  updateInstrumentWeightInGroup,
} from "@/lib/wizard-allocation";
import type { Instrument } from "@/types/api";
import type { WizardHoldingSelection } from "@/lib/wizard-allocation";

export interface AssetClassHoldingPickerProps {
  assetClass: string;
  classWeight: number;
  regionWeight: number;
  region?: "domestic" | "foreign";
  totalAssetsMinor: number;
  instruments: Instrument[];
  selected: WizardHoldingSelection[];
  onSelectedChange: (next: WizardHoldingSelection[]) => void;
  /** Sub-container title; omit for top-level single container. */
  subTitle?: string;
  /** When true, omit outer section border (nested under parent). */
  nested?: boolean;
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
  regionWeight,
  region,
  totalAssetsMinor,
  instruments,
  selected,
  onSelectedChange,
  subTitle,
  nested = false,
}: AssetClassHoldingPickerProps) {
  const [filter, setFilter] = useState("");

  const selectedIds = useMemo(() => new Set(selected.map((s) => s.inst.id)), [selected]);

  const effectiveRegion = region ?? "domestic";

  const searchResults = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return [];
    return instruments
      .filter((i) => isSelectableInstrument(i))
      .filter((i) => i.asset_class === assetClass)
      .filter((i) => i.region === effectiveRegion)
      .filter((i) => !selectedIds.has(i.id))
      .filter(
        (i) =>
          i.code.toLowerCase().includes(q) ||
          i.name.toLowerCase().includes(q) ||
          regionLabel(i.region).toLowerCase().includes(q),
      )
      .slice(0, 20);
  }, [filter, instruments, assetClass, effectiveRegion, selectedIds]);

  const addInstrument = (inst: Instrument) => {
    onSelectedChange(addInstrumentToGroup(selected, inst));
    setFilter("");
  };

  const updateSelection = (
    instrumentId: string,
    patch: Partial<Pick<WizardHoldingSelection, "weight" | "amount">>,
  ) => {
    if (patch.weight !== undefined) {
      onSelectedChange(updateInstrumentWeightInGroup(selected, instrumentId, patch.weight));
      return;
    }
    onSelectedChange(
      selected.map((s) => (s.inst.id === instrumentId ? { ...s, ...patch } : s)),
    );
  };

  const removeSelection = (instrumentId: string) => {
    onSelectedChange(removeInstrumentFromGroup(selected, instrumentId));
  };

  const searchAriaLabel = subTitle
    ? `${subTitle}搜索`
    : `${assetClassLabel(assetClass)}${region ? regionLabel(region) : ""}搜索`;

  const sectionClass = nested
    ? "mt-3 rounded-md border border-slate-100 bg-white p-3"
    : "rounded-lg border border-slate-200 p-4";

  const sectionAriaLabel = nested ? undefined : (subTitle ?? `${assetClassLabel(assetClass)}选标`);

  return (
    <section className={sectionClass} aria-label={sectionAriaLabel}>
      {subTitle && <h4 className="text-sm font-medium text-slate-800">{subTitle}</h4>}
      <input
        className={`${subTitle ? "mt-2" : "mt-3"} w-full rounded-md border px-3 py-2 text-sm`}
        placeholder={`搜索${assetClassLabel(assetClass)}标的（代码或名称）`}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        aria-label={searchAriaLabel}
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
        <ul className="mt-2 space-y-1">
          {selected.map((s) => {
            const expectedMinor = computeExpectedAmountMinor(
              totalAssetsMinor,
              classWeight,
              regionWeight,
              s.weight,
            );
            return (
              <WizardHoldingRow
                key={s.inst.id}
                selection={s}
                expectedMinor={expectedMinor}
                onWeightChange={(w) => updateSelection(s.inst.id, { weight: w })}
                onAmountChange={(a) => updateSelection(s.inst.id, { amount: a })}
                onRemove={() => removeSelection(s.inst.id)}
                ariaLabel={`${s.inst.name} ${s.inst.code}`}
              />
            );
          })}
        </ul>
      )}
    </section>
  );
}
