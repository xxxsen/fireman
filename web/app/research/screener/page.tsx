"use client";

import Link from "next/link";
import { Suspense, useCallback, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter, useSearchParams } from "next/navigation";
import {
  addCollectionItem,
  createCollection,
  getCollection,
  listResearchAssets,
  type ResearchAssetView,
} from "@/lib/api/research";
import { syncMarketAssetHistory } from "@/lib/api/market-assets";
import {
  EMPTY_FILTERS,
  filtersToParams,
  activeFilterCount,
  type ScreenerFilters,
} from "@/lib/research/screener-filters";
import { queryErrorMessage } from "@/lib/query-error";
import { formatPercent, instrumentTypeLabel } from "@/lib/format";
import { downloadCsv } from "@/lib/csv";
import { PageHeader } from "@/components/ui/PageHeader";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Button } from "@/components/ui/Button";
import { ScreenerFilterPanel } from "@/components/research/ScreenerFilterPanel";
import { CandidatePoolPanel } from "@/components/research/CandidatePoolPanel";
import { equalWeights } from "@/components/research/WeightEditor";
import { CandidateCompareDialog } from "@/components/research/CandidateCompareDialog";
import { QualityBadges } from "@/components/research/qualityBadges";

const PAGE_SIZE = 50;

type MobileTab = "filters" | "results" | "candidates";

function metricCell(value: number | null | undefined, ratio = false): string {
  if (value == null || Number.isNaN(value)) return "—";
  return ratio ? value.toFixed(2) : formatPercent(value);
}

function signClass(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) return "text-ink-muted";
  return value >= 0 ? "text-positive" : "text-danger";
}

interface ColumnDef {
  key: string;
  label: string;
  /** Sort key sent to the API; undefined means the column is not sortable. */
  sortKey?: string;
  render: (a: ResearchAssetView) => React.ReactNode;
}

