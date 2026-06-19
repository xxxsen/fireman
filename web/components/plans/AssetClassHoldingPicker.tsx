"use client";

import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
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
import { searchInstruments, type ResolveCandidate } from "@/lib/api/instruments";
import type { Instrument } from "@/types/api";
import type { WizardHoldingSelection } from "@/lib/wizard-allocation";

const PAGE_SIZE = 10;

export interface AssetClassHoldingPickerProps {
  assetClass: string;
  classWeight: number;
  regionWeight: number;
  region?: "domestic" | "foreign";
  totalAssetsMinor: number;
  selected: WizardHoldingSelection[];
  onSelectedChange: (next: WizardHoldingSelection[]) => void;
  /** Sub-container title; omit for top-level single container. */
  subTitle?: string;
  /** When true, omit outer section border (nested under parent). */
  nested?: boolean;
}

function canAddToPlan(inst: Instrument): boolean {
  return inst.simulation_eligible === true;
}

export function AssetClassHoldingPicker({
  assetClass,
  classWeight,
  regionWeight,
  region,
  totalAssetsMinor,
  selected,
  onSelectedChange,
  subTitle,
  nested = false,
}: AssetClassHoldingPickerProps) {
  const queryClient = useQueryClient();
  const [filter, setFilter] = useState("");
  const [debouncedFilter, setDebouncedFilter] = useState("");
  const [open, setOpen] = useState(false);
  const [resolveLoading, setResolveLoading] = useState(false);
  const [importLoading, setImportLoading] = useState(false);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [externalCandidates, setExternalCandidates] = useState<ResolveCandidate[]>([]);

  const selectedCodes = useMemo(
    () => new Set(selected.map((s) => s.inst.code.toLowerCase())),
    [selected],
  );
  const excludeIds = useMemo(() => selected.map((s) => s.inst.id).sort(), [selected]);

  const effectiveRegion = region ?? "domestic";

  // Debounce the typed query to avoid one request per keystroke.
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedFilter(filter.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [filter]);

  const listQuery = useInfiniteQuery({
    queryKey: [
      "instrument-picker",
      assetClass,
      effectiveRegion,
      debouncedFilter,
      excludeIds.join(","),
    ],
    enabled: open,
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      searchInstruments({
        q: debouncedFilter || undefined,
        assetClass,
        region: effectiveRegion,
        status: "active",
        excludeIds,
        limit: PAGE_SIZE,
        cursor: pageParam as number,
      }),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
  });

  const libraryResults = useMemo(
    () => (listQuery.data?.pages ?? []).flatMap((page) => page.instruments),
    [listQuery.data],
  );

  const hasExactLibraryHit = useMemo(
    () =>
      libraryResults.some((inst) => inst.code.toLowerCase() === debouncedFilter.toLowerCase()),
    [libraryResults, debouncedFilter],
  );

  // Resolve via AKShare only when the query looks like a fund code and the
  // backend library has no exact match for it.
  useEffect(() => {
    const q = debouncedFilter;
    let cancelled = false;
    const shouldResolve = open && looksLikeFundCode(q) && !hasExactLibraryHit;

    // All state updates run inside the timer callback (asynchronously) so the
    // effect body never calls setState synchronously.
    const timer = window.setTimeout(() => {
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
          if (!cancelled) setResolveLoading(false);
        }
      })();
    }, 0);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [debouncedFilter, hasExactLibraryHit, open, selectedCodes]);

  // Auto-load the next page when the sentinel scrolls into view.
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver((entries) => {
      if (
        entries[0]?.isIntersecting &&
        listQuery.hasNextPage &&
        !listQuery.isFetchingNextPage
      ) {
        void listQuery.fetchNextPage();
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [listQuery.hasNextPage, listQuery.isFetchingNextPage, listQuery, libraryResults.length]);

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
      await queryClient.invalidateQueries({ queryKey: ["instrument-picker"] });
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
    open &&
    !listQuery.isLoading &&
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
        onFocus={() => setOpen(true)}
        onChange={(e) => {
          setOpen(true);
          setFilter(e.target.value);
        }}
        aria-label={searchAriaLabel}
        data-testid="wizard-holding-search"
      />
      {open && (libraryResults.length > 0 || listQuery.isLoading) && (
        <div
          className="mt-2 max-h-80 overflow-y-auto rounded-md border"
          data-testid="wizard-library-results"
        >
          <ul className="divide-y text-sm">
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
          {(listQuery.isLoading || listQuery.isFetchingNextPage) && (
            <p className="px-3 py-2 text-xs text-ink-muted" role="status">
              加载中…
            </p>
          )}
          <div ref={sentinelRef} aria-hidden="true" />
        </div>
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
      {showEmptyHint && !looksLikeFundCode(debouncedFilter) && (
        <p className="mt-2 text-sm text-ink-muted">未找到匹配的{assetClassLabel(assetClass)}标的。</p>
      )}
      {showEmptyHint && looksLikeFundCode(debouncedFilter) && !resolveError && (
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
