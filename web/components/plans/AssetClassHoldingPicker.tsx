"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MarketAssetSearchPicker } from "@/components/plans/MarketAssetPickerDialog";
import { WizardHoldingRow } from "@/components/plans/WizardHoldingRow";
import { assetClassLabel, regionLabel } from "@/lib/format";
import type { MarketAsset } from "@/lib/api/market-assets";
import {
  addInstrumentToGroup,
  computeExpectedAmountMinor,
  removeInstrumentFromGroup,
  updateInstrumentWeightInGroup,
} from "@/lib/wizard-allocation";
import type { WizardAsset, WizardHoldingSelection } from "@/lib/wizard-allocation";

export function marketAssetToWizardAsset(
  asset: MarketAsset,
  assetClass: string,
  region: string,
): WizardAsset {
  return {
    id: asset.asset_key,
    code: asset.symbol,
    name: asset.name,
    asset_class: assetClass,
    region,
    has_history: asset.has_history === true,
    history_data_as_of: asset.history_data_as_of,
    history_source_name: asset.history_source_name,
  };
}

export interface AssetClassHoldingPickerProps {
  assetClass: string;
  classWeight: number;
  regionWeight: number;
  region?: "domestic" | "foreign";
  totalAssetsMinor: number;
  selected: WizardHoldingSelection[];
  onSelectedChange: (next: WizardHoldingSelection[]) => void;
  /**
   * Every asset_key already selected anywhere in the plan (all asset
   * classes/regions). Candidates in this set are hidden so one market asset
   * can only be owned by a single class+region. Falls back to this
   * picker's own selection when omitted.
   */
  selectedAssetKeys?: Set<string>;
  /** Sub-container title; omit for top-level single container. */
  subTitle?: string;
  /** When true, omit outer section border (nested under parent). */
  nested?: boolean;
}

export function AssetClassHoldingPicker({
  assetClass,
  classWeight,
  regionWeight,
  region,
  totalAssetsMinor,
  selected,
  onSelectedChange,
  selectedAssetKeys,
  subTitle,
  nested = false,
}: AssetClassHoldingPickerProps) {
  const rootRef = useRef<HTMLElement | null>(null);
  const [open, setOpen] = useState(false);

  const localSelectedKeys = useMemo(() => new Set(selected.map((s) => s.inst.id)), [selected]);
  // Plan-wide blocked set: an asset picked in any class/region never shows up
  // as a candidate again anywhere.
  const blockedAssetKeys = selectedAssetKeys ?? localSelectedKeys;

  const effectiveRegion = region ?? "domestic";

  const closeDropdown = useCallback(() => {
    setOpen(false);
  }, []);

  // Close the candidate dropdown when clicking outside the whole picker. We use
  // a capture-phase pointerdown (not blur) so clicking a candidate inside the
  // picker still registers as a selection before any close logic runs.
  useEffect(() => {
    if (!open) return;
    const handlePointerDown = (event: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        closeDropdown();
      }
    };
    document.addEventListener("pointerdown", handlePointerDown, true);
    return () => document.removeEventListener("pointerdown", handlePointerDown, true);
  }, [open, closeDropdown]);

  const addAsset = (asset: MarketAsset) => {
    // Defence against stale candidate lists (race between pickers): never
    // add an asset the plan already owns, or group weights would be
    // redistributed for an item the parent merge immediately drops.
    if (!blockedAssetKeys.has(asset.asset_key)) {
      onSelectedChange(
        addInstrumentToGroup(
          selected,
          marketAssetToWizardAsset(asset, assetClass, effectiveRegion),
        ),
      );
    }
    setOpen(false);
  };

  const updateSelection = (
    assetKey: string,
    patch: Partial<Pick<WizardHoldingSelection, "weight" | "amount">>,
  ) => {
    if (patch.weight !== undefined) {
      onSelectedChange(updateInstrumentWeightInGroup(selected, assetKey, patch.weight));
      return;
    }
    onSelectedChange(
      selected.map((s) => (s.inst.id === assetKey ? { ...s, ...patch } : s)),
    );
  };

  const removeSelection = (assetKey: string) => {
    onSelectedChange(removeInstrumentFromGroup(selected, assetKey));
  };

  const searchAriaLabel = subTitle
    ? `${subTitle}搜索`
    : `${assetClassLabel(assetClass)}${region ? regionLabel(region) : ""}搜索`;

  // The top-level picker fills its parent (the wizard tab panel already draws
  // the border), so panel border and content width always match. Nested
  // domestic/foreign sub-containers keep their own inner border.
  const sectionClass = nested
    ? "mt-3 w-full rounded-md border border-line bg-surface p-3"
    : "w-full";

  const sectionAriaLabel = nested ? undefined : (subTitle ?? `${assetClassLabel(assetClass)}选标`);

  return (
    <section ref={rootRef} className={sectionClass} aria-label={sectionAriaLabel}>
      {subTitle && <h4 className="text-sm font-medium text-ink">{subTitle}</h4>}
      {selected.length > 0 && (
        <ul
          className={`${subTitle ? "mt-2" : nested ? "mt-3" : ""} space-y-2`}
          data-testid="wizard-selected-rows"
        >
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
      <div
        className={
          subTitle || selected.length > 0 ? "mt-2" : nested ? "mt-3" : ""
        }
      >
        <MarketAssetSearchPicker
          active={open}
          resultsVisible={open}
          excludeAssetKeys={blockedAssetKeys}
          onSelect={addAsset}
          onActivate={() => setOpen(true)}
          onEscape={closeDropdown}
          placeholder={`搜索${assetClassLabel(assetClass)}标的（代码或名称）`}
          searchAriaLabel={searchAriaLabel}
        />
      </div>
    </section>
  );
}
