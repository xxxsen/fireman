"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createSavedFilter,
  deleteSavedFilter,
  listSavedFilters,
  updateSavedFilter,
  type ResearchSavedFilter,
} from "@/lib/api/research";
import {
  EMPTY_FILTERS,
  filtersFromJSON,
  filtersToJSON,
  type ScreenerFilters,
} from "@/lib/research/screener-filters";
import { queryErrorMessage } from "@/lib/query-error";
import { Button } from "@/components/ui/Button";

const MARKET_OPTIONS = [
  { value: "", label: "全部市场" },
  { value: "cn", label: "中国" },
  { value: "hk", label: "香港" },
  { value: "us", label: "美国" },
  { value: "system", label: "系统内置" },
];

const INSTRUMENT_TYPE_OPTIONS = [
  { value: "cn_exchange_stock", label: "A 股" },
  { value: "cn_exchange_fund", label: "场内 ETF/LOF" },
  { value: "cn_mutual_fund", label: "场外基金" },
  { value: "hk_stock", label: "港股" },
  { value: "hk_etf", label: "港股 ETF" },
  { value: "us_stock", label: "美股" },
  { value: "us_etf", label: "美股 ETF" },
  { value: "cash", label: "现金" },
];

const HISTORY_STATUS_OPTIONS = [
  { value: "", label: "全部" },
  { value: "synced", label: "已同步" },
  { value: "syncing", label: "同步中" },
  { value: "failed", label: "同步失败" },
  { value: "missing", label: "未同步" },
  { value: "stale", label: "数据过期" },
];

const CURRENCY_OPTIONS = ["CNY", "HKD", "USD"];

/** ISO date N days before today, for the data-as-of quick presets. */
function daysAgoISO(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() - days);
  return d.toISOString().slice(0, 10);
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return <span className="mb-1 block text-xs font-medium text-ink-muted">{children}</span>;
}

function NumberField({
  label,
  value,
  onChange,
  placeholder,
  suffix,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  suffix?: string;
}) {
  return (
    <label className="block">
      <FieldLabel>{label}</FieldLabel>
      <span className="flex items-center gap-1">
        <input
          type="number"
          inputMode="decimal"
          value={value}
          placeholder={placeholder}
          onChange={(e) => onChange(e.target.value)}
          className="w-full rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
        />
        {suffix && <span className="text-xs text-ink-muted">{suffix}</span>}
      </span>
    </label>
  );
}

export interface ScreenerFilterPanelProps {
  filters: ScreenerFilters;
  onChange: (filters: ScreenerFilters) => void;
}

