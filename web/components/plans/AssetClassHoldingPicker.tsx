"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { WizardHoldingRow } from "@/components/plans/WizardHoldingRow";
import { assetClassLabel, dataSourceLabel, regionLabel } from "@/lib/format";
import { isTaskActive, listMarketAssets, type MarketAsset } from "@/lib/api/market-assets";
import {
  addInstrumentToGroup,
  computeExpectedAmountMinor,
  removeInstrumentFromGroup,
  updateInstrumentWeightInGroup,
} from "@/lib/wizard-allocation";
import type { WizardAsset, WizardHoldingSelection } from "@/lib/wizard-allocation";

const PAGE_SIZE = 10;

const PICKER_MARKET_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "全部市场" },
  { value: "CN", label: "CN" },
  { value: "HK", label: "HK" },
  { value: "US", label: "US" },
];

const PICKER_TYPE_FILTERS: { value: string; label: string }[] = [
  { value: "", label: "全部类型" },
  { value: "cn_exchange_stock", label: "A 股" },
  { value: "cn_exchange_fund", label: "场内 ETF / LOF" },
  { value: "cn_mutual_fund", label: "公募基金" },
  { value: "hk_stock", label: "港股" },
  { value: "hk_etf", label: "香港 ETF" },
  { value: "us_stock", label: "美国股票" },
  { value: "us_etf", label: "美国 ETF" },
];

