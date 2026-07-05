"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import Link from "next/link";
import { Dialog } from "@/components/ui/Dialog";
import { dataSourceLabel, instrumentTypeLabel } from "@/lib/format";
import { isTaskActive, listMarketAssets, type MarketAsset } from "@/lib/api/market-assets";

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

/**
 * Puts exact-symbol hits before fuzzy matches and orders the exact-hit group
 * (same code, several identities) by the backend-provided instrument type
 * priority (mutual funds first). Fuzzy results keep the API order. Never
 * auto-selects.
 */
function orderCandidates(assets: MarketAsset[], query: string): MarketAsset[] {
  const q = query.trim().toUpperCase();
  if (!q) return assets;
  const exact: MarketAsset[] = [];
  const rest: MarketAsset[] = [];
  for (const asset of assets) {
    (asset.symbol.toUpperCase() === q ? exact : rest).push(asset);
  }
  exact.sort(
    (a, b) => (a.instrument_type_priority ?? 3) - (b.instrument_type_priority ?? 3),
  );
  return [...exact, ...rest];
}

/** Full identity line: market plus exchange/board when known, e.g. CN / SZ. */
function marketIdentityLabel(asset: MarketAsset): string {
  const region = (asset.region_code || "").toUpperCase();
  return region ? `${asset.market} / ${region}` : asset.market;
}

export interface MarketAssetSearchPickerProps {
  /** Whether directory queries may run (e.g. dropdown/dialog is open). */
  active: boolean;
  /** Whether the result listbox is rendered; defaults to true. */
  resultsVisible?: boolean;
  /** Asset keys that are already owned and must be hidden from candidates. */
  excludeAssetKeys: Set<string>;
  onSelect: (asset: MarketAsset) => void;
  /** Fired when the user focuses or types in the search input. */
  onActivate?: () => void;
  /** Fired when the user presses Escape inside the search input. */
  onEscape?: () => void;
  placeholder?: string;
  searchAriaLabel?: string;
  inputTestId?: string;
  resultsTestId?: string;
  /** Height utility for the result list viewport. */
  listHeightClass?: string;
  /** Extra filter controls rendered between the search row and the results. */
  children?: ReactNode;
}

/**
 * Shared market-asset search core: debounced query, market/type filters,
 * paginated results with identity-conflict hint. Composed by the wizard's
 * AssetClassHoldingPicker (inline dropdown) and MarketAssetPickerDialog
 * (asset refresh) so both entry points behave identically.
 */