const ALL_COLUMNS: ColumnDef[] = [
  {
    key: "name",
    label: "资产",
    sortKey: "name",
    render: (a) => (
      <>
        <span className="block max-w-44 truncate font-medium text-ink">{a.name}</span>
        <span className="block text-xs text-ink-muted">{a.symbol}</span>
      </>
    ),
  },
  {
    key: "market",
    label: "市场/类型",
    sortKey: "market",
    render: (a) => (
      <span className="text-xs">
        <span className="block uppercase text-ink">{a.market}</span>
        <span className="block text-ink-muted">
          {a.instrument_type_label || instrumentTypeLabel(a.instrument_type)}
        </span>
      </span>
    ),
  },
  {
    key: "exchange",
    label: "交易所",
    render: (a) => <span className="text-xs">{a.exchange || "—"}</span>,
  },
  {
    key: "currency",
    label: "币种",
    sortKey: "currency",
    render: (a) => <span className="text-xs">{a.currency}</span>,
  },
  {
    key: "history_status",
    label: "数据状态",
    sortKey: "history_status",
    render: (a) => <QualityBadges badges={a.quality_badges} />,
  },
  {
    key: "data_as_of",
    label: "数据截至",
    sortKey: "data_as_of",
    render: (a) => <span className="font-mono-numeric text-xs">{a.data_as_of ?? "—"}</span>,
  },
  {
    key: "point_count",
    label: "点位数",
    sortKey: "point_count",
    render: (a) => <span className="font-mono-numeric text-xs">{a.point_count || "—"}</span>,
  },
  {
    key: "source",
    label: "来源",
    render: (a) => <span className="text-xs">{a.history_source || "—"}</span>,
  },
  {
    key: "return_1y",
    label: "近 1 年",
    sortKey: "return_1y",
    render: (a) => (
      <span className={`font-mono-numeric text-xs ${signClass(a.metrics?.return_1y)}`}>
        {metricCell(a.metrics?.return_1y)}
      </span>
    ),
  },
  {
    key: "return_3y",
    label: "近 3 年",
    sortKey: "return_3y",
    render: (a) => (
      <span className={`font-mono-numeric text-xs ${signClass(a.metrics?.return_3y)}`}>
        {metricCell(a.metrics?.return_3y)}
      </span>
    ),
  },
  {
    key: "return_5y",
    label: "近 5 年",
    sortKey: "return_5y",
    render: (a) => (
      <span className={`font-mono-numeric text-xs ${signClass(a.metrics?.return_5y)}`}>
        {metricCell(a.metrics?.return_5y)}
      </span>
    ),
  },
  {
    key: "cagr",
    label: "CAGR",
    sortKey: "cagr",
    render: (a) => (
      <span className={`font-mono-numeric text-xs ${signClass(a.metrics?.cagr)}`}>
        {metricCell(a.metrics?.cagr)}
      </span>
    ),
  },
  {
    key: "volatility",
    label: "波动率",
    sortKey: "volatility",
    render: (a) => (
      <span className="font-mono-numeric text-xs">
        {metricCell(a.metrics?.annual_volatility)}
      </span>
    ),
  },
  {
    key: "downside_vol",
    label: "下行波动",
    sortKey: "downside_vol",
    render: (a) => (
      <span className="font-mono-numeric text-xs">
        {metricCell(a.metrics?.downside_volatility)}
      </span>
    ),
  },
  {
    key: "max_drawdown",
    label: "最大回撤",
    sortKey: "max_drawdown",
    render: (a) => (
      <span className="font-mono-numeric text-xs text-danger">
        {metricCell(a.metrics?.max_drawdown)}
      </span>
    ),
  },
  {
    key: "sharpe",
    label: "Sharpe",
    sortKey: "sharpe",
    render: (a) => (
      <span className="font-mono-numeric text-xs">{metricCell(a.metrics?.sharpe, true)}</span>
    ),
  },
  {
    key: "calmar",
    label: "Calmar",
    sortKey: "calmar",
    render: (a) => (
      <span className="font-mono-numeric text-xs">{metricCell(a.metrics?.calmar, true)}</span>
    ),
  },
  {
    key: "return_drawdown",
    label: "收益回撤比",
    sortKey: "return_drawdown",
    render: (a) => (
      <span className="font-mono-numeric text-xs">
        {metricCell(a.metrics?.return_drawdown_ratio, true)}
      </span>
    ),
  },
];

/** Default visible columns per td/099 §4.3. */
const DEFAULT_COLUMN_KEYS = [
  "name",
  "market",
  "currency",
  "history_status",
  "data_as_of",
  "return_1y",
  "return_3y",
  "cagr",
  "volatility",
  "max_drawdown",
  "sharpe",
];