/** A query that is only letters/digits/dots is treated as a symbol search. */
function looksLikeSymbolQuery(q: string): boolean {
  return /^[A-Za-z0-9.]+$/.test(q);
}

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
  subTitle,
  nested = false,
}: AssetClassHoldingPickerProps) {
  const rootRef = useRef<HTMLElement | null>(null);
  const listboxId = useId();
  const [filter, setFilter] = useState("");
  const [debouncedFilter, setDebouncedFilter] = useState("");
  const [market, setMarket] = useState("");
  const [instrumentType, setInstrumentType] = useState("");
  const [open, setOpen] = useState(false);

  const selectedKeys = useMemo(() => new Set(selected.map((s) => s.inst.id)), [selected]);

  const effectiveRegion = region ?? "domestic";

  // Debounce the typed query to avoid one request per keystroke.
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedFilter(filter.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [filter]);

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

  const symbolQ = looksLikeSymbolQuery(debouncedFilter) ? debouncedFilter : undefined;
  const nameQ = symbolQ ? undefined : debouncedFilter || undefined;

  const listQuery = useInfiniteQuery({
    queryKey: [
      "market-asset-picker",
      market,
      instrumentType,
      debouncedFilter,
    ],
    enabled: open,
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      listMarketAssets({
        market: market || undefined,
        instrumentTypes: instrumentType ? [instrumentType] : undefined,
        symbolQ,
        nameQ,
        limit: PAGE_SIZE,
        offset: pageParam as number,
      }),
    getNextPageParam: (lastPage, allPages) => {
      const loaded = allPages.reduce((sum, page) => sum + page.assets.length, 0);
      return loaded < lastPage.total && lastPage.assets.length > 0 ? loaded : undefined;
    },
  });

  const results = useMemo(
    () =>
      (listQuery.data?.pages ?? [])
        .flatMap((page) => page.assets)
        .filter((asset) => !selectedKeys.has(asset.asset_key)),
    [listQuery.data, selectedKeys],
  );

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
  }, [listQuery.hasNextPage, listQuery.isFetchingNextPage, listQuery, results.length]);

  const addAsset = (asset: MarketAsset) => {
    onSelectedChange(
      addInstrumentToGroup(
        selected,
        marketAssetToWizardAsset(asset, assetClass, effectiveRegion),
      ),
    );
    setFilter("");
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

  const showEmptyHint =
    open && !listQuery.isLoading && !listQuery.isFetching && results.length === 0;

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
        className={`flex flex-col gap-2 sm:flex-row sm:items-center ${
          subTitle || selected.length > 0 ? "mt-2" : nested ? "mt-3" : ""
        }`}
      >
        <input
          className="input-base min-w-0 flex-1"
          placeholder={`搜索${assetClassLabel(assetClass)}标的（代码或名称）`}
          value={filter}
          role="combobox"
          aria-expanded={open}
          aria-controls={listboxId}
          aria-autocomplete="list"
          onFocus={() => setOpen(true)}
          onChange={(e) => {
            setOpen(true);
            setFilter(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              closeDropdown();
            }
          }}
          aria-label={searchAriaLabel}
          data-testid="wizard-holding-search"
        />
        <select
          value={market}
          onChange={(e) => setMarket(e.target.value)}
          className="input-base w-auto shrink-0 text-xs"
          aria-label="按市场筛选候选"
          data-testid="wizard-picker-market-filter"
        >
          {PICKER_MARKET_FILTERS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
        <select
          value={instrumentType}
          onChange={(e) => setInstrumentType(e.target.value)}
          className="input-base w-auto shrink-0 text-xs"
          aria-label="按资产类型筛选候选"
          data-testid="wizard-picker-type-filter"
        >
          {PICKER_TYPE_FILTERS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>
      {open && (results.length > 0 || listQuery.isLoading) && (
        <div
          id={listboxId}
          role="listbox"
          // Fixed viewport of exactly 10 standard rows (10 × 3rem); content scrolls
          // inside so paging never resizes the dropdown or shifts surrounding UI.
          className="mt-2 h-[30rem] overflow-y-auto rounded-md border border-line"
          data-testid="wizard-library-results"
        >
          <ul className="divide-y divide-line text-sm">
            {results.map((asset) => (
              <li
                key={asset.asset_key}
                role="option"
                aria-selected={false}
                className="flex h-12 items-center gap-2 overflow-hidden whitespace-nowrap pr-3 hover:bg-surface-muted"
              >
                <button
                  type="button"
                  className="flex h-full min-w-0 flex-1 items-center gap-2 overflow-hidden px-3 text-left"
                  onClick={() => addAsset(asset)}
                >
                  <span className="truncate font-medium">{asset.name}</span>
                  <span className="shrink-0 text-ink-muted">{asset.symbol}</span>
                  <span className="shrink-0 text-xs text-ink-muted">{asset.market}</span>
                  {asset.has_history ? (
                    <span className="shrink-0 text-xs text-ink-muted">
                      数据截至 {asset.history_data_as_of || "—"} ·{" "}
                      {dataSourceLabel(asset.history_source_name)}
                    </span>
                  ) : asset.history_sync_status === "failed" ? (
                    <span className="shrink-0 text-xs text-danger">
                      历史同步失败
                      {asset.history_sync_error ? `：${asset.history_sync_error}` : ""}
                      ，可在详情页重新同步
                    </span>
                  ) : isTaskActive(asset.history_sync_status) ? (
                    <span className="shrink-0 text-xs text-ink-muted">
                      历史同步中…
                    </span>
                  ) : (
                    <span className="shrink-0 text-xs text-warning">
                      未同步历史，模拟前需要同步
                    </span>
                  )}
                </button>
                <Link
                  href={`/assets/market/${encodeURIComponent(asset.asset_key)}`}
                  target="_blank"
                  className="shrink-0 text-xs text-brand underline-offset-2 hover:underline"
                >
                  详情
                </Link>
              </li>
            ))}
          </ul>
          {(listQuery.isLoading || listQuery.isFetchingNextPage) && (
            <p className="px-3 py-2 text-xs text-ink-muted" role="status">
              加载中…
            </p>
          )}
          <div ref={sentinelRef} aria-hidden="true" />
        </div>
      )}
      {open && listQuery.isError && (
        <p className="mt-2 text-sm text-danger" role="alert">
          资产目录查询失败，请稍后重试。
        </p>
      )}
      {showEmptyHint && (
        <p className="mt-2 text-sm text-ink-muted">
          未在本地资产目录中找到匹配标的；若目录较旧，可先到资产页同步资产列表。
        </p>
      )}
    </section>
  );
}