export function MarketAssetSearchPicker({
  active,
  resultsVisible = true,
  excludeAssetKeys,
  onSelect,
  onActivate,
  onEscape,
  placeholder = "按代码或名称搜索市场资产目录",
  searchAriaLabel = "搜索市场资产",
  inputTestId = "wizard-holding-search",
  resultsTestId = "wizard-library-results",
  listHeightClass = "h-[30rem]",
  children,
}: MarketAssetSearchPickerProps) {
  const listboxId = useId();
  const [filter, setFilter] = useState("");
  const [debouncedFilter, setDebouncedFilter] = useState("");
  const [market, setMarket] = useState("");
  const [instrumentType, setInstrumentType] = useState("");

  // Debounce the typed query to avoid one request per keystroke.
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedFilter(filter.trim()), 250);
    return () => window.clearTimeout(timer);
  }, [filter]);

  const symbolQ = looksLikeSymbolQuery(debouncedFilter) ? debouncedFilter : undefined;
  const nameQ = symbolQ ? undefined : debouncedFilter || undefined;

  const listQuery = useInfiniteQuery({
    queryKey: ["market-asset-picker", market, instrumentType, debouncedFilter],
    enabled: active,
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
      orderCandidates(
        (listQuery.data?.pages ?? [])
          .flatMap((page) => page.assets)
          .filter((asset) => !excludeAssetKeys.has(asset.asset_key)),
        symbolQ ?? "",
      ),
    [listQuery.data, excludeAssetKeys, symbolQ],
  );

  // Same code under several instrument types: surface the ambiguity instead
  // of letting the user silently pick the wrong identity.
  const identityConflict = useMemo(() => {
    if (!symbolQ) return false;
    const q = symbolQ.toUpperCase();
    return results.filter((asset) => asset.symbol.toUpperCase() === q).length >= 2;
  }, [results, symbolQ]);

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

  const selectAsset = (asset: MarketAsset) => {
    setFilter("");
    onSelect(asset);
  };

  const showEmptyHint =
    resultsVisible && !listQuery.isLoading && !listQuery.isFetching && results.length === 0;

  return (
    <div className="w-full">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <input
          className="input-base min-w-0 flex-1"
          placeholder={placeholder}
          value={filter}
          role="combobox"
          aria-expanded={resultsVisible}
          aria-controls={listboxId}
          aria-autocomplete="list"
          onFocus={() => onActivate?.()}
          onChange={(e) => {
            onActivate?.();
            setFilter(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              onEscape?.();
            }
          }}
          aria-label={searchAriaLabel}
          data-testid={inputTestId}
        />
        {/* Width utilities on the selects themselves lose to the unlayered
            .input-base { width: 100% } rule (Tailwind utilities live in
            @layer utilities), which stretched each select to the full row
            width and squeezed the search input to nothing. The fixed widths
            therefore live on shrink-0 wrappers; the selects fill them. */}
        <div className="w-full shrink-0 sm:w-28">
          <select
            value={market}
            onChange={(e) => setMarket(e.target.value)}
            className="input-base text-xs"
            aria-label="按市场筛选候选"
            data-testid="wizard-picker-market-filter"
          >
            {PICKER_MARKET_FILTERS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>
        <div className="w-full shrink-0 sm:w-44">
          <select
            value={instrumentType}
            onChange={(e) => setInstrumentType(e.target.value)}
            className="input-base text-xs"
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
      </div>
      {children}
      {resultsVisible && (results.length > 0 || listQuery.isLoading) && (
        <div
          id={listboxId}
          role="listbox"
          // Fixed viewport of exactly 10 standard rows (10 × 3rem); content scrolls
          // inside so paging never resizes the dropdown or shifts surrounding UI.
          className={`mt-2 ${listHeightClass} overflow-y-auto rounded-md border border-line`}
          data-testid={resultsTestId}
        >
          {identityConflict && (
            <p
              className="border-b border-line bg-warning/10 px-3 py-2 text-xs text-warning"
              role="note"
              data-testid="picker-identity-conflict-hint"
            >
              该代码存在多个资产类型，请按实际持仓选择。货币基金/场外基金通常应选择「公募基金」。
            </p>
          )}
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
                  onClick={() => selectAsset(asset)}
                >
                  <span className="truncate font-medium">{asset.name}</span>
                  <span className="shrink-0 text-ink-muted">{asset.symbol}</span>
                  <span className="shrink-0 text-xs text-ink-muted">
                    {asset.instrument_type_label ||
                      instrumentTypeLabel(asset.instrument_type)}
                  </span>
                  <span className="shrink-0 text-xs text-ink-muted">
                    {marketIdentityLabel(asset)}
                  </span>
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
                    <span className="shrink-0 text-xs text-ink-muted">历史同步中…</span>
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
      {resultsVisible && listQuery.isError && (
        <p className="mt-2 text-sm text-danger" role="alert">
          资产目录查询失败，请稍后重试。
        </p>
      )}
      {showEmptyHint && (
        <p className="mt-2 text-sm text-ink-muted">
          未在本地资产目录中找到匹配标的；若目录较旧，可先到资产页同步资产列表。
        </p>
      )}
    </div>
  );
}

export interface MarketAssetPickerDialogProps {
  open: boolean;
  onClose: () => void;
  onSelect: (asset: MarketAsset) => void;
  excludeAssetKeys: Set<string>;
  title?: string;
  inputTestId?: string;
  resultsTestId?: string;
  /** Extra controls (e.g. class/region selects) between search and results. */
  children?: ReactNode;
}

/**
 * Modal market-asset picker sharing the wizard's search behavior (debounce,
 * market/type filters, pagination, identity-conflict hint).
 */
export function MarketAssetPickerDialog({
  open,
  onClose,
  onSelect,
  excludeAssetKeys,
  title = "选择标的",
  inputTestId = "wizard-holding-search",
  resultsTestId = "wizard-library-results",
  children,
}: MarketAssetPickerDialogProps) {
  return (
    <Dialog open={open} onClose={onClose} title={title} className="max-w-3xl">
      <MarketAssetSearchPicker
        active={open}
        excludeAssetKeys={excludeAssetKeys}
        onSelect={onSelect}
        inputTestId={inputTestId}
        resultsTestId={resultsTestId}
        listHeightClass="max-h-[24rem]"
      >
        {children}
      </MarketAssetSearchPicker>
    </Dialog>
  );
}
