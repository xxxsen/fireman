"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { WizardHoldingRow } from "@/components/plans/WizardHoldingRow";
import { ApiError } from "@/lib/api/client";
import { assetClassLabel, historyDepthLabel, regionLabel } from "@/lib/format";
import {
  flattenResolveCandidates,
  importResolvedCandidate,
  looksLikeFundCode,
  resolveCNInstrumentCode,
} from "@/lib/instrument-resolve-search";
import {
  addInstrumentToGroup,
  computeExpectedAmountMinor,
  removeInstrumentFromGroup,
  updateInstrumentWeightInGroup,
} from "@/lib/wizard-allocation";
import type { ResolveCandidate } from "@/lib/api/instruments";
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

function matchesLibraryQuery(inst: Instrument, query: string): boolean {
  const q = query.toLowerCase();
  return (
    inst.code.toLowerCase().includes(q) ||
    inst.name.toLowerCase().includes(q) ||
    regionLabel(inst.region).toLowerCase().includes(q)
  );
}

function isActiveLibraryInstrument(inst: Instrument): boolean {
  return !inst.is_system && inst.status === "active";
}

function canAddToPlan(inst: Instrument): boolean {
  return isActiveLibraryInstrument(inst) && inst.simulation_eligible === true;
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
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState("");
  const [resolveLoading, setResolveLoading] = useState(false);
  const [importLoading, setImportLoading] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [externalCandidates, setExternalCandidates] = useState<ResolveCandidate[]>([]);

  const selectedIds = useMemo(() => new Set(selected.map((s) => s.inst.id)), [selected]);
  const selectedCodes = useMemo(
    () => new Set(selected.map((s) => s.inst.code.toLowerCase())),
    [selected],
  );

  const effectiveRegion = region ?? "domestic";

  const libraryResults = useMemo(() => {
    const q = filter.trim();
    if (!q) return [];
    return instruments
      .filter((i) => isActiveLibraryInstrument(i))
      .filter((i) => i.asset_class === assetClass)
      .filter((i) => i.region === effectiveRegion)
      .filter((i) => !selectedIds.has(i.id))
      .filter((i) => matchesLibraryQuery(i, q))
      .slice(0, 20);
  }, [filter, instruments, assetClass, effectiveRegion, selectedIds]);

  useEffect(() => {
    const q = filter.trim();
    let cancelled = false;

    const hasExactLibraryHit = instruments.some(
      (inst) =>
        isActiveLibraryInstrument(inst) &&
        inst.asset_class === assetClass &&
        inst.region === effectiveRegion &&
        inst.code.toLowerCase() === q.toLowerCase(),
    );
    const shouldResolve = looksLikeFundCode(q) && !hasExactLibraryHit;

    // All state updates run inside the timer callback (asynchronously) so the
    // effect body never calls setState synchronously.
    const timer = window.setTimeout(
      () => {
        if (cancelled) return;
        if (!shouldResolve) {
          setExternalCandidates([]);
          setResolveError(null);
          setResolveLoading(false);
          return;
        }
        setResolveLoading(true);
        setResolveError(null);
        void (async () => {
          try {
            const result = await resolveCNInstrumentCode(q);
            if (cancelled) return;
            const candidates = flattenResolveCandidates(result).filter(
              (c) => !selectedCodes.has(c.code.toLowerCase()),
            );
            setExternalCandidates(candidates);
            if (candidates.length === 0) {
              setResolveError("未在 AKShare 找到可录入的标的");
            }
          } catch (error) {
            if (cancelled) return;
            setExternalCandidates([]);
            if (error instanceof ApiError && error.code === "market_provider_timeout") {
              setResolveError("数据源响应超时，请重试");
            } else if (error instanceof ApiError) {
              setResolveError(error.message);
            } else {
              setResolveError(error instanceof Error ? error.message : "查询失败");
            }
          } finally {
            if (!cancelled) {
              setResolveLoading(false);
            }
          }
        })();
      },
      shouldResolve ? 400 : 0,
    );

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [filter, instruments, assetClass, effectiveRegion, selectedCodes]);

  const addInstrument = (inst: Instrument) => {
    onSelectedChange(addInstrumentToGroup(selected, inst));
    setFilter("");
    setExternalCandidates([]);
    setResolveError(null);
  };

  const importAndAdd = async (candidate: ResolveCandidate) => {
    setImportLoading(true);
    setResolveError(null);
    try {
      const inst = await importResolvedCandidate(candidate, assetClass, effectiveRegion);
      await queryClient.invalidateQueries({ queryKey: ["instruments"] });
      addInstrument(inst);
    } catch (error) {
      if (error instanceof ApiError && error.code === "market_provider_timeout") {
        setResolveError("数据源响应超时，请重试");
      } else {
        setResolveError(error instanceof Error ? error.message : "录入失败");
      }
    } finally {
      setImportLoading(false);
    }
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
    ? "mt-3 rounded-md border border-line bg-surface p-3"
    : "rounded-lg border border-line p-4";

  const sectionAriaLabel = nested ? undefined : (subTitle ?? `${assetClassLabel(assetClass)}选标`);

  const showEmptyHint =
    filter.trim().length > 0 &&
    libraryResults.length === 0 &&
    externalCandidates.length === 0 &&
    !resolveLoading &&
    !importLoading;

  return (
    <section className={sectionClass} aria-label={sectionAriaLabel}>
      {subTitle && <h4 className="text-sm font-medium text-ink">{subTitle}</h4>}
      <input
        className={`${subTitle ? "mt-2" : "mt-3"} w-full rounded-md border px-3 py-2 text-sm`}
        placeholder={`搜索${assetClassLabel(assetClass)}标的（代码或名称）`}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        aria-label={searchAriaLabel}
        data-testid="wizard-holding-search"
      />
      {libraryResults.length > 0 && (
        <ul
          className="mt-2 max-h-40 overflow-y-auto rounded-md border divide-y text-sm"
          data-testid="wizard-library-results"
        >
          {libraryResults.map((inst) => {
            const addable = canAddToPlan(inst);
            return (
              <li key={inst.id}>
                <button
                  type="button"
                  className="w-full px-3 py-2 text-left hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60"
                  disabled={!addable || importLoading}
                  onClick={() => addInstrument(inst)}
                >
                  <span className="font-medium">{inst.name}</span>
                  <span className="ml-2 text-ink-muted">{inst.code}</span>
                  {inst.complete_year_count != null && (
                    <span className="ml-2 text-xs text-ink-muted">{inst.complete_year_count} 完整年</span>
                  )}
                  {inst.monthly_return_count != null && (
                    <span className="ml-2 text-xs text-ink-muted">{inst.monthly_return_count} 月</span>
                  )}
                  {inst.history_depth === "one_year" && (
                    <span className="ml-2 text-xs text-warning">{historyDepthLabel(inst.history_depth)}</span>
                  )}
                  {!addable && (
                    <span className="ml-2 text-xs text-ink-muted">历史不足，暂不可用于模拟</span>
                  )}
                </button>
              </li>
            );
          })}
        </ul>
      )}
      {(resolveLoading || importLoading) && (
        <p className="mt-2 text-sm text-ink-muted" role="status">
          {importLoading ? "正在录入并抓取历史数据…" : "正在查询 AKShare…"}
        </p>
      )}
      {externalCandidates.length > 0 && (
        <ul
          className="mt-2 max-h-40 overflow-y-auto rounded-md border border-dashed border-line divide-y text-sm"
          data-testid="wizard-external-results"
        >
          {externalCandidates.map((candidate) => (
            <li key={`${candidate.code}-${candidate.provider_symbol}`}>
              <button
                type="button"
                className="w-full px-3 py-2 text-left hover:bg-surface-muted disabled:opacity-50"
                disabled={importLoading}
                onClick={() => void importAndAdd(candidate)}
              >
                <span className="font-medium">{candidate.name}</span>
                <span className="ml-2 text-ink-muted">{candidate.code}</span>
                <span className="ml-2 text-xs text-ink-muted">资料库未收录 · 点击录入并添加</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {resolveError && (
        <p className="mt-2 text-sm text-danger" role="alert">
          {resolveError}
        </p>
      )}
      {showEmptyHint && !looksLikeFundCode(filter) && (
        <p className="mt-2 text-sm text-ink-muted">未找到匹配的{assetClassLabel(assetClass)}标的。</p>
      )}
      {showEmptyHint && looksLikeFundCode(filter) && !resolveError && (
        <p className="mt-2 text-sm text-ink-muted">
          资料库中暂无该代码；输入完整基金编号后会自动查询 AKShare。
        </p>
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