export function ScreenerFilterPanel({ filters, onChange }: ScreenerFilterPanelProps) {
  const queryClient = useQueryClient();
  const [saveName, setSaveName] = useState("");
  const [savedError, setSavedError] = useState<string | null>(null);

  const savedQuery = useQuery({
    queryKey: ["research", "saved-filters"],
    queryFn: listSavedFilters,
  });

  const invalidateSaved = () =>
    queryClient.invalidateQueries({ queryKey: ["research", "saved-filters"] });

  const createMutation = useMutation({
    mutationFn: () => createSavedFilter({ name: saveName.trim(), filters: filtersToJSON(filters) }),
    onSuccess: () => {
      setSaveName("");
      setSavedError(null);
      void invalidateSaved();
    },
    onError: (err) => setSavedError(queryErrorMessage(err)),
  });

  const overwriteMutation = useMutation({
    mutationFn: (filter: ResearchSavedFilter) =>
      updateSavedFilter(filter.id, { filters: filtersToJSON(filters) }),
    onSuccess: () => {
      setSavedError(null);
      void invalidateSaved();
    },
    onError: (err) => setSavedError(queryErrorMessage(err)),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteSavedFilter(id),
    onSuccess: () => {
      setSavedError(null);
      void invalidateSaved();
    },
    onError: (err) => setSavedError(queryErrorMessage(err)),
  });

  function set<K extends keyof ScreenerFilters>(key: K, value: ScreenerFilters[K]) {
    onChange({ ...filters, [key]: value });
  }

  function toggleInList(key: "instrumentTypes" | "currencies", value: string) {
    const list = filters[key];
    set(
      key,
      list.includes(value) ? list.filter((v) => v !== value) : [...list, value],
    );
  }

  function applySaved(filter: ResearchSavedFilter) {
    try {
      const parsed: unknown = JSON.parse(filter.filters_json);
      onChange(filtersFromJSON(parsed));
      setSavedError(null);
    } catch {
      setSavedError("筛选条件格式无效，无法应用。");
    }
  }

  const savedFilters = savedQuery.data?.filters ?? [];

  return (
    <div className="space-y-5" data-testid="screener-filter-panel">
      <section>
        <h3 className="mb-2 text-sm font-semibold text-ink">已保存筛选</h3>
        {savedFilters.length === 0 ? (
          <p className="text-xs text-ink-muted">暂无保存的筛选条件。</p>
        ) : (
          <ul className="space-y-1">
            {savedFilters.map((f) => (
              <li key={f.id} className="flex items-center gap-1 text-sm">
                <button
                  type="button"
                  onClick={() => applySaved(f)}
                  className="flex-1 truncate rounded px-2 py-1 text-left text-brand hover:bg-surface-muted"
                  data-testid={`saved-filter-${f.id}`}
                >
                  {f.name}
                </button>
                <button
                  type="button"
                  onClick={() => overwriteMutation.mutate(f)}
                  className="rounded px-1.5 py-1 text-xs text-ink-muted hover:bg-surface-muted hover:text-ink"
                  title="用当前条件覆盖"
                >
                  覆盖
                </button>
                <button
                  type="button"
                  onClick={() => deleteMutation.mutate(f.id)}
                  className="rounded px-1.5 py-1 text-xs text-danger/70 hover:bg-danger/5 hover:text-danger"
                  title="删除"
                >
                  删除
                </button>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-2 flex gap-1">
          <input
            type="text"
            value={saveName}
            onChange={(e) => setSaveName(e.target.value)}
            placeholder="保存当前条件为…"
            className="min-w-0 flex-1 rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
            data-testid="save-filter-name"
          />
          <Button
            variant="secondary"
            disabled={!saveName.trim()}
            pending={createMutation.isPending}
            onClick={() => createMutation.mutate()}
            data-testid="save-filter-btn"
          >
            保存
          </Button>
        </div>
        {savedError && (
          <p className="mt-1 text-xs text-danger" role="alert">
            {savedError}
          </p>
        )}
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold text-ink">基础条件</h3>
        <label className="block">
          <FieldLabel>市场</FieldLabel>
          <select
            value={filters.market}
            onChange={(e) => set("market", e.target.value)}
            className="w-full rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
            data-testid="filter-market"
          >
            {MARKET_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>

        <div>
          <FieldLabel>类型</FieldLabel>
          <div className="flex flex-wrap gap-1.5">
            {INSTRUMENT_TYPE_OPTIONS.map((o) => (
              <button
                key={o.value}
                type="button"
                onClick={() => toggleInList("instrumentTypes", o.value)}
                className={
                  filters.instrumentTypes.includes(o.value)
                    ? "rounded-full border border-brand bg-brand/10 px-2.5 py-1 text-xs font-medium text-brand"
                    : "rounded-full border border-line bg-surface px-2.5 py-1 text-xs text-ink-muted hover:bg-surface-muted"
                }
                data-testid={`filter-type-${o.value}`}
              >
                {o.label}
              </button>
            ))}
          </div>
        </div>

        <div>
          <FieldLabel>币种</FieldLabel>
          <div className="flex flex-wrap gap-1.5">
            {CURRENCY_OPTIONS.map((cur) => (
              <button
                key={cur}
                type="button"
                onClick={() => toggleInList("currencies", cur)}
                className={
                  filters.currencies.includes(cur)
                    ? "rounded-full border border-brand bg-brand/10 px-2.5 py-1 text-xs font-medium text-brand"
                    : "rounded-full border border-line bg-surface px-2.5 py-1 text-xs text-ink-muted hover:bg-surface-muted"
                }
              >
                {cur}
              </button>
            ))}
          </div>
        </div>

        <label className="block">
          <FieldLabel>历史数据状态</FieldLabel>
          <select
            value={filters.historyStatus}
            onChange={(e) =>
              set("historyStatus", e.target.value as ScreenerFilters["historyStatus"])
            }
            className="w-full rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
            data-testid="filter-history-status"
          >
            {HISTORY_STATUS_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>

        <div>
          <FieldLabel>数据截至不早于</FieldLabel>
          <div className="mb-1.5 flex gap-1.5">
            {[7, 30, 90].map((days) => (
              <button
                key={days}
                type="button"
                onClick={() => set("dataAsOfMin", daysAgoISO(days))}
                className={
                  filters.dataAsOfMin === daysAgoISO(days)
                    ? "rounded-full border border-brand bg-brand/10 px-2.5 py-1 text-xs font-medium text-brand"
                    : "rounded-full border border-line bg-surface px-2.5 py-1 text-xs text-ink-muted hover:bg-surface-muted"
                }
                data-testid={`data-as-of-preset-${days}`}
              >
                近 {days} 天
              </button>
            ))}
          </div>
          <input
            type="date"
            value={filters.dataAsOfMin}
            onChange={(e) => set("dataAsOfMin", e.target.value)}
            className="w-full rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-ink focus:border-brand focus:outline-none"
            aria-label="数据截至不早于"
          />
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="flex items-center gap-2 text-sm text-ink">
            <input
              type="checkbox"
              checked={filters.includeInactive}
              onChange={(e) => set("includeInactive", e.target.checked)}
            />
            包含已退市 / inactive
          </label>
          <label className="flex items-center gap-2 text-sm text-ink">
            <input
              type="checkbox"
              checked={filters.backtestReady}
              onChange={(e) => set("backtestReady", e.target.checked)}
              data-testid="filter-backtest-ready"
            />
            仅可用于组合回测
          </label>
        </div>
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold text-ink">指标条件</h3>
        <NumberField
          label="最短历史（年）"
          value={filters.minHistoryYears}
          onChange={(v) => set("minHistoryYears", v)}
          placeholder="如 3"
        />
        <div className="grid grid-cols-2 gap-2">
          <NumberField label="CAGR ≥" value={filters.minCagr} onChange={(v) => set("minCagr", v)} suffix="%" />
          <NumberField
            label="近 1 年 ≥"
            value={filters.minReturn1y}
            onChange={(v) => set("minReturn1y", v)}
            suffix="%"
          />
          <NumberField
            label="近 3 年 ≥"
            value={filters.minReturn3y}
            onChange={(v) => set("minReturn3y", v)}
            suffix="%"
          />
          <NumberField
            label="近 5 年 ≥"
            value={filters.minReturn5y}
            onChange={(v) => set("minReturn5y", v)}
            suffix="%"
          />
          <NumberField
            label="波动率 ≤"
            value={filters.maxVolatility}
            onChange={(v) => set("maxVolatility", v)}
            suffix="%"
          />
          <NumberField
            label="最大回撤 ≥"
            value={filters.minMaxDrawdown}
            onChange={(v) => set("minMaxDrawdown", v)}
            placeholder="如 -30"
            suffix="%"
          />
          <NumberField
            label="下行波动率 ≤"
            value={filters.maxDownsideVolatility}
            onChange={(v) => set("maxDownsideVolatility", v)}
            suffix="%"
          />
          <NumberField
            label="收益回撤比 ≥"
            value={filters.minReturnDrawdown}
            onChange={(v) => set("minReturnDrawdown", v)}
            placeholder="如 2"
          />
          <NumberField label="Sharpe ≥" value={filters.minSharpe} onChange={(v) => set("minSharpe", v)} />
          <NumberField label="Calmar ≥" value={filters.minCalmar} onChange={(v) => set("minCalmar", v)} />
        </div>
      </section>

      <Button
        variant="secondary"
        className="w-full"
        onClick={() => onChange({ ...EMPTY_FILTERS, instrumentTypes: [], currencies: [] })}
        data-testid="filter-reset"
      >
        重置全部条件
      </Button>
    </div>
  );
}