function ScreenerPageInner() {
  const queryClient = useQueryClient();
  const router = useRouter();
  const targetCollectionId = useSearchParams().get("collection") ?? "";
  const [filters, setFilters] = useState<ScreenerFilters>({
    ...EMPTY_FILTERS,
    instrumentTypes: [],
    currencies: [],
  });
  const [sortBy, setSortBy] = useState("");
  const [sortDesc, setSortDesc] = useState(true);
  const [page, setPage] = useState(0);
  const [candidates, setCandidates] = useState<ResearchAssetView[]>([]);
  const [compareOpen, setCompareOpen] = useState(false);
  const [avgCorrelation, setAvgCorrelation] = useState<number | null>(null);
  const [mobileTab, setMobileTab] = useState<MobileTab>("results");
  const [syncNotice, setSyncNotice] = useState<string | null>(null);
  const [visibleCols, setVisibleCols] = useState<string[]>(DEFAULT_COLUMN_KEYS);
  const [colConfigOpen, setColConfigOpen] = useState(false);

  const params = useMemo(() => {
    const p = filtersToParams(filters);
    if (sortBy) {
      p.sortBy = sortBy;
      p.sortDesc = sortDesc;
    }
    p.limit = PAGE_SIZE;
    p.offset = page * PAGE_SIZE;
    return p;
  }, [filters, sortBy, sortDesc, page]);

  const assetsQuery = useQuery({
    queryKey: ["research", "assets", params],
    queryFn: () => listResearchAssets(params),
    placeholderData: (prev) => prev,
  });

  const targetCollectionQuery = useQuery({
    queryKey: ["research", "collection", targetCollectionId],
    queryFn: () => getCollection(targetCollectionId),
    enabled: targetCollectionId !== "",
  });
  const targetCollection = targetCollectionQuery.data;
  const collectionAssetKeys = useMemo(
    () => new Set(targetCollection?.items.map((it) => it.asset_key) ?? []),
    [targetCollection],
  );

  const addToCollectionMutation = useMutation({
    mutationFn: (asset: ResearchAssetView) =>
      addCollectionItem(targetCollectionId, {
        asset_key: asset.asset_key,
        weight: 0,
        enabled: true,
        adjust_policy: asset.adjust_policy,
        point_type: asset.point_type,
      }),
    onSuccess: (detail, asset) => {
      queryClient.setQueryData(["research", "collection", targetCollectionId], detail);
      setSyncNotice(`已把「${asset.name}」加入集合「${detail.name}」（权重 0，请回集合页调整）。`);
    },
    onError: (err) => setSyncNotice(`加入集合失败：${queryErrorMessage(err)}`),
  });

  // Multi-select add from the compare dialog (td/099 §3.2): with a target
  // collection each asset is appended; otherwise a new equal-weight
  // collection is created from the selection.
  const addSelectedMutation = useMutation({
    mutationFn: async (assets: ResearchAssetView[]) => {
      if (targetCollectionId) {
        let added = 0;
        for (const asset of assets) {
          if (collectionAssetKeys.has(asset.asset_key)) continue;
          const detail = await addCollectionItem(targetCollectionId, {
            asset_key: asset.asset_key,
            weight: 0,
            enabled: true,
            adjust_policy: asset.adjust_policy,
            point_type: asset.point_type,
          });
          queryClient.setQueryData(["research", "collection", targetCollectionId], detail);
          added += 1;
        }
        return { mode: "append" as const, added };
      }
      const weights = equalWeights(assets.length);
      const detail = await createCollection({
        name: "候选组合",
        items: assets.map((asset, idx) => ({
          asset_key: asset.asset_key,
          weight: weights[idx]!,
          enabled: true,
          adjust_policy: asset.adjust_policy,
          point_type: asset.point_type,
        })),
      });
      return { mode: "create" as const, id: detail.id };
    },
    onSuccess: (result) => {
      setCompareOpen(false);
      if (result.mode === "create") {
        router.push(`/research/collections/${result.id}`);
        return;
      }
      setSyncNotice(`已把 ${result.added} 只资产加入集合（权重 0，请回集合页调整）。`);
    },
    onError: (err) => setSyncNotice(`加入集合失败：${queryErrorMessage(err)}`),
  });

  const refreshMutation = useMutation({
    mutationFn: (asset: ResearchAssetView) =>
      syncMarketAssetHistory({
        asset_key: asset.asset_key,
        adjust_policy: asset.adjust_policy,
        point_type: asset.point_type,
        mode: "default_refresh",
      }),
    onSuccess: (result, asset) => {
      setSyncNotice(
        result.existed
          ? `「${asset.name}」已有进行中的同步任务。`
          : `已为「${asset.name}」创建历史同步任务。`,
      );
      void queryClient.invalidateQueries({ queryKey: ["research", "assets"] });
    },
    onError: (err) => setSyncNotice(`同步失败：${queryErrorMessage(err)}`),
  });

  const handleFiltersChange = useCallback((next: ScreenerFilters) => {
    setFilters(next);
    setPage(0);
  }, []);

  function toggleSort(key: string) {
    if (sortBy === key) {
      setSortDesc(!sortDesc);
    } else {
      setSortBy(key);
      setSortDesc(true);
    }
    setPage(0);
  }

  function addCandidate(asset: ResearchAssetView) {
    setCandidates((prev) =>
      prev.some((c) => c.asset_key === asset.asset_key) ? prev : [...prev, asset],
    );
    setAvgCorrelation(null);
  }

  function removeCandidate(assetKey: string) {
    setCandidates((prev) => prev.filter((c) => c.asset_key !== assetKey));
    setAvgCorrelation(null);
  }

  function toggleColumn(key: string) {
    setVisibleCols((prev) =>
      prev.includes(key)
        ? prev.filter((k) => k !== key)
        : // Keep the registry order when re-enabling a column.
          ALL_COLUMNS.map((c) => c.key).filter((k) => prev.includes(k) || k === key),
    );
  }

  const assets = assetsQuery.data?.assets ?? [];
  const total = assetsQuery.data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const candidateKeys = useMemo(
    () => new Set(candidates.map((c) => c.asset_key)),
    [candidates],
  );
  const columns = ALL_COLUMNS.filter((c) => visibleCols.includes(c.key));

  function exportCSV() {
    const headers = [
      "资产",
      "代码",
      "市场",
      "类型",
      "币种",
      "数据状态",
      "数据截至",
      "点位数",
      "来源",
      "近1年",
      "近3年",
      "近5年",
      "CAGR",
      "波动率",
      "下行波动率",
      "最大回撤",
      "Sharpe",
      "Calmar",
      "收益回撤比",
    ];
    const rows = assets.map((a) => [
      a.name,
      a.symbol,
      a.market,
      a.instrument_type_label || instrumentTypeLabel(a.instrument_type),
      a.currency,
      a.quality_badges.join("/"),
      a.data_as_of ?? "",
      a.point_count,
      a.history_source ?? "",
      metricCell(a.metrics?.return_1y),
      metricCell(a.metrics?.return_3y),
      metricCell(a.metrics?.return_5y),
      metricCell(a.metrics?.cagr),
      metricCell(a.metrics?.annual_volatility),
      metricCell(a.metrics?.downside_volatility),
      metricCell(a.metrics?.max_drawdown),
      metricCell(a.metrics?.sharpe, true),
      metricCell(a.metrics?.calmar, true),
      metricCell(a.metrics?.return_drawdown_ratio, true),
    ]);
    downloadCsv("research-screener.csv", headers, rows);
  }

  const filterPanel = (
    <ScreenerFilterPanel filters={filters} onChange={handleFiltersChange} />
  );

  const candidatePanel = (
    <CandidatePoolPanel
      candidates={candidates}
      averageCorrelation={avgCorrelation}
      onRemove={removeCandidate}
      onClear={() => {
        setCandidates([]);
        setAvgCorrelation(null);
      }}
      onCompare={() => setCompareOpen(true)}
    />
  );

  const resultsPanel = (
    <div className="min-w-0 flex-1">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <input
          type="search"
          value={filters.q}
          onChange={(e) => handleFiltersChange({ ...filters, q: e.target.value })}
          placeholder="搜索代码 / 名称 / 交易所…"
          className="min-w-0 flex-1 rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none sm:max-w-xs"
          data-testid="screener-search"
        />
        <span className="text-xs text-ink-muted" data-testid="screener-total">
          共 {total} 项
        </span>
        <div className="relative">
          <Button
            variant="secondary"
            onClick={() => setColConfigOpen(!colConfigOpen)}
            data-testid="column-config-toggle"
          >
            列配置
          </Button>
          {colConfigOpen && (
            <div
              className="absolute right-0 z-20 mt-1 w-48 rounded-md border border-line bg-surface p-2 shadow-lg"
              data-testid="column-config-panel"
            >
              {ALL_COLUMNS.map((col) => (
                <label
                  key={col.key}
                  className="flex items-center gap-2 rounded px-1.5 py-1 text-sm text-ink hover:bg-surface-muted"
                >
                  <input
                    type="checkbox"
                    checked={visibleCols.includes(col.key)}
                    disabled={col.key === "name"}
                    onChange={() => toggleColumn(col.key)}
                    data-testid={`column-toggle-${col.key}`}
                  />
                  {col.label}
                </label>
              ))}
            </div>
          )}
        </div>
        <Button
          variant="secondary"
          onClick={exportCSV}
          disabled={assets.length === 0}
          data-testid="screener-export-csv"
        >
          导出 CSV
        </Button>
      </div>

      {syncNotice && (
        <p
          className="mb-2 rounded-md border border-info/25 bg-info/10 px-3 py-1.5 text-xs text-info"
          role="status"
        >
          {syncNotice}
        </p>
      )}

      {assetsQuery.isLoading ? (
        <LoadingState label="加载资产…" className="py-16" />
      ) : assetsQuery.isError ? (
        <ErrorState
          message="加载筛选结果失败。"
          onRetry={() => void assetsQuery.refetch()}
          technicalDetail={queryErrorMessage(assetsQuery.error)}
        />
      ) : assets.length === 0 ? (
        <p className="rounded-lg border border-dashed border-line px-4 py-10 text-center text-sm text-ink-muted">
          没有符合条件的资产，请调整筛选条件（当前 {activeFilterCount(filters)} 个条件）。
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-line bg-surface">
          <table className="w-full min-w-[960px] text-sm" data-testid="screener-table">
            <thead>
              <tr className="border-b border-line text-left text-xs text-ink-muted">
                {columns.map((col) => (
                  <th key={col.key} className="px-3 py-2 font-medium">
                    {col.sortKey ? (
                      <button
                        type="button"
                        onClick={() => toggleSort(col.sortKey!)}
                        className="inline-flex items-center gap-0.5 hover:text-ink"
                        data-testid={`sort-${col.sortKey}`}
                      >
                        {col.label}
                        {sortBy === col.sortKey && <span>{sortDesc ? "↓" : "↑"}</span>}
                      </button>
                    ) : (
                      col.label
                    )}
                  </th>
                ))}
                <th className="px-3 py-2 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {assets.map((a) => (
                <tr key={a.asset_key} className="border-b border-line/60 last:border-0 hover:bg-surface-muted/40">
                  {columns.map((col) => (
                    <td key={col.key} className="px-3 py-2">
                      {col.render(a)}
                    </td>
                  ))}
                  <td className="px-3 py-2">
                    <span className="flex items-center justify-end gap-1.5 text-xs">
                      <Link
                        href={`/assets/market/${encodeURIComponent(a.asset_key)}`}
                        className="text-brand underline-offset-2 hover:underline"
                      >
                        详情
                      </Link>
                      {targetCollectionId &&
                        (collectionAssetKeys.has(a.asset_key) ? (
                          <span className="text-ink-muted">已在集合</span>
                        ) : (
                          <button
                            type="button"
                            onClick={() => addToCollectionMutation.mutate(a)}
                            disabled={addToCollectionMutation.isPending}
                            className="text-brand underline-offset-2 hover:underline disabled:opacity-50"
                            data-testid={`add-to-collection-${a.asset_key}`}
                          >
                            加入集合
                          </button>
                        ))}
                      {candidateKeys.has(a.asset_key) ? (
                        <button
                          type="button"
                          onClick={() => removeCandidate(a.asset_key)}
                          className="text-ink-muted hover:text-danger"
                          data-testid={`remove-candidate-${a.asset_key}`}
                        >
                          移出候选
                        </button>
                      ) : (
                        <button
                          type="button"
                          onClick={() => addCandidate(a)}
                          className="text-brand underline-offset-2 hover:underline"
                          data-testid={`add-candidate-${a.asset_key}`}
                        >
                          加入候选
                        </button>
                      )}
                      {!a.is_cash && (
                        <button
                          type="button"
                          onClick={() => refreshMutation.mutate(a)}
                          disabled={refreshMutation.isPending}
                          className="text-ink-muted hover:text-ink disabled:opacity-50"
                          data-testid={`refresh-${a.asset_key}`}
                        >
                          刷新历史
                        </button>
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="mt-3 flex items-center justify-between text-sm">
          <Button
            variant="secondary"
            disabled={page === 0}
            onClick={() => setPage((p) => Math.max(0, p - 1))}
          >
            上一页
          </Button>
          <span className="text-xs text-ink-muted">
            第 {page + 1} / {totalPages} 页
          </span>
          <Button
            variant="secondary"
            disabled={page + 1 >= totalPages}
            onClick={() => setPage((p) => p + 1)}
          >
            下一页
          </Button>
        </div>
      )}
    </div>
  );

  return (
    <div className="content-enter">
      <PageHeader
        backHref="/research"
        backLabel="组合研究"
        title="资产筛选器"
        description="按市场、类型、历史数据质量与收益风险指标筛选候选资产，加入候选池后横向比较并创建研究集合。"
      />

      {targetCollectionId && (
        <p
          className="mb-4 rounded-md border border-brand/25 bg-brand/5 px-3 py-2 text-sm text-ink"
          data-testid="target-collection-banner"
        >
          正在为集合
          <Link
            href={`/research/collections/${targetCollectionId}`}
            className="mx-1 font-medium text-brand underline-offset-2 hover:underline"
          >
            {targetCollection?.name ?? targetCollectionId}
          </Link>
          添加资产，每行可直接「加入集合」。
        </p>
      )}

      <div className="mb-4 flex rounded-md border border-line text-sm lg:hidden" role="tablist">
        {(
          [
            ["filters", "筛选"],
            ["results", "结果"],
            ["candidates", `候选 (${candidates.length})`],
          ] as [MobileTab, string][]
        ).map(([tab, label], idx) => (
          <button
            key={tab}
            type="button"
            role="tab"
            aria-selected={mobileTab === tab}
            onClick={() => setMobileTab(tab)}
            className={
              (mobileTab === tab
                ? "bg-brand font-medium text-surface"
                : "text-ink-muted hover:bg-surface-muted") +
              " flex-1 px-3 py-2 " +
              (idx === 0 ? "rounded-l-md" : idx === 2 ? "rounded-r-md" : "")
            }
            data-testid={`mobile-tab-${tab}`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="flex gap-6">
        <div
          className={
            "w-full shrink-0 lg:block lg:w-64 " + (mobileTab === "filters" ? "" : "hidden")
          }
        >
          {filterPanel}
        </div>
        <div className={"min-w-0 flex-1 lg:block " + (mobileTab === "results" ? "" : "hidden")}>
          {resultsPanel}
        </div>
        <div
          className={
            "w-full shrink-0 lg:block lg:w-72 " + (mobileTab === "candidates" ? "" : "hidden")
          }
        >
          {candidatePanel}
        </div>
      </div>

      <CandidateCompareDialog
        open={compareOpen}
        onClose={() => setCompareOpen(false)}
        candidates={candidates}
        onRemove={removeCandidate}
        onAverageCorrelation={setAvgCorrelation}
        onAddSelected={(assets) => addSelectedMutation.mutate(assets)}
        addSelectedLabel={targetCollectionId ? "将选中加入集合" : "用选中创建集合"}
      />
    </div>
  );
}

export default function ResearchScreenerPage() {
  return (
    <Suspense fallback={<LoadingState label="加载筛选器…" />}>
      <ScreenerPageInner />
    </Suspense>
  );
}
