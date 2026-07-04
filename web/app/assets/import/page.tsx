"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import {
  getMarketAssetDetail,
  importFromMarketAsset,
  listMarketAssets,
  type MarketAsset,
} from "@/lib/api/market-assets";
import { ApiError } from "@/lib/api/client";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { LoadingState } from "@/components/ui/LoadingState";
import { assetClassLabel, instrumentTypeLabel, regionLabel } from "@/lib/format";

const ASSET_CLASSES = [
  { value: "equity", label: "股票 / 权益" },
  { value: "bond", label: "债券" },
  { value: "cash", label: "现金 / 货币" },
] as const;

const REGIONS = [
  { value: "domestic", label: "国内" },
  { value: "foreign", label: "国外" },
] as const;

const MARKET_FILTERS = [
  { value: "", label: "全部市场" },
  { value: "CN", label: "中国市场" },
  { value: "HK", label: "香港市场" },
  { value: "US", label: "美国市场" },
];

function marketAssetDetailHref(assetKey: string): string {
  return `/assets/market/${encodeURIComponent(assetKey)}`;
}

function ImportAssetPageInner() {
  const router = useRouter();
  const params = useSearchParams();
  const presetKey = params.get("asset_key") ?? "";

  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [market, setMarket] = useState("");
  const [chosen, setChosen] = useState<MarketAsset | null>(null);
  const [presetDismissed, setPresetDismissed] = useState(false);
  const [assetClass, setAssetClass] = useState("");
  const [region, setRegion] = useState("");
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [historyEmptyKey, setHistoryEmptyKey] = useState<string | null>(null);
  const [existingInstrumentId, setExistingInstrumentId] = useState<string | null>(null);

  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchInput.trim()), 300);
    return () => clearTimeout(timer);
  }, [searchInput]);

  // Preselect the asset when arriving from the directory/detail pages.
  const presetQ = useQuery({
    queryKey: ["market-assets-import-preset", presetKey],
    queryFn: () => getMarketAssetDetail(presetKey),
    enabled: !!presetKey && !chosen && !presetDismissed,
  });

  // The confirm stage shows either an explicitly chosen asset or the preset
  // one; deriving (instead of adopting via effect) avoids cascading renders.
  const presetAsset = !presetDismissed && !chosen ? (presetQ.data?.asset ?? null) : null;
  const selected = chosen ?? presetAsset;

  const searchQ = useQuery({
    queryKey: ["market-assets-import-search", market, search],
    queryFn: () =>
      listMarketAssets({ market: market || undefined, q: search || undefined, limit: 20 }),
    enabled: !selected && (search.length > 0 || market.length > 0),
    placeholderData: keepPreviousData,
  });

  const beginConfirm = (asset: MarketAsset) => {
    setChosen(asset);
    setAssetClass("");
    setRegion("");
    setError(null);
    setHistoryEmptyKey(null);
    setExistingInstrumentId(null);
  };

  const handleImport = async () => {
    if (!selected) return;
    if (!assetClass) {
      setError("请选择资产类别");
      return;
    }
    if (!region) {
      setError("请选择投资地区");
      return;
    }
    setImporting(true);
    setError(null);
    setHistoryEmptyKey(null);
    setExistingInstrumentId(null);
    try {
      const inst = await importFromMarketAsset({
        asset_key: selected.asset_key,
        asset_class: assetClass,
        region,
      });
      router.push(`/assets/${inst.id}`);
    } catch (e) {
      if (e instanceof ApiError && e.code === "market_asset_history_empty") {
        setHistoryEmptyKey(selected.asset_key);
        setError("该资产还没有本地历史数据，请先在资产详情页同步历史数据后再录入。");
      } else if (e instanceof ApiError && e.code === "instrument_already_exists") {
        const instId = e.details?.instrument_id;
        setExistingInstrumentId(typeof instId === "string" ? instId : null);
        setError("该资产已录入资产库，无需重复录入。");
      } else if (e instanceof ApiError && e.code === "market_asset_not_found") {
        setError("本地目录中找不到该资产，请先同步资产列表。");
      } else {
        setError(e instanceof ApiError ? e.message : "录入失败");
      }
    } finally {
      setImporting(false);
    }
  };

  return (
    <div className="mx-auto max-w-2xl">
      <Link
        href="/assets"
        className="text-sm text-ink-muted underline-offset-2 hover:text-ink hover:underline"
      >
        ← 资产目录
      </Link>
      <h1 className="mt-4 text-2xl font-semibold text-ink">录入资产</h1>
      <p className="mt-1 text-sm text-ink-muted">
        从本地资产目录选择标的并录入到我的资产库；不会触发外部数据请求。
      </p>

      {!selected && (
        <div className="mt-6 space-y-4 rounded-lg border border-line bg-surface p-6">
          <h2 className="font-medium text-ink">1. 搜索本地资产目录</h2>
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              type="search"
              className="input-base flex-1"
              value={searchInput}
              onChange={(e) => setSearchInput(e.target.value)}
              placeholder="输入代码或名称，如 510300 或 沪深300"
              aria-label="搜索资产"
              data-testid="import-search-input"
            />
            <select
              className="input-base sm:max-w-[10rem]"
              value={market}
              onChange={(e) => setMarket(e.target.value)}
              aria-label="按市场筛选"
            >
              {MARKET_FILTERS.map((m) => (
                <option key={m.value} value={m.value}>
                  {m.label}
                </option>
              ))}
            </select>
          </div>

          {presetKey && presetQ.isLoading && <LoadingState label="载入预选资产…" />}
          {presetKey && presetQ.isError && (
            <Alert variant="warning">
              未能载入预选资产（可能目录尚未同步），请直接搜索本地目录。
            </Alert>
          )}
          {searchQ.isLoading && <LoadingState label="搜索本地目录…" />}
          {searchQ.isError && (
            <Alert variant="danger">搜索失败，请确认后端服务可用后重试。</Alert>
          )}

          {searchQ.data && !searchQ.data.assets.length && (
            <p className="text-sm text-ink-muted">
              未在本地资产目录中找到匹配资产。若目录较旧，可先在
              <Link href="/assets" className="mx-1 text-brand underline-offset-2 hover:underline">
                资产目录页
              </Link>
              同步资产列表后重试。
            </p>
          )}

          {searchQ.data && searchQ.data.assets.length > 0 && (
            <ul className="divide-y divide-line rounded-md border border-line" role="listbox">
              {searchQ.data.assets.map((asset) => (
                <li key={asset.asset_key}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-surface-muted"
                    data-testid={`import-candidate-${asset.asset_key}`}
                    onClick={() => beginConfirm(asset)}
                  >
                    <span className="font-mono-numeric font-medium text-ink">{asset.symbol}</span>
                    <span className="min-w-0 flex-1 truncate text-ink">{asset.name}</span>
                    <span className="shrink-0 text-xs text-ink-muted">
                      {asset.market} · {instrumentTypeLabel(asset.instrument_type)}
                      {asset.exchange ? ` · ${asset.exchange}` : ""}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {selected && (
        <div className="mt-6 space-y-4 rounded-lg border border-line bg-surface p-6">
          <h2 className="font-medium text-ink">2. 确认录入</h2>
          <dl className="grid gap-2 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-ink-muted">名称</dt>
              <dd className="text-ink">{selected.name}</dd>
            </div>
            <div>
              <dt className="text-ink-muted">代码</dt>
              <dd className="font-mono-numeric text-ink">{selected.symbol}</dd>
            </div>
            <div>
              <dt className="text-ink-muted">市场 / 类型</dt>
              <dd className="text-ink">
                {selected.market} / {instrumentTypeLabel(selected.instrument_type)}
              </dd>
            </div>
            <div>
              <dt className="text-ink-muted">交易所 / kind</dt>
              <dd className="text-ink">
                {selected.exchange || "—"} · {selected.instrument_kind || "—"}
              </dd>
            </div>
          </dl>
          <label className="block text-sm text-ink">
            资产类别
            <span className="ml-1 text-ink-muted">（用于组合配置，请手动确认）</span>
            <select
              className="input-base mt-1"
              value={assetClass}
              onChange={(e) => setAssetClass(e.target.value)}
              data-testid="asset-class-select"
            >
              <option value="">请选择…</option>
              {ASSET_CLASSES.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          {assetClass && (
            <p className="text-sm text-ink-muted">
              已选择：{assetClassLabel(assetClass)}
              {selected.instrument_type === "cn_mutual_fund" && assetClass === "bond" && (
                <span>（短债/纯债等场外公募基金通常归入债券）</span>
              )}
              {selected.instrument_type === "cn_mutual_fund" && assetClass === "equity" && (
                <span>（标普500、纳指等联接基金若投资海外市场，地区请选「国外」）</span>
              )}
            </p>
          )}
          <label className="block text-sm text-ink">
            投资地区
            <span className="ml-1 text-ink-muted">（用于国内/国外权重配置，请手动确认）</span>
            <select
              className="input-base mt-1"
              value={region}
              onChange={(e) => setRegion(e.target.value)}
              data-testid="region-select"
            >
              <option value="">请选择…</option>
              {REGIONS.map((item) => (
                <option key={item.value} value={item.value}>
                  {item.label}
                </option>
              ))}
            </select>
          </label>
          {region && <p className="text-sm text-ink-muted">已选择：{regionLabel(region)}</p>}
          <p className="text-sm text-ink-muted">
            录入基于本地已同步的历史数据完成，无需等待抓取；若该资产尚未同步历史数据，请先前往详情页刷新。
          </p>

          {error && (
            <Alert variant="danger">
              <p data-testid="import-error">{error}</p>
              {historyEmptyKey && (
                <div className="mt-2">
                  <Button
                    variant="secondary"
                    href={marketAssetDetailHref(historyEmptyKey)}
                    data-testid="go-sync-history"
                  >
                    前往详情页同步历史数据
                  </Button>
                </div>
              )}
              {existingInstrumentId && (
                <div className="mt-2">
                  <Button variant="secondary" href={`/assets/${existingInstrumentId}`}>
                    查看已录入的标的
                  </Button>
                </div>
              )}
            </Alert>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <Button
              data-testid="confirm-import"
              disabled={importing || !assetClass || !region}
              onClick={() => void handleImport()}
            >
              {importing ? "录入中…" : "确认录入"}
            </Button>
            <Button
              variant="ghost"
              className="px-2 py-1"
              onClick={() => {
                setChosen(null);
                setPresetDismissed(true);
                setError(null);
                setHistoryEmptyKey(null);
                setExistingInstrumentId(null);
              }}
            >
              重新选择
            </Button>
            <Button
              variant="ghost"
              className="px-2 py-1"
              href={marketAssetDetailHref(selected.asset_key)}
            >
              查看资产详情
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}

export default function ImportAssetPage() {
  return (
    <Suspense fallback={<LoadingState label="加载录入页…" />}>
      <ImportAssetPageInner />
    </Suspense>
  );
}
